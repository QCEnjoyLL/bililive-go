package hlsmouflon

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/pkg/browser"
	"github.com/bililive-go/bililive-go/src/pkg/livelogger"
)

// keystream 引导的防抖：引导要启无头浏览器（十几秒、吃资源），而 recorder 每 ~5s
// 就会重建 parser 实例重试，故用包级时间戳限流——bootstrapCooldown 内最多引导一次，
// 成败都更新时间戳，避免站点真改算法时反复狂启浏览器。
var (
	bootstrapMu   sync.Mutex
	lastBootstrap time.Time
)

const bootstrapCooldown = 5 * time.Minute

// segMsnRe 从分段文件名 {id}_{msn}_{hash}_{ts}(_partN).mp4 提取 (msn, hash)。
var segMsnRe = regexp.MustCompile(`_(\d+)_([A-Za-z0-9]+)_\d+(?:_part\d+)?\.mp4`)

// ageDismissJS：过年龄门（置常见 localStorage 标记 + 只点对话框内确认按钮，不碰页脚链接）。
const ageDismissJS = `(() => {
  try {
    for (const k of ['ageVerified','age_verified','isAdult','ageConfirmed','agree','ageGatePassed']) {
      localStorage.setItem(k, 'true');
    }
  } catch (e) {}
  const scope = document.querySelector('[role=dialog], .modal, [class*=age], [class*=Age], [class*=consent], [class*=Consent]') || document;
  for (const b of scope.querySelectorAll('button')) {
    const t = (b.textContent || '').trim();
    if (/^(enter|i am over|i'm over|yes,? i am|agree|continue|我已满|我已年满|同意|确认进入|进入)/i.test(t)) b.click();
  }
  return 0;
})()`

// bootstrapKeystream 在内置/缓存 keystream 失效时，用一次无头浏览器播目标房间，
// 截获浏览器实际请求的真实分段 URL（明文 hash），与同 msn 的播放列表加密 hash 配对，
// 逐对反推 keystream 并取众数交叉验证，成功返回该 keystream。
//
// 失败原因可能是：未开播、无可用浏览器、年龄门拦截、或站点已改变 MOUFLON 算法
// （非仅轮换密钥）——后者需退回 browser 引擎并跟进上游实现。
func bootstrapKeystream(ctx context.Context, liveObj live.Live, modelID, pkey string, logger *livelogger.LiveLogger) ([]byte, error) {
	bootstrapMu.Lock()
	if !lastBootstrap.IsZero() && time.Since(lastBootstrap) < bootstrapCooldown {
		left := bootstrapCooldown - time.Since(lastBootstrap)
		bootstrapMu.Unlock()
		return nil, fmt.Errorf("引导冷却中（还需 %s）", left.Round(time.Second))
	}
	lastBootstrap = time.Now()
	bootstrapMu.Unlock()

	pageURL := liveObj.GetRawUrl()
	logger.Infof("hlsmouflon: keystream 疑似失效，启动无头浏览器引导新密钥（model=%s）", modelID)

	// 1) 启浏览器（headless），复用共享定位逻辑
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		chromedp.WindowSize(1280, 720),
	)
	if execPath := browser.ResolveExecPath("", logger); execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}
	allocCtx, cancelA := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelA()
	bctx, cancelB := chromedp.NewContext(allocCtx)
	defer cancelB()

	// 2) 监听网络：收集浏览器实际请求的真实分段 URL（media-hls 域、.mp4，含明文 hash）
	var netMu sync.Mutex
	realSegs := []string{}
	chromedp.ListenTarget(bctx, func(ev interface{}) {
		if rr, ok := ev.(*network.EventResponseReceived); ok {
			lu := strings.ToLower(rr.Response.URL)
			if strings.Contains(lu, "media-hls") && strings.Contains(lu, ".mp4") {
				netMu.Lock()
				realSegs = append(realSegs, rr.Response.URL)
				netMu.Unlock()
			}
		}
	})

	// 3) cookie 注入 + 导航（重试，站点广告多偶发超时）
	if err := chromedp.Run(bctx,
		network.Enable(),
		runtime.Enable(),
		chromedp.ActionFunc(func(c context.Context) error { return injectCookies(c, liveObj, pageURL) }),
	); err != nil {
		return nil, fmt.Errorf("引导初始化浏览器失败: %w", err)
	}
	navOK := false
	for i := 0; i < 3; i++ {
		if e := chromedp.Run(bctx, chromedp.Navigate(pageURL)); e == nil {
			navOK = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !navOK {
		return nil, fmt.Errorf("引导导航 %s 失败", pageURL)
	}

	// 4) 过年龄门 + 等浏览器请求到 ≥2 个真实段（够配对+交叉验证），最多 ~30s
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		_ = chromedp.Run(bctx, chromedp.Evaluate(ageDismissJS, nil))
		netMu.Lock()
		n := len(realSegs)
		netMu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	netMu.Lock()
	segs := append([]string(nil), realSegs...)
	netMu.Unlock()
	if len(segs) == 0 {
		return nil, fmt.Errorf("引导未截获到真实分段（未开播 / 无可用浏览器 / 年龄门拦截）")
	}

	// 5) 用 pkey 拉播放列表，建立 msn → 加密 hash 映射
	hc := &http.Client{Timeout: 15 * time.Second}
	encByMsn, err := fetchEncHashes(hc, modelID, pkey)
	if err != nil {
		return nil, fmt.Errorf("引导拉播放列表失败: %w", err)
	}
	if len(encByMsn) == 0 {
		return nil, fmt.Errorf("引导：播放列表无 MOUFLON 加密段（站点结构可能已变）")
	}

	// 6) 按 msn 配对真实段(明文 hash) 与加密段(密文 hash)，逐对反推 keystream，取众数
	realByMsn := map[string]string{}
	for _, u := range segs {
		if m := segMsnRe.FindStringSubmatch(u); m != nil {
			realByMsn[m[1]] = m[2]
		}
	}
	votes := map[string]int{}
	sample := map[string][]byte{}
	for msn, real := range realByMsn {
		enc, ok := encByMsn[msn]
		if !ok {
			continue
		}
		ks, ok := deriveKeystream(enc, real)
		if !ok {
			continue
		}
		h := hex.EncodeToString(ks)
		votes[h]++
		sample[h] = ks
	}
	bestH, best := "", 0
	for h, c := range votes {
		if c > best {
			bestH, best = h, c
		}
	}
	if best == 0 {
		return nil, fmt.Errorf("引导无法配对真实段与加密段（截获 %d 段 / 列表 %d 段）", len(realByMsn), len(encByMsn))
	}
	if best < 2 {
		logger.Warnf("hlsmouflon: 引导仅 1 段验证（建议 ≥2 段一致），仍尝试使用")
	}
	logger.Infof("hlsmouflon: 引导成功，新 keystream=%s（%d 段交叉验证一致）", bestH, best)
	return sample[bestH], nil
}

// injectCookies 把用户 Cookie 注入浏览器（与站点同域），解除未登录限制。
func injectCookies(ctx context.Context, liveObj live.Live, pageURL string) error {
	opts := liveObj.GetOptions()
	if opts == nil || opts.Cookies == nil {
		return nil
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	for _, c := range opts.Cookies.Cookies(u) {
		if e := network.SetCookie(c.Name, c.Value).WithDomain(u.Hostname()).WithPath("/").Do(ctx); e != nil {
			return e
		}
	}
	return nil
}

// deriveKeystream 从一对 (加密 hash enc, 明文 hash real) 反推 keystream：
// 因 real = XOR(base64decode(reverse(enc)), ks)，故 ks = base64decode(reverse(enc)) XOR real（逐字节）。
func deriveKeystream(enc, real string) ([]byte, bool) {
	rev := reverseStr(enc)
	if pad := (4 - len(rev)%4) % 4; pad > 0 {
		rev += strings.Repeat("=", pad)
	}
	data, err := base64.StdEncoding.DecodeString(rev)
	if err != nil {
		if data, err = base64.URLEncoding.DecodeString(rev); err != nil {
			return nil, false
		}
	}
	if len(data) == 0 || len(data) != len(real) {
		return nil, false
	}
	ks := make([]byte, len(data))
	for i := range data {
		ks[i] = data[i] ^ real[i]
	}
	return ks, true
}

// fetchEncHashes 拉变体播放列表，解析所有 #EXT-X-MOUFLON:URI 段，返回 msn → 加密 hash。
func fetchEncHashes(hc *http.Client, modelID, pkey string) (map[string]string, error) {
	master := fmt.Sprintf("https://%s/hls/%s/master/%s.m3u8", masterHost, modelID, modelID)
	mbody, err := fetch(hc, master)
	if err != nil {
		return nil, err
	}
	variant := firstHTTPSLine(mbody)
	if variant == "" {
		return nil, fmt.Errorf("master 无变体地址")
	}
	body, err := fetch(hc, fmt.Sprintf("%s?psch=v2&pkey=%s", variant, pkey))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, l := range strings.Split(string(body), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "#EXT-X-MOUFLON:URI:") {
			continue
		}
		if m := segMsnRe.FindStringSubmatch(strings.TrimPrefix(l, "#EXT-X-MOUFLON:URI:")); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out, nil
}
