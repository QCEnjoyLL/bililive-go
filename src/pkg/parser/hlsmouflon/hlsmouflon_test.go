package hlsmouflon

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDeriveKeystreamRoundTrip 验证自愈的核心：deriveKeystream 能从一对
// (加密 hash, 明文 hash) 正确反推 keystream，且反推出的 keystream 经 decode 能还原明文
// （derive 与 decode 互逆）。这条逻辑错了，keystream 自愈会引导出错误密钥、录到乱码。
func TestDeriveKeystreamRoundTrip(t *testing.T) {
	ks := []byte("0123456789abcdef") // 16 字节 keystream
	real := "Zm9vYmFyMTIzNDU2"       // 16 字符明文 hash 样例（可打印 ASCII）

	// 按 decode 的逆构造加密 hash：data = real XOR ks；enc = reverse(base64(data) 去填充)
	data := make([]byte, len(real))
	for i := range data {
		data[i] = real[i] ^ ks[i]
	}
	enc := reverseStr(strings.TrimRight(base64.StdEncoding.EncodeToString(data), "="))

	got, ok := deriveKeystream(enc, real)
	if !ok || !bytes.Equal(got, ks) {
		t.Fatalf("反推失败: ok=%v got=%x want=%x", ok, got, ks)
	}

	// 用反推出的 keystream 解 enc 应得回 real，确保 derive 与 decode 自洽
	dec, ok := (&Parser{keystream: got}).decode(enc)
	if !ok || dec != real {
		t.Fatalf("derive 与 decode 不自洽: ok=%v dec=%q want=%q", ok, dec, real)
	}
}

func TestParseMouflonSegments(t *testing.T) {
	body := []byte(strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_43_encB_222_part2.mp4",
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_42_encA_111.mp4",
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_44_bad_333.mp4",
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_45_enc+Plus_444.mp4",
	}, "\n"))
	decode := func(enc string) (string, bool) {
		switch enc {
		case "encA":
			return "realA", true
		case "encB":
			return "realB", true
		case "enc+Plus":
			return "realPlus", true
		default:
			return "", false
		}
	}

	segs, failed := parseMouflonSegments(body, decode)
	if failed != 1 {
		t.Fatalf("解码失败计数错误: got=%d want=1", failed)
	}
	if len(segs) != 3 {
		t.Fatalf("分段数量错误: got=%d want=3", len(segs))
	}
	if segs[0].key != (hlsSegmentKey{msn: 42, part: 0}) || !strings.Contains(segs[0].url, "realA") {
		t.Fatalf("第一个分段解析错误: %+v", segs[0])
	}
	if segs[1].key != (hlsSegmentKey{msn: 43, part: 2}) || !strings.Contains(segs[1].url, "realB") {
		t.Fatalf("第二个分段解析错误: %+v", segs[1])
	}
	if segs[2].key != (hlsSegmentKey{msn: 45, part: 0}) || !strings.Contains(segs[2].url, "realPlus") {
		t.Fatalf("包含 + 的分段解析错误: %+v", segs[2])
	}
}

func TestPreferCompleteMouflonSegments(t *testing.T) {
	segs := []hlsSegmentRef{
		{key: hlsSegmentKey{msn: 10, part: 0}, url: "seg10_part0", partial: true},
		{key: hlsSegmentKey{msn: 10, part: 1}, url: "seg10_part1", partial: true},
		{key: hlsSegmentKey{msn: 10, part: 0}, url: "seg10_full"},
		{key: hlsSegmentKey{msn: 11, part: 0}, url: "seg11_full"},
	}

	got, full, part := preferCompleteMouflonSegments(segs)
	if full != 2 || part != 2 {
		t.Fatalf("完整/part 统计错误: full=%d part=%d", full, part)
	}
	if len(got) != 2 || got[0].url != "seg10_full" || got[1].url != "seg11_full" {
		t.Fatalf("应只采用完整 segment: %+v", got)
	}

	partsOnly := []hlsSegmentRef{{key: hlsSegmentKey{msn: 12, part: 0}, url: "seg12_part0", partial: true}}
	got, full, part = preferCompleteMouflonSegments(partsOnly)
	if full != 0 || part != 1 || len(got) != 1 || got[0].url != "seg12_part0" {
		t.Fatalf("纯 part playlist 应保留 part 兜底: got=%+v full=%d part=%d", got, full, part)
	}
}

func TestParseMouflonSegmentsPrefersFullOverPartZero(t *testing.T) {
	body := []byte(strings.Join([]string{
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_42_encPart_111_part0.mp4",
		"#EXT-X-MOUFLON:URI:https://media-hls.example/room_42_encFull_222.mp4",
	}, "\n"))
	decode := func(enc string) (string, bool) {
		switch enc {
		case "encPart":
			return "realPart", true
		case "encFull":
			return "realFull", true
		default:
			return "", false
		}
	}

	segs, failed := parseMouflonSegments(body, decode)
	if failed != 0 || len(segs) != 1 {
		t.Fatalf("解析结果错误: failed=%d segs=%+v", failed, segs)
	}
	if segs[0].partial || !strings.Contains(segs[0].url, "realFull") {
		t.Fatalf("完整 segment 应覆盖 part0: %+v", segs[0])
	}
}

func TestBuildMouflonPlaylistURLPreservesQueryAndTargetsMSN(t *testing.T) {
	got := buildMouflonPlaylistURL("https://edge.example/live/room.m3u8?foo=bar", "key value", 123)
	if !strings.HasPrefix(got, "https://edge.example/live/room.m3u8?") {
		t.Fatalf("playlist URL 路径错误: %s", got)
	}
	for _, want := range []string{"foo=bar", "psch=v2", "pkey=key+value", "_HLS_msn=123"} {
		if !strings.Contains(got, want) {
			t.Fatalf("playlist URL 缺少参数 %q: %s", want, got)
		}
	}

	got = buildMouflonPlaylistURL("https://edge.example/live/room.m3u8?_HLS_msn=10", "key", 0)
	if strings.Contains(got, "_HLS_msn=") {
		t.Fatalf("普通 playlist URL 不应保留旧 _HLS_msn: %s", got)
	}
}

func TestWithPlaylistCacheBusterPreservesMouflonParams(t *testing.T) {
	got := withPlaylistCacheBuster("https://edge.example/live/room.m3u8?psch=v2&pkey=abc&_HLS_msn=42")
	for _, want := range []string{"psch=v2", "pkey=abc", "_HLS_msn=42", "_bgo_reload="} {
		if !strings.Contains(got, want) {
			t.Fatalf("cache buster URL 缺少参数 %q: %s", want, got)
		}
	}
}

func TestHLSMonitorSummaryHighlightsPlaylistMiss(t *testing.T) {
	got := hlsMonitorSummary(hlsSegmentStats{
		discovered:      9,
		suspectedMissed: 6,
		liveLagMSN:      3,
		windowMinMSN:    3725,
		windowMaxMSN:    3725,
		windowSegments:  1,
		maxDownloadMs:   212,
	}, 15, 10, 7, "回退")
	for _, want := range []string{"覆盖率=60%", "playlist滑窗漏段", "窗口过短", "定向playlist回退中"} {
		if !strings.Contains(got, want) {
			t.Fatalf("监视器摘要缺少 %q: %s", want, got)
		}
	}
}

func TestClassifyTargetPlaylistDistinguishesPendingFromMiss(t *testing.T) {
	segs := []hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1790}, url: "seg1790"},
		{key: hlsSegmentKey{msn: 1791}, url: "seg1791"},
		{key: hlsSegmentKey{msn: 1792}, url: "seg1792"},
	}

	if got := classifyTargetPlaylist(segs, 1791); got != targetPlaylistHit {
		t.Fatalf("目标在窗口内应为命中: got=%v", got)
	}
	if got := classifyTargetPlaylist(segs, 1793); got != targetPlaylistPending {
		t.Fatalf("目标还未出现在 playlist 最新之后，应为待出而不是未中: got=%v", got)
	}
	if got := classifyTargetPlaylist(segs, 1789); got != targetPlaylistMiss {
		t.Fatalf("playlist 已越过目标但未包含，应为未中: got=%v", got)
	}
	if got := classifyTargetPlaylist([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1790}, url: "seg1790"},
		{key: hlsSegmentKey{msn: 1792}, url: "seg1792"},
	}, 1791); got != targetPlaylistMiss {
		t.Fatalf("目标位于窗口范围内但缺失，应为未中: got=%v", got)
	}
}

func TestSegmentSchedulerRetriesFailedDownload(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	var calls atomic.Int32
	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, errTestDownload
		}
		return []byte("ok"), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 1}, url: "seg1"}})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "ok" {
		t.Fatalf("重试后写入内容错误: %q", string(got[0].body))
	}
	if calls.Load() < 2 {
		t.Fatalf("下载失败后没有重试: calls=%d", calls.Load())
	}
	st := s.snapshot(false)
	if st.downloadFailures != 1 || st.retrySuccess != 1 {
		t.Fatalf("重试统计错误: %+v", st)
	}
}

func TestSegmentSchedulerWritesInOrderWhenDownloadsFinishOutOfOrder(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()
	hlsPendingGapWait = 200 * time.Millisecond
	hlsObservedGapWait = 200 * time.Millisecond

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		if url == "seg1" {
			time.Sleep(50 * time.Millisecond)
		}
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
	})
	got := waitWritable(t, s, 2)
	if string(got[0].body) != "seg1" || string(got[1].body) != "seg2" {
		t.Fatalf("写入顺序错误: %q, %q", string(got[0].body), string(got[1].body))
	}
}

func TestSegmentSchedulerWaitsLongerForObservedSlowSegment(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()
	hlsPendingGapWait = 20 * time.Millisecond
	hlsObservedGapWait = 200 * time.Millisecond

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		if url == "seg1" {
			time.Sleep(80 * time.Millisecond)
		}
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
	})
	time.Sleep(40 * time.Millisecond)
	if got := s.takeWritable(time.Now()); len(got) != 0 {
		t.Fatalf("已发现但下载稍慢的分段不应按短缺口等待跳过: %+v", got)
	}

	got := waitWritable(t, s, 2)
	if string(got[0].body) != "seg1" || string(got[1].body) != "seg2" {
		t.Fatalf("慢下载分段完成后应保持顺序写入: %q, %q", string(got[0].body), string(got[1].body))
	}
	st := s.snapshot(false)
	if st.gaps != 0 {
		t.Fatalf("慢下载不应计为丢段: %+v", st)
	}
}

func TestSegmentSchedulerNextRequestMSNTargetsMissingGap(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 1}, url: "seg1"}})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "seg1" {
		t.Fatalf("第一个分段写入错误: %q", string(got[0].body))
	}
	if msn := s.nextRequestMSN(); msn != 2 {
		t.Fatalf("已写到 1 后应请求 2: got=%d", msn)
	}

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 3}, url: "seg3"}})
	if msn := s.nextRequestMSN(); msn != 2 {
		t.Fatalf("发现 3 但缺 2 时应优先请求缺口: got=%d", msn)
	}

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 2}, url: "seg2"}})
	if msn := s.nextRequestMSN(); msn != 4 {
		t.Fatalf("缺口已进入队列后应请求最新后继: got=%d", msn)
	}
}

func TestSegmentSchedulerNextLiveEdgeMSNTargetsLatestSuccessor(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 10}, url: "seg10"}})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "seg10" {
		t.Fatalf("第一个分段写入错误: %q", string(got[0].body))
	}

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 12}, url: "seg12"}})
	if msn := s.nextLiveEdgeMSN(); msn != 13 {
		t.Fatalf("直播边缘探测应请求 playlist 最新 msn 后继: got=%d", msn)
	}
	if msn := s.nextRequestMSN(); msn != 11 {
		t.Fatalf("普通定向应优先请求已写入后缺失的 msn: got=%d", msn)
	}
}

func TestSegmentSchedulerTracksSuspectedMissedPlaylistWindow(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
	})
	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 5}, url: "seg5"}})
	st := s.snapshot(false)
	if st.suspectedMissed != 2 || st.suspectedTotal != 2 {
		t.Fatalf("疑似漏看统计错误: %+v", st)
	}
	_ = s.snapshot(true)
	if st = s.snapshot(false); st.suspectedMissed != 0 {
		t.Fatalf("周期疑似漏看统计应被重置: %+v", st)
	}
	if st.suspectedTotal != 2 {
		t.Fatalf("累计疑似漏看不应被周期重置: %+v", st)
	}
}

func TestSegmentSchedulerTargetAddDoesNotUpdatePlaylistWindow(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 10}, url: "seg10"},
		{key: hlsSegmentKey{msn: 11}, url: "seg11"},
		{key: hlsSegmentKey{msn: 12}, url: "seg12"},
	})
	st := s.snapshot(false)
	if st.windowMinMSN != 10 || st.windowMaxMSN != 12 || st.windowSegments != 3 {
		t.Fatalf("普通 playlist 应更新窗口: %+v", st)
	}

	s.addTarget([]hlsSegmentRef{{key: hlsSegmentKey{msn: 13}, url: "seg13"}})
	st = s.snapshot(false)
	if st.windowMinMSN != 10 || st.windowMaxMSN != 12 || st.windowSegments != 3 {
		t.Fatalf("定向命中不应覆盖普通 playlist 窗口: %+v", st)
	}
	if st.suspectedMissed != 0 || st.gaps != 0 {
		t.Fatalf("定向命中不应制造漏段统计: %+v", st)
	}
}

func TestSegmentSchedulerCountsWrittenMSNJumpAsGap(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 3}, url: "seg3"},
	})
	got := waitWritable(t, s, 2)
	if string(got[0].body) != "seg1" || string(got[1].body) != "seg3" {
		t.Fatalf("缺口超时后的写入顺序错误: %q, %q", string(got[0].body), string(got[1].body))
	}
	st := s.snapshot(false)
	if st.gaps != 1 {
		t.Fatalf("写入 msn 跨越缺口时应计为确认丢段: %+v", st)
	}
}

func TestSegmentSchedulerWaitsForSuspectedMissingMSNsBeforeAdvancing(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 1}, url: "seg1"}})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "seg1" {
		t.Fatalf("第一个分段写入错误: %q", string(got[0].body))
	}

	s.add([]hlsSegmentRef{{key: hlsSegmentKey{msn: 5}, url: "seg5"}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if probes := s.targetProbeMSNs(3); len(probes) >= 3 && probes[0] == 2 && probes[1] == 4 && probes[2] == 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if probes := s.targetProbeMSNs(3); len(probes) < 3 || probes[0] != 2 || probes[1] != 4 || probes[2] != 3 {
		t.Fatalf("应优先探测阻塞缺口和最近疑似缺口 msn: %+v", probes)
	}
	if next := s.takeWritable(time.Now()); len(next) != 0 {
		t.Fatalf("疑似缺口等待期内不应直接写后续分段: %+v", next)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = s.takeWritable(time.Now().Add(hlsPendingGapWait + 10*time.Millisecond))
		if len(got) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(got) != 1 || string(got[0].body) != "seg5" {
		t.Fatalf("缺口等待超时后应继续写后续分段: %+v", got)
	}
	st := s.snapshot(false)
	if st.gaps != 3 {
		t.Fatalf("跳过疑似缺口后应记录实际跳过数量: %+v", st)
	}
}

func TestSegmentSchedulerCountsOnlyObservedPendingGap(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		if url == "seg2" {
			return nil, errTestDownload
		}
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
		{key: hlsSegmentKey{msn: 3}, url: "seg3"},
	})
	got := waitWritable(t, s, 2)
	if string(got[0].body) != "seg1" || string(got[1].body) != "seg3" {
		t.Fatalf("观察到的缺口跳过后写入顺序错误: %q, %q", string(got[0].body), string(got[1].body))
	}
	st := s.snapshot(false)
	if st.gaps != 1 || st.writeWaits == 0 {
		t.Fatalf("观察到的待处理缺口统计错误: %+v", st)
	}
}

func TestSegmentSchedulerHoldsNearLiveEdge(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()
	hlsLiveEdgeHold = 2
	hlsLiveEdgeMaxWait = 40 * time.Millisecond

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
		{key: hlsSegmentKey{msn: 3}, url: "seg3"},
	})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "seg1" {
		t.Fatalf("第一个分段应立即写入: %q", string(got[0].body))
	}

	if next := s.takeWritable(time.Now()); len(next) != 0 {
		t.Fatalf("靠近直播边缘的分段应先等待: got=%d", len(next))
	}
	time.Sleep(50 * time.Millisecond)
	if next := s.takeWritable(time.Now()); len(next) == 0 || string(next[0].body) != "seg2" {
		t.Fatalf("等待上限后应继续写入: %+v", next)
	}
}

func TestSegmentSchedulerFinalFlushBypassesLiveEdgeHold(t *testing.T) {
	restore := tuneSchedulerForTest()
	defer restore()
	hlsLiveEdgeHold = 2
	hlsLiveEdgeMaxWait = time.Second

	s := newHLSSegmentScheduler(context.Background(), func(url string) ([]byte, error) {
		return []byte(url), nil
	})
	defer s.stop()

	s.add([]hlsSegmentRef{
		{key: hlsSegmentKey{msn: 1}, url: "seg1"},
		{key: hlsSegmentKey{msn: 2}, url: "seg2"},
		{key: hlsSegmentKey{msn: 3}, url: "seg3"},
	})
	got := waitWritable(t, s, 1)
	if string(got[0].body) != "seg1" {
		t.Fatalf("第一个分段应立即写入: %q", string(got[0].body))
	}

	var final []hlsWritableSegment
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		final = s.takeWritableFinal(time.Now())
		if len(final) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(final) == 0 || string(final[0].body) != "seg2" {
		t.Fatalf("final flush 应绕过直播边缘等待: %+v", final)
	}
}

var errTestDownload = &testDownloadError{}

type testDownloadError struct{}

func (*testDownloadError) Error() string { return "test download failed" }

func tuneSchedulerForTest() func() {
	oldPendingGapWait := hlsPendingGapWait
	oldObservedGapWait := hlsObservedGapWait
	oldRetryBase, oldRetryMax := hlsRetryBase, hlsRetryMax
	oldLiveEdgeHold, oldLiveEdgeMaxWait := hlsLiveEdgeHold, hlsLiveEdgeMaxWait
	hlsPendingGapWait = 30 * time.Millisecond
	hlsObservedGapWait = 30 * time.Millisecond
	hlsRetryBase = 5 * time.Millisecond
	hlsRetryMax = 20 * time.Millisecond
	hlsLiveEdgeHold = 0
	hlsLiveEdgeMaxWait = 30 * time.Millisecond
	return func() {
		hlsPendingGapWait = oldPendingGapWait
		hlsObservedGapWait = oldObservedGapWait
		hlsRetryBase = oldRetryBase
		hlsRetryMax = oldRetryMax
		hlsLiveEdgeHold = oldLiveEdgeHold
		hlsLiveEdgeMaxWait = oldLiveEdgeMaxWait
	}
}

func waitWritable(t *testing.T, s *hlsSegmentScheduler, want int) []hlsWritableSegment {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got []hlsWritableSegment
	for time.Now().Before(deadline) {
		got = append(got, s.takeWritable(time.Now())...)
		if len(got) >= want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待可写分段超时: got=%d want=%d stats=%+v", len(got), want, s.snapshot(false))
	return nil
}
