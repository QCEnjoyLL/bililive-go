package servers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectExternalThemeHTML(t *testing.T) {
	html := []byte("<html><head><title>x</title></head><body>ok</body></html>")
	injected := string(injectExternalThemeHTML(html))

	assert.Contains(t, injected, "bgo-external-theme-bridge")
	assert.Contains(t, injected, "bg-white")
	assert.Contains(t, injected, "bg-card")
	assert.Contains(t, injected, "text-gray-900")
	assert.Contains(t, injected, "text-muted-foreground")
	assert.Contains(t, injected, "text-white")
	assert.Contains(t, injected, `button[role="switch"]`)
	assert.Contains(t, injected, "bgo-external-switch-style-late")
	assert.Contains(t, injected, ".rc-switch")
	assert.Contains(t, injected, "refreshExternalTextHints")
	assert.Contains(t, injected, "refreshExternalToolVersionPicker")
	assert.Contains(t, injected, "bgo-tool-version-compact")
	assert.Contains(t, injected, "loadToolVersionMeta")
	assert.Contains(t, injected, "data-bgo-has-multiple-versions")
	assert.Contains(t, injected, "./api/pin-version")
	assert.Contains(t, injected, `\u9ed8\u8ba4\u7248\u672c`)
	assert.Contains(t, injected, "data-bgo-tool-status")
	assert.Contains(t, injected, "运行平台:")
	assert.Contains(t, injected, "默认版本")
	assert.Contains(t, injected, "已启用")
	assert.Contains(t, injected, "已禁用")
	assert.Contains(t, injected, "已安装")
	assert.Contains(t, injected, "未安装")
	assert.Contains(t, injected, "该工具组暂无工具")
	assert.Contains(t, injected, "bgoVersionPattern")
	assert.Contains(t, injected, `data-bgo-tool-status="version"`)
	assert.Contains(t, injected, `color:#d7c48d!important`)
	assert.Contains(t, injected, `color:#c7d2fe!important`)
	assert.Contains(t, injected, `color:#bfdbfe!important`)
	assert.Contains(t, injected, `color: #333`)
	assert.Contains(t, injected, "#34c759")
	assert.Contains(t, injected, "#39393d")
	assert.Contains(t, injected, "width: 28px")
	assert.Contains(t, injected, ".3s all ease-in-out")
	assert.Contains(t, injected, "word-break: keep-all")
	assert.Contains(t, injected, "--bgo-ext-panel-bg")
	assert.Contains(t, injected, "--card-foreground")
	assert.Contains(t, injected, "bg-gray-950")
	assert.Contains(t, injected, "pageBg: colors.pageBg")
	assert.NotContains(t, injected, "#2f3b3f")
	assert.Less(t, strings.Index(injected, "bgo-external-theme-bridge"), strings.Index(injected, "</head>"))

	injectedAgain := string(injectExternalThemeHTML([]byte(injected)))
	assert.Equal(t, 1, strings.Count(injectedAgain, "bgo-external-theme-bridge"))
}

func TestInjectExternalThemeResponseSkipsNonHTML(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(strings.NewReader("console.log(1);")),
	}

	require.NoError(t, injectExternalThemeResponse(resp))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "console.log(1);", string(body))
}
