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
        return nativeFetch.call(this, input, init);
      };
    }

    if (window.XMLHttpRequest && window.XMLHttpRequest.prototype && window.XMLHttpRequest.prototype.open) {
      var nativeOpen = window.XMLHttpRequest.prototype.open;
      window.XMLHttpRequest.prototype.open = function (method, url) {
        arguments[1] = rewriteURL(url);
        return nativeOpen.apply(this, arguments);
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
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status]{display:inline-flex!important;align-items:center!important;min-height:18px!important;padding:1px 6px!important;border-radius:5px!important;border:1px solid transparent!important;font-weight:600!important;line-height:1.35!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="enabled"],html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="installed"]{color:#7ee787!important;background:rgba(52,199,89,.13)!important;border-color:rgba(52,199,89,.32)!important;}',
      'html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status="missing"]{color:#d7c48d!important;background:rgba(215,196,141,.14)!important;border-color:rgba(215,196,141,.34)!important;}',
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
  function normalizeVersionList(versions) {
    return Array.prototype.slice.call(versions || []).filter(Boolean).sort(compactToolVersionCompare);
  }
  function rememberToolVersionMeta(groups) {
    var next = {};
    (groups || []).forEach(function (group) {
      if (!group || !group.name || !Array.isArray(group.tools)) return;
      var rawVersions = group.tools.map(function (tool) { return tool && tool.version; }).filter(Boolean);
      if (rawVersions.length <= 1) return;
      var enabledTool = group.tools.find(function (tool) { return tool && tool.enabled; });
      next[group.name] = {
        versions: normalizeVersionList(rawVersions),
        pinnedVersion: group.pinnedVersion || '',
        defaultVersion: group.pinnedVersion || (enabledTool && enabledTool.version) || rawVersions[0] || ''
      };
    });
    bgoToolVersionMeta = next;
    bgoToolVersionMetaLoaded = true;
    markToolVersionGroups();
  }
  function loadToolVersionMeta() {
    if (bgoToolVersionMetaLoaded || bgoToolVersionMetaLoading) return;
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
      var meta = bgoToolVersionMeta[groupName];
      var groupCard = title.closest('.ant-card');
      if (!groupCard || !meta || !meta.versions || meta.versions.length <= 1) return;
      groupCard.dataset.bgoHasMultipleVersions = 'true';
      groupCard.dataset.bgoGroupName = groupName;
      if (!groupCard.dataset.bgoSelectedToolVersion && meta.defaultVersion) {
        groupCard.dataset.bgoSelectedToolVersion = meta.defaultVersion;
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
  function hideOriginalVersionSections(groupCard) {
    var labels = [
      '\u9501\u5b9a\u7248\u672c', '\u5df2\u5b89\u88c5\u7248\u672c', '\u5f85\u4e0b\u8f7d\u7248\u672c',
      '\u4e0b\u8f7d\u4e2d\u7248\u672c', '\u5df2\u6682\u505c\u7248\u672c',
      'Pinned Version', 'Installed Versions', 'Pending Versions', 'Downloading Versions', 'Paused Versions'
    ];
    Array.prototype.slice.call(groupCard.querySelectorAll('section')).forEach(function (section) {
      var text = section.textContent || '';
      for (var i = 0; i < labels.length; i++) {
        if (text.indexOf(labels[i]) >= 0) {
          section.dataset.bgoOriginalVersionSection = 'hidden';
          return;
        }
      }
    });
  }
  function createToolVersionCompact(groupName, versions, selectedVersion, onChange) {
    var box = document.createElement('div');
    box.className = 'bgo-tool-version-compact';
    var label = document.createElement('label');
    label.className = 'bgo-tool-version-label';
    label.textContent = '\u9ed8\u8ba4\u7248\u672c';
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
    var hint = document.createElement('span');
    hint.className = 'bgo-tool-version-hint';
    hint.textContent = '\u5207\u6362\u540e\u4f1a\u7acb\u5373\u4fdd\u5b58\u4e3a\u8be5\u5de5\u5177\u7ec4\u9ed8\u8ba4\u7248\u672c\uff0c\u5b89\u88c5\u6216\u542f\u52a8\u65f6\u4f7f\u7528\u6240\u9009\u7248\u672c';
    box.appendChild(label);
    box.appendChild(select);
    box.appendChild(hint);
    return box;
  }
  function syncToolPinnedVersion(toolName, version) {
    if (!toolName || !version) return;
    var endpoint = new URL('./api/pin-version', window.location.href).toString();
    fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ toolName: toolName, version: version })
    }).catch(function () {});
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
      var meta = bgoToolVersionMeta[groupName];
      var cards = Array.prototype.slice.call(groupCard.querySelectorAll('.ant-card')).filter(function (card) {
        return card !== groupCard && getCardVersion(card);
      });
      if (cards.length <= 1) {
        if (meta && meta.versions && meta.versions.length > 1) {
          groupCard.dataset.bgoHasMultipleVersions = 'true';
          groupCard.dataset.bgoVersionPickerReady = 'false';
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
        var selectedVersion = groupCard.dataset.bgoSelectedToolVersion || getPinnedVersionFromGroup(groupCard) || (meta && (meta.pinnedVersion || meta.defaultVersion)) || versions[0];
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
            wrapper.dataset.bgoToolVersionWrapper = 'true';
            wrapper.dataset.bgoToolVersionVisible = card.dataset.bgoToolVersion === version ? 'true' : 'false';
            wrapper.style.display = card.dataset.bgoToolVersion === version ? '' : 'none';
          });
          var current = groupCard.querySelector(':scope .bgo-tool-version-current');
          if (current) current.textContent = version;
          if (shouldSync) syncToolPinnedVersion(groupName, version);
        };
        if (!compact) {
          compact = createToolVersionCompact(groupName, versions, selectedVersion, function (version) {
            applySelection(version, true);
          });
          var current = document.createElement('span');
          current.className = 'bgo-tool-version-current';
          current.textContent = selectedVersion;
          compact.appendChild(current);
          container.insertBefore(compact, firstWrapper);
        } else {
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
        }
        applySelection(selectedVersion, false);
        groupCard.dataset.bgoVersionPickerReady = 'true';
      } finally {
        groupCard.dataset.bgoVersionPickerBusy = '0';
      }
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
  function runExternalThemeRefresh() {
    installLateSwitchStyle();
    refreshExternalTextHints();
    refreshExternalToolVersionPicker();
  }
  function scheduleExternalThemeRefresh(delay) {
    if (bgoExternalRefreshTimer) return;
    bgoExternalRefreshTimer = window.setTimeout(function () {
      bgoExternalRefreshTimer = 0;
      runExternalThemeRefresh();
    }, delay == null ? 120 : delay);
  }
  runExternalThemeRefresh();
  window.addEventListener('click', function (event) {
    var target = event.target && event.target.closest ? event.target.closest('button,.ant-btn,[role="button"]') : null;
    if (!target) return;
    var text = (target.textContent || '').trim();
    if (text.indexOf('\u5c55\u5f00\u8be6\u60c5') >= 0 || text.indexOf('View details') >= 0 || text.indexOf('\u6536\u8d77\u8be6\u60c5') >= 0 || text.indexOf('Hide details') >= 0) {
      scheduleExternalThemeRefresh(220);
    }
  }, true);
  window.addEventListener('load', function () {
    runExternalThemeRefresh();
    setTimeout(runExternalThemeRefresh, 300);
    setTimeout(runExternalThemeRefresh, 1200);
    if (window.MutationObserver) {
      var observer = new MutationObserver(function () {
        scheduleExternalThemeRefresh(180);
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
html.bgo-external-theme .ant-card[data-bgo-has-multiple-versions="true"]:not([data-bgo-version-picker-ready="true"]) .ant-card {
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
html.bgo-external-theme[data-bgo-theme="dark"] [data-bgo-tool-status] {
  display: inline-flex !important;
  align-items: center !important;
  min-height: 18px !important;
  padding: 1px 6px !important;
  border-radius: 5px !important;
  border: 1px solid transparent !important;
  font-weight: 600 !important;
  line-height: 1.35 !important;
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
