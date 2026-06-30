package servers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

func newExternalThemeReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		// Keep HTML responses uncompressed so the proxy can inject the theme bridge.
		r.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = injectExternalThemeResponse
	return proxy
}

func newToolsExternalThemeReverseProxy(target *url.URL) http.Handler {
	return guardProtectedToolUninstall(target, newExternalThemeReverseProxy(target))
}

type remoteToolGroupStatus struct {
	Name          string                 `json:"name"`
	PinnedVersion string                 `json:"pinnedVersion,omitempty"`
	Tools         []remoteToolItemStatus `json:"tools"`
}

type remoteToolItemStatus struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

type remoteToolUninstallRequest struct {
	ToolName string `json:"toolName"`
	Name     string `json:"name"`
	Tool     string `json:"tool"`
	Version  string `json:"version"`
}

type remoteToolToggleRequest struct {
	ToolName string `json:"toolName"`
	Name     string `json:"name"`
	Tool     string `json:"tool"`
	Enabled  bool   `json:"enabled"`
}

func (req remoteToolUninstallRequest) effectiveToolName() string {
	for _, value := range []string{req.ToolName, req.Name, req.Tool} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (req remoteToolToggleRequest) effectiveToolName() string {
	for _, value := range []string{req.ToolName, req.Name, req.Tool} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var requiredRemoteToolNames = map[string]struct{}{
	"ffmpeg":             {},
	"dotnet":             {},
	"bililive-tools":     {},
	"node":               {},
	"bililive-scheduler": {},
}

var internalRemoteToolNames = map[string]struct{}{
	"bililive-go": {},
	"klive":       {},
}

const (
	requiredToolLastVersionUninstallMessage = "\u8fd9\u662f\u5fc5\u9700\u5de5\u5177\uff0c\u81f3\u5c11\u8981\u4fdd\u7559\u4e00\u4e2a\u5df2\u5b89\u88c5\u7248\u672c\uff0c\u4e0d\u80fd\u5378\u8f7d\u6240\u6709\u7248\u672c\uff1b\u5982\u9700\u5207\u6362\uff0c\u5148\u5b89\u88c5\u5176\u4ed6\u7248\u672c\u518d\u5378\u8f7d\u5f53\u524d\u7248\u672c\u3002"
	uninstalledToolVersionMessage           = "\u8be5\u7248\u672c\u5c1a\u672a\u5b89\u88c5\uff0c\u65e0\u9700\u5378\u8f7d\u3002"
	internalToolUninstallMessage            = "\u8fd9\u662f BiliLive-go \u81ea\u52a8\u7ba1\u7406\u7684\u5185\u90e8\u7ec4\u4ef6\uff0c\u4e0d\u80fd\u901a\u8fc7\u5916\u90e8\u5de5\u5177\u9875\u624b\u52a8\u5378\u8f7d\u3002"
	internalToolToggleMessage               = "\u8fd9\u662f BiliLive-go \u81ea\u52a8\u7ba1\u7406\u7684\u5185\u90e8\u7ec4\u4ef6\uff0c\u4e0d\u80fd\u901a\u8fc7\u5916\u90e8\u5de5\u5177\u9875\u624b\u52a8\u542f\u7528\u6216\u7981\u7528\u3002"
	requiredToolDisableMessage              = "\u8fd9\u662f\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5\u5de5\u5177\uff0c\u5f55\u5236\u548c\u76f8\u5173\u529f\u80fd\u4f9d\u8d56\u5b83\u4fdd\u6301\u542f\u7528\uff0c\u4e0d\u80fd\u505c\u7528\u3002"
	enableMissingToolGroupMessage           = "\u8be5\u5de5\u5177\u7ec4\u6ca1\u6709\u4efb\u4f55\u5df2\u5b89\u88c5\u7248\u672c\uff0c\u4e0d\u80fd\u542f\u7528\u3002\u8bf7\u5148\u5b89\u88c5\u4e00\u4e2a\u7248\u672c\uff0c\u518d\u542f\u7528\u8be5\u5de5\u5177\u7ec4\u3002"
	enablePinnedMissingToolGroupMessage     = "\u8be5\u5de5\u5177\u7ec4\u5f53\u524d\u6307\u5411\u7684\u7248\u672c\u5c1a\u672a\u5b89\u88c5\uff0c\u4e0d\u80fd\u542f\u7528\u3002\u8bf7\u5148\u5b89\u88c5\u5f53\u524d\u7248\u672c\uff0c\u6216\u5207\u6362\u5230\u5df2\u5b89\u88c5\u7248\u672c\u3002"
)

func writeRemoteToolGuardError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set(contentType, contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"err_no":  status,
		"err_msg": msg,
		"message": msg,
		"data":    map[string]string{"reason": msg},
	})
}

func guardProtectedToolUninstall(target *url.URL, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			path := strings.TrimRight(r.URL.Path, "/")
			path = strings.TrimPrefix(path, "/tools")
			if path != "/api/uninstall" && path != "/api/toggle" {
				next.ServeHTTP(w, r)
				return
			}

			body, err := readAndRestoreRequestBody(r)
			if err != nil {
				writeRemoteToolGuardError(w, http.StatusBadRequest, "读取工具请求失败: "+err.Error())
				return
			}

			if path == "/api/uninstall" {
				var req remoteToolUninstallRequest
				if err := json.Unmarshal(body, &req); err == nil {
					toolName := req.effectiveToolName()
					if isInternalRemoteTool(toolName) {
						writeRemoteToolGuardError(w, http.StatusConflict, internalToolUninstallMessage)
						return
					}

					decision, err := shouldBlockRemoteToolUninstall(r, target, req)
					if err != nil {
						if isRequiredRemoteTool(toolName) {
							writeRemoteToolGuardError(w, http.StatusBadGateway, "无法确认必需工具安装状态，已取消卸载: "+err.Error())
							return
						}
					} else if decision.block {
						writeRemoteToolGuardError(w, http.StatusConflict, decision.reason)
						return
					}
				}
			}

			if path == "/api/toggle" {
				var req remoteToolToggleRequest
				if err := json.Unmarshal(body, &req); err == nil {
					decision, err := shouldBlockRemoteToolToggle(r, target, req)
					if err != nil {
						writeRemoteToolGuardError(w, http.StatusBadGateway, "无法确认工具安装状态，已取消启用: "+err.Error())
						return
					}
					if decision.block {
						writeRemoteToolGuardError(w, http.StatusConflict, decision.reason)
						return
					}
				}
			}

			if path == "/api/uninstall" || path == "/api/toggle" {
				serveRemoteToolAPIWithGuardFallback(w, r, next, path, body)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type bufferedToolAPIResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedToolAPIResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedToolAPIResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedToolAPIResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func serveRemoteToolAPIWithGuardFallback(w http.ResponseWriter, r *http.Request, next http.Handler, path string, body []byte) {
	rec := &bufferedToolAPIResponseWriter{header: make(http.Header)}
	next.ServeHTTP(rec, r)
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	if status >= 400 && strings.TrimSpace(rec.body.String()) == "" {
		writeRemoteToolGuardError(w, status, remoteToolFallbackMessage(path, body))
		return
	}
	for key, values := range rec.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(rec.body.Bytes())
}

func remoteToolFallbackMessage(path string, body []byte) string {
	if path == "/api/uninstall" {
		var req remoteToolUninstallRequest
		if err := json.Unmarshal(body, &req); err == nil && isRequiredRemoteTool(req.effectiveToolName()) {
			return requiredToolLastVersionUninstallMessage
		}
		return "卸载失败：外部工具服务未返回具体原因，请刷新工具状态后重试。"
	}
	if path == "/api/toggle" {
		var req remoteToolToggleRequest
		if err := json.Unmarshal(body, &req); err == nil && isRequiredRemoteTool(req.effectiveToolName()) && !req.Enabled {
			return requiredToolDisableMessage
		}
		return "启用状态切换失败：外部工具服务未返回具体原因，请刷新工具状态后重试。"
	}
	return "外部工具操作失败。"
}

func readAndRestoreRequestBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	closeErr := r.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func shouldBlockRemoteToolToggle(r *http.Request, target *url.URL, req remoteToolToggleRequest) (uninstallGuardDecision, error) {
	if strings.TrimSpace(req.effectiveToolName()) == "" {
		return uninstallGuardDecision{}, nil
	}
	if isInternalRemoteTool(req.effectiveToolName()) {
		return uninstallGuardDecision{
			block:  true,
			reason: internalToolToggleMessage,
		}, nil
	}
	if !req.Enabled {
		if isRequiredRemoteTool(req.ToolName) {
			return uninstallGuardDecision{
				block:  true,
				reason: requiredToolDisableMessage,
			}, nil
		}
		return uninstallGuardDecision{}, nil
	}

	groups, err := fetchRemoteToolGroups(r, target)
	if err != nil {
		return uninstallGuardDecision{}, err
	}
	return decideRemoteToolEnable(req, groups)
}

func decideRemoteToolEnable(req remoteToolToggleRequest, groups []remoteToolGroupStatus) (uninstallGuardDecision, error) {
	groupName := normalizeRemoteToolName(req.effectiveToolName())
	for _, group := range groups {
		if normalizeRemoteToolName(group.Name) != groupName {
			continue
		}

		installedCount := 0
		pinnedVersion := strings.TrimSpace(group.PinnedVersion)
		pinnedInstalled := pinnedVersion == ""
		for _, tool := range group.Tools {
			if !tool.Installed {
				continue
			}
			installedCount++
			if tool.Version == pinnedVersion {
				pinnedInstalled = true
			}
		}

		if installedCount == 0 {
			return uninstallGuardDecision{
				block:  true,
				reason: enableMissingToolGroupMessage,
			}, nil
		}
		if !pinnedInstalled {
			return uninstallGuardDecision{
				block:  true,
				reason: enablePinnedMissingToolGroupMessage,
			}, nil
		}
		return uninstallGuardDecision{}, nil
	}

	return uninstallGuardDecision{}, fmt.Errorf("\u672a\u627e\u5230\u5de5\u5177\u7ec4 %s", req.effectiveToolName())
}

type uninstallGuardDecision struct {
	block  bool
	reason string
}

func shouldBlockRemoteToolUninstall(r *http.Request, target *url.URL, req remoteToolUninstallRequest) (uninstallGuardDecision, error) {
	if strings.TrimSpace(req.effectiveToolName()) == "" || strings.TrimSpace(req.Version) == "" {
		return uninstallGuardDecision{}, nil
	}

	groups, err := fetchRemoteToolGroups(r, target)
	if err != nil {
		return uninstallGuardDecision{}, err
	}
	return decideRemoteToolUninstall(req, groups)
}

func decideRemoteToolUninstall(req remoteToolUninstallRequest, groups []remoteToolGroupStatus) (uninstallGuardDecision, error) {
	groupName := normalizeRemoteToolName(req.effectiveToolName())
	for _, group := range groups {
		if normalizeRemoteToolName(group.Name) != groupName {
			continue
		}

		installedCount := 0
		targetInstalled := false
		for _, tool := range group.Tools {
			if tool.Installed {
				installedCount++
				if tool.Version == req.Version {
					targetInstalled = true
				}
			}
		}

		if !targetInstalled {
			return uninstallGuardDecision{
				block:  true,
				reason: uninstalledToolVersionMessage,
			}, nil
		}
		if isRequiredRemoteTool(req.effectiveToolName()) && installedCount <= 1 {
			return uninstallGuardDecision{
				block:  true,
				reason: requiredToolLastVersionUninstallMessage,
			}, nil
		}
		return uninstallGuardDecision{}, nil
	}

	return uninstallGuardDecision{}, fmt.Errorf("\u672a\u627e\u5230\u5de5\u5177\u7ec4 %s", req.effectiveToolName())
}

func fetchRemoteToolGroups(r *http.Request, target *url.URL) ([]remoteToolGroupStatus, error) {
	statusURL := *target
	statusURL.Path = strings.TrimRight(statusURL.Path, "/") + "/api/tools"
	statusURL.RawQuery = ""
	statusURL.Fragment = ""

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, statusURL.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RemoteTools 返回状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var groups []remoteToolGroupStatus
	if err := json.Unmarshal(body, &groups); err == nil {
		return groups, nil
	}
	var wrapped struct {
		Value []remoteToolGroupStatus `json:"value"`
		Data  []remoteToolGroupStatus `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	groups = wrapped.Value
	if len(groups) == 0 {
		groups = wrapped.Data
	}
	return groups, nil
}

func isRequiredRemoteTool(toolName string) bool {
	_, ok := requiredRemoteToolNames[normalizeRemoteToolName(toolName)]
	return ok
}

func isInternalRemoteTool(toolName string) bool {
	_, ok := internalRemoteToolNames[normalizeRemoteToolName(toolName)]
	return ok
}

func normalizeRemoteToolName(toolName string) string {
	return strings.ToLower(strings.TrimSpace(toolName))
}

func injectExternalThemeResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/html") || resp.Header.Get("Content-Encoding") != "" {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	body = injectExternalThemeHTML(body)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("ETag")
	return nil
}

func injectExternalThemeHTML(body []byte) []byte {
	if bytes.Contains(body, []byte("bgo-external-theme-bridge")) {
		return body
	}
	lower := bytes.ToLower(body)
	headEnd := bytes.Index(lower, []byte("</head>"))
	if headEnd < 0 {
		return append([]byte(externalThemeBridgeHTML), body...)
	}

	out := make([]byte, 0, len(body)+len(externalThemeBridgeHTML))
	out = append(out, body[:headEnd]...)
	out = append(out, externalThemeBridgeHTML...)
	out = append(out, body[headEnd:]...)
	return out
}

const externalThemeBridgeHTML = `
<script id="bgo-external-theme-bridge">
(function () {
  var KEY = 'bililive_go_local_settings';
  var lightBase = {
    accent: '#0b6bcb', accentHover: '#0759ad', accentSoft: 'rgba(11, 107, 203, 0.12)',
    pageBg: '#f6f7f9', panelBg: '#ffffff', elevatedBg: '#f7f8fa', sidebarBg: '#eef2f6',
    text: '#17202a', muted: '#667085', border: '#d9dee7', borderSoft: '#edf0f4',
    selectedBg: 'rgba(11, 107, 203, 0.12)', selectedText: '#0759ad'
  };
  var darkBase = {
    accent: '#0169cc', accentHover: '#218bff', accentSoft: 'rgba(1, 105, 204, 0.20)',
    pageBg: '#111820', panelBg: '#151b23', elevatedBg: '#1b222c', sidebarBg: '#18212a',
    text: '#eef3f8', muted: '#9da7b3', border: '#2a333d', borderSoft: '#202832',
    selectedBg: 'rgba(255, 255, 255, 0.10)', selectedText: '#ffffff'
  };
  function merge(base, patch) {
    var next = {};
    Object.keys(base).forEach(function (key) { next[key] = base[key]; });
    Object.keys(patch || {}).forEach(function (key) { next[key] = patch[key]; });
    return next;
  }
  function makePalette(lightAccent, lightHover, lightSoft, darkAccent, darkHover, darkSoft, lightPatch, darkPatch) {
    return {
      light: merge(lightBase, Object.assign({ accent: lightAccent, accentHover: lightHover, accentSoft: lightSoft, selectedBg: lightSoft, selectedText: lightHover }, lightPatch || {})),
      dark: merge(darkBase, Object.assign({ accent: darkAccent, accentHover: darkHover, accentSoft: darkSoft, selectedBg: darkSoft, selectedText: darkHover }, darkPatch || {}))
    };
  }
  var palettes = {
    one: makePalette('#4078f2', '#2864d8', 'rgba(64, 120, 242, 0.13)', '#61afef', '#7ec7ff', 'rgba(97, 175, 239, 0.20)', { pageBg: '#fafafa', sidebarBg: '#f0f2f5', text: '#24292f' }, { pageBg: '#282c34', panelBg: '#2c313c', elevatedBg: '#343b48', sidebarBg: '#21252b', text: '#d7dae0', muted: '#abb2bf', border: '#3e4451' }),
    absolutely: makePalette('#b85f43', '#9c4c34', 'rgba(184, 95, 67, 0.14)', '#cc7d5e', '#dc8c69', 'rgba(204, 125, 94, 0.22)', { pageBg: '#f5f1ec', panelBg: '#fffaf5', elevatedBg: '#f6eee7', sidebarBg: '#eee7df', text: '#2d2926', border: '#dfd3c9' }, { pageBg: '#2d2d2b', panelBg: '#353532', elevatedBg: '#3d3d39', sidebarBg: '#242421', text: '#f9f9f7', border: '#4b4944' }),
    ayu: makePalette('#c46f00', '#9f5a00', 'rgba(196, 111, 0, 0.14)', '#ffb454', '#ffd580', 'rgba(255, 180, 84, 0.18)', { pageBg: '#faf7ef', sidebarBg: '#f0eadf', panelBg: '#fffdf7' }, { pageBg: '#111722', panelBg: '#11151c', elevatedBg: '#1b202a', sidebarBg: '#141922', text: '#e6e1cf', border: '#27313f' }),
    catppuccin: makePalette('#8839ef', '#6c2bd9', 'rgba(136, 57, 239, 0.13)', '#cba6f7', '#ddb6ff', 'rgba(203, 166, 247, 0.18)', { pageBg: '#eff1f5', sidebarBg: '#e6e9ef', text: '#4c4f69' }, { pageBg: '#1e1e2e', panelBg: '#181825', elevatedBg: '#252538', sidebarBg: '#181825', text: '#cdd6f4', border: '#313244' }),
    codex: makePalette('#0b6bcb', '#0759ad', 'rgba(11, 107, 203, 0.12)', '#0169cc', '#218bff', 'rgba(1, 105, 204, 0.20)', { pageBg: '#f6f7f9', sidebarBg: '#eef2f5', text: '#17202a' }, darkBase),
    dracula: makePalette('#7c3aed', '#6d28d9', 'rgba(124, 58, 237, 0.13)', '#bd93f9', '#d6acff', 'rgba(189, 147, 249, 0.20)', { pageBg: '#f8f7ff', sidebarBg: '#eeebff', text: '#282a36' }, { pageBg: '#282a36', panelBg: '#21222c', elevatedBg: '#343746', sidebarBg: '#21222c', text: '#f8f8f2', border: '#44475a' }),
    everforest: makePalette('#6c8f43', '#557136', 'rgba(108, 143, 67, 0.14)', '#a7c080', '#b9d18f', 'rgba(167, 192, 128, 0.18)', { pageBg: '#f4f0d9', panelBg: '#fffbea', sidebarBg: '#e8e0bf', text: '#3c4841', border: '#d8cfad' }, { pageBg: '#2d353b', panelBg: '#343f44', elevatedBg: '#3d484d', sidebarBg: '#263238', text: '#d3c6aa', border: '#4f585e' }),
    github: makePalette('#0969da', '#0756b6', 'rgba(9, 105, 218, 0.12)', '#1f6feb', '#388bfd', 'rgba(31, 111, 235, 0.20)', { pageBg: '#f6f8fa', sidebarBg: '#f0f3f6', border: '#d0d7de' }, { pageBg: '#0d1117', panelBg: '#161b22', elevatedBg: '#21262d', sidebarBg: '#161b22', border: '#30363d' }),
    gruvbox: makePalette('#af6f00', '#8f5900', 'rgba(175, 111, 0, 0.15)', '#fabd2f', '#ffd75f', 'rgba(250, 189, 47, 0.18)', { pageBg: '#fbf1c7', panelBg: '#fff7d7', sidebarBg: '#ebdbb2', text: '#3c3836', border: '#d5c4a1' }, { pageBg: '#282828', panelBg: '#32302f', elevatedBg: '#3c3836', sidebarBg: '#1d2021', text: '#ebdbb2', border: '#504945' }),
    linear: makePalette('#5e6ad2', '#4f5bbc', 'rgba(94, 106, 210, 0.13)', '#5e6ad2', '#7c86e8', 'rgba(94, 106, 210, 0.20)', { pageBg: '#f7f7f8', sidebarBg: '#f0f0f2', text: '#1f2023' }, { pageBg: '#121416', panelBg: '#17191d', elevatedBg: '#1c1f25', sidebarBg: '#15171b', text: '#f7f8f8', border: '#2a2d33' }),
    lobster: makePalette('#c23a2f', '#9f2f27', 'rgba(194, 58, 47, 0.14)', '#ff7a70', '#ff9a92', 'rgba(255, 122, 112, 0.20)', { pageBg: '#fff4f1', panelBg: '#fffaf8', sidebarBg: '#f8e4df', text: '#35211f', border: '#ead0ca' }, { pageBg: '#2b1e22', panelBg: '#33242a', elevatedBg: '#3b2a31', sidebarBg: '#251a1f', text: '#ffece7', border: '#563a42' }),
    material: makePalette('#1976d2', '#115293', 'rgba(25, 118, 210, 0.13)', '#80cbc4', '#a7ffeb', 'rgba(128, 203, 196, 0.20)', { pageBg: '#f6f8fb', sidebarBg: '#edf2f7', text: '#263238' }, { pageBg: '#263238', panelBg: '#2f3d46', elevatedBg: '#37474f', sidebarBg: '#202b31', text: '#eeffff', border: '#455a64' }),
    matrix: makePalette('#0f8f4a', '#0b6f39', 'rgba(15, 143, 74, 0.14)', '#00d26a', '#50fa7b', 'rgba(0, 210, 106, 0.18)', { pageBg: '#f1fbf4', sidebarBg: '#e1f3e8', text: '#14351f', border: '#c7e6d2' }, { pageBg: '#122116', panelBg: '#16291c', elevatedBg: '#1c3323', sidebarBg: '#101d14', text: '#d7ffe4', border: '#2a4c35' }),
    monokai: makePalette('#6f8f00', '#536d00', 'rgba(111, 143, 0, 0.16)', '#a6e22e', '#c2ff55', 'rgba(166, 226, 46, 0.18)', { pageBg: '#f7f4ea', panelBg: '#fffaf0', sidebarBg: '#ece5d6', text: '#272822', border: '#ddd4c0' }, { pageBg: '#272822', panelBg: '#303127', elevatedBg: '#383a2e', sidebarBg: '#20211c', text: '#f8f8f2', border: '#49483e' }),
    'night-owl': makePalette('#3268d8', '#2450ad', 'rgba(50, 104, 216, 0.14)', '#82aaff', '#b4ccff', 'rgba(130, 170, 255, 0.20)', { pageBg: '#eef5ff', sidebarBg: '#e1ecfb', text: '#16243d', border: '#cbd9ef' }, { pageBg: '#101a2a', panelBg: '#12213a', elevatedBg: '#172a46', sidebarBg: '#0e1726', text: '#d6deeb', border: '#263c5a' }),
    nord: makePalette('#4c7a92', '#3b6479', 'rgba(76, 122, 146, 0.14)', '#88c0d0', '#a3d7e6', 'rgba(136, 192, 208, 0.20)', { pageBg: '#eceff4', sidebarBg: '#e5e9f0', text: '#2e3440', border: '#d8dee9' }, { pageBg: '#2e3440', panelBg: '#343b49', elevatedBg: '#3b4252', sidebarBg: '#252b35', text: '#eceff4', border: '#4c566a' }),
    notion: makePalette('#2f3437', '#1f2326', 'rgba(47, 52, 55, 0.12)', '#e9e5dc', '#ffffff', 'rgba(233, 229, 220, 0.16)', { pageBg: '#f7f6f3', sidebarBg: '#eeeae4', text: '#2f3437', border: '#ded9d1' }, { pageBg: '#191919', panelBg: '#202020', elevatedBg: '#2a2a2a', sidebarBg: '#202020', text: '#f1f1ef', border: '#373737' }),
    oscurance: makePalette('#7357d8', '#5b43b0', 'rgba(115, 87, 216, 0.14)', '#b4a7ff', '#d2caff', 'rgba(180, 167, 255, 0.20)', { pageBg: '#f5f3ff', sidebarBg: '#ece8ff', text: '#27213f', border: '#d8d0f5' }, { pageBg: '#1c1b2e', panelBg: '#23223a', elevatedBg: '#2c2a45', sidebarBg: '#171629', text: '#efeaff', border: '#3d395f' }),
    raycast: makePalette('#e5484d', '#c7353b', 'rgba(229, 72, 77, 0.14)', '#ff6363', '#ff8a8a', 'rgba(255, 99, 99, 0.20)', { pageBg: '#fff5f5', sidebarBg: '#ffe7e7', text: '#311c1f', border: '#f0caca' }, { pageBg: '#23191a', panelBg: '#2d2021', elevatedBg: '#382829', sidebarBg: '#1e1516', text: '#fff1f1', border: '#523334' }),
    'rose-pine': makePalette('#d7827e', '#b4637a', 'rgba(215, 130, 126, 0.15)', '#eb6f92', '#f6a1b6', 'rgba(235, 111, 146, 0.20)', { pageBg: '#faf4ed', panelBg: '#fffaf6', sidebarBg: '#f2e9de', text: '#575279', border: '#dfdad9' }, { pageBg: '#191724', panelBg: '#1f1d2e', elevatedBg: '#26233a', sidebarBg: '#171521', text: '#e0def4', border: '#403d52' }),
    sentry: makePalette('#5f4bb6', '#4c3b91', 'rgba(95, 75, 182, 0.14)', '#c59cff', '#d8baff', 'rgba(197, 156, 255, 0.20)', { pageBg: '#f8f4ff', sidebarBg: '#eee7fb', text: '#30263f', border: '#dccff0' }, { pageBg: '#241b2f', panelBg: '#2b2138', elevatedBg: '#352947', sidebarBg: '#201729', text: '#f5edff', border: '#49385f' }),
    solarized: makePalette('#268bd2', '#1f6f9f', 'rgba(38, 139, 210, 0.14)', '#2aa198', '#55d6c2', 'rgba(42, 161, 152, 0.20)', { pageBg: '#fdf6e3', panelBg: '#fffaf0', sidebarBg: '#eee8d5', text: '#586e75', border: '#d8cfb0' }, { pageBg: '#073642', panelBg: '#0b404d', elevatedBg: '#124b59', sidebarBg: '#002b36', text: '#eee8d5', border: '#2f5d68' }),
    temple: makePalette('#a66c1f', '#805218', 'rgba(166, 108, 31, 0.15)', '#f6c177', '#ffd59a', 'rgba(246, 193, 119, 0.20)', { pageBg: '#f7f1df', panelBg: '#fff9ea', sidebarBg: '#ebe0c3', text: '#342d21', border: '#d9c9a5' }, { pageBg: '#24201a', panelBg: '#2d281f', elevatedBg: '#383124', sidebarBg: '#1f1b16', text: '#f6ead0', border: '#51442f' }),
    'tokyo-night': makePalette('#3d68d8', '#2d50aa', 'rgba(61, 104, 216, 0.14)', '#7aa2f7', '#9ab8ff', 'rgba(122, 162, 247, 0.20)', { pageBg: '#f1f5ff', sidebarBg: '#e5ebfa', text: '#1f2335', border: '#cfd8ee' }, { pageBg: '#1a1b26', panelBg: '#202331', elevatedBg: '#292e42', sidebarBg: '#16161e', text: '#c0caf5', border: '#3b4261' }),
    vercel: makePalette('#111827', '#0f172a', 'rgba(17, 24, 39, 0.12)', '#f5f5f5', '#ffffff', 'rgba(245, 245, 245, 0.14)', { pageBg: '#fafafa', sidebarBg: '#f0f0f0', text: '#111827', border: '#dedede' }, { pageBg: '#171717', panelBg: '#1f1f1f', elevatedBg: '#292929', sidebarBg: '#202020', text: '#f5f5f5', border: '#3f3f3f' }),
    'vs-code-plus': makePalette('#007acc', '#0067ad', 'rgba(0, 122, 204, 0.13)', '#007acc', '#2499e8', 'rgba(0, 122, 204, 0.22)', { pageBg: '#f3f3f3', sidebarBg: '#ebebeb', text: '#1f1f1f', border: '#d4d4d4' }, { pageBg: '#1e1e1e', panelBg: '#252526', elevatedBg: '#2d2d30', sidebarBg: '#252526', text: '#cccccc', border: '#3c3c3c' }),
    xcode: makePalette('#007aff', '#005ecb', 'rgba(0, 122, 255, 0.13)', '#0a84ff', '#5eb1ff', 'rgba(10, 132, 255, 0.22)', { pageBg: '#f7fbff', sidebarBg: '#edf5ff', text: '#1d1d1f', border: '#d5e3f5' }, { pageBg: '#242833', panelBg: '#2b303d', elevatedBg: '#343b4c', sidebarBg: '#20242e', text: '#f2f2f7', border: '#465066' })
  };
  function installToolsApiPathRewrite() {
    if (window.__bgoToolsApiPathRewriteInstalled) return;
    var path = location.pathname || '';
    if (path.indexOf('/tools') !== 0) return;
    window.__bgoToolsApiPathRewriteInstalled = true;

    function rewriteURL(input) {
      if (typeof input !== 'string' || !input) return input;
      if (input === '/api' || input.indexOf('/api/') === 0) return '/tools' + input;
      try {
        var url = new URL(input, location.href);
        if (url.origin === location.origin && (url.pathname === '/api' || url.pathname.indexOf('/api/') === 0)) {
          url.pathname = '/tools' + url.pathname;
          return url.toString();
        }
      } catch (err) {}
      return input;
    }

    var nativeFetch = window.fetch;
    if (typeof nativeFetch === 'function') {
      window.fetch = function (input, init) {
        if (typeof input === 'string') {
          input = rewriteURL(input);
        } else if (typeof Request !== 'undefined' && input instanceof Request) {
          var rewritten = rewriteURL(input.url);
          if (rewritten !== input.url) input = new Request(rewritten, input);
        }
        var requestURL = typeof input === 'string' ? input : (input && input.url || '');
        return nativeFetch.call(this, input, init).then(function (resp) {
          if (resp && !resp.ok && /\/api\/(?:uninstall|toggle)(?:[?#]|$)/.test(String(requestURL))) {
            resp.clone().json().then(function (body) {
              var reason = body && (body.err_msg || body.message || (body.data && body.data.reason));
              if (reason) showToolGuardNotice(reason, 'error');
            }).catch(function () {});
          }
          return resp;
        });
      };
    }

    if (window.XMLHttpRequest && window.XMLHttpRequest.prototype && window.XMLHttpRequest.prototype.open) {
      var nativeOpen = window.XMLHttpRequest.prototype.open;
      var nativeSend = window.XMLHttpRequest.prototype.send;
      window.XMLHttpRequest.prototype.open = function (method, url) {
        arguments[1] = rewriteURL(url);
        this.__bgoRequestURL = arguments[1];
        return nativeOpen.apply(this, arguments);
      };
      window.XMLHttpRequest.prototype.send = function () {
        this.addEventListener('loadend', function () {
          if (this.status >= 400 && /\/api\/(?:uninstall|toggle)(?:[?#]|$)/.test(String(this.__bgoRequestURL || ''))) {
            try {
              var body = JSON.parse(this.responseText || '{}');
              var reason = body && (body.err_msg || body.message || (body.data && body.data.reason));
              if (reason) showToolGuardNotice(reason, 'error');
            } catch (err) {}
          }
        });
        return nativeSend.apply(this, arguments);
      };
    }

    if (typeof window.EventSource === 'function') {
      var NativeEventSource = window.EventSource;
      window.EventSource = function (url, config) {
        return new NativeEventSource(rewriteURL(url), config);
      };
      window.EventSource.prototype = NativeEventSource.prototype;
    }
  }
  installToolsApiPathRewrite();
  function readSettings() {
    try { return JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch (err) { return {}; }
  }
  function resolveMode(mode) {
    return mode === 'dark' || (mode === 'system' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light';
  }
  function applyTheme() {
    var settings = readSettings();
    var mode = ['system', 'light', 'dark'].indexOf(settings.themeMode) >= 0 ? settings.themeMode : 'system';
    var resolved = resolveMode(mode);
    var palette = palettes[settings.themePalette] || palettes.one;
    var colors = palette[resolved] || palettes.one[resolved];
    var root = document.documentElement;
    var externalColors = {
      pageBg: colors.pageBg,
      panelBg: colors.panelBg,
      elevatedBg: colors.elevatedBg,
      headerBg: colors.elevatedBg,
      text: colors.text,
      muted: colors.muted,
      subtleText: colors.muted,
      border: colors.border,
      borderSoft: colors.borderSoft,
      inputBg: resolved === 'dark' ? colors.elevatedBg : colors.panelBg,
      buttonBg: colors.elevatedBg
    };
    root.classList.add('bgo-external-theme');
    root.dataset.bgoTheme = resolved;
    root.dataset.bgoThemeMode = mode;
    root.dataset.bgoPalette = settings.themePalette || 'one';
    Object.keys(colors).forEach(function (key) {
      root.style.setProperty('--bgo-' + key.replace(/[A-Z]/g, function (m) { return '-' + m.toLowerCase(); }), colors[key]);
    });
    Object.keys(externalColors).forEach(function (key) {
      root.style.setProperty('--bgo-ext-' + key.replace(/[A-Z]/g, function (m) { return '-' + m.toLowerCase(); }), externalColors[key]);
    });
  }
  applyTheme();
  window.addEventListener('storage', function (event) {
    if (event.key === KEY) applyTheme();
  });
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
  }
  function installLateSwitchStyle() {
    var old = document.getElementById('bgo-external-switch-style-late');
    if (old) old.remove();
    var style = document.createElement('style');
    style.id = 'bgo-external-switch-style-late';
    style.textContent = [
      'html.bgo-external-theme[data-bgo-theme="dark"] :where(.bg-white,.bg-gray-50,.bg-gray-100,.bg-slate-50,.bg-slate-100,.bg-zinc-50,.bg-zinc-100,.bg-neutral-50,.bg-neutral-100,.bg-card,.bg-background,[class*="bg-white"],[class*="bg-gray-50"],[class*="bg-gray-100"],[class*="bg-slate-50"],[class*="bg-slate-100"],[class*="bg-zinc-50"],[class*="bg-zinc-100"],[class*="bg-neutral-50"],[class*="bg-neutral-100"],[class*="bg-card"],[class*="bg-background"]){background-color:var(--bgo-ext-panel-bg,var(--bgo-panel-bg))!important;border-color:var(--bgo-ext-border,var(--bgo-border))!important;color:var(--bgo-ext-text,var(--bgo-text))!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] :where(.bg-gray-200,.bg-slate-200,.bg-zinc-200,.bg-neutral-200,.bg-secondary,.bg-muted,[class*="bg-gray-200"],[class*="bg-slate-200"],[class*="bg-zinc-200"],[class*="bg-neutral-200"],[class*="bg-secondary"],[class*="bg-muted"]){background-color:color-mix(in srgb,var(--bgo-ext-panel-bg,var(--bgo-panel-bg)) 82%,var(--bgo-ext-page-bg,var(--bgo-page-bg)))!important;border-color:var(--bgo-ext-border,var(--bgo-border))!important;color:var(--bgo-ext-text,var(--bgo-text))!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] input,html.bgo-external-theme[data-bgo-theme="dark"] textarea,html.bgo-external-theme[data-bgo-theme="dark"] select,html.bgo-external-theme[data-bgo-theme="dark"] .ant-input,html.bgo-external-theme[data-bgo-theme="dark"] .ant-input-number,html.bgo-external-theme[data-bgo-theme="dark"] .ant-select-selector,html.bgo-external-theme[data-bgo-theme="dark"] .ant-picker{background-color:color-mix(in srgb,var(--bgo-ext-panel-bg,var(--bgo-panel-bg)) 78%,var(--bgo-ext-page-bg,var(--bgo-page-bg)))!important;border-color:var(--bgo-ext-border,var(--bgo-border))!important;color:var(--bgo-ext-text,var(--bgo-text))!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-text-fix="muted"]{color:var(--bgo-ext-muted,var(--bgo-muted))!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status]{display:inline-flex!important;align-items:center!important;min-height:22px!important;padding:2px 8px!important;border-radius:6px!important;border:1px solid transparent!important;font-size:13px!important;font-weight:700!important;line-height:1.25!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="enabled"],html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="installed"]{color:#7ee787!important;background:rgba(52,199,89,.13)!important;border-color:rgba(52,199,89,.32)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="missing"]{color:#d7c48d!important;background:rgba(215,196,141,.14)!important;border-color:rgba(215,196,141,.34)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="failed"]{color:#ff9aa2!important;background:rgba(255,119,125,.14)!important;border-color:rgba(255,119,125,.34)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="downloading"]{color:#bfdbfe!important;background:rgba(96,165,250,.15)!important;border-color:rgba(96,165,250,.34)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="paused"]{color:#d2a8ff!important;background:rgba(210,168,255,.14)!important;border-color:rgba(210,168,255,.32)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="internal"]{color:#d2a8ff!important;background:rgba(210,168,255,.14)!important;border-color:rgba(210,168,255,.32)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="disabled"]{color:#c7d0da!important;background:rgba(148,163,184,.15)!important;border-color:rgba(148,163,184,.32)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="platform"]{color:#c7d2fe!important;background:rgba(129,140,248,.16)!important;border-color:rgba(129,140,248,.34)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="default-version"],html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="version"]{color:#bfdbfe!important;background:rgba(96,165,250,.15)!important;border-color:rgba(96,165,250,.34)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="empty"]{color:var(--bgo-ext-muted,var(--bgo-muted))!important;background:rgba(148,163,184,.10)!important;border-color:rgba(148,163,184,.22)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] .text-green-500,html.bgo-external-theme[data-bgo-theme="dark"] .text-green-600,html.bgo-external-theme[data-bgo-theme="dark"] .text-emerald-500,html.bgo-external-theme[data-bgo-theme="dark"] .text-emerald-600,html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-green-"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-emerald-"]{color:#7ee787!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] .text-red-500,html.bgo-external-theme[data-bgo-theme="dark"] .text-red-600,html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-red-"]{color:#ff9b9b!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] .text-blue-500,html.bgo-external-theme[data-bgo-theme="dark"] .text-blue-600,html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-blue-"]{color:#8fc7ff!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] .text-gray-700,html.bgo-external-theme[data-bgo-theme="dark"] .text-gray-600,html.bgo-external-theme[data-bgo-theme="dark"] .text-gray-500,html.bgo-external-theme[data-bgo-theme="dark"] .text-gray-400,html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-gray-700"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-gray-600"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-gray-500"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="text-gray-400"],html.bgo-external-theme[data-bgo-theme="dark"] .muted,html.bgo-external-theme[data-bgo-theme="dark"] .subtext,html.bgo-external-theme[data-bgo-theme="dark"] .description{color:var(--bgo-ext-muted,var(--bgo-muted))!important;}',
      'html.bgo-external-theme button[role="switch"],html.bgo-external-theme .ant-switch,html.bgo-external-theme .rc-switch,html.bgo-external-theme [data-slot="switch"],html.bgo-external-theme [class*="SwitchRoot"]{position:relative!important;width:44px!important;min-width:44px!important;height:26px!important;padding:0!important;border:0!important;border-radius:999px!important;background:#e9e9e9!important;box-shadow:inset 0 0 0 1px rgba(0,0,0,.04)!important;transition:.3s all ease-in-out!important;overflow:hidden!important;}',
      'html.bgo-external-theme button[role="switch"][aria-checked="true"],html.bgo-external-theme button[role="switch"][data-state="checked"],html.bgo-external-theme .ant-switch.ant-switch-checked,html.bgo-external-theme .rc-switch.rc-switch-checked,html.bgo-external-theme [data-slot="switch"][data-state="checked"],html.bgo-external-theme [class*="SwitchRoot"][data-state="checked"]{background:#34c759!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"],html.bgo-external-theme[data-bgo-theme="dark"] .ant-switch,html.bgo-external-theme[data-bgo-theme="dark"] .rc-switch,html.bgo-external-theme[data-bgo-theme="dark"] [data-slot="switch"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="SwitchRoot"]{background:#39393d!important;box-shadow:inset 0 0 0 1px rgba(255,255,255,.06)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"][aria-checked="true"],html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"][data-state="checked"],html.bgo-external-theme[data-bgo-theme="dark"] .ant-switch.ant-switch-checked,html.bgo-external-theme[data-bgo-theme="dark"] .rc-switch.rc-switch-checked,html.bgo-external-theme[data-bgo-theme="dark"] [data-slot="switch"][data-state="checked"],html.bgo-external-theme[data-bgo-theme="dark"] [class*="SwitchRoot"][data-state="checked"]{background:#30d158!important;}',
      'html.bgo-external-theme button[role="switch"]>.ant-switch-inner,html.bgo-external-theme button[role="switch"]>.rc-switch-inner,html.bgo-external-theme .ant-switch-inner,html.bgo-external-theme .rc-switch-inner{background:transparent!important;}',
      'html.bgo-external-theme .ant-switch .ant-switch-handle,html.bgo-external-theme .rc-switch .rc-switch-handle,html.bgo-external-theme button[role="switch"]>.ant-switch-handle,html.bgo-external-theme button[role="switch"]>.rc-switch-handle{position:absolute!important;top:2px!important;left:2px!important;inset-inline-start:2px!important;width:22px!important;height:22px!important;border-radius:999px!important;transform:translateX(0)!important;transition:.3s all ease-in-out!important;}',
      'html.bgo-external-theme .ant-switch .ant-switch-handle:before,html.bgo-external-theme .rc-switch .rc-switch-handle:before,html.bgo-external-theme button[role="switch"]>.ant-switch-handle:before,html.bgo-external-theme button[role="switch"]>.rc-switch-handle:before{position:absolute!important;inset:0!important;background:#fff!important;border-radius:999px!important;box-shadow:2px 0 8px rgba(0,0,0,.16)!important;transition:.3s all ease-in-out!important;content:""!important;}',
      'html.bgo-external-theme .ant-switch.ant-switch-checked .ant-switch-handle,html.bgo-external-theme .rc-switch.rc-switch-checked .rc-switch-handle,html.bgo-external-theme button[role="switch"][aria-checked="true"]>.ant-switch-handle,html.bgo-external-theme button[role="switch"][aria-checked="true"]>.rc-switch-handle,html.bgo-external-theme button[role="switch"][data-state="checked"]>.ant-switch-handle,html.bgo-external-theme button[role="switch"][data-state="checked"]>.rc-switch-handle{transform:translateX(18px)!important;}',
      'html.bgo-external-theme .ant-switch.ant-switch-checked .ant-switch-handle:before,html.bgo-external-theme .rc-switch.rc-switch-checked .rc-switch-handle:before{box-shadow:-2px 0 8px rgba(0,0,0,.16)!important;}',
      'html.bgo-external-theme .ant-switch:active .ant-switch-handle,html.bgo-external-theme .rc-switch:active .rc-switch-handle,html.bgo-external-theme button[role="switch"]:active>.ant-switch-handle,html.bgo-external-theme button[role="switch"]:active>.rc-switch-handle{width:28px!important;}',
      'html.bgo-external-theme .ant-switch.ant-switch-checked:active .ant-switch-handle,html.bgo-external-theme .rc-switch.rc-switch-checked:active .rc-switch-handle,html.bgo-external-theme button[role="switch"][aria-checked="true"]:active>.ant-switch-handle,html.bgo-external-theme button[role="switch"][aria-checked="true"]:active>.rc-switch-handle,html.bgo-external-theme button[role="switch"][data-state="checked"]:active>.ant-switch-handle,html.bgo-external-theme button[role="switch"][data-state="checked"]:active>.rc-switch-handle{transform:translateX(12px)!important;}'
    ].join('\n');
    document.head.appendChild(style);
  }
  function parseRGB(color) {
    var match = String(color || '').match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/i);
    if (!match) return null;
    return [Number(match[1]), Number(match[2]), Number(match[3])];
  }
  function isTooDark(color) {
    var rgb = parseRGB(color);
    if (!rgb) return false;
    return (rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722) < 92;
  }
  var bgoVersionPattern = /^(?:[a-z]+-?v?|v)?\d+(?:\.\d+)+(?:[-._a-zA-Z0-9]+)?$/;
  function isToolVersionText(text) {
    return bgoVersionPattern.test(String(text || '').trim());
  }
  function compactToolVersionCompare(a, b) {
    return String(b || '').localeCompare(String(a || ''), undefined, { numeric: true, sensitivity: 'base' });
  }
  var bgoToolVersionMeta = {};
  var bgoToolVersionMetaLoaded = false;
  var bgoToolVersionMetaLoading = false;
  var bgoToolVersionMetaLastLoadedAt = 0;
  var bgoRequiredToolUninstallMessage = '\u8fd9\u662f\u5fc5\u9700\u5de5\u5177\uff0c\u81f3\u5c11\u8981\u4fdd\u7559\u4e00\u4e2a\u5df2\u5b89\u88c5\u7248\u672c\uff0c\u4e0d\u80fd\u5378\u8f7d\u6240\u6709\u7248\u672c\uff1b\u5982\u9700\u5207\u6362\uff0c\u5148\u5b89\u88c5\u5176\u4ed6\u7248\u672c\u518d\u5378\u8f7d\u5f53\u524d\u7248\u672c\u3002';
  var bgoRequiredToolDisableMessage = '\u8fd9\u662f\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5\u5de5\u5177\uff0c\u5f55\u5236\u548c\u76f8\u5173\u529f\u80fd\u4f9d\u8d56\u5b83\u4fdd\u6301\u542f\u7528\uff0c\u4e0d\u80fd\u505c\u7528\u3002';
  var bgoToolGuardNoticeTimer = 0;
  function showToolGuardNotice(text, type) {
    text = String(text || '').trim();
    if (!text) return;
    var notice = document.querySelector('.bgo-tool-guard-notice');
    if (!notice) {
      notice = document.createElement('div');
      notice.className = 'bgo-tool-guard-notice';
      notice.setAttribute('role', 'alert');
      document.body.appendChild(notice);
    }
    notice.dataset.bgoNoticeType = type || 'warning';
    notice.textContent = text;
    notice.dataset.bgoVisible = 'true';
    if (bgoToolGuardNoticeTimer) window.clearTimeout(bgoToolGuardNoticeTimer);
    bgoToolGuardNoticeTimer = window.setTimeout(function () {
      notice.dataset.bgoVisible = 'false';
    }, 7000);
  }
  var bgoToolInfoMap = {
    'ffmpeg': {
      role: 'required',
      roleLabel: '\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5',
      title: '\u6838\u5fc3\u5f55\u5236\u7ec4\u4ef6',
      description: '\u7528\u4e8e\u5f55\u5236\u3001\u5c01\u88c5\u3001\u8f6c\u7801\u3001\u622a\u56fe\u548c\u70e7\u5f55\u5b57\u5e55\u3002\u7f3a\u5931\u65f6 FFmpeg \u5f55\u5236\u3001WebRTC/HLS \u8f6c\u5c01\u88c5\u548c\u591a\u6570\u540e\u5904\u7406\u4f1a\u4e0d\u53ef\u7528\uff0c\u7a0b\u5e8f\u4f1a\u81ea\u52a8\u51c6\u5907\u3002'
    },
    'dotnet': {
      role: 'required',
      roleLabel: '\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5',
      title: '.NET \u8fd0\u884c\u65f6',
      description: 'BililiveRecorder \u4e0e FLV \u4fee\u590d\u4f9d\u8d56\u7684\u8fd0\u884c\u73af\u5883\u3002\u901a\u5e38\u968f\u9ed8\u8ba4\u5de5\u5177\u4e00\u8d77\u51c6\u5907\uff0c\u4e0d\u9700\u8981\u5355\u72ec\u64cd\u4f5c\u3002'
    },
    'bililive-tools': {
      role: 'required',
      roleLabel: '\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5',
      title: '\u5e73\u53f0\u89e3\u6790\u670d\u52a1',
      description: '\u90e8\u5206\u5e73\u53f0\u89e3\u6790\u3001\u8f85\u52a9\u80fd\u529b\u548c\u5de5\u5177\u670d\u52a1\u4f9d\u8d56\u5b83\u8fd0\u884c\uff1b\u7531\u7a0b\u5e8f\u81ea\u52a8\u542f\u52a8\u3002\u4f9d\u8d56 Node\u3002'
    },
    'node': {
      role: 'required',
      roleLabel: '\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5',
      title: 'Node \u8fd0\u884c\u65f6',
      description: 'biliLive-tools \u7684\u8fd0\u884c\u73af\u5883\u3002\u9ed8\u8ba4\u51c6\u5907\uff0c\u901a\u5e38\u4e0d\u9700\u8981\u624b\u52a8\u5207\u6362\u7248\u672c\u3002'
    },
    'bililive-scheduler': {
      role: 'required',
      roleLabel: '\u5fc5\u9700 / \u9ed8\u8ba4\u5b89\u88c5',
      title: '\u8c03\u5ea6\u5668\u670d\u52a1',
      description: '\u7528\u4e8e\u8c03\u5ea6\u5668\u9875\u9762\u3001\u623f\u95f4\u72b6\u6001\u5237\u65b0\u6392\u961f\u548c\u8bf7\u6c42\u8282\u6d41\u3002\u9ed8\u8ba4\u51c6\u5907\u5e76\u7531\u7a0b\u5e8f\u542f\u52a8\u3002'
    },
    'bililive-recorder': {
      role: 'recommended',
      roleLabel: '\u63a8\u8350\u5b89\u88c5',
      title: '\u5f55\u64ad\u59ec\u6838\u5fc3\u5e93',
      description: '\u7528\u4e8e FLV \u4fee\u590d\u7b49\u540e\u5904\u7406\u80fd\u529b\u3002\u9ed8\u8ba4\u4f1a\u5c1d\u8bd5\u51c6\u5907\uff0c\u53ea\u6709\u76f8\u5173\u4fee\u590d\u6d41\u7a0b\u4f1a\u5b9e\u9645\u8c03\u7528\u3002'
    },
    'bililive-recorder-cli': {
      role: 'recommended',
      roleLabel: '\u63a8\u8350\u5b89\u88c5',
      title: '\u5f55\u64ad\u59ec\u4e0b\u8f7d\u5668',
      description: '\u9009\u62e9 BililiveRecorder \u4e0b\u8f7d\u5668\u65f6\u9700\u8981\uff0c\u540c\u65f6\u4f9d\u8d56 dotnet\u3002\u4e0d\u5f00\u542f\u8be5\u4e0b\u8f7d\u5668\u65f6\u4e0d\u662f\u5fc5\u9700\u3002'
    },
    'chromium': {
      role: 'optional',
      roleLabel: '\u53ef\u9009\u5b89\u88c5',
      title: '\u6d4f\u89c8\u5668\u5f55\u5236\u4f9d\u8d56',
      description: '\u4ec5\u5728\u9700\u8981\u6d4f\u89c8\u5668\u5f55\u5236\u3001\u767b\u5f55\u6001\u6293\u53d6\u6216\u9875\u9762\u81ea\u52a8\u5316\u80fd\u529b\u65f6\u5b89\u88c5\u3002\u666e\u901a FFmpeg/\u539f\u751f\u5f55\u5236\u4e0d\u9700\u8981\u3002'
    },
    'openlist': {
      role: 'optional',
      roleLabel: '\u53ef\u9009\u5b89\u88c5',
      title: '\u4e91\u76d8\u4e0a\u4f20\u670d\u52a1',
      description: '\u53ea\u5728\u4f7f\u7528\u5185\u7f6e OpenList \u505a\u4e91\u76d8\u4e0a\u4f20\u65f6\u9700\u8981\u3002\u82e5\u914d\u7f6e\u4e86\u5916\u90e8 OpenList\uff0c\u53ef\u4e0d\u5b89\u88c5\u5185\u90e8\u7248\u672c\u3002'
    },
    'klive': {
      role: 'internal',
      roleLabel: '\u5185\u90e8\u7ec4\u4ef6',
      title: '\u8fdc\u7a0b\u8bbf\u95ee\u7ec4\u4ef6',
      description: '\u7531 BiliLive-go \u81ea\u52a8\u7ba1\u7406\u7684\u8fdc\u7a0b\u8bbf\u95ee/\u96a7\u9053\u80fd\u529b\u3002\u5f53\u524d\u4e0d\u63d0\u4f9b\u53ef\u624b\u52a8\u5b89\u88c5\u7248\u672c\uff0c\u4e0d\u9700\u8981\u5728\u8fd9\u91cc\u5378\u8f7d\u6216\u5207\u6362\u3002'
    },
    'bililive-go': {
      role: 'internal',
      roleLabel: '\u5185\u90e8\u7ec4\u4ef6',
      title: '\u7a0b\u5e8f\u66f4\u65b0\u7f13\u5b58',
      description: '\u8fd9\u662f\u81ea\u52a8\u66f4\u65b0\u7cfb\u7edf\u6ce8\u5165\u7684\u5185\u90e8\u6761\u76ee\uff0c\u4e0d\u4ee3\u8868\u5f53\u524d\u8fd0\u884c\u7248\u672c\u3002\u5df2\u6e05\u7a7a\u65f6\u4f1a\u5728\u68c0\u6d4b\u5230\u65b0\u7248\u672c\u540e\u81ea\u52a8\u751f\u6210\uff0c\u4e0d\u9700\u8981\u624b\u52a8\u5b89\u88c5\u6216\u5378\u8f7d\u3002'
    }
  };
  var bgoToolLegendItems = [
    { role: 'required', label: '\u5fc5\u9700/\u9ed8\u8ba4', text: '\u5fc5\u9700/\u9ed8\u8ba4\u5b89\u88c5\uff1a\u6838\u5fc3\u5f55\u5236\u6216\u9ed8\u8ba4\u670d\u52a1\u4f1a\u81ea\u52a8\u51c6\u5907\u3002' },
    { role: 'recommended', label: '\u63a8\u8350', text: '\u63a8\u8350\u5b89\u88c5\uff1a\u5f00\u542f\u5bf9\u5e94\u4e0b\u8f7d\u5668\u3001\u4fee\u590d\u6216\u5e73\u53f0\u80fd\u529b\u65f6\u5efa\u8bae\u51c6\u5907\u3002' },
    { role: 'optional', label: '\u53ef\u9009', text: '\u53ef\u9009\u5b89\u88c5\uff1a\u53ea\u6709\u542f\u7528\u7279\u5b9a\u529f\u80fd\u65f6\u624d\u9700\u8981\u3002' },
    { role: 'internal', label: '\u5185\u90e8', text: '\u5185\u90e8\u7ec4\u4ef6\uff1a\u7cfb\u7edf\u7f13\u5b58\u6216\u5185\u90e8\u80fd\u529b\uff0c\u4e0d\u5efa\u8bae\u624b\u52a8\u64cd\u4f5c\u3002' }
  ];
  function normalizeToolName(name) {
    return String(name || '').trim().toLowerCase();
  }
  function getToolInfo(groupName) {
    return bgoToolInfoMap[normalizeToolName(groupName)] || null;
  }
  function normalizeVersionList(versions) {
    return Array.prototype.slice.call(versions || []).filter(Boolean).sort(compactToolVersionCompare);
  }
  function getToolInstallState(tool) {
    var status = tool && tool.downloadProcess && tool.downloadProcess.status;
    if (tool && tool.installed) return { key: 'installed', label: '\u5df2\u5b89\u88c5' };
    if (status === 'failed') return { key: 'failed', label: '\u5b89\u88c5\u5931\u8d25' };
    if (status === 'downloading' || status === 'trying') return { key: 'downloading', label: '\u5b89\u88c5\u4e2d' };
    if (status === 'extracting') return { key: 'downloading', label: '\u89e3\u538b\u4e2d' };
    if (status === 'paused') return { key: 'paused', label: '\u5df2\u6682\u505c' };
    return { key: 'missing', label: '\u672a\u5b89\u88c5' };
  }
  function rememberToolVersionMeta(groups) {
    var next = {};
    (groups || []).forEach(function (group) {
      if (!group || !group.name || !Array.isArray(group.tools)) return;
      var rawVersions = group.tools.map(function (tool) { return tool && tool.version; }).filter(Boolean);
      var versions = normalizeVersionList(rawVersions);
      var enabledTool = group.tools.find(function (tool) { return tool && tool.enabled; });
      var recommendedVersion = group.defaultVersion || group.recommendedVersion || group.latestVersion || versions[0] || rawVersions[0] || '';
      var groupEnabled = group.enabled !== false;
      var currentVersionCandidate = groupEnabled ? (group.pinnedVersion || (enabledTool && enabledTool.version) || '') : '';
      var byVersion = {};
      group.tools.forEach(function (tool) {
        if (!tool || !tool.version) return;
        var state = getToolInstallState(tool);
        byVersion[tool.version] = {
          installed: !!tool.installed,
          status: state.key,
          label: state.label,
          downloadStatus: tool.downloadProcess && tool.downloadProcess.status || ''
        };
      });
      var currentState = currentVersionCandidate && byVersion[currentVersionCandidate];
      var currentVersion = currentState && currentState.installed ? currentVersionCandidate : '';
      var entry = {
        versions: versions,
        pinnedVersion: currentVersion,
        currentVersion: currentVersion,
        requestedPinnedVersion: group.pinnedVersion || '',
        enabled: groupEnabled,
        defaultVersion: recommendedVersion,
        recommendedVersion: recommendedVersion,
        byVersion: byVersion
      };
      next[group.name] = entry;
      next[normalizeToolName(group.name)] = entry;
    });
    bgoToolVersionMeta = next;
    bgoToolVersionMetaLoaded = true;
    bgoToolVersionMetaLastLoadedAt = Date.now();
    markToolVersionGroups();
  }
  function loadToolVersionMeta(force) {
    if (bgoToolVersionMetaLoading) return;
    if (!force && bgoToolVersionMetaLoaded && Date.now() - bgoToolVersionMetaLastLoadedAt < 2500) return;
    if (location.pathname.indexOf('/tools') < 0 && location.pathname.indexOf('/remotetools') < 0) return;
    bgoToolVersionMetaLoading = true;
    fetch(new URL('./api/tools', window.location.href).toString())
      .then(function (resp) { return resp.ok ? resp.json() : []; })
      .then(rememberToolVersionMeta)
      .catch(function () {})
      .finally(function () {
        bgoToolVersionMetaLoading = false;
        scheduleExternalThemeRefresh(0);
      });
  }
  function markToolVersionGroups() {
    if (location.pathname.indexOf('/tools') < 0 && location.pathname.indexOf('/remotetools') < 0) return;
    Array.prototype.slice.call(document.querySelectorAll('h4')).forEach(function (title) {
      var groupName = (title.textContent || '').trim();
      var meta = getToolVersionMeta(groupName);
      var groupCard = title.closest('.ant-card');
      if (!groupCard || !meta || !meta.versions) return;
      groupCard.dataset.bgoGroupName = groupName;
      groupCard.dataset.bgoToolGroupEmpty = meta.versions.length ? 'false' : 'true';
      if (meta.versions.length > 1) {
        groupCard.dataset.bgoHasMultipleVersions = 'true';
      }
      if (!groupCard.dataset.bgoSelectedToolVersion) {
        groupCard.dataset.bgoSelectedToolVersion = meta.currentVersion || meta.defaultVersion || meta.versions[0] || '';
      }
    });
  }
  function getCardVersion(card) {
    var tags = Array.prototype.slice.call(card.querySelectorAll('.ant-tag, [class*="tag"]'));
    for (var i = 0; i < tags.length; i++) {
      var text = (tags[i].textContent || '').trim();
      if (isToolVersionText(text)) return text;
    }
    return '';
  }
  function getCardWrapper(card) {
    return card.parentElement && card.parentElement.classList.contains('ant-space-item') ? card.parentElement : card;
  }
  function getPinnedVersionFromGroup(groupCard) {
    var selectedItems = Array.prototype.slice.call(groupCard.querySelectorAll('.ant-select-selection-item'));
    for (var i = 0; i < selectedItems.length; i++) {
      var text = (selectedItems[i].textContent || '').trim();
      if (isToolVersionText(text)) return text;
    }
    return '';
  }
  function isInCompactVersionUI(el) {
    return !!(el && el.closest && el.closest('.bgo-tool-version-compact,[data-bgo-tool-version-card="true"]'));
  }
  function isDefaultVersionLabelText(text) {
    text = String(text || '').trim();
    return text.indexOf('\u9ed8\u8ba4\u7248\u672c') === 0 || /^Default\s+Version/i.test(text);
  }
  function isCurrentVersionLabelText(text) {
    text = String(text || '').trim();
    return text.indexOf('\u5f53\u524d\u7248\u672c') === 0 || /^Current\s+Version/i.test(text);
  }
  function findVersionTagInScope(scope) {
    if (!scope) return null;
    var tags = Array.prototype.slice.call(scope.querySelectorAll('.ant-tag,[class*="tag"]'));
    for (var i = 0; i < tags.length; i++) {
      if (isInCompactVersionUI(tags[i])) continue;
      if (tags[i].dataset && tags[i].dataset.bgoHiddenRecommendedVersion === 'true') continue;
      if (tags[i].dataset && tags[i].dataset.bgoToolStatus === 'version') return tags[i];
      if (isToolVersionText(tags[i].textContent || '')) return tags[i];
    }
    return null;
  }
  function getVersionState(groupName, version) {
    var meta = getToolVersionMeta(groupName);
    var state = meta.byVersion && meta.byVersion[version];
    if (state) return state;
    return { installed: false, status: 'missing', label: '\u672a\u5b89\u88c5' };
  }
  function getToolVersionMeta(groupName) {
    return bgoToolVersionMeta[groupName] || bgoToolVersionMeta[normalizeToolName(groupName)] || {};
  }
  function getCurrentToolVersion(groupName) {
    var meta = getToolVersionMeta(groupName);
    return meta.currentVersion || meta.pinnedVersion || '';
  }
  function getRecommendedToolVersion(groupName) {
    var meta = getToolVersionMeta(groupName);
    return meta.recommendedVersion || meta.defaultVersion || (meta.versions && meta.versions[0]) || '';
  }
  function getCurrentVersionState(groupName) {
    var version = getCurrentToolVersion(groupName);
    return version ? getVersionState(groupName, version) : { installed: false, status: 'missing', label: '\u672a\u5b89\u88c5' };
  }
  function isTagLike(el) {
    var className = String(el && el.className || '');
    return !!(el && (el.classList && el.classList.contains('ant-tag') || className.indexOf('tag') >= 0));
  }
  function isInstallStatusText(text) {
    text = String(text || '').trim();
    return /^(?:\u5df2\u5b89\u88c5|\u672a\u5b89\u88c5|\u5b89\u88c5\u5931\u8d25|\u5b89\u88c5\u4e2d|\u89e3\u538b\u4e2d|\u5df2\u6682\u505c|\u5df2\u5b8c\u6210|Installed|Not installed|Install failed|Installing|Extracting|Paused)$/i.test(text);
  }
  function findStatusNodeAfterVersionTag(tag) {
    if (!tag || !tag.parentElement) return null;
    var candidates = Array.prototype.slice.call(tag.parentElement.querySelectorAll('span,div'));
    for (var i = 0; i < candidates.length; i++) {
      var node = candidates[i];
      if (node === tag || node.children.length > 0) continue;
      if (!(tag.compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING)) continue;
      if (isInstallStatusText(node.textContent || '')) return node;
    }
    return null;
  }
  function setToolStatusNode(node, state) {
    if (!node || !state) return;
    node.textContent = state.label || '\u672a\u5b89\u88c5';
    node.dataset.bgoToolStatus = state.status || 'missing';
    node.style.marginLeft = '8px';
  }
  function updateInlineDefaultVersionText(el, version) {
    if (!el || el.children.length > 0) return false;
    var text = (el.textContent || '').trim();
    if (!isDefaultVersionLabelText(text)) return false;
    var next = text.replace(/((?:\u9ed8\u8ba4\u7248\u672c|Default\s+Version)\s*[:\uff1a]?\s*)(?:[a-z]+-?v?|v)?\d+(?:\.\d+)+(?:[-._a-zA-Z0-9]+)?/i, '$1' + version);
    if (next === text) return false;
    el.textContent = next;
    return true;
  }
  function syncDisplayedDefaultVersion(groupCard, groupName) {
    if (!groupCard || !groupName) return;
    var currentVersion = getCurrentToolVersion(groupName);
    var recommendedVersion = getRecommendedToolVersion(groupName);
    var currentText = currentVersion || '\u65e0';
    var currentState = getCurrentVersionState(groupName);
    var enabledText = currentVersion ? '\u5df2\u542f\u7528' : '\u672a\u542f\u7528';
    var labels = Array.prototype.slice.call(groupCard.querySelectorAll('span,div,label,strong'));
    for (var i = 0; i < labels.length; i++) {
      var label = labels[i];
      if (isInCompactVersionUI(label)) continue;
      if (label.children.length > 0) continue;
      var labelText = (label.textContent || '').trim();
      if (!labelText) continue;
      if (/^(?:\u5df2\u542f\u7528|\u5df2\u7981\u7528|\u672a\u542f\u7528|Enabled|Disabled)$/i.test(labelText)) {
        label.textContent = enabledText;
        label.dataset.bgoToolStatus = currentVersion ? 'enabled' : 'disabled';
        continue;
      }
      if (labelText.indexOf('\u63a8\u8350\u7248\u672c') === 0) {
        label.textContent = '\u63a8\u8350\u7248\u672c\uff1a' + (recommendedVersion || '\u65e0');
        label.dataset.bgoToolStatus = 'default-version';
        continue;
      }
      if (isCurrentVersionLabelText(labelText)) {
        label.textContent = '\u5f53\u524d\u7248\u672c:';
        var currentScope = label.closest('.ant-space') || label.closest('.ant-row') || label.closest('.ant-descriptions-item') || label.parentElement;
        var currentTag = findVersionTagInScope(currentScope);
        var currentCursor = (label.closest('.ant-space-item') || label).nextElementSibling;
        while (!currentTag && currentCursor) {
          currentTag = findVersionTagInScope(currentCursor);
          currentCursor = currentCursor.nextElementSibling;
        }
        if (!currentTag) currentTag = findVersionTagInScope(groupCard);
        if (currentTag) {
          currentTag.textContent = currentText;
          currentTag.style.display = '';
          currentTag.dataset.bgoToolStatus = 'version';
          var currentStatusNode = findStatusNodeAfterVersionTag(currentTag);
          if (!currentStatusNode && currentTag.parentNode) {
            currentStatusNode = document.createElement('span');
            currentTag.insertAdjacentElement('afterend', currentStatusNode);
          }
          setToolStatusNode(currentStatusNode, currentState);
          return;
        }
      }
      if (!isDefaultVersionLabelText(labelText)) continue;
      var updated = false;
      if (isTagLike(label) || labelText === '\u9ed8\u8ba4\u7248\u672c' || /^Default\s+Version$/i.test(labelText)) {
        label.textContent = '\u63a8\u8350\u7248\u672c\uff1a' + (recommendedVersion || '\u65e0');
        label.dataset.bgoToolStatus = 'default-version';
        var recommendedScope = label.closest('.ant-space') || label.closest('.ant-row') || label.parentElement;
        var recommendedTag = findVersionTagInScope(recommendedScope);
        if (recommendedTag) {
          recommendedTag.dataset.bgoHiddenRecommendedVersion = 'true';
          recommendedTag.style.display = 'none';
        }
        continue;
      }
      label.textContent = '\u5f53\u524d\u7248\u672c:';
      var scope = label.closest('.ant-space') || label.closest('.ant-row') || label.closest('.ant-descriptions-item') || label.parentElement;
      var tag = findVersionTagInScope(scope);
      var cursor = (label.closest('.ant-space-item') || label).nextElementSibling;
      while (!tag && cursor) {
        tag = findVersionTagInScope(cursor);
        cursor = cursor.nextElementSibling;
      }
      if (!tag) tag = findVersionTagInScope(groupCard);
      if (tag) {
        tag.textContent = currentText;
        tag.style.display = '';
        tag.dataset.bgoToolStatus = 'version';
        var statusNode = findStatusNodeAfterVersionTag(tag);
        if (!statusNode && tag.parentNode) {
          statusNode = document.createElement('span');
          tag.insertAdjacentElement('afterend', statusNode);
        }
        setToolStatusNode(statusNode, currentState);
        updated = true;
      }
      if (updated) return;
    }
  }
  function updateToolVersionMeta(groupName, version) {
    if (!groupName || !version) return;
    var meta = getToolVersionMeta(groupName);
    if (!meta.versions) meta = { versions: [] };
    meta.pinnedVersion = version;
    meta.currentVersion = version;
    meta.enabled = true;
    bgoToolVersionMeta[groupName] = meta;
    bgoToolVersionMeta[normalizeToolName(groupName)] = meta;
  }
  function setVersionStateBadge(groupCard, groupName, version) {
    var badge = groupCard.querySelector(':scope .bgo-tool-version-state');
    if (!badge) return;
    var state = version ? getVersionState(groupName, version) : { installed: false, status: 'missing', label: '\u672a\u5b89\u88c5' };
    setToolStatusNode(badge, state);
  }
  function isRequiredToolGroup(groupName) {
    var info = getToolInfo(groupName);
    return !!(info && info.role === 'required');
  }
  function isInternalToolGroup(groupName) {
    var info = getToolInfo(groupName);
    return !!(info && info.role === 'internal');
  }
  function isEmptyToolGroup(groupName) {
    var meta = getToolVersionMeta(groupName);
    return !!(meta && Array.isArray(meta.versions) && meta.versions.length === 0);
  }
  function countInstalledToolVersions(groupName) {
    var meta = getToolVersionMeta(groupName);
    var byVersion = meta.byVersion || {};
    return Object.keys(byVersion).filter(function (version) {
      return byVersion[version] && byVersion[version].installed;
    }).length;
  }
  function isToolGroupEnableAvailable(groupName) {
    var meta = getToolVersionMeta(groupName);
    if (!meta || !Array.isArray(meta.versions)) return true;
    if (countInstalledToolVersions(groupName) <= 0) return false;
    var pinned = meta.requestedPinnedVersion || '';
    if (!pinned) return true;
    var pinnedState = meta.byVersion && meta.byVersion[pinned];
    return !!(pinnedState && pinnedState.installed);
  }
  function setToolGroupSwitchChecked(sw, checked) {
    if (!sw) return;
    sw.setAttribute('aria-checked', checked ? 'true' : 'false');
    if (sw.dataset && sw.dataset.state) sw.dataset.state = checked ? 'checked' : 'unchecked';
    if (sw.classList) {
      sw.classList.toggle('ant-switch-checked', checked);
      sw.classList.toggle('rc-switch-checked', checked);
      sw.classList.toggle('checked', checked);
    }
    var input = sw.querySelector && sw.querySelector('input[type="checkbox"]');
    if (input) input.checked = checked;
  }
  function getToolGroupSwitches(groupCard) {
    if (!groupCard) return [];
    return Array.prototype.slice.call(groupCard.querySelectorAll('button[role="switch"],.ant-switch,.rc-switch,[data-slot="switch"],[class*="SwitchRoot"]')).filter(function (sw) {
      if (!sw || isInCompactVersionUI(sw)) return false;
      if (sw.closest && sw.closest('[data-bgo-tool-version-card="true"]')) return false;
      var ownerCard = sw.closest && sw.closest('.ant-card');
      return !ownerCard || ownerCard === groupCard || !getCardVersion(ownerCard);
    });
  }
  function syncToolGroupToggleState(groupCard, groupName) {
    var meta = getToolVersionMeta(groupName);
    if (!groupCard || !meta) return;
    var available = isToolGroupEnableAvailable(groupName);
    var required = isRequiredToolGroup(groupName);
    var checked = required && available ? true : !!(meta.enabled && available);
    groupCard.dataset.bgoToolToggleBlocked = available ? 'false' : 'true';
    groupCard.dataset.bgoToolRequiredToggleBlocked = required && available ? 'true' : 'false';
    getToolGroupSwitches(groupCard).forEach(function (sw) {
      if (!sw.dataset.bgoOriginalDisabled) {
        sw.dataset.bgoOriginalDisabled = sw.disabled ? 'true' : 'false';
      }
      setToolGroupSwitchChecked(sw, checked);
      sw.dataset.bgoToolToggleBlocked = available ? 'false' : 'true';
      sw.dataset.bgoToolRequiredToggleBlocked = required && available ? 'true' : 'false';
      sw.disabled = !available || sw.dataset.bgoOriginalDisabled === 'true';
      sw.setAttribute('aria-disabled', !available ? 'true' : 'false');
      sw.title = !available ? '\u8be5\u5de5\u5177\u7ec4\u6ca1\u6709\u53ef\u542f\u7528\u7684\u5df2\u5b89\u88c5\u7248\u672c\uff0c\u8bf7\u5148\u5b89\u88c5\u4e00\u4e2a\u7248\u672c' : (required ? bgoRequiredToolDisableMessage : '');
      if (sw.classList) sw.classList.toggle('ant-switch-disabled', !available);
      sw.style.pointerEvents = !available ? 'none' : '';
      sw.style.opacity = !available ? '.62' : '';
    });
  }
  function isLastRequiredInstalledVersion(groupName, state) {
    return !!(state && state.installed && isRequiredToolGroup(groupName) && countInstalledToolVersions(groupName) <= 1);
  }
  function syncRequiredToolUninstallNotice(card, button, show) {
    if (!card) return;
    var notice = card.querySelector(':scope .bgo-tool-required-uninstall-notice');
    if (!show) {
      if (notice) notice.remove();
      return;
    }
    if (!notice) {
      notice = document.createElement('span');
      notice.className = 'bgo-tool-required-uninstall-notice';
      notice.setAttribute('role', 'note');
    }
    notice.textContent = bgoRequiredToolUninstallMessage;
    var anchor = button && (button.closest('.ant-space-item') || button);
    if (anchor && anchor.parentElement && notice.parentElement !== anchor.parentElement) {
      anchor.insertAdjacentElement('afterend', notice);
    } else if (!notice.parentElement) {
      card.appendChild(notice);
    }
  }
  function syncToolCardActionState(card, groupName, version) {
    if (!card || !version) return;
    var state = getVersionState(groupName, version);
    var keepRequiredVersion = isLastRequiredInstalledVersion(groupName, state);
    var internalTool = isInternalToolGroup(groupName);
    card.dataset.bgoToolInstallState = state.status || 'missing';
    card.dataset.bgoToolRequiredLastInstalled = keepRequiredVersion ? 'true' : 'false';
    card.dataset.bgoToolInternal = internalTool ? 'true' : 'false';
    Array.prototype.slice.call(card.querySelectorAll('button')).forEach(function (button) {
      var text = (button.textContent || '').trim();
      if (text !== '\u5378\u8f7d' && text !== 'Uninstall') return;
      button.dataset.bgoToolAction = 'uninstall';
      if (!button.dataset.bgoOriginalDisabled) {
        button.dataset.bgoOriginalDisabled = button.disabled ? 'true' : 'false';
      }
      var hidden = !state.installed || internalTool;
      button.style.display = hidden ? 'none' : '';
      button.setAttribute('aria-hidden', hidden ? 'true' : 'false');
      button.dataset.bgoToolRequiredLastInstalled = keepRequiredVersion ? 'true' : 'false';
      button.disabled = hidden ? true : button.dataset.bgoOriginalDisabled === 'true';
      button.title = keepRequiredVersion ? bgoRequiredToolUninstallMessage : '';
      syncRequiredToolUninstallNotice(card, button, keepRequiredVersion && !hidden);
      var item = button.closest('.ant-space-item');
      if (item) item.style.display = hidden ? 'none' : '';
    });
  }
  function startsWithOriginalVersionSectionText(text) {
    text = String(text || '').trim();
    var labels = [
      '\u9501\u5b9a\u7248\u672c', '\u5df2\u5b89\u88c5\u7248\u672c', '\u5f85\u4e0b\u8f7d\u7248\u672c',
      '\u4e0b\u8f7d\u4e2d\u7248\u672c', '\u5df2\u6682\u505c\u7248\u672c',
      'Pinned Version', 'Installed Versions', 'Pending Versions', 'Downloading Versions', 'Paused Versions'
    ];
    for (var i = 0; i < labels.length; i++) {
      if (text.indexOf(labels[i]) === 0) return true;
    }
    return false;
  }
  function markOriginalVersionElementHidden(el) {
    if (!el) return;
    if (el.querySelector && el.querySelector('.ant-card,[data-bgo-tool-version-card="true"],button,.ant-btn')) return;
    el.dataset.bgoOriginalVersionSection = 'hidden';
    [el.previousElementSibling, el.nextElementSibling].forEach(function (sibling) {
      if (!sibling) return;
      var className = String(sibling.className || '');
      if (sibling.tagName === 'HR' || className.indexOf('divider') >= 0 || className.indexOf('Divider') >= 0) {
        sibling.dataset.bgoOriginalVersionDivider = 'hidden';
      }
    });
  }
  function closestOriginalVersionContainer(node, groupCard) {
    if (!node || !groupCard || isInCompactVersionUI(node)) return null;
    if (node.closest && node.closest('.bgo-tool-info,[data-bgo-tool-version-card="true"]')) return null;
    var container = node.closest('.ant-space-item') ||
      node.closest('.ant-descriptions-item') ||
      node.closest('.ant-row') ||
      node.parentElement;
    if (container && container.classList && container.classList.contains('ant-card-body')) return null;
    if (!container || container === groupCard || (container.classList && container.classList.contains('ant-card'))) return null;
    if (container.querySelector && container.querySelector('.ant-card,[data-bgo-tool-version-card="true"],button,.ant-btn')) return null;
    return container;
  }
  function hideOriginalVersionSections(groupCard) {
    if (!groupCard) return;
    Array.prototype.slice.call(groupCard.querySelectorAll('span,div,label,strong,p')).forEach(function (node) {
      if (!startsWithOriginalVersionSectionText(node.textContent || '')) return;
      var container = closestOriginalVersionContainer(node, groupCard);
      if (container) markOriginalVersionElementHidden(container);
    });
  }
  function getToolGroupCardFromControl(control) {
    var card = control && control.closest ? control.closest('.ant-card') : null;
    while (card) {
      if (card.querySelector && card.querySelector(':scope h4')) return card;
      card = card.parentElement && card.parentElement.closest ? card.parentElement.closest('.ant-card') : null;
    }
    return null;
  }
  function createToolVersionCompact(groupName, versions, selectedVersion, onChange, onEnable) {
    var box = document.createElement('div');
    box.className = 'bgo-tool-version-compact';
    var label = document.createElement('label');
    label.className = 'bgo-tool-version-label';
    label.textContent = '\u53ef\u9009\u7248\u672c';
    var select = document.createElement('select');
    select.className = 'bgo-tool-version-select';
    select.setAttribute('aria-label', groupName + ' version');
    versions.forEach(function (version) {
      var option = document.createElement('option');
      option.value = version;
      option.textContent = version;
      select.appendChild(option);
    });
    select.value = selectedVersion;
    select.addEventListener('change', function () { onChange(select.value); });
    var enableButton = document.createElement('button');
    enableButton.type = 'button';
    enableButton.className = 'bgo-tool-version-enable';
    enableButton.textContent = '\u542f\u7528';
    enableButton.addEventListener('click', function () { onEnable(select.value, enableButton); });
    var hint = document.createElement('span');
    hint.className = 'bgo-tool-version-hint';
    hint.textContent = '\u9009\u62e9\u7248\u672c\u540e\u67e5\u770b\u8be6\u60c5\uff1b\u53ea\u6709\u5df2\u5b89\u88c5\u7248\u672c\u624d\u80fd\u542f\u7528';
    box.appendChild(label);
    box.appendChild(select);
    box.appendChild(enableButton);
    box.appendChild(hint);
    return box;
  }
  function syncToolPinnedVersion(toolName, version) {
    if (!toolName || !version) return Promise.reject(new Error('missing tool version'));
    var endpoint = new URL('./api/pin-version', window.location.href).toString();
    return fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ toolName: toolName, version: version })
    }).then(function (resp) {
      if (!resp.ok) throw new Error('pin-version failed: ' + resp.status);
      return resp;
    });
  }
  function updateToolVersionEnableButton(groupCard, groupName, selectedVersion) {
    var button = groupCard && groupCard.querySelector(':scope .bgo-tool-version-enable');
    if (!button) return;
    var currentVersion = getCurrentToolVersion(groupName);
    var selectedState = selectedVersion ? getVersionState(groupName, selectedVersion) : null;
    var shouldShow = !!selectedVersion && selectedVersion !== currentVersion && !!(selectedState && selectedState.installed);
    button.style.display = shouldShow ? '' : 'none';
    button.setAttribute('aria-hidden', shouldShow ? 'false' : 'true');
    button.title = !selectedVersion || selectedVersion === currentVersion ? '' : (shouldShow ? '\u542f\u7528\u8be5\u5df2\u5b89\u88c5\u7248\u672c' : '\u8bf7\u5148\u5b89\u88c5\u8be5\u7248\u672c\uff0c\u7136\u540e\u518d\u542f\u7528');
  }
  function syncToolCurrentVersionCompact(groupCard, groupName) {
    var currentNode = groupCard && groupCard.querySelector(':scope .bgo-tool-version-current');
    if (!currentNode) return;
    var version = getCurrentToolVersion(groupName);
    currentNode.textContent = '\u5f53\u524d\u7248\u672c: ' + (version || '\u65e0');
    setVersionStateBadge(groupCard, groupName, version);
  }
  function createToolRoleBadge(role, label) {
    var badge = document.createElement('span');
    badge.className = 'bgo-tool-role-badge';
    badge.dataset.bgoToolRole = role;
    badge.textContent = label;
    return badge;
  }
  function createToolInfoBlock(groupName, info) {
    var block = document.createElement('div');
    block.className = 'bgo-tool-info';
    block.dataset.bgoToolName = groupName;
    block.dataset.bgoToolRole = info.role;
    var head = document.createElement('div');
    head.className = 'bgo-tool-info-head';
    head.appendChild(createToolRoleBadge(info.role, info.roleLabel));
    var title = document.createElement('strong');
    title.className = 'bgo-tool-info-title';
    title.textContent = info.title;
    head.appendChild(title);
    var desc = document.createElement('p');
    desc.className = 'bgo-tool-info-desc';
    desc.textContent = info.description;
    block.appendChild(head);
    block.appendChild(desc);
    return block;
  }
  function getDirectToolInfoBlock(body) {
    var children = Array.prototype.slice.call(body ? body.children : []);
    return children.find(function (child) {
      return child.classList && child.classList.contains('bgo-tool-info');
    }) || null;
  }
  function getEmptyToolGroupMessage(groupName) {
    var info = getToolInfo(groupName);
    if (info && info.role === 'internal') {
      return '\u8be5\u5185\u90e8\u7ec4\u4ef6\u5f53\u524d\u6ca1\u6709\u53ef\u624b\u52a8\u5b89\u88c5\u7248\u672c\uff0c\u7531 BiliLive-go \u81ea\u52a8\u7ba1\u7406\uff0c\u4e0d\u9700\u8981\u5728\u8fd9\u91cc\u5b89\u88c5\u3001\u5378\u8f7d\u6216\u5207\u6362\u3002';
    }
    return '\u8be5\u5de5\u5177\u7ec4\u5f53\u524d\u6ca1\u6709\u53ef\u5b89\u88c5\u7248\u672c\u3002\u5982\u679c\u8fd9\u662f\u672c\u5730\u72b6\u6001\u5f02\u5e38\uff0c\u8bf7\u91cd\u542f\u7a0b\u5e8f\u8ba9 RemoteTools \u91cd\u65b0\u52a0\u8f7d\u914d\u7f6e\u3002';
  }
  function syncEmptyToolGroupNotice(groupCard, groupName) {
    if (!groupCard || !groupName) return;
    var empty = isEmptyToolGroup(groupName);
    var notice = groupCard.querySelector(':scope .bgo-tool-empty-notice');
    if (!empty) {
      groupCard.dataset.bgoToolGroupEmpty = 'false';
      if (notice) notice.remove();
      return;
    }
    groupCard.dataset.bgoToolGroupEmpty = 'true';
    if (!notice) {
      notice = document.createElement('div');
      notice.className = 'bgo-tool-empty-notice';
      notice.setAttribute('role', 'note');
    }
    notice.textContent = getEmptyToolGroupMessage(groupName);
    var body = Array.prototype.slice.call(groupCard.children).find(function (child) {
      return child.classList && child.classList.contains('ant-card-body');
    }) || groupCard.querySelector('.ant-card-body') || groupCard;
    var infoBlock = getDirectToolInfoBlock(body);
    if (infoBlock && notice.parentElement !== body) {
      infoBlock.insertAdjacentElement('afterend', notice);
    } else if (!notice.parentElement) {
      body.insertBefore(notice, body.firstChild);
    }
    Array.prototype.slice.call(groupCard.querySelectorAll('span,div,p')).forEach(function (node) {
      if (!node || node === notice || notice.contains(node) || node.children.length > 0) return;
      if (node.closest && node.closest('.bgo-tool-info,.bgo-tool-empty-notice')) return;
      var text = (node.textContent || '').trim();
      if (text === '\u8be5\u5de5\u5177\u7ec4\u6682\u65e0\u5de5\u5177' || text === 'No tools in this group') {
        node.dataset.bgoToolStatus = 'empty';
        node.style.display = 'none';
      } else if (isInternalToolGroup(groupName) && /^(?:\u5df2\u542f\u7528|\u672a\u542f\u7528|\u5df2\u7981\u7528|Enabled|Disabled)$/i.test(text)) {
        node.textContent = '\u5185\u90e8\u7ba1\u7406';
        node.dataset.bgoToolStatus = 'internal';
      }
    });
  }
  function refreshExternalToolLegend() {
    if (location.pathname.indexOf('/tools') < 0 && location.pathname.indexOf('/remotetools') < 0) return;
    if (document.querySelector('.bgo-tool-legend')) return;
    var title = document.querySelector('h1');
    if (!title) return;
    var legend = document.createElement('div');
    legend.className = 'bgo-tool-legend';
    bgoToolLegendItems.forEach(function (item) {
      var entry = document.createElement('div');
      entry.className = 'bgo-tool-legend-item';
      entry.dataset.bgoToolRole = item.role;
      entry.appendChild(createToolRoleBadge(item.role, item.label));
      var text = document.createElement('span');
      text.textContent = item.text;
      entry.appendChild(text);
      legend.appendChild(entry);
    });
    var anchor = title.parentElement || title;
    var parent = anchor.parentElement;
    if (parent) {
      parent.insertBefore(legend, anchor.nextSibling);
    }
  }
  function refreshExternalToolInfo() {
    if (location.pathname.indexOf('/tools') < 0 && location.pathname.indexOf('/remotetools') < 0) return;
    Array.prototype.slice.call(document.querySelectorAll('h4')).forEach(function (title) {
      var groupName = (title.textContent || '').trim();
      var info = getToolInfo(groupName);
      if (!info) return;
      var groupCard = title.closest('.ant-card');
      if (!groupCard) return;
      groupCard.dataset.bgoToolRole = info.role;
      groupCard.dataset.bgoToolName = normalizeToolName(groupName);
      var body = Array.prototype.slice.call(groupCard.children).find(function (child) {
        return child.classList && child.classList.contains('ant-card-body');
      }) || groupCard.querySelector('.ant-card-body') || groupCard;
      var existing = getDirectToolInfoBlock(body);
      if (!existing) {
        existing = createToolInfoBlock(groupName, info);
        body.insertBefore(existing, body.firstChild);
        syncEmptyToolGroupNotice(groupCard, groupName);
        return;
      }
      existing.dataset.bgoToolName = groupName;
      existing.dataset.bgoToolRole = info.role;
      var badge = existing.querySelector('.bgo-tool-role-badge');
      if (badge) {
        badge.dataset.bgoToolRole = info.role;
        badge.textContent = info.roleLabel;
      }
      var infoTitle = existing.querySelector('.bgo-tool-info-title');
      if (infoTitle) infoTitle.textContent = info.title;
      var desc = existing.querySelector('.bgo-tool-info-desc');
      if (desc) desc.textContent = info.description;
      syncEmptyToolGroupNotice(groupCard, groupName);
    });
  }
  function refreshExternalToolVersionPicker() {
    if (location.pathname.indexOf('/tools') < 0 && location.pathname.indexOf('/remotetools') < 0) return;
    loadToolVersionMeta();
    markToolVersionGroups();
    Array.prototype.slice.call(document.querySelectorAll('h4')).forEach(function (title) {
      var groupName = (title.textContent || '').trim();
      if (!groupName) return;
      var groupCard = title.closest('.ant-card');
      if (!groupCard || groupCard.dataset.bgoVersionPickerBusy === '1') return;
      var meta = getToolVersionMeta(groupName);
      hideOriginalVersionSections(groupCard);
      if (meta) {
        syncToolGroupToggleState(groupCard, groupName);
      }
      if (meta && meta.versions && meta.versions.length) {
        if (meta.versions.length > 1) {
          groupCard.dataset.bgoHasMultipleVersions = 'true';
        }
        groupCard.dataset.bgoVersionSummaryReady = 'true';
        if (!groupCard.dataset.bgoSelectedToolVersion) {
          groupCard.dataset.bgoSelectedToolVersion = getCurrentToolVersion(groupName) || getRecommendedToolVersion(groupName) || meta.versions[0] || '';
        }
        syncDisplayedDefaultVersion(groupCard, groupName);
      }
      var cards = Array.prototype.slice.call(groupCard.querySelectorAll('.ant-card')).filter(function (card) {
        return card !== groupCard && getCardVersion(card);
      });
      if (cards.length <= 1) {
        if (meta && meta.versions && meta.versions.length > 1) {
          groupCard.dataset.bgoVersionPickerReady = 'true';
        }
        return;
      }
      groupCard.dataset.bgoVersionPickerBusy = '1';
      try {
        var versionToCard = {};
        cards.forEach(function (card) {
          var version = getCardVersion(card);
          if (!version) return;
          card.dataset.bgoToolVersionCard = 'true';
          card.dataset.bgoToolVersion = version;
          versionToCard[version] = card;
        });
        var versions = meta && meta.versions && meta.versions.length ? meta.versions.filter(function (version) { return versionToCard[version]; }) : Object.keys(versionToCard).sort(compactToolVersionCompare);
        if (!versions.length) versions = Object.keys(versionToCard).sort(compactToolVersionCompare);
        if (!versions.length) return;
        hideOriginalVersionSections(groupCard);
        var selectedVersion = groupCard.dataset.bgoSelectedToolVersion || getCurrentToolVersion(groupName) || getPinnedVersionFromGroup(groupCard) || getRecommendedToolVersion(groupName) || versions[0];
        if (versions.indexOf(selectedVersion) < 0) selectedVersion = versions[0];
        groupCard.dataset.bgoSelectedToolVersion = selectedVersion;
        var firstWrapper = getCardWrapper(versionToCard[versions[versions.length - 1]] || versionToCard[versions[0]]);
        var container = firstWrapper.parentElement || groupCard;
        var compact = groupCard.querySelector(':scope .bgo-tool-version-compact');
        var applySelection = function (version, shouldSync) {
          if (versions.indexOf(version) < 0) version = versions[0];
          groupCard.dataset.bgoSelectedToolVersion = version;
          cards.forEach(function (card) {
            var wrapper = getCardWrapper(card);
            syncToolCardActionState(card, groupName, card.dataset.bgoToolVersion);
            wrapper.dataset.bgoToolVersionWrapper = 'true';
            wrapper.dataset.bgoToolVersionVisible = card.dataset.bgoToolVersion === version ? 'true' : 'false';
            wrapper.style.display = card.dataset.bgoToolVersion === version ? '' : 'none';
          });
          var current = groupCard.querySelector(':scope .bgo-tool-version-current');
          var activeSelect = groupCard.querySelector(':scope .bgo-tool-version-select');
          if (activeSelect && activeSelect.value !== version) activeSelect.value = version;
          syncDisplayedDefaultVersion(groupCard, groupName);
          syncToolCurrentVersionCompact(groupCard, groupName);
          updateToolVersionEnableButton(groupCard, groupName, version);
        };
        var enableSelection = function (version, button) {
          if (versions.indexOf(version) < 0) return;
          var selectedState = getVersionState(groupName, version);
          if (!selectedState || !selectedState.installed) {
            updateToolVersionEnableButton(groupCard, groupName, version);
            return;
          }
          if (button) {
            button.disabled = true;
            button.dataset.bgoLoading = 'true';
            button.textContent = '\u542f\u7528\u4e2d';
          }
          syncToolPinnedVersion(groupName, version).then(function () {
            updateToolVersionMeta(groupName, version);
            syncDisplayedDefaultVersion(groupCard, groupName);
            syncToolCurrentVersionCompact(groupCard, groupName);
            updateToolVersionEnableButton(groupCard, groupName, version);
            loadToolVersionMeta(true);
          }).catch(function () {
            if (button) button.dataset.bgoError = 'true';
          }).finally(function () {
            if (button) {
              button.disabled = false;
              button.dataset.bgoLoading = 'false';
              button.textContent = '\u542f\u7528';
            }
          });
        };
        if (!compact) {
          compact = createToolVersionCompact(groupName, versions, selectedVersion, function (version) {
            applySelection(version, false);
          }, enableSelection);
          var current = document.createElement('span');
          current.className = 'bgo-tool-version-current';
          current.textContent = '\u5f53\u524d\u7248\u672c: ' + (getCurrentToolVersion(groupName) || '\u65e0');
          compact.appendChild(current);
          var stateBadge = document.createElement('span');
          stateBadge.className = 'bgo-tool-version-state';
          compact.appendChild(stateBadge);
          container.insertBefore(compact, firstWrapper);
        } else {
          var existingLabel = compact.querySelector('.bgo-tool-version-label');
          if (existingLabel) existingLabel.textContent = '\u53ef\u9009\u7248\u672c';
          var existingHint = compact.querySelector('.bgo-tool-version-hint');
          if (existingHint) existingHint.textContent = '\u9009\u62e9\u7248\u672c\u540e\u67e5\u770b\u8be6\u60c5\uff1b\u53ea\u6709\u5df2\u5b89\u88c5\u7248\u672c\u624d\u80fd\u542f\u7528';
          var select = compact.querySelector('select');
          var oldOptions = Array.prototype.slice.call(select ? select.options : []).map(function (option) { return option.value; }).join('|');
          if (select && oldOptions !== versions.join('|')) {
            select.innerHTML = '';
            versions.forEach(function (version) {
              var option = document.createElement('option');
              option.value = version;
              option.textContent = version;
              select.appendChild(option);
            });
          }
          if (select) select.value = selectedVersion;
          if (!compact.querySelector('.bgo-tool-version-state')) {
            var existingStateBadge = document.createElement('span');
            existingStateBadge.className = 'bgo-tool-version-state';
            compact.appendChild(existingStateBadge);
          }
          if (!compact.querySelector('.bgo-tool-version-enable')) {
            var enableButton = document.createElement('button');
            enableButton.type = 'button';
            enableButton.className = 'bgo-tool-version-enable';
            enableButton.textContent = '\u542f\u7528';
            enableButton.addEventListener('click', function () {
              var activeSelect = groupCard.querySelector(':scope .bgo-tool-version-select');
              enableSelection(activeSelect ? activeSelect.value : selectedVersion, enableButton);
            });
            var hint = compact.querySelector('.bgo-tool-version-hint');
            compact.insertBefore(enableButton, hint || null);
          }
        }
        applySelection(selectedVersion, false);
        groupCard.dataset.bgoVersionPickerReady = 'true';
      } finally {
        groupCard.dataset.bgoVersionPickerBusy = '0';
      }
    });
  }
  var bgoExternalVersionLoaded = false;
  function formatExternalVersionLabel(version) {
    version = String(version || '').trim();
    if (!version) return '';
    return version.toLowerCase().indexOf('v') === 0 ? 'v ' + version.slice(1) : 'v ' + version;
  }
  function setExternalVersionBadge(version) {
    var label = formatExternalVersionLabel(version);
    if (!label) return;
    var badge = document.querySelector('.bgo-external-version-badge');
    if (!badge) {
      badge = document.createElement('div');
      badge.className = 'bgo-external-version-badge';
      badge.setAttribute('aria-label', '\u5f53\u524d\u7248\u672c ' + label);
      document.body.appendChild(badge);
    }
    badge.textContent = label;
  }
  function refreshExternalVersionBadge() {
    if (bgoExternalVersionLoaded) return;
    bgoExternalVersionLoaded = true;
    fetch('/api/info', { cache: 'no-store', credentials: 'same-origin' })
      .then(function (resp) {
        if (!resp.ok) throw new Error('info failed');
        return resp.json();
      })
      .then(function (info) {
        setExternalVersionBadge(info && (info.app_version || info.appVersion));
      })
      .catch(function () {
        bgoExternalVersionLoaded = false;
      });
  }
  function refreshExternalTextHints() {
    if (document.documentElement.dataset.bgoTheme !== 'dark') return;
    document.querySelectorAll('span,div,p,small,label').forEach(function (el) {
      if (!el || el.children.length > 0) return;
      var text = (el.textContent || '').trim();
      if (!text) return;
      if (text.indexOf('运行平台:') === 0) {
        el.dataset.bgoToolStatus = 'platform';
        return;
      }
      if (text === '默认版本') {
        el.dataset.bgoToolStatus = 'default-version';
        return;
      }
      if (text === '已启用') {
        el.dataset.bgoToolStatus = 'enabled';
        return;
      }
      if (text === '已禁用') {
        el.dataset.bgoToolStatus = 'disabled';
        return;
      }
      if (text === '已安装') {
        el.dataset.bgoToolStatus = 'installed';
        return;
      }
      if (text === '未安装') {
        el.dataset.bgoToolStatus = 'missing';
        return;
      }
      if (text === '安装失败') {
        el.dataset.bgoToolStatus = 'failed';
        return;
      }
      if (text === '安装中' || text === '解压中') {
        el.dataset.bgoToolStatus = 'downloading';
        return;
      }
      if (text === '已暂停') {
        el.dataset.bgoToolStatus = 'paused';
        return;
      }
      if (text === '该工具组暂无工具') {
        el.dataset.bgoToolStatus = 'empty';
        return;
      }
      if (isToolVersionText(text)) {
        el.dataset.bgoToolStatus = 'version';
        return;
      }
      if (isTooDark(window.getComputedStyle(el).color)) {
        el.dataset.bgoTextFix = 'muted';
      }
    });
  }
  var bgoExternalRefreshTimer = 0;
  var bgoExternalRefreshFrame = 0;
  function runExternalThemeRefresh() {
    installLateSwitchStyle();
    refreshExternalToolLegend();
    refreshExternalTextHints();
    refreshExternalToolInfo();
    refreshExternalToolVersionPicker();
    refreshExternalVersionBadge();
  }
  function scheduleExternalThemeRefresh(delay) {
    if (bgoExternalRefreshTimer) return;
    bgoExternalRefreshTimer = window.setTimeout(function () {
      bgoExternalRefreshTimer = 0;
      runExternalThemeRefresh();
    }, delay == null ? 120 : delay);
  }
  function requestExternalThemeFrameRefresh() {
    if (bgoExternalRefreshFrame) return;
    if (!window.requestAnimationFrame) {
      scheduleExternalThemeRefresh(0);
      return;
    }
    bgoExternalRefreshFrame = window.requestAnimationFrame(function () {
      bgoExternalRefreshFrame = 0;
      runExternalThemeRefresh();
    });
  }
  function markToolVersionPreparingFromButton(button) {
    var groupCard = getToolGroupCardFromControl(button);
    if (!groupCard) return;
    groupCard.dataset.bgoVersionPreparing = 'true';
    hideOriginalVersionSections(groupCard);
    requestExternalThemeFrameRefresh();
    window.setTimeout(function () {
      hideOriginalVersionSections(groupCard);
      runExternalThemeRefresh();
    }, 40);
    window.setTimeout(function () {
      hideOriginalVersionSections(groupCard);
      runExternalThemeRefresh();
    }, 140);
    window.setTimeout(function () {
      hideOriginalVersionSections(groupCard);
      groupCard.dataset.bgoVersionPreparing = 'false';
    }, 700);
  }
  function getToolGroupNameFromCard(groupCard) {
    if (!groupCard) return '';
    if (groupCard.dataset && groupCard.dataset.bgoGroupName) return groupCard.dataset.bgoGroupName;
    var title = groupCard.querySelector && groupCard.querySelector(':scope h4');
    return title ? (title.textContent || '').trim() : '';
  }
  function isInstalledVersionCardFromDOM(card) {
    var text = String(card && card.textContent || '');
    return /\u5df2\u5b89\u88c5|\u5df2\u5b8c\u6210|Installed|Completed/i.test(text);
  }
  function countInstalledVersionCardsFromDOM(groupCard) {
    if (!groupCard) return 0;
    var cards = Array.prototype.slice.call(groupCard.querySelectorAll('[data-bgo-tool-version-card="true"]'));
    if (!cards.length) {
      cards = Array.prototype.slice.call(groupCard.querySelectorAll('.ant-card')).filter(function (card) {
        return card !== groupCard && getCardVersion(card);
      });
    }
    return cards.filter(isInstalledVersionCardFromDOM).length;
  }
  function shouldBlockRequiredUninstallButton(button) {
    if (!button) return false;
    var text = (button.textContent || '').trim();
    if (button.dataset && button.dataset.bgoToolAction === 'uninstall') {
      // Continue.
    } else if (text !== '\u5378\u8f7d' && text !== 'Uninstall') {
      return false;
    }
    var groupCard = getToolGroupCardFromControl(button);
    var groupName = getToolGroupNameFromCard(groupCard);
    if (!groupName || !isRequiredToolGroup(groupName)) return false;
    var versionCard = button.closest && (button.closest('[data-bgo-tool-version-card="true"]') || button.closest('.ant-card'));
    if (!versionCard || versionCard === groupCard) return false;
    var version = (versionCard.dataset && versionCard.dataset.bgoToolVersion) || getCardVersion(versionCard);
    if (!version) return false;
    if (isLastRequiredInstalledVersion(groupName, getVersionState(groupName, version))) return true;
    return isInstalledVersionCardFromDOM(versionCard) && countInstalledVersionCardsFromDOM(groupCard) <= 1;
  }
  runExternalThemeRefresh();
  window.addEventListener('click', function (event) {
    var target = event.target && event.target.closest ? event.target.closest('button,.ant-btn,[role="button"],[role="switch"],.ant-switch,.rc-switch') : null;
    if (!target) return;
    if (target.dataset && target.dataset.bgoToolToggleBlocked === 'true') {
      event.preventDefault();
      event.stopImmediatePropagation();
      return;
    }
    if (target.dataset && target.dataset.bgoToolRequiredToggleBlocked === 'true') {
      event.preventDefault();
      event.stopImmediatePropagation();
      setToolGroupSwitchChecked(target, true);
      window.alert(bgoRequiredToolDisableMessage);
      return;
    }
    if ((target.dataset && target.dataset.bgoToolRequiredLastInstalled === 'true') || shouldBlockRequiredUninstallButton(target)) {
      event.preventDefault();
      event.stopImmediatePropagation();
      showToolGuardNotice(bgoRequiredToolUninstallMessage, 'error');
      return;
    }
    var text = (target.textContent || '').trim();
    if (text.indexOf('\u5c55\u5f00\u8be6\u60c5') >= 0 || text.indexOf('View details') >= 0 || text.indexOf('\u6536\u8d77\u8be6\u60c5') >= 0 || text.indexOf('Hide details') >= 0) {
      markToolVersionPreparingFromButton(target);
    }
  }, true);
  window.addEventListener('load', function () {
    runExternalThemeRefresh();
    setTimeout(runExternalThemeRefresh, 300);
    setTimeout(runExternalThemeRefresh, 1200);
    if (window.MutationObserver) {
      var observer = new MutationObserver(function () {
        requestExternalThemeFrameRefresh();
        scheduleExternalThemeRefresh(80);
      });
      observer.observe(document.body, { childList: true, subtree: true });
    }
  });
})();
</script>
<style id="bgo-external-theme-style">
html.bgo-external-theme {
  color-scheme: light;
  background: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
  --background: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
  --foreground: var(--bgo-ext-text, var(--bgo-text)) !important;
  --card: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
  --card-foreground: var(--bgo-ext-text, var(--bgo-text)) !important;
  --popover: var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) !important;
  --popover-foreground: var(--bgo-ext-text, var(--bgo-text)) !important;
  --muted: var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) !important;
  --muted-foreground: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  --secondary: var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) !important;
  --secondary-foreground: var(--bgo-ext-text, var(--bgo-text)) !important;
  --border: var(--bgo-ext-border, var(--bgo-border)) !important;
  --input: var(--bgo-ext-border, var(--bgo-border)) !important;
  --ring: var(--bgo-accent) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] {
  color-scheme: dark;
}
html.bgo-external-theme body,
html.bgo-external-theme #root,
html.bgo-external-theme #app,
html.bgo-external-theme .app,
html.bgo-external-theme main {
  background: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme header,
html.bgo-external-theme nav,
html.bgo-external-theme aside,
html.bgo-external-theme section,
html.bgo-external-theme article,
html.bgo-external-theme .ant-layout,
html.bgo-external-theme .ant-layout-header,
html.bgo-external-theme .ant-layout-sider,
html.bgo-external-theme .ant-layout-content,
html.bgo-external-theme .ant-menu,
html.bgo-external-theme .ant-card,
html.bgo-external-theme .ant-card-head,
html.bgo-external-theme .ant-card-body,
html.bgo-external-theme .ant-table,
html.bgo-external-theme .ant-table-container,
html.bgo-external-theme .ant-table-cell,
html.bgo-external-theme .ant-list,
html.bgo-external-theme .ant-list-item,
html.bgo-external-theme .ant-tabs,
html.bgo-external-theme .ant-tabs-nav,
html.bgo-external-theme .ant-tabs-content-holder,
html.bgo-external-theme .ant-collapse,
html.bgo-external-theme .ant-collapse-item,
html.bgo-external-theme .ant-collapse-content,
html.bgo-external-theme .ant-modal-content,
html.bgo-external-theme .ant-drawer-content,
html.bgo-external-theme .ant-dropdown-menu,
html.bgo-external-theme .card,
html.bgo-external-theme .panel,
html.bgo-external-theme .container,
html.bgo-external-theme .content,
html.bgo-external-theme .page,
html.bgo-external-theme .rounded-lg,
html.bgo-external-theme .rounded-xl,
html.bgo-external-theme .shadow,
html.bgo-external-theme .shadow-sm,
html.bgo-external-theme .shadow-md,
html.bgo-external-theme [class*="rounded-lg"],
html.bgo-external-theme [class*="rounded-xl"],
html.bgo-external-theme [class*="shadow-"] {
  background-color: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme .ant-table-thead > tr > th,
html.bgo-external-theme .ant-card-head,
html.bgo-external-theme .ant-collapse-header,
html.bgo-external-theme .toolbar,
html.bgo-external-theme .header,
html.bgo-external-theme .sidebar {
  background-color: var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme input,
html.bgo-external-theme textarea,
html.bgo-external-theme select,
html.bgo-external-theme .ant-input,
html.bgo-external-theme .ant-input-number,
html.bgo-external-theme .ant-select-selector,
html.bgo-external-theme .ant-picker {
  background-color: var(--bgo-ext-input-bg, var(--bgo-panel-bg)) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme button,
html.bgo-external-theme .ant-btn {
  background-color: var(--bgo-ext-button-bg, var(--bgo-elevated-bg)) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme .ant-btn-primary,
html.bgo-external-theme button[type="submit"] {
  background-color: var(--bgo-accent) !important;
  border-color: var(--bgo-accent) !important;
  color: #fff !important;
}
html.bgo-external-theme button[role="switch"],
html.bgo-external-theme .ant-switch,
html.bgo-external-theme [data-slot="switch"],
html.bgo-external-theme [class*="SwitchRoot"] {
  position: relative !important;
  width: 44px !important;
  min-width: 44px !important;
  height: 26px !important;
  padding: 0 !important;
  border-radius: 999px !important;
  border: 0 !important;
  background: #e9e9e9 !important;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.04) !important;
  transition: .3s all ease-in-out !important;
}
html.bgo-external-theme button[role="switch"][aria-checked="true"],
html.bgo-external-theme button[role="switch"][data-state="checked"],
html.bgo-external-theme .ant-switch.ant-switch-checked,
html.bgo-external-theme [data-slot="switch"][data-state="checked"],
html.bgo-external-theme [class*="SwitchRoot"][data-state="checked"] {
  background: #34c759 !important;
}
html.bgo-external-theme button[role="switch"][aria-checked="false"],
html.bgo-external-theme button[role="switch"][data-state="unchecked"],
html.bgo-external-theme [data-slot="switch"][data-state="unchecked"],
html.bgo-external-theme [class*="SwitchRoot"][data-state="unchecked"] {
  background: #e9e9e9 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"],
html.bgo-external-theme[data-bgo-theme="dark"] .ant-switch,
html.bgo-external-theme[data-bgo-theme="dark"] [data-slot="switch"],
html.bgo-external-theme[data-bgo-theme="dark"] [class*="SwitchRoot"] {
  background: #39393d !important;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.06) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"][aria-checked="true"],
html.bgo-external-theme[data-bgo-theme="dark"] button[role="switch"][data-state="checked"],
html.bgo-external-theme[data-bgo-theme="dark"] .ant-switch.ant-switch-checked,
html.bgo-external-theme[data-bgo-theme="dark"] [data-slot="switch"][data-state="checked"],
html.bgo-external-theme[data-bgo-theme="dark"] [class*="SwitchRoot"][data-state="checked"] {
  background: #30d158 !important;
}
html.bgo-external-theme button[role="switch"] > span,
html.bgo-external-theme .ant-switch .ant-switch-handle,
html.bgo-external-theme [data-slot="switch"] > span,
html.bgo-external-theme [class*="SwitchRoot"] > span {
  position: absolute !important;
  top: 2px !important;
  left: 2px !important;
  width: 22px !important;
  height: 22px !important;
  border-radius: 999px !important;
  background: #fff !important;
  box-shadow: 2px 0 8px rgba(0, 0, 0, .16) !important;
  transform: translateX(0) !important;
  transition: .3s all ease-in-out !important;
}
html.bgo-external-theme .ant-switch .ant-switch-handle::before {
  border-radius: 999px !important;
  background: #fff !important;
  box-shadow: 2px 0 8px rgba(0, 0, 0, .16) !important;
  transition: .3s all ease-in-out !important;
}
html.bgo-external-theme button[role="switch"][aria-checked="true"] > span,
html.bgo-external-theme button[role="switch"][data-state="checked"] > span,
html.bgo-external-theme [data-slot="switch"][data-state="checked"] > span,
html.bgo-external-theme [class*="SwitchRoot"][data-state="checked"] > span {
  transform: translateX(18px) !important;
  box-shadow: -2px 0 8px rgba(0, 0, 0, .16) !important;
}
html.bgo-external-theme .ant-switch.ant-switch-checked .ant-switch-handle {
  transform: translateX(18px) !important;
}
html.bgo-external-theme .ant-switch.ant-switch-checked .ant-switch-handle::before {
  box-shadow: -2px 0 8px rgba(0, 0, 0, .16) !important;
}
html.bgo-external-theme button[role="switch"]:active > span,
html.bgo-external-theme [data-slot="switch"]:active > span,
html.bgo-external-theme [class*="SwitchRoot"]:active > span,
html.bgo-external-theme .ant-switch:active .ant-switch-handle {
  width: 28px !important;
}
html.bgo-external-theme button[role="switch"][aria-checked="true"]:active > span,
html.bgo-external-theme button[role="switch"][data-state="checked"]:active > span,
html.bgo-external-theme [data-slot="switch"][data-state="checked"]:active > span,
html.bgo-external-theme [class*="SwitchRoot"][data-state="checked"]:active > span,
html.bgo-external-theme .ant-switch.ant-switch-checked:active .ant-switch-handle {
  transform: translateX(12px) !important;
}
html.bgo-external-theme button[role="switch"]:focus-visible,
html.bgo-external-theme .ant-switch:focus-visible {
  outline: 2px solid #34c759 !important;
  outline-offset: 2px !important;
}
html.bgo-external-theme a,
html.bgo-external-theme .ant-tabs-tab-active .ant-tabs-tab-btn {
  color: var(--bgo-accent-hover) !important;
}
html.bgo-external-theme .ant-menu-item-selected,
html.bgo-external-theme .active,
html.bgo-external-theme [aria-selected="true"] {
  background-color: var(--bgo-selected-bg) !important;
  color: var(--bgo-selected-text) !important;
}
html.bgo-external-theme [data-bgo-original-version-section="hidden"] {
  display: none !important;
}
html.bgo-external-theme [data-bgo-original-version-divider="hidden"] {
  display: none !important;
}
html.bgo-external-theme [data-bgo-tool-version-wrapper="true"][data-bgo-tool-version-visible="false"] {
  display: none !important;
}
html.bgo-external-theme .bgo-tool-version-compact {
  display: flex !important;
  align-items: center !important;
  gap: 8px !important;
  flex-wrap: wrap !important;
  width: 100% !important;
  margin: 2px 0 10px !important;
  padding: 10px 12px !important;
  border: 1px solid var(--bgo-ext-border, var(--bgo-border)) !important;
  border-radius: 8px !important;
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 76%, var(--bgo-ext-page-bg, var(--bgo-page-bg))) !important;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, .04) !important;
}
html.bgo-external-theme .bgo-tool-version-label {
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
  font-weight: 700 !important;
  white-space: nowrap !important;
}
html.bgo-external-theme .bgo-tool-version-select {
  min-width: 150px !important;
  height: 30px !important;
  padding: 0 28px 0 10px !important;
  border: 1px solid var(--bgo-ext-border, var(--bgo-border)) !important;
  border-radius: 7px !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
  background: var(--bgo-ext-input-bg, var(--bgo-panel-bg)) !important;
  outline: none !important;
}
html.bgo-external-theme .bgo-tool-version-select:focus {
  border-color: var(--bgo-accent) !important;
  box-shadow: 0 0 0 2px var(--bgo-accent-soft) !important;
}
html.bgo-external-theme .bgo-tool-version-enable {
  display: inline-flex !important;
  align-items: center !important;
  justify-content: center !important;
  min-width: 58px !important;
  height: 30px !important;
  padding: 0 12px !important;
  border: 1px solid rgba(38, 170, 85, .75) !important;
  border-radius: 7px !important;
  color: #062814 !important;
  background: linear-gradient(180deg, #7ee787, #30d158) !important;
  box-shadow: 0 6px 14px rgba(0, 0, 0, .14) !important;
  font-size: 12px !important;
  font-weight: 800 !important;
  cursor: pointer !important;
  transition: transform .16s ease, opacity .16s ease, box-shadow .16s ease !important;
  text-shadow: none !important;
}
html.bgo-external-theme .bgo-tool-version-enable:hover {
  transform: translateY(-1px) !important;
  box-shadow: 0 8px 18px rgba(0, 0, 0, .18) !important;
}
html.bgo-external-theme .bgo-tool-version-enable:disabled {
  opacity: .62 !important;
  cursor: not-allowed !important;
  transform: none !important;
}
html.bgo-external-theme .bgo-tool-version-enable[aria-hidden="true"] {
  display: none !important;
}
html.bgo-external-theme .bgo-tool-version-hint {
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  font-size: 12px !important;
}
html.bgo-external-theme .bgo-tool-version-current {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 20px !important;
  padding: 1px 7px !important;
  border: 1px solid rgba(96, 165, 250, .34) !important;
  border-radius: 6px !important;
  color: #bfdbfe !important;
  background: rgba(96, 165, 250, .15) !important;
  font-weight: 700 !important;
}
html.bgo-external-theme .bgo-tool-version-state {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 20px !important;
  padding: 1px 7px !important;
  border-radius: 6px !important;
  border: 1px solid transparent !important;
  font-weight: 700 !important;
}
html.bgo-external-theme .bgo-tool-required-uninstall-notice {
  display: inline-flex !important;
  align-items: center !important;
  max-width: min(100%, 680px) !important;
  min-height: 24px !important;
  margin-left: 8px !important;
  padding: 3px 8px !important;
  border-radius: 6px !important;
  border: 1px solid rgba(246, 193, 119, .34) !important;
  color: #b7791f !important;
  background: rgba(246, 193, 119, .13) !important;
  font-size: 12px !important;
  font-weight: 600 !important;
  line-height: 1.45 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] .bgo-tool-required-uninstall-notice {
  color: #f6c177 !important;
  background: rgba(246, 193, 119, .15) !important;
  border-color: rgba(246, 193, 119, .34) !important;
}
html.bgo-external-theme .bgo-tool-empty-notice {
  display: flex !important;
  align-items: flex-start !important;
  max-width: min(100%, 760px) !important;
  margin: 0 0 12px !important;
  padding: 9px 11px !important;
  border-radius: 8px !important;
  border: 1px solid rgba(148, 163, 184, .28) !important;
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 80%, transparent) !important;
  font-size: 12px !important;
  font-weight: 600 !important;
  line-height: 1.55 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] .bgo-tool-empty-notice {
  color: #c7d0da !important;
  background: rgba(148, 163, 184, .10) !important;
  border-color: rgba(148, 163, 184, .24) !important;
}
html.bgo-external-theme .bgo-tool-legend {
  display: grid !important;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)) !important;
  gap: 8px !important;
  margin: 12px 0 18px !important;
  padding: 12px !important;
  border: 1px solid var(--bgo-ext-border, var(--bgo-border)) !important;
  border-radius: 8px !important;
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 70%, transparent) !important;
}
html.bgo-external-theme .bgo-tool-legend-item {
  display: flex !important;
  align-items: flex-start !important;
  gap: 8px !important;
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  font-size: 12px !important;
  line-height: 1.55 !important;
}
html.bgo-external-theme .bgo-tool-info {
  margin: 0 0 12px !important;
  padding: 10px 12px !important;
  border: 1px solid var(--bgo-ext-border, var(--bgo-border)) !important;
  border-radius: 8px !important;
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 72%, var(--bgo-ext-panel-bg, var(--bgo-panel-bg))) !important;
}
html.bgo-external-theme .bgo-tool-info-head {
  display: flex !important;
  align-items: center !important;
  gap: 8px !important;
  flex-wrap: wrap !important;
  margin-bottom: 6px !important;
}
html.bgo-external-theme .bgo-tool-info-title {
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
  font-size: 13px !important;
  line-height: 1.35 !important;
}
html.bgo-external-theme .bgo-tool-info-desc {
  margin: 0 !important;
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  font-size: 12px !important;
  line-height: 1.55 !important;
}
html.bgo-external-theme .bgo-tool-role-badge {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 20px !important;
  padding: 2px 7px !important;
  border-radius: 6px !important;
  border: 1px solid transparent !important;
  font-size: 12px !important;
  font-weight: 700 !important;
  line-height: 1.25 !important;
  white-space: nowrap !important;
}
html.bgo-external-theme .bgo-tool-role-badge[data-bgo-tool-role="required"] {
  color: #7ee787 !important;
  background: rgba(52, 199, 89, .14) !important;
  border-color: rgba(52, 199, 89, .34) !important;
}
html.bgo-external-theme .bgo-tool-role-badge[data-bgo-tool-role="recommended"] {
  color: #f6c177 !important;
  background: rgba(246, 193, 119, .15) !important;
  border-color: rgba(246, 193, 119, .34) !important;
}
html.bgo-external-theme .bgo-tool-role-badge[data-bgo-tool-role="optional"] {
  color: #bfdbfe !important;
  background: rgba(96, 165, 250, .15) !important;
  border-color: rgba(96, 165, 250, .34) !important;
}
html.bgo-external-theme .bgo-tool-role-badge[data-bgo-tool-role="internal"] {
  color: #d2a8ff !important;
  background: rgba(210, 168, 255, .14) !important;
  border-color: rgba(210, 168, 255, .32) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  h1,
  h2,
  h3,
  h4,
  h5,
  h6,
  p,
  label,
  strong,
  small,
  dt,
  dd,
  li,
  th,
  td
) {
  color: var(--bgo-ext-text) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  span,
  div
) {
  border-color: var(--bgo-ext-border, var(--bgo-border));
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-text-fix="muted"] {
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
}
html.bgo-external-theme [data-bgo-tool-status] {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 22px !important;
  padding: 2px 8px !important;
  border-radius: 6px !important;
  font-size: 13px !important;
  font-weight: 700 !important;
  line-height: 1.25 !important;
  word-break: keep-all !important;
  white-space: nowrap !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status] {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 22px !important;
  padding: 2px 8px !important;
  border-radius: 6px !important;
  border: 1px solid transparent !important;
  font-size: 13px !important;
  font-weight: 700 !important;
  line-height: 1.25 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="enabled"],
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="installed"] {
  color: #7ee787 !important;
  background: rgba(52, 199, 89, .13) !important;
  border-color: rgba(52, 199, 89, .32) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="missing"] {
  color: #d7c48d !important;
  background: rgba(215, 196, 141, .14) !important;
  border-color: rgba(215, 196, 141, .34) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="failed"] {
  color: #ff9aa2 !important;
  background: rgba(255, 119, 125, .14) !important;
  border-color: rgba(255, 119, 125, .34) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="downloading"] {
  color: #bfdbfe !important;
  background: rgba(96, 165, 250, .15) !important;
  border-color: rgba(96, 165, 250, .34) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="paused"] {
  color: #d2a8ff !important;
  background: rgba(210, 168, 255, .14) !important;
  border-color: rgba(210, 168, 255, .32) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="internal"] {
  color: #d2a8ff !important;
  background: rgba(210, 168, 255, .14) !important;
  border-color: rgba(210, 168, 255, .32) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="disabled"] {
  color: #c7d0da !important;
  background: rgba(148, 163, 184, .15) !important;
  border-color: rgba(148, 163, 184, .32) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="platform"] {
  color: #c7d2fe !important;
  background: rgba(129, 140, 248, .16) !important;
  border-color: rgba(129, 140, 248, .34) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="default-version"],
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="version"] {
  color: #bfdbfe !important;
  background: rgba(96, 165, 250, .15) !important;
  border-color: rgba(96, 165, 250, .34) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="empty"] {
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  background: rgba(148, 163, 184, .10) !important;
  border-color: rgba(148, 163, 184, .22) !important;
}
html.bgo-external-theme .bgo-tool-guard-notice {
  position: fixed !important;
  top: 18px !important;
  left: 50% !important;
  z-index: 2147483000 !important;
  max-width: min(720px, calc(100vw - 32px)) !important;
  padding: 10px 14px !important;
  border-radius: 10px !important;
  border: 1px solid rgba(255, 120, 117, .34) !important;
  background: color-mix(in srgb, #fff 90%, #fff2f0) !important;
  color: #a8071a !important;
  box-shadow: 0 16px 42px rgba(15, 23, 42, .18) !important;
  font-size: 13px !important;
  font-weight: 700 !important;
  line-height: 1.55 !important;
  transform: translate(-50%, -10px) !important;
  opacity: 0 !important;
  pointer-events: none !important;
  transition: opacity .18s ease, transform .18s ease !important;
  backdrop-filter: blur(16px) saturate(1.25) !important;
  -webkit-backdrop-filter: blur(16px) saturate(1.25) !important;
}
html.bgo-external-theme .bgo-tool-guard-notice[data-bgo-visible="true"] {
  transform: translate(-50%, 0) !important;
  opacity: 1 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] .bgo-tool-guard-notice {
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 88%, rgba(255, 120, 117, .16)) !important;
  color: #ffb4ab !important;
  border-color: rgba(255, 120, 117, .36) !important;
  box-shadow: 0 18px 48px rgba(0, 0, 0, .32) !important;
}
html.bgo-external-theme .bgo-external-version-badge {
  position: fixed !important;
  left: 18px !important;
  bottom: 18px !important;
  z-index: 1200 !important;
  min-width: 64px !important;
  height: 30px !important;
  padding: 0 12px !important;
  display: inline-flex !important;
  align-items: center !important;
  justify-content: center !important;
  border: 1px solid color-mix(in srgb, var(--bgo-ext-border, var(--bgo-border)) 72%, transparent) !important;
  border-radius: 999px !important;
  background: color-mix(in srgb, var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) 82%, transparent) !important;
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
  font-size: 12px !important;
  font-weight: 700 !important;
  line-height: 1 !important;
  letter-spacing: 0 !important;
  box-shadow: 0 10px 26px rgba(0, 0, 0, .14) !important;
  backdrop-filter: blur(16px) saturate(1.35) !important;
  -webkit-backdrop-filter: blur(16px) saturate(1.35) !important;
  pointer-events: none !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-green-500,
  .text-green-600,
  .text-emerald-500,
  .text-emerald-600,
  [class*="text-green-"],
  [class*="text-emerald-"]
) {
  color: #7ee787 !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-red-500,
  .text-red-600,
  [class*="text-red-"]
) {
  color: #ff9b9b !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-blue-500,
  .text-blue-600,
  [class*="text-blue-"]
) {
  color: #8fc7ff !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-green-50,
  .bg-green-100,
  .bg-emerald-50,
  .bg-emerald-100,
  [class*="bg-green-50"],
  [class*="bg-green-100"],
  [class*="bg-emerald-50"],
  [class*="bg-emerald-100"]
) {
  background-color: rgba(52, 199, 89, 0.16) !important;
  border-color: rgba(52, 199, 89, 0.38) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-blue-50,
  .bg-blue-100,
  .bg-sky-50,
  .bg-sky-100,
  [class*="bg-blue-50"],
  [class*="bg-blue-100"],
  [class*="bg-sky-50"],
  [class*="bg-sky-100"]
) {
  background-color: rgba(96, 165, 250, 0.16) !important;
  border-color: rgba(96, 165, 250, 0.36) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-red-50,
  .bg-red-100,
  [class*="bg-red-50"],
  [class*="bg-red-100"]
) {
  background-color: rgba(248, 113, 113, 0.15) !important;
  border-color: rgba(248, 113, 113, 0.34) !important;
}
html.bgo-external-theme :where(
  .text-2xl,
  .text-3xl,
  .text-4xl,
  .text-green-500,
  .text-green-600,
  .text-emerald-500,
  .text-emerald-600,
  [class*="text-2xl"],
  [class*="text-3xl"],
  [class*="text-4xl"],
  [class*="text-green-"],
  [class*="text-emerald-"]
) {
  white-space: nowrap !important;
  word-break: keep-all !important;
  overflow-wrap: normal !important;
}
html.bgo-external-theme :where(
  .text-2xl,
  .text-3xl,
  .text-4xl,
  [class*="text-2xl"],
  [class*="text-3xl"],
  [class*="text-4xl"]
) {
  min-width: max-content;
}
html.bgo-external-theme .ant-divider,
html.bgo-external-theme hr {
  border-color: var(--bgo-border) !important;
}
html.bgo-external-theme [style*="background: #fff"],
html.bgo-external-theme [style*="background:#fff"],
html.bgo-external-theme [style*="background-color: #fff"],
html.bgo-external-theme [style*="background: white"],
html.bgo-external-theme [style*="background-color: white"],
html.bgo-external-theme [style*="background: rgb(255, 255, 255)"],
html.bgo-external-theme [style*="background-color: rgb(255, 255, 255)"] {
  background: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
  background-color: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #111"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#111"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #222"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#222"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #333"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#333"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(0, 0, 0)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(17, 17, 17)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(24, 24, 27)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(31, 41, 55)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(34, 34, 34)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(51, 51, 51)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgba(0, 0, 0"] {
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #fff"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#fff"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: white"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(255, 255, 255)"] {
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background: #000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background:#000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background-color: #000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background: black"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background-color: black"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background: rgb(0, 0, 0)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="background-color: rgb(0, 0, 0)"] {
  background: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
  background-color: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-black,
  .bg-gray-950,
  .bg-gray-900,
  .bg-gray-800,
  .bg-slate-950,
  .bg-slate-900,
  .bg-slate-800,
  .bg-zinc-950,
  .bg-zinc-900,
  .bg-zinc-800,
  .bg-neutral-950,
  .bg-neutral-900,
  .bg-neutral-800,
  .bg-stone-950,
  .bg-stone-900,
  .bg-stone-800,
  [class*="bg-black"],
  [class*="bg-gray-950"],
  [class*="bg-gray-900"],
  [class*="bg-gray-800"],
  [class*="bg-slate-950"],
  [class*="bg-slate-900"],
  [class*="bg-slate-800"],
  [class*="bg-zinc-950"],
  [class*="bg-zinc-900"],
  [class*="bg-zinc-800"],
  [class*="bg-neutral-950"],
  [class*="bg-neutral-900"],
  [class*="bg-neutral-800"],
  [class*="bg-stone-950"],
  [class*="bg-stone-900"],
  [class*="bg-stone-800"]
) {
  background-color: var(--bgo-ext-page-bg, var(--bgo-page-bg)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-white,
  .bg-gray-50,
  .bg-gray-100,
  .bg-slate-50,
  .bg-slate-100,
  .bg-zinc-50,
  .bg-zinc-100,
  .bg-neutral-50,
  .bg-neutral-100,
  .bg-stone-50,
  .bg-stone-100,
  .bg-background,
  .bg-card,
  .bg-popover,
  .bg-primary-foreground,
  .bg-secondary,
  .bg-muted,
  [class*="bg-white"],
  [class*="bg-gray-50"],
  [class*="bg-gray-100"],
  [class*="bg-slate-50"],
  [class*="bg-slate-100"],
  [class*="bg-zinc-50"],
  [class*="bg-zinc-100"],
  [class*="bg-neutral-50"],
  [class*="bg-neutral-100"],
  [class*="bg-background"],
  [class*="bg-card"],
  [class*="bg-popover"],
  [class*="bg-primary-foreground"],
  [class*="bg-secondary"],
  [class*="bg-muted"]
) {
  background-color: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-white,
  .bg-gray-50,
  .bg-gray-100,
  .bg-slate-50,
  .bg-slate-100,
  .bg-zinc-50,
  .bg-zinc-100,
  .bg-neutral-50,
  .bg-neutral-100,
  .bg-card,
  .bg-background,
  [class*="bg-white"],
  [class*="bg-gray-50"],
  [class*="bg-gray-100"],
  [class*="bg-slate-50"],
  [class*="bg-slate-100"],
  [class*="bg-zinc-50"],
  [class*="bg-zinc-100"],
  [class*="bg-neutral-50"],
  [class*="bg-neutral-100"],
  [class*="bg-card"],
  [class*="bg-background"]
) {
  background-color: var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-gray-200,
  .bg-slate-200,
  .bg-zinc-200,
  .bg-neutral-200,
  .bg-stone-200,
  .bg-gray-700,
  .bg-slate-700,
  .bg-zinc-700,
  .bg-neutral-700,
  .bg-stone-700,
  .bg-muted,
  .bg-secondary,
  [class*="bg-gray-200"],
  [class*="bg-slate-200"],
  [class*="bg-zinc-200"],
  [class*="bg-neutral-200"],
  [class*="bg-gray-700"],
  [class*="bg-slate-700"],
  [class*="bg-zinc-700"],
  [class*="bg-neutral-700"],
  [class*="bg-stone-700"],
  [class*="bg-muted"],
  [class*="bg-secondary"]
) {
  background-color: var(--bgo-ext-elevated-bg, var(--bgo-elevated-bg)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-gray-200,
  .bg-slate-200,
  .bg-zinc-200,
  .bg-neutral-200,
  .bg-secondary,
  .bg-muted,
  [class*="bg-gray-200"],
  [class*="bg-slate-200"],
  [class*="bg-zinc-200"],
  [class*="bg-neutral-200"],
  [class*="bg-secondary"],
  [class*="bg-muted"]
) {
  background-color: color-mix(in srgb, var(--bgo-ext-panel-bg, var(--bgo-panel-bg)) 82%, var(--bgo-ext-page-bg, var(--bgo-page-bg))) !important;
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-black,
  .text-white,
  .text-gray-950,
  .text-gray-900,
  .text-gray-800,
  .text-slate-950,
  .text-slate-900,
  .text-slate-800,
  .text-zinc-950,
  .text-zinc-900,
  .text-zinc-800,
  .text-neutral-950,
  .text-neutral-900,
  .text-neutral-800,
  .text-stone-950,
  .text-stone-900,
  .text-stone-800,
  .text-foreground,
  .text-card-foreground,
  .text-popover-foreground,
  [class*="text-black"],
  [class*="text-white"],
  [class*="text-gray-950"],
  [class*="text-gray-900"],
  [class*="text-gray-800"],
  [class*="text-slate-950"],
  [class*="text-slate-900"],
  [class*="text-slate-800"],
  [class*="text-zinc-950"],
  [class*="text-zinc-900"],
  [class*="text-zinc-800"],
  [class*="text-neutral-950"],
  [class*="text-neutral-900"],
  [class*="text-neutral-800"],
  [class*="text-foreground"],
  [class*="text-card-foreground"],
  [class*="text-popover-foreground"]
) {
  color: var(--bgo-ext-text, var(--bgo-text)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-gray-700,
  .text-gray-600,
  .text-gray-500,
  .text-gray-400,
  .text-slate-700,
  .text-slate-600,
  .text-slate-500,
  .text-slate-400,
  .text-zinc-700,
  .text-zinc-600,
  .text-zinc-500,
  .text-zinc-400,
  .text-neutral-700,
  .text-neutral-600,
  .text-neutral-500,
  .text-neutral-400,
  .text-muted-foreground,
  .text-muted,
  .text-secondary,
  .muted,
  .subtext,
  .description,
  [class*="text-gray-700"],
  [class*="text-gray-600"],
  [class*="text-gray-500"],
  [class*="text-gray-400"],
  [class*="text-slate-700"],
  [class*="text-slate-600"],
  [class*="text-slate-500"],
  [class*="text-slate-400"],
  [class*="text-zinc-700"],
  [class*="text-zinc-600"],
  [class*="text-zinc-500"],
  [class*="text-zinc-400"],
  [class*="text-neutral-700"],
  [class*="text-neutral-600"],
  [class*="text-neutral-500"],
  [class*="text-neutral-400"],
  [class*="text-muted-foreground"]
) {
  color: var(--bgo-ext-muted, var(--bgo-muted)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .border,
  .border-gray-100,
  .border-gray-200,
  .border-gray-300,
  .border-slate-100,
  .border-slate-200,
  .border-slate-300,
  .border-zinc-100,
  .border-zinc-200,
  .border-zinc-300,
  .border-neutral-100,
  .border-neutral-200,
  .border-neutral-300,
  .border-border,
  .border-input,
  [class*="border-gray-"],
  [class*="border-slate-"],
  [class*="border-zinc-"],
  [class*="border-neutral-"],
  [class*="border-border"],
  [class*="border-input"]
) {
  border-color: var(--bgo-ext-border, var(--bgo-border)) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .shadow,
  .shadow-sm,
  .shadow-md,
  .shadow-lg,
  [class*="shadow-"]
) {
  box-shadow: 0 12px 30px rgba(0, 0, 0, 0.26) !important;
}
html.bgo-external-theme[data-bgo-theme="light"] :where(
  header,
  nav,
  aside,
  .navbar,
  .topbar,
  .sidebar,
  .menu
) :where(.text-white, [class*="text-white"]) {
  color: var(--bgo-text) !important;
}
html.bgo-external-theme :where(
  .card,
  .panel,
  .rounded-lg,
  .rounded-xl,
  .ant-card,
  .ant-modal-content,
  .ant-popover-inner
) :where(h1, h2, h3, h4, h5, h6, p, span, div, label, strong, small) {
  color: inherit;
}
</style>
`
