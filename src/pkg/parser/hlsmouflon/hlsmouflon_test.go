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

func TestSegmentSchedulerSkipsMissingGapImmediately(t *testing.T) {
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
	if st.gaps != 1 || st.writeWaits != 0 {
		t.Fatalf("缺口统计错误: %+v", st)
	}
}

var errTestDownload = &testDownloadError{}

type testDownloadError struct{}

func (*testDownloadError) Error() string { return "test download failed" }

func tuneSchedulerForTest() func() {
	oldPendingGapWait := hlsPendingGapWait
	oldRetryBase, oldRetryMax := hlsRetryBase, hlsRetryMax
	hlsPendingGapWait = 30 * time.Millisecond
	hlsRetryBase = 5 * time.Millisecond
	hlsRetryMax = 20 * time.Millisecond
	return func() {
		hlsPendingGapWait = oldPendingGapWait
		hlsRetryBase = oldRetryBase
		hlsRetryMax = oldRetryMax
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
