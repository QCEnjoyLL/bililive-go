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
	}, "\n"))
	decode := func(enc string) (string, bool) {
		switch enc {
		case "encA":
			return "realA", true
		case "encB":
			return "realB", true
		default:
			return "", false
		}
	}

	segs, failed := parseMouflonSegments(body, decode)
	if failed != 1 {
		t.Fatalf("解码失败计数错误: got=%d want=1", failed)
	}
	if len(segs) != 2 {
		t.Fatalf("分段数量错误: got=%d want=2", len(segs))
	}
	if segs[0].key != (hlsSegmentKey{msn: 42, part: 0}) || !strings.Contains(segs[0].url, "realA") {
		t.Fatalf("第一个分段解析错误: %+v", segs[0])
	}
	if segs[1].key != (hlsSegmentKey{msn: 43, part: 2}) || !strings.Contains(segs[1].url, "realB") {
		t.Fatalf("第二个分段解析错误: %+v", segs[1])
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
	if st.suspectedMissed != 2 {
		t.Fatalf("疑似漏看统计错误: %+v", st)
	}
	_ = s.snapshot(true)
	if st = s.snapshot(false); st.suspectedMissed != 0 {
		t.Fatalf("周期疑似漏看统计应被重置: %+v", st)
	}
}

func TestSegmentSchedulerDoesNotCountUnobservedMSNAsGap(t *testing.T) {
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
	if st.gaps != 0 {
		t.Fatalf("未观察到的 msn 不应计为确认丢段: %+v", st)
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

	final := s.takeWritableFinal(time.Now())
	if len(final) == 0 || string(final[0].body) != "seg2" {
		t.Fatalf("final flush 应绕过直播边缘等待: %+v", final)
	}
}

var errTestDownload = &testDownloadError{}

type testDownloadError struct{}

func (*testDownloadError) Error() string { return "test download failed" }

func tuneSchedulerForTest() func() {
	oldPendingGapWait := hlsPendingGapWait
	oldRetryBase, oldRetryMax := hlsRetryBase, hlsRetryMax
	oldLiveEdgeHold, oldLiveEdgeMaxWait := hlsLiveEdgeHold, hlsLiveEdgeMaxWait
	hlsPendingGapWait = 30 * time.Millisecond
	hlsRetryBase = 5 * time.Millisecond
	hlsRetryMax = 20 * time.Millisecond
	hlsLiveEdgeHold = 0
	hlsLiveEdgeMaxWait = 30 * time.Millisecond
	return func() {
		hlsPendingGapWait = oldPendingGapWait
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
