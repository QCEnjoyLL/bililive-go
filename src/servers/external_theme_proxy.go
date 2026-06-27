package servers

import (
	"bytes"
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
    root.classList.add('bgo-external-theme');
    root.dataset.bgoTheme = resolved;
    root.dataset.bgoThemeMode = mode;
    root.dataset.bgoPalette = settings.themePalette || 'one';
    Object.keys(colors).forEach(function (key) {
      root.style.setProperty('--bgo-' + key.replace(/[A-Z]/g, function (m) { return '-' + m.toLowerCase(); }), colors[key]);
    });
  }
  applyTheme();
  window.addEventListener('storage', function (event) {
    if (event.key === KEY) applyTheme();
  });
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
  }
})();
</script>
<style id="bgo-external-theme-style">
html.bgo-external-theme {
  color-scheme: light;
  background: var(--bgo-page-bg) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] {
  color-scheme: dark;
}
html.bgo-external-theme body,
html.bgo-external-theme #root,
html.bgo-external-theme #app,
html.bgo-external-theme .app,
html.bgo-external-theme main {
  background: var(--bgo-page-bg) !important;
  color: var(--bgo-text) !important;
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
html.bgo-external-theme .page {
  background-color: var(--bgo-panel-bg) !important;
  border-color: var(--bgo-border) !important;
  color: var(--bgo-text) !important;
}
html.bgo-external-theme .ant-table-thead > tr > th,
html.bgo-external-theme .ant-card-head,
html.bgo-external-theme .ant-collapse-header,
html.bgo-external-theme .toolbar,
html.bgo-external-theme .header,
html.bgo-external-theme .sidebar {
  background-color: var(--bgo-elevated-bg) !important;
  border-color: var(--bgo-border) !important;
  color: var(--bgo-text) !important;
}
html.bgo-external-theme input,
html.bgo-external-theme textarea,
html.bgo-external-theme select,
html.bgo-external-theme button,
html.bgo-external-theme .ant-input,
html.bgo-external-theme .ant-input-number,
html.bgo-external-theme .ant-select-selector,
html.bgo-external-theme .ant-picker,
html.bgo-external-theme .ant-btn {
  background-color: var(--bgo-elevated-bg) !important;
  border-color: var(--bgo-border) !important;
  color: var(--bgo-text) !important;
}
html.bgo-external-theme .ant-btn-primary,
html.bgo-external-theme button[type="submit"] {
  background-color: var(--bgo-accent) !important;
  border-color: var(--bgo-accent) !important;
  color: #fff !important;
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
  background: var(--bgo-panel-bg) !important;
  background-color: var(--bgo-panel-bg) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: #000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color:#000"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgb(0, 0, 0)"],
html.bgo-external-theme[data-bgo-theme="dark"] [style*="color: rgba(0, 0, 0"] {
  color: var(--bgo-text) !important;
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
  [class*="bg-popover"]
) {
  background-color: var(--bgo-panel-bg) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .bg-gray-200,
  .bg-slate-200,
  .bg-zinc-200,
  .bg-neutral-200,
  .bg-stone-200,
  .bg-muted,
  .bg-secondary,
  [class*="bg-gray-200"],
  [class*="bg-slate-200"],
  [class*="bg-zinc-200"],
  [class*="bg-neutral-200"],
  [class*="bg-muted"],
  [class*="bg-secondary"]
) {
  background-color: var(--bgo-elevated-bg) !important;
}
html.bgo-external-theme[data-bgo-theme="dark"] :where(
  .text-black,
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
  color: var(--bgo-text) !important;
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
  color: var(--bgo-muted) !important;
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
  border-color: var(--bgo-border) !important;
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
