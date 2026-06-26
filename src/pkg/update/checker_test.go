package update

import (
	"strings"
	"testing"
)

func TestReleaseInfoFromTagBuildsFallbackDownloadURL(t *testing.T) {
	c := NewChecker("v1.1.1")

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
}
