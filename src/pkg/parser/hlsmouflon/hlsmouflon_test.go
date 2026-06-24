package hlsmouflon

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
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
