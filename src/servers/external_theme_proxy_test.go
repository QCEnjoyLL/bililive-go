package servers

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	assert.Contains(t, injected, "refreshExternalToolInfo")
	assert.Contains(t, injected, "refreshExternalToolLegend")
	assert.Contains(t, injected, "refreshExternalVersionBadge")
	assert.Contains(t, injected, "bgo-external-version-badge")
	assert.Contains(t, injected, "syncDisplayedDefaultVersion")
	assert.Contains(t, injected, "isCurrentVersionLabelText")
	assert.Contains(t, injected, "updateToolVersionMeta")
	assert.Contains(t, injected, "getToolInstallState")
	assert.Contains(t, injected, "syncToolCardActionState")
	assert.Contains(t, injected, "syncToolGroupToggleState")
	assert.Contains(t, injected, "bgoToolToggleBlocked")
	assert.Contains(t, injected, "bgoToolRequiredToggleBlocked")
	assert.Contains(t, injected, "requestedPinnedVersion")
	assert.Contains(t, injected, "isLastRequiredInstalledVersion")
	assert.Contains(t, injected, "hideOriginalVersionSections")
	assert.Contains(t, injected, "getToolGroupCardFromControl")
	assert.Contains(t, injected, "markToolVersionPreparingFromButton")
	assert.Contains(t, injected, "requestExternalThemeFrameRefresh")
	assert.Contains(t, injected, "bgoVersionPreparing")
	assert.Contains(t, injected, "startsWithOriginalVersionSectionText")
	assert.Contains(t, injected, "node.closest('.ant-space-item')")
	assert.Contains(t, injected, `querySelector('.ant-card,[data-bgo-tool-version-card="true"],button,.ant-btn')`)
	assert.Contains(t, injected, "bgoToolInfoMap")
	assert.Contains(t, injected, "bgo-tool-info")
	assert.Contains(t, injected, "bgo-tool-legend")
	assert.Contains(t, injected, `\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5`)
	assert.Contains(t, injected, `\u7a0b\u5e8f\u66f4\u65b0\u7f13\u5b58`)
	assert.Contains(t, injected, `\u4e91\u76d8\u4e0a\u4f20\u670d\u52a1`)
	assert.Contains(t, injected, "bgo-tool-version-compact")
	assert.Contains(t, injected, "loadToolVersionMeta")
	assert.NotContains(t, injected, "if (!rawVersions.length) return")
	assert.Contains(t, injected, "currentState.installed")
	assert.Contains(t, injected, "bgoHasMultipleVersions")
	assert.Contains(t, injected, "bgoToolGroupEmpty")
	assert.Contains(t, injected, "bgoVersionSummaryReady")
	assert.Contains(t, injected, "data-bgo-original-version-divider")
	assert.Contains(t, injected, "./api/pin-version")
	assert.NotContains(t, injected, `.ant-card[data-bgo-has-multiple-versions="true"]:not([data-bgo-version-picker-ready="true"]) .ant-card`)
	assert.Contains(t, injected, `\u9ed8\u8ba4\u7248\u672c`)
	assert.Contains(t, injected, `\u63a8\u8350\u7248\u672c`)
	assert.Contains(t, injected, `\u5f53\u524d\u7248\u672c`)
	assert.Contains(t, injected, `\u53ef\u9009\u7248\u672c`)
	assert.Contains(t, injected, `bgo-tool-version-enable`)
	assert.Contains(t, injected, `\u53ea\u6709\u5df2\u5b89\u88c5\u7248\u672c\u624d\u80fd\u542f\u7528`)
	assert.Contains(t, injected, "selectedState.installed")
	assert.Contains(t, injected, `role="switch"`)
	assert.Contains(t, injected, "#30d158")
	assert.Contains(t, injected, "bgoRequiredToolUninstallMessage")
	assert.Contains(t, injected, "bgo-tool-required-uninstall-notice")
	assert.Contains(t, injected, "showToolGuardNotice")
	assert.Contains(t, injected, "shouldBlockRequiredUninstallButton")
	assert.Contains(t, injected, "countInstalledVersionCardsFromDOM")
	assert.Contains(t, injected, "__bgoRequestURL")
	assert.Contains(t, injected, "bgo-tool-guard-notice")
	assert.Contains(t, injected, "body.err_msg || body.message")
	assert.Contains(t, injected, "getToolVersionMeta")
	assert.Contains(t, injected, "bgo-tool-empty-notice")
	assert.Contains(t, injected, "isInternalToolGroup")
	assert.Contains(t, injected, `\u5185\u90e8\u7ba1\u7406`)
	assert.Contains(t, injected, "bgoToolRequiredLastInstalled")
	assert.Contains(t, injected, "data-bgo-tool-status")
	assert.Contains(t, injected, "bgo-tool-version-state")
	assert.Contains(t, injected, "运行平台:")
	assert.Contains(t, injected, "默认版本")
	assert.Contains(t, injected, "已启用")
	assert.Contains(t, injected, "已禁用")
	assert.Contains(t, injected, "已安装")
	assert.Contains(t, injected, "未安装")
	assert.Contains(t, injected, "安装失败")
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
	assert.Contains(t, injected, `\u4e0d\u80fd\u5378\u8f7d\u6240\u6709\u7248\u672c`)
	assert.Contains(t, injected, `\u4e0d\u80fd\u505c\u7528`)
	assert.Contains(t, injected, "font-size:13px!important")
	assert.Contains(t, injected, "min-height: 22px")
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

func TestRequiredToolUninstallGuardBlocksLastInstalledVersion(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"biliLive-tools","tools":[{"version":"3.1.2-bgo.1","installed":true},{"version":"3.1.2-bgo.2","installed":false}]}]`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/uninstall", strings.NewReader(`{"toolName":"biliLive-tools","version":"3.1.2-bgo.1"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.Contains(t, rr.Header().Get(contentType), contentTypeJSON)
	assert.Contains(t, rr.Body.String(), `"err_msg"`)
	assert.Contains(t, rr.Body.String(), requiredToolLastVersionUninstallMessage)
	assert.Contains(t, rr.Body.String(), "不能卸载所有版本")
}

func TestRequiredToolUninstallGuardBlocksToolsPrefixedWrappedStatus(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `{"value":[{"name":"biliLive-tools","tools":[{"version":"3.1.2-bgo.1","installed":false},{"version":"3.1.2-bgo.2","installed":true}]}]}`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/tools/api/uninstall", strings.NewReader(`{"name":"biliLive-tools","version":"3.1.2-bgo.2"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.Contains(t, rr.Body.String(), requiredToolLastVersionUninstallMessage)
}

func TestRemoteToolAPIFallbackRewritesEmptyUninstallError(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	body := []byte(`{"name":"biliLive-tools","version":"3.1.2-bgo.2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/uninstall", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	serveRemoteToolAPIWithGuardFallback(rr, req, next, "/api/uninstall", body)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.Contains(t, rr.Header().Get(contentType), contentTypeJSON)
	assert.Contains(t, rr.Body.String(), `"err_msg"`)
	assert.Contains(t, rr.Body.String(), requiredToolLastVersionUninstallMessage)
}

func TestRequiredToolUninstallGuardAllowsWhenAnotherVersionInstalled(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"biliLive-tools","tools":[{"version":"3.1.2-bgo.1","installed":true},{"version":"3.1.2-bgo.2","installed":true}]}]`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		assert.Contains(t, string(body), "3.1.2-bgo.1")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/uninstall", strings.NewReader(`{"toolName":"biliLive-tools","version":"3.1.2-bgo.1"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, called)
}

func TestProtectedToolUninstallGuardBlocksInternalTool(t *testing.T) {
	remoteCalled := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/uninstall", strings.NewReader(`{"toolName":"bililive-go","version":"1.2.3"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.False(t, remoteCalled)
	assert.Contains(t, rr.Body.String(), internalToolUninstallMessage)
}

func TestProtectedToolUninstallGuardBlocksUninstalledVersion(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"openlist","tools":[{"version":"v4.2.2","installed":false}]}]`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/uninstall", strings.NewReader(`{"toolName":"openlist","version":"v4.2.2"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.Contains(t, rr.Body.String(), uninstalledToolVersionMessage)
}

func TestProtectedToolToggleGuardBlocksEnableWithoutInstalledVersion(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"chromium","tools":[{"version":"150.0.7871.24","installed":false}]}]`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", strings.NewReader(`{"toolName":"chromium","enabled":true}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.Contains(t, rr.Body.String(), enableMissingToolGroupMessage)
}

func TestProtectedToolToggleGuardBlocksEnableWithUninstalledPinnedVersion(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tools", r.URL.Path)
		_, _ = io.WriteString(w, `[{"name":"openlist","pinnedVersion":"v4.2.2","tools":[{"version":"v4.2.1","installed":true},{"version":"v4.2.2","installed":false}]}]`)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", strings.NewReader(`{"toolName":"openlist","enabled":true}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.Contains(t, rr.Body.String(), enablePinnedMissingToolGroupMessage)
}

func TestProtectedToolToggleGuardBlocksRequiredToolDisable(t *testing.T) {
	remoteCalled := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", strings.NewReader(`{"toolName":"ffmpeg","enabled":false}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
	assert.False(t, called)
	assert.False(t, remoteCalled)
	assert.Contains(t, rr.Header().Get(contentType), contentTypeJSON)
	assert.Contains(t, rr.Body.String(), requiredToolDisableMessage)
	assert.Contains(t, rr.Body.String(), "不能停用")
}

func TestProtectedToolToggleGuardAllowsDisableWithoutInstalledVersion(t *testing.T) {
	remoteCalled := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer remote.Close()
	target, err := url.Parse(remote.URL)
	require.NoError(t, err)

	called := false
	handler := guardProtectedToolUninstall(target, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", strings.NewReader(`{"toolName":"chromium","enabled":false}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, called)
	assert.False(t, remoteCalled)
}
