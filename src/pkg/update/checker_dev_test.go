package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsNewerVersionTreatsDevBuildAsSameCoreRelease(t *testing.T) {
	c := NewChecker("v1.2.3-dev")

	got, err := c.isNewerVersion("v1.2.3")
	if err != nil {
		t.Fatalf("isNewerVersion returned error: %v", err)
	}
	if got {
		t.Fatalf("same-core stable release must not be newer than local dev build")
	}
}

func TestIsNewerVersionDevBuildAllowsHigherCoreRelease(t *testing.T) {
	c := NewChecker("v1.2.3-dev")

	got, err := c.isNewerVersion("v1.2.4")
	if err != nil {
		t.Fatalf("isNewerVersion returned error: %v", err)
	}
	if !got {
		t.Fatalf("higher-core release must be newer than local dev build")
	}
}

func TestCheckForUpdateIgnoresSameCoreReleaseForDevBuild(t *testing.T) {
	c := NewChecker("v1.2.3-dev")
	c.SetRawBaseURL("")
	assetName := c.getExpectedAssetName() + ".zip"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"tag_name":"v1.2.3",
			"draft":false,
			"prerelease":false,
			"published_at":"2026-06-30T00:00:00Z",
			"assets":[{
				"name":"` + assetName + `",
				"size":1,
				"browser_download_url":"https://example.com/download"
			}]
		}]`))
	}))
	defer server.Close()

	c.SetReleaseURL(server.URL)
	info, err := c.CheckForUpdate(false)
	if err != nil {
		t.Fatalf("CheckForUpdate returned error: %v", err)
	}
	if info != nil {
		t.Fatalf("same-core stable release must not be returned as update: %+v", info)
	}
}
