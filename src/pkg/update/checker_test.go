package update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseInfoFromTagBuildsFallbackDownloadURL(t *testing.T) {
	c := NewChecker("v1.1.1")
	c.SetRawBaseURL("")

	info := c.releaseInfoFromTag("v1.1.2")

	if info.Version != "1.1.2" || info.TagName != "v1.1.2" {
		t.Fatalf("版本信息错误: %+v", info)
	}
	if info.AssetName != c.getExpectedArchiveName() {
		t.Fatalf("资源名错误: got=%q want=%q", info.AssetName, c.getExpectedArchiveName())
	}
	if len(info.DownloadURLs) != 2 {
		t.Fatalf("下载链接数量错误: got=%d want=2", len(info.DownloadURLs))
	}
	want := GitHubRepoURL + "/releases/download/v1.1.2/" + c.getExpectedArchiveName()
	if info.DownloadURLs[0] != want {
		t.Fatalf("直连下载链接错误: got=%q want=%q", info.DownloadURLs[0], want)
	}
	if !strings.Contains(info.DownloadURLs[1], "remotetools/download") {
		t.Fatalf("备用中转下载链接错误: %q", info.DownloadURLs[1])
	}
	if !strings.Contains(info.Changelog, "版本 v1.1.2 更新说明") {
		t.Fatalf("备用更新说明不是中文默认说明: %q", info.Changelog)
	}
}

func TestNormalizeChangelogUsesChineseFallback(t *testing.T) {
	got := normalizeChangelog("  ", "v1.1.6")
	if !strings.Contains(got, "版本 v1.1.6 更新说明") {
		t.Fatalf("空更新说明未生成中文 fallback: %q", got)
	}
	if !strings.Contains(got, "建议更新到此版本") {
		t.Fatalf("中文 fallback 内容不完整: %q", got)
	}

	body := "custom release note"
	if normalizeChangelog(body, "v1.1.6") != body {
		t.Fatalf("非空更新说明不应被覆盖")
	}
}

func TestChangelogForReleaseReadsRepositoryNotesWhenBodyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.1.11/docs/releases/v1.1.11.md" {
			t.Fatalf("更新说明请求路径错误: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("## v1.1.11\n\n- 中文更新说明"))
	}))
	defer server.Close()

	c := NewChecker("v1.1.10")
	c.SetRawBaseURL(server.URL)

	got := c.changelogForRelease("", "v1.1.11")
	if !strings.Contains(got, "中文更新说明") {
		t.Fatalf("未读取仓库更新说明: %q", got)
	}
}

func TestChangelogForReleasePrefersReleaseBody(t *testing.T) {
	c := NewChecker("v1.1.10")
	c.SetRawBaseURL("")

	got := c.changelogForRelease("GitHub Release 正文", "v1.1.11")
	if got != "GitHub Release 正文" {
		t.Fatalf("非空 release body 不应被覆盖: %q", got)
	}
}
