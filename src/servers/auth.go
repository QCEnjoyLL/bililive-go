package servers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/bililive-go/bililive-go/src/configs"
)

const (
	webAuthCookieName = "bililive_go_session"
	webAuthSessionTTL = 7 * 24 * time.Hour
)

type webAuthSession struct {
	username string
	expires  time.Time
}

type webAuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]webAuthSession
}

var webAuthSessions = &webAuthSessionStore{sessions: map[string]webAuthSession{}}

func (s *webAuthSessionStore) create(username string) (string, time.Time, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	expires := time.Now().Add(webAuthSessionTTL)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.sessions[token] = webAuthSession{username: username, expires: expires}
	return token, expires, nil
}

func (s *webAuthSessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok {
		return false
	}
	if now.After(session.expires) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *webAuthSessionStore) delete(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *webAuthSessionStore) cleanupLocked(now time.Time) {
	for token, session := range s.sessions {
		if now.After(session.expires) {
			delete(s.sessions, token)
		}
	}
}

func webAuthMiddleware(auth configs.RPCAuth) mux.MiddlewareFunc {
	if !auth.Enable {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isWebAuthPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if hasValidWebAuth(r, auth) {
				next.ServeHTTP(w, r)
				return
			}
			if shouldRedirectToLogin(r) {
				target := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			writeAuthJSON(w, http.StatusUnauthorized, commonResp{
				ErrNo:  http.StatusUnauthorized,
				ErrMsg: "请先登录 WebUI",
			})
		})
	}
}

func isWebAuthPublicPath(path string) bool {
	switch path {
	case "/login", "/api/auth/login", "/api/auth/logout":
		return true
	default:
		return false
	}
}

func hasValidWebAuth(r *http.Request, auth configs.RPCAuth) bool {
	if cookie, err := r.Cookie(webAuthCookieName); err == nil && webAuthSessions.valid(cookie.Value) {
		return true
	}
	user, pass, ok := r.BasicAuth()
	return ok && validWebAuthCredentials(auth, user, pass)
}

func validWebAuthCredentials(auth configs.RPCAuth, username, password string) bool {
	userHash := sha256.Sum256([]byte(strings.TrimSpace(username)))
	expectedUserHash := sha256.Sum256([]byte(auth.Username))
	passHash := sha256.Sum256([]byte(password))
	expectedPassHash := sha256.Sum256([]byte(auth.Password))
	userOK := subtle.ConstantTimeCompare(userHash[:], expectedUserHash[:]) == 1
	passOK := subtle.ConstantTimeCompare(passHash[:], expectedPassHash[:]) == 1
	return userOK && passOK
}

func shouldRedirectToLogin(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, apiRouterPrefix+"/") {
		return false
	}
	if r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		return true
	}
	accept := r.Header.Get("Accept")
	return accept == "" || strings.Contains(accept, "text/html")
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	auth := configs.GetCurrentConfig().RPC.Auth
	if !auth.Enable || hasValidWebAuth(r, auth) {
		http.Redirect(w, r, sanitizeAuthNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	w.Header().Set(contentType, "text/html; charset=utf-8")
	_, _ = io.WriteString(w, webAuthLoginHTML)
}

func loginWebUI(w http.ResponseWriter, r *http.Request) {
	auth := configs.GetCurrentConfig().RPC.Auth
	if !auth.Enable {
		writeAuthJSON(w, http.StatusOK, commonResp{Data: map[string]any{"redirect": "/"}})
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Next     string `json:"next"`
	}
	if strings.Contains(r.Header.Get(contentType), "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeAuthJSON(w, http.StatusBadRequest, commonResp{
				ErrNo:  http.StatusBadRequest,
				ErrMsg: "请求体格式错误",
			})
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeAuthJSON(w, http.StatusBadRequest, commonResp{
				ErrNo:  http.StatusBadRequest,
				ErrMsg: "请求体格式错误",
			})
			return
		}
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
		req.Next = r.FormValue("next")
	}

	if !validWebAuthCredentials(auth, req.Username, req.Password) {
		writeAuthJSON(w, http.StatusUnauthorized, commonResp{
			ErrNo:  http.StatusUnauthorized,
			ErrMsg: "用户名或密码不正确",
		})
		return
	}

	token, expires, err := webAuthSessions.create(strings.TrimSpace(req.Username))
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, commonResp{
			ErrNo:  http.StatusInternalServerError,
			ErrMsg: "创建登录会话失败",
		})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webAuthCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(webAuthSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
	writeAuthJSON(w, http.StatusOK, commonResp{
		Data: map[string]any{"redirect": sanitizeAuthNext(req.Next)},
	})
}

func logoutWebUI(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(webAuthCookieName); err == nil {
		webAuthSessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webAuthCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
	writeAuthJSON(w, http.StatusOK, commonResp{Data: "OK"})
}

func sanitizeAuthNext(next string) string {
	if strings.TrimSpace(next) == "" || strings.HasPrefix(next, "//") {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || u.Path == "/login" {
		return "/"
	}
	return u.RequestURI()
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeAuthJSON(w http.ResponseWriter, status int, resp commonResp) {
	w.Header().Set(contentType, contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

const webAuthLoginHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>BiliLive-go 登录</title>
  <script>
    (function () {
      try {
        var raw = localStorage.getItem('bililive_go_local_settings');
        var settings = raw ? JSON.parse(raw) : {};
        var mode = settings.themeMode || 'system';
        var resolved = mode === 'dark' || (mode === 'system' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light';
        document.documentElement.dataset.bgoTheme = resolved;
        document.documentElement.dataset.bgoThemeMode = mode;
        document.documentElement.dataset.bgoPalette = settings.themePalette || 'one';
      } catch (err) {
        document.documentElement.dataset.bgoTheme = 'light';
        document.documentElement.dataset.bgoThemeMode = 'system';
        document.documentElement.dataset.bgoPalette = 'one';
      }
    })();
  </script>
  <style>
    :root,
    html[data-bgo-theme="light"] {
      color-scheme: light;
      --page: #f6f7f9;
      --panel: #ffffff;
      --panel-strong: #eef2f5;
      --ink: #17202a;
      --muted: #667085;
      --soft: #8a96a3;
      --line: #d9dee7;
      --line-strong: #b9c2cf;
      --button: #0b6bcb;
      --button-text: #ffffff;
      --button-hover: #0759ad;
      --danger: #b4232c;
      --shadow: 0 20px 60px rgba(15, 15, 15, 0.14);
      --input: #ffffff;
      --focus: rgba(11, 107, 203, 0.18);
      --accent: #0b6bcb;
    }
    html[data-bgo-theme="dark"] {
      color-scheme: dark;
      --page: #111820;
      --panel: #151b23;
      --panel-strong: #18212a;
      --ink: #eef3f8;
      --muted: #9da7b3;
      --soft: #7f8a96;
      --line: #2a333d;
      --line-strong: #3b4652;
      --button: #0169cc;
      --button-text: #ffffff;
      --button-hover: #218bff;
      --danger: #ff777d;
      --shadow: 0 20px 60px rgba(0, 0, 0, 0.42);
      --input: #111820;
      --focus: rgba(1, 105, 204, 0.24);
      --accent: #0169cc;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
      color: var(--ink);
      background: var(--page);
      display: grid;
      place-items: center;
      padding: 24px;
    }
    .shell {
      width: min(980px, 100%);
      min-height: 600px;
      display: grid;
      grid-template-columns: minmax(0, 1fr) 420px;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .intro {
      padding: 42px;
      background: var(--panel-strong);
      border-right: 1px solid var(--line);
      display: flex;
      flex-direction: column;
      justify-content: space-between;
      gap: 48px;
    }
    .topline {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
    }
    .brand-lockup {
      display: inline-flex;
      align-items: center;
      gap: 12px;
      min-width: 0;
    }
    .brand-lockup img {
      width: 34px;
      height: 34px;
      border-radius: 8px;
      border: 1px solid var(--line);
      background: var(--panel);
      padding: 4px;
    }
    .brand-name {
      display: grid;
      gap: 2px;
      min-width: 0;
    }
    .brand-name strong {
      font-size: 16px;
      line-height: 1.2;
      white-space: nowrap;
    }
    .brand-name span {
      color: var(--muted);
      font-size: 12px;
      line-height: 1.2;
    }
    .appearance-controls {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      flex: 0 0 auto;
    }
    .glass-select {
      position: relative;
      min-width: 104px;
    }
    .glass-select-button {
      height: 34px;
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: color-mix(in srgb, var(--panel) 72%, transparent);
      color: var(--ink);
      padding: 0 9px;
      font-size: 12px;
      font-weight: 650;
      cursor: pointer;
      outline: none;
      display: inline-flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      transition: background .16s ease, border-color .16s ease, box-shadow .16s ease, transform .16s ease;
      backdrop-filter: blur(14px) saturate(1.3);
      -webkit-backdrop-filter: blur(14px) saturate(1.3);
    }
    .glass-select-button:hover,
    .glass-select[data-open="true"] .glass-select-button {
      background: color-mix(in srgb, var(--panel) 84%, transparent);
      border-color: var(--line-strong);
    }
    .glass-select-button:focus {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px var(--focus);
    }
    .glass-select-label {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .glass-select-left {
      min-width: 0;
      display: inline-flex;
      align-items: center;
      gap: 7px;
    }
    .glass-swatch {
      width: 10px;
      height: 10px;
      flex: 0 0 auto;
      border: 1px solid var(--line);
      border-radius: 50%;
    }
    .glass-chevron {
      color: var(--muted);
      font-size: 10px;
      line-height: 1;
      transition: transform .16s ease;
    }
    .glass-select[data-open="true"] .glass-chevron {
      transform: rotate(180deg);
    }
    .glass-menu {
      position: absolute;
      top: calc(100% + 8px);
      right: 0;
      z-index: 20;
      min-width: 152px;
      max-height: min(360px, 62vh);
      overflow-y: auto;
      display: none;
      padding: 6px;
      border: 1px solid color-mix(in srgb, var(--line) 82%, transparent);
      border-radius: 8px;
      background: color-mix(in srgb, var(--panel) 78%, transparent);
      box-shadow: 0 18px 44px rgba(0,0,0,0.24), inset 0 1px 0 rgba(255,255,255,0.08);
      backdrop-filter: blur(18px) saturate(1.45);
      -webkit-backdrop-filter: blur(18px) saturate(1.45);
    }
    .glass-select[data-open="true"] .glass-menu {
      display: grid;
      gap: 2px;
    }
    .glass-option {
      min-height: 30px;
      width: 100%;
      border: 0;
      border-radius: 7px;
      background: transparent;
      color: var(--ink);
      padding: 0 9px;
      font-size: 12px;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      text-align: left;
    }
    .glass-option:hover {
      background: color-mix(in srgb, var(--focus) 56%, transparent);
    }
    .glass-option[data-active="true"] {
      background: color-mix(in srgb, var(--focus) 72%, transparent);
    }
    .glass-option-check {
      color: var(--accent);
      font-size: 12px;
    }
    h1 {
      margin: 0 0 16px;
      max-width: 480px;
      font-size: 36px;
      line-height: 1.18;
      font-weight: 780;
      letter-spacing: 0;
    }
    .intro-copy p {
      margin: 0;
      max-width: 420px;
      line-height: 1.75;
      color: var(--muted);
      font-size: 15px;
    }
    .access-list {
      display: grid;
      gap: 0;
      border-top: 1px solid var(--line);
      border-bottom: 1px solid var(--line);
    }
    .access-row {
      display: grid;
      grid-template-columns: 120px 1fr;
      gap: 16px;
      padding: 14px 0;
      border-top: 1px solid var(--line);
      font-size: 13px;
    }
    .access-row:first-child {
      border-top: 0;
    }
    .access-row span:first-child {
      color: var(--soft);
    }
    .access-row span:last-child {
      color: var(--ink);
      font-weight: 650;
    }
    .form-pane {
      padding: 42px;
      display: flex;
      align-items: center;
    }
    form {
      width: 100%;
    }
    .form-title {
      margin: 0 0 8px;
      font-size: 26px;
      line-height: 1.25;
      font-weight: 760;
      letter-spacing: 0;
    }
    .form-subtitle {
      margin: 0 0 30px;
      color: var(--muted);
      font-size: 14px;
      line-height: 1.7;
    }
    label {
      display: block;
      margin: 18px 0 8px;
      color: var(--ink);
      font-size: 14px;
      font-weight: 650;
    }
    input {
      width: 100%;
      height: 46px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--input);
      color: var(--ink);
      padding: 0 13px;
      font-size: 15px;
      outline: none;
      transition: border-color .16s ease, box-shadow .16s ease, background .16s ease;
    }
    input:focus {
      border-color: var(--line-strong);
      box-shadow: 0 0 0 3px var(--focus);
    }
    .submit-button {
      width: 100%;
      height: 46px;
      margin-top: 24px;
      border: 0;
      border-radius: 8px;
      background: var(--button);
      color: var(--button-text);
      font-size: 15px;
      font-weight: 720;
      cursor: pointer;
      transition: transform .14s ease, background .14s ease, opacity .14s ease;
    }
    .submit-button:hover { background: var(--button-hover); }
    .submit-button:active { transform: translateY(1px); }
    .submit-button:disabled { cursor: wait; opacity: .72; }
    .error {
      min-height: 22px;
      margin-top: 14px;
      color: var(--danger);
      font-size: 13px;
      line-height: 1.6;
    }
    .hint {
      margin-top: 18px;
      padding-top: 16px;
      border-top: 1px solid var(--line);
      color: var(--muted);
      font-size: 12px;
      line-height: 1.7;
    }
    .code {
      color: var(--ink);
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
    }
    @media (max-width: 760px) {
      body { padding: 16px; }
      .shell { min-height: auto; grid-template-columns: 1fr; }
      .intro { padding: 28px; gap: 34px; border-right: 0; border-bottom: 1px solid var(--line); }
      h1 { font-size: 30px; }
      .form-pane { padding: 28px; }
      .access-row { grid-template-columns: 96px 1fr; }
      .topline { align-items: flex-start; }
      .appearance-controls { flex-direction: column; align-items: stretch; }
      .glass-select { min-width: 104px; }
    }
    @media (max-width: 420px) {
      body { padding: 10px; }
      .intro, .form-pane { padding: 22px; }
      h1 { font-size: 27px; }
      .brand-name span { display: none; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="intro" aria-label="BiliLive-go">
      <div class="topline">
        <div class="brand-lockup">
          <img src="/favicon.ico" alt="">
          <div class="brand-name">
            <strong>BiliLive-go</strong>
            <span>录制管理面板</span>
          </div>
        </div>
        <div class="appearance-controls" aria-label="外观">
          <div id="theme-mode-select" class="glass-select">
            <button id="theme-mode-button" class="glass-select-button" type="button" aria-haspopup="listbox" aria-expanded="false">
              <span id="theme-mode-label" class="glass-select-label">系统</span>
              <span class="glass-chevron">⌄</span>
            </button>
            <div id="theme-mode-panel" class="glass-menu" role="listbox" aria-label="主题模式"></div>
          </div>
          <div id="theme-palette-select" class="glass-select">
            <button id="theme-palette-button" class="glass-select-button" type="button" aria-haspopup="listbox" aria-expanded="false">
              <span class="glass-select-left">
                <span id="theme-palette-swatch" class="glass-swatch"></span>
                <span id="theme-palette-label" class="glass-select-label">One</span>
              </span>
              <span class="glass-chevron">⌄</span>
            </button>
            <div id="theme-palette-panel" class="glass-menu" role="listbox" aria-label="配色"></div>
          </div>
        </div>
      </div>
      <div class="intro-copy">
        <h1>进入 WebUI</h1>
        <p>访问保护已经开启。登录后可以继续管理直播间、查看录制文件和处理更新任务。</p>
      </div>
      <div class="access-list" aria-label="访问状态">
        <div class="access-row"><span>保护状态</span><span>已开启</span></div>
        <div class="access-row"><span>登录方式</span><span>配置账号</span></div>
        <div class="access-row"><span>会话有效期</span><span>7 天</span></div>
      </div>
    </section>
    <section class="form-pane">
      <form id="login-form" autocomplete="on">
        <h2 class="form-title">欢迎回来</h2>
        <p class="form-subtitle">请输入 WebUI 账号，继续使用当前实例。</p>
        <label for="username">用户名</label>
        <input id="username" name="username" type="text" autocomplete="username" required autofocus>
        <label for="password">密码</label>
        <input id="password" name="password" type="password" autocomplete="current-password" required>
        <button id="submit" class="submit-button" type="submit">登录</button>
        <div id="error" class="error" role="alert" aria-live="polite"></div>
        <div class="hint">账号来自配置文件中的 <span class="code">rpc.auth</span>。</div>
      </form>
    </section>
  </main>
  <script>
    const SETTINGS_KEY = 'bililive_go_local_settings';
    const modeSelect = document.getElementById('theme-mode-select');
    const modeButton = document.getElementById('theme-mode-button');
    const modeLabel = document.getElementById('theme-mode-label');
    const modePanel = document.getElementById('theme-mode-panel');
    const paletteSelect = document.getElementById('theme-palette-select');
    const paletteButton = document.getElementById('theme-palette-button');
    const paletteLabel = document.getElementById('theme-palette-label');
    const paletteSwatch = document.getElementById('theme-palette-swatch');
    const palettePanel = document.getElementById('theme-palette-panel');
    const form = document.getElementById('login-form');
    const submit = document.getElementById('submit');
    const error = document.getElementById('error');
    const params = new URLSearchParams(window.location.search);
    const next = params.get('next') || '/';
    const LIGHT_BASE = {
      'page': '#f6f7f9',
      'panel': '#ffffff',
      'panel-strong': '#eef2f5',
      'ink': '#17202a',
      'muted': '#667085',
      'soft': '#8a96a3',
      'line': '#d9dee7',
      'line-strong': '#b9c2cf',
      'button': '#0b6bcb',
      'button-text': '#ffffff',
      'button-hover': '#0759ad',
      'danger': '#b4232c',
      'shadow': '0 20px 60px rgba(15, 15, 15, 0.14)',
      'input': '#ffffff',
      'focus': 'rgba(11, 107, 203, 0.18)',
      'accent': '#0b6bcb'
    };
    const DARK_BASE = {
      'page': '#111820',
      'panel': '#151b23',
      'panel-strong': '#18212a',
      'ink': '#eef3f8',
      'muted': '#9da7b3',
      'soft': '#7f8a96',
      'line': '#2a333d',
      'line-strong': '#3b4652',
      'button': '#0169cc',
      'button-text': '#ffffff',
      'button-hover': '#218bff',
      'danger': '#ff777d',
      'shadow': '0 20px 60px rgba(0, 0, 0, 0.42)',
      'input': '#111820',
      'focus': 'rgba(1, 105, 204, 0.24)',
      'accent': '#0169cc'
    };
    function merge(base, patch) {
      return Object.assign({}, base, patch);
    }
    function makeAccentPalette(key, label, lightAccent, lightHover, lightFocus, darkAccent, darkHover, darkFocus, lightPatch, darkPatch) {
      return {
        key,
        label,
        light: merge(LIGHT_BASE, Object.assign({
          'accent': lightAccent,
          'button': lightAccent,
          'button-hover': lightHover,
          'focus': lightFocus
        }, lightPatch || {})),
        dark: merge(DARK_BASE, Object.assign({
          'accent': darkAccent,
          'button': darkAccent,
          'button-text': '#ffffff',
          'button-hover': darkHover,
          'focus': darkFocus
        }, darkPatch || {}))
      };
    }
    const PALETTES = [
      makeAccentPalette('one', 'One', '#4078f2', '#2864d8', 'rgba(64, 120, 242, 0.18)', '#61afef', '#7ec7ff', 'rgba(97, 175, 239, 0.24)', { 'page': '#fafafa', 'panel-strong': '#f0f2f5', 'ink': '#24292f' }, { 'button-text': '#0f172a', 'page': '#282c34', 'panel': '#2c313c', 'panel-strong': '#21252b', 'ink': '#d7dae0', 'line': '#3e4451' }),
      makeAccentPalette('absolutely', 'Absolutely', '#b85f43', '#9c4c34', 'rgba(184, 95, 67, 0.20)', '#cc7d5e', '#dc8c69', 'rgba(204, 125, 94, 0.24)', { 'page': '#f5f1ec', 'panel': '#fffaf5', 'panel-strong': '#eee7df', 'ink': '#2d2926', 'line': '#dfd3c9' }, { 'page': '#2d2d2b', 'panel': '#353532', 'panel-strong': '#242421', 'ink': '#f9f9f7', 'line': '#4b4944' }),
      makeAccentPalette('ayu', 'Ayu', '#c46f00', '#9f5a00', 'rgba(196, 111, 0, 0.20)', '#ffb454', '#ffd580', 'rgba(255, 180, 84, 0.24)', { 'page': '#faf7ef', 'panel': '#fffdf7', 'panel-strong': '#f0eadf' }, { 'button-text': '#11151c', 'page': '#111722', 'panel': '#11151c', 'panel-strong': '#141922', 'ink': '#e6e1cf', 'line': '#27313f' }),
      makeAccentPalette('catppuccin', 'Catppuccin', '#8839ef', '#6c2bd9', 'rgba(136, 57, 239, 0.20)', '#cba6f7', '#ddb6ff', 'rgba(203, 166, 247, 0.24)', { 'page': '#eff1f5', 'panel-strong': '#e6e9ef', 'ink': '#4c4f69' }, { 'button-text': '#11111b', 'page': '#1e1e2e', 'panel': '#181825', 'panel-strong': '#181825', 'ink': '#cdd6f4', 'line': '#313244' }),
      makeAccentPalette('codex', 'Codex', '#0b6bcb', '#0759ad', 'rgba(11, 107, 203, 0.18)', '#0169cc', '#218bff', 'rgba(1, 105, 204, 0.24)', { 'page': '#f6f7f9', 'panel-strong': '#eef2f5', 'ink': '#17202a' }, { 'page': '#111820', 'panel': '#151b23', 'panel-strong': '#18212a', 'ink': '#eef3f8', 'line': '#2a333d' }),
      makeAccentPalette('dracula', 'Dracula', '#7c3aed', '#6d28d9', 'rgba(124, 58, 237, 0.20)', '#bd93f9', '#d6acff', 'rgba(189, 147, 249, 0.24)', { 'page': '#f8f7ff', 'panel-strong': '#eeebff', 'ink': '#282a36' }, { 'button-text': '#282a36', 'page': '#282a36', 'panel': '#21222c', 'panel-strong': '#21222c', 'ink': '#f8f8f2', 'line': '#44475a' }),
      makeAccentPalette('everforest', 'Everforest', '#6c8f43', '#557136', 'rgba(108, 143, 67, 0.20)', '#a7c080', '#b9d18f', 'rgba(167, 192, 128, 0.24)', { 'page': '#f4f0d9', 'panel': '#fffbea', 'panel-strong': '#e8e0bf', 'ink': '#3c4841', 'line': '#d8cfad' }, { 'button-text': '#2d353b', 'page': '#2d353b', 'panel': '#343f44', 'panel-strong': '#263238', 'ink': '#d3c6aa', 'line': '#4f585e' }),
      makeAccentPalette('github', 'GitHub', '#0969da', '#0756b6', 'rgba(9, 105, 218, 0.18)', '#1f6feb', '#388bfd', 'rgba(31, 111, 235, 0.24)', { 'page': '#f6f8fa', 'panel-strong': '#f0f3f6', 'line': '#d0d7de' }, { 'page': '#0d1117', 'panel': '#161b22', 'panel-strong': '#161b22', 'line': '#30363d' }),
      makeAccentPalette('gruvbox', 'Gruvbox', '#af6f00', '#8f5900', 'rgba(175, 111, 0, 0.20)', '#fabd2f', '#ffd75f', 'rgba(250, 189, 47, 0.24)', { 'page': '#fbf1c7', 'panel': '#fff7d7', 'panel-strong': '#ebdbb2', 'ink': '#3c3836', 'line': '#d5c4a1' }, { 'button-text': '#282828', 'page': '#282828', 'panel': '#32302f', 'panel-strong': '#1d2021', 'ink': '#ebdbb2', 'line': '#504945' }),
      makeAccentPalette('linear', 'Linear', '#5e6ad2', '#4f5bbc', 'rgba(94, 106, 210, 0.20)', '#5e6ad2', '#7c86e8', 'rgba(94, 106, 210, 0.24)', {}, { 'page': '#121416', 'panel': '#17191d', 'panel-strong': '#1c1f25', 'ink': '#f7f8f8', 'line': '#2a2d33' }),
      makeAccentPalette('lobster', 'Lobster', '#c23a2f', '#9f2f27', 'rgba(194, 58, 47, 0.18)', '#ff7a70', '#ff9a92', 'rgba(255, 122, 112, 0.24)', { 'page': '#fff4f1', 'panel': '#fffaf8', 'panel-strong': '#f8e4df', 'ink': '#35211f', 'line': '#ead0ca' }, { 'page': '#2b1e22', 'panel': '#33242a', 'panel-strong': '#251a1f', 'ink': '#ffece7', 'line': '#563a42' }),
      makeAccentPalette('material', 'Material', '#1976d2', '#115293', 'rgba(25, 118, 210, 0.18)', '#80cbc4', '#a7ffeb', 'rgba(128, 203, 196, 0.24)', { 'page': '#f6f8fb', 'panel-strong': '#edf2f7', 'ink': '#263238' }, { 'button-text': '#263238', 'page': '#263238', 'panel': '#2f3d46', 'panel-strong': '#202b31', 'ink': '#eeffff', 'line': '#455a64' }),
      makeAccentPalette('matrix', 'Matrix', '#0f8f4a', '#0b6f39', 'rgba(15, 143, 74, 0.18)', '#00d26a', '#50fa7b', 'rgba(0, 210, 106, 0.24)', { 'page': '#f1fbf4', 'panel-strong': '#e1f3e8', 'ink': '#14351f', 'line': '#c7e6d2' }, { 'button-text': '#122116', 'page': '#122116', 'panel': '#16291c', 'panel-strong': '#101d14', 'ink': '#d7ffe4', 'line': '#2a4c35' }),
      makeAccentPalette('monokai', 'Monokai', '#6f8f00', '#536d00', 'rgba(111, 143, 0, 0.20)', '#a6e22e', '#c2ff55', 'rgba(166, 226, 46, 0.24)', { 'page': '#f7f4ea', 'panel': '#fffaf0', 'panel-strong': '#ece5d6', 'ink': '#272822', 'line': '#ddd4c0' }, { 'button-text': '#272822', 'page': '#272822', 'panel': '#303127', 'panel-strong': '#20211c', 'ink': '#f8f8f2', 'line': '#49483e' }),
      makeAccentPalette('night-owl', 'Night Owl', '#3268d8', '#2450ad', 'rgba(50, 104, 216, 0.18)', '#82aaff', '#b4ccff', 'rgba(130, 170, 255, 0.24)', { 'page': '#eef5ff', 'panel-strong': '#e1ecfb', 'ink': '#16243d', 'line': '#cbd9ef' }, { 'button-text': '#101a2a', 'page': '#101a2a', 'panel': '#12213a', 'panel-strong': '#0e1726', 'ink': '#d6deeb', 'line': '#263c5a' }),
      makeAccentPalette('nord', 'Nord', '#4c7a92', '#3b6479', 'rgba(76, 122, 146, 0.18)', '#88c0d0', '#a3d7e6', 'rgba(136, 192, 208, 0.24)', { 'page': '#eceff4', 'panel-strong': '#e5e9f0', 'ink': '#2e3440', 'line': '#d8dee9' }, { 'button-text': '#2e3440', 'page': '#2e3440', 'panel': '#343b49', 'panel-strong': '#252b35', 'ink': '#eceff4', 'line': '#4c566a' }),
      makeAccentPalette('notion', 'Notion', '#2f3437', '#1f2326', 'rgba(47, 52, 55, 0.16)', '#e9e5dc', '#ffffff', 'rgba(233, 229, 220, 0.20)', { 'page': '#f7f6f3', 'panel-strong': '#eeeae4', 'ink': '#2f3437', 'line': '#ded9d1' }, { 'button-text': '#191919', 'page': '#191919', 'panel': '#202020', 'panel-strong': '#202020', 'ink': '#f1f1ef', 'line': '#373737' }),
      makeAccentPalette('oscurance', 'Oscurance', '#7357d8', '#5b43b0', 'rgba(115, 87, 216, 0.18)', '#b4a7ff', '#d2caff', 'rgba(180, 167, 255, 0.24)', { 'page': '#f5f3ff', 'panel-strong': '#ece8ff', 'ink': '#27213f', 'line': '#d8d0f5' }, { 'button-text': '#1c1b2e', 'page': '#1c1b2e', 'panel': '#23223a', 'panel-strong': '#171629', 'ink': '#efeaff', 'line': '#3d395f' }),
      makeAccentPalette('raycast', 'Raycast', '#e5484d', '#c7353b', 'rgba(229, 72, 77, 0.18)', '#ff6363', '#ff8a8a', 'rgba(255, 99, 99, 0.24)', { 'page': '#fff5f5', 'panel-strong': '#ffe7e7', 'ink': '#311c1f', 'line': '#f0caca' }, { 'page': '#23191a', 'panel': '#2d2021', 'panel-strong': '#1e1516', 'ink': '#fff1f1', 'line': '#523334' }),
      makeAccentPalette('rose-pine', 'Rose Pine', '#d7827e', '#b4637a', 'rgba(215, 130, 126, 0.20)', '#eb6f92', '#f6a1b6', 'rgba(235, 111, 146, 0.24)', { 'page': '#faf4ed', 'panel': '#fffaf6', 'panel-strong': '#f2e9de', 'ink': '#575279', 'line': '#dfdad9' }, { 'page': '#191724', 'panel': '#1f1d2e', 'panel-strong': '#171521', 'ink': '#e0def4', 'line': '#403d52' }),
      makeAccentPalette('sentry', 'Sentry', '#5f4bb6', '#4c3b91', 'rgba(95, 75, 182, 0.18)', '#c59cff', '#d8baff', 'rgba(197, 156, 255, 0.24)', { 'page': '#f8f4ff', 'panel-strong': '#eee7fb', 'ink': '#30263f', 'line': '#dccff0' }, { 'button-text': '#241b2f', 'page': '#241b2f', 'panel': '#2b2138', 'panel-strong': '#201729', 'ink': '#f5edff', 'line': '#49385f' }),
      makeAccentPalette('solarized', 'Solarized', '#268bd2', '#1f6f9f', 'rgba(38, 139, 210, 0.18)', '#2aa198', '#55d6c2', 'rgba(42, 161, 152, 0.24)', { 'page': '#fdf6e3', 'panel': '#fffaf0', 'panel-strong': '#eee8d5', 'ink': '#586e75', 'line': '#d8cfb0' }, { 'page': '#073642', 'panel': '#0b404d', 'panel-strong': '#002b36', 'ink': '#eee8d5', 'line': '#2f5d68' }),
      makeAccentPalette('temple', 'Temple', '#a66c1f', '#805218', 'rgba(166, 108, 31, 0.20)', '#f6c177', '#ffd59a', 'rgba(246, 193, 119, 0.24)', { 'page': '#f7f1df', 'panel': '#fff9ea', 'panel-strong': '#ebe0c3', 'ink': '#342d21', 'line': '#d9c9a5' }, { 'button-text': '#24201a', 'page': '#24201a', 'panel': '#2d281f', 'panel-strong': '#1f1b16', 'ink': '#f6ead0', 'line': '#51442f' }),
      makeAccentPalette('tokyo-night', 'Tokyo Night', '#3d68d8', '#2d50aa', 'rgba(61, 104, 216, 0.18)', '#7aa2f7', '#9ab8ff', 'rgba(122, 162, 247, 0.24)', { 'page': '#f1f5ff', 'panel-strong': '#e5ebfa', 'ink': '#1f2335', 'line': '#cfd8ee' }, { 'button-text': '#1a1b26', 'page': '#1a1b26', 'panel': '#202331', 'panel-strong': '#16161e', 'ink': '#c0caf5', 'line': '#3b4261' }),
      makeAccentPalette('vercel', 'Vercel', '#111827', '#0f172a', 'rgba(17, 24, 39, 0.16)', '#f5f5f5', '#ffffff', 'rgba(245, 245, 245, 0.20)', { 'page': '#fafafa', 'panel-strong': '#f0f0f0', 'ink': '#111827', 'line': '#dedede' }, { 'button-text': '#171717', 'page': '#171717', 'panel': '#1f1f1f', 'panel-strong': '#202020', 'ink': '#f5f5f5', 'line': '#3f3f3f' }),
      makeAccentPalette('vs-code-plus', 'VS Code Plus', '#007acc', '#0067ad', 'rgba(0, 122, 204, 0.18)', '#007acc', '#2499e8', 'rgba(0, 122, 204, 0.24)', { 'page': '#f3f3f3', 'panel-strong': '#ebebeb', 'ink': '#1f1f1f', 'line': '#d4d4d4' }, { 'page': '#1e1e1e', 'panel': '#252526', 'panel-strong': '#252526', 'ink': '#cccccc', 'line': '#3c3c3c' }),
      makeAccentPalette('xcode', 'Xcode', '#007aff', '#005ecb', 'rgba(0, 122, 255, 0.18)', '#0a84ff', '#5eb1ff', 'rgba(10, 132, 255, 0.24)', { 'page': '#f7fbff', 'panel-strong': '#edf5ff', 'ink': '#1d1d1f', 'line': '#d5e3f5' }, { 'page': '#242833', 'panel': '#2b303d', 'panel-strong': '#20242e', 'ink': '#f2f2f7', 'line': '#465066' })
    ].sort((a, b) => a.key.localeCompare(b.key));
    const MODES = [
      { key: 'system', label: '系统' },
      { key: 'light', label: '浅色' },
      { key: 'dark', label: '深色' }
    ];

    function readSettings() {
      try {
        return JSON.parse(localStorage.getItem(SETTINGS_KEY) || '{}') || {};
      } catch (err) {
        return {};
      }
    }

    function findPalette(key) {
      return PALETTES.find((item) => item.key === key) || PALETTES.find((item) => item.key === 'one') || PALETTES[0];
    }

    function resolveMode(mode) {
      return mode === 'dark' || (mode === 'system' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light';
    }

    function applyVars(colors) {
      Object.keys(colors).forEach((key) => {
        document.documentElement.style.setProperty('--' + key, colors[key]);
      });
    }

    function applyAppearance(settings) {
      const mode = ['system', 'light', 'dark'].includes(settings.themeMode) ? settings.themeMode : 'system';
      const palette = findPalette(settings.themePalette);
      const resolvedMode = resolveMode(mode);
      document.documentElement.dataset.bgoTheme = resolvedMode;
      document.documentElement.dataset.bgoThemeMode = mode;
      document.documentElement.dataset.bgoPalette = palette.key;
      applyVars(palette[resolvedMode]);
      modeLabel.textContent = (MODES.find((item) => item.key === mode) || MODES[0]).label;
      paletteLabel.textContent = palette.label;
      paletteSwatch.style.background = palette[resolvedMode].accent;
      modePanel.querySelectorAll('.glass-option').forEach((option) => {
        option.dataset.active = String(option.dataset.value === mode);
        option.querySelector('.glass-option-check').textContent = option.dataset.value === mode ? '✓' : '';
      });
      palettePanel.querySelectorAll('.glass-option').forEach((option) => {
        option.dataset.active = String(option.dataset.value === palette.key);
        option.querySelector('.glass-option-check').textContent = option.dataset.value === palette.key ? '✓' : '';
      });
    }

    function saveAppearance(nextSettings) {
      const settings = readSettings();
      Object.assign(settings, nextSettings);
      localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
      applyAppearance(settings);
    }

    function closeGlassSelects() {
      [modeSelect, paletteSelect].forEach((select) => {
        select.dataset.open = 'false';
        select.querySelector('.glass-select-button').setAttribute('aria-expanded', 'false');
      });
    }

    function toggleGlassSelect(select) {
      const willOpen = select.dataset.open !== 'true';
      closeGlassSelects();
      select.dataset.open = String(willOpen);
      select.querySelector('.glass-select-button').setAttribute('aria-expanded', String(willOpen));
    }

    function makeOption(value, label, swatch, onSelect) {
      const option = document.createElement('button');
      option.type = 'button';
      option.className = 'glass-option';
      option.dataset.value = value;
      option.setAttribute('role', 'option');
      option.innerHTML = '<span class="glass-select-left">' + (swatch ? '<span class="glass-swatch" style="background:' + swatch + '"></span>' : '') + '<span>' + label + '</span></span><span class="glass-option-check"></span>';
      option.addEventListener('click', () => {
        onSelect(value);
        closeGlassSelects();
      });
      return option;
    }

    MODES.forEach((item) => {
      modePanel.appendChild(makeOption(item.key, item.label, '', (value) => {
        saveAppearance({ themeMode: value });
      }));
    });

    PALETTES.forEach((item) => {
      palettePanel.appendChild(makeOption(item.key, item.label, item.dark.accent, (value) => {
        saveAppearance({ themePalette: value });
      }));
    });

    modeButton.addEventListener('click', (event) => {
      event.stopPropagation();
      toggleGlassSelect(modeSelect);
    });
    paletteButton.addEventListener('click', (event) => {
      event.stopPropagation();
      toggleGlassSelect(paletteSelect);
    });
    document.addEventListener('click', closeGlassSelects);
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape') {
        closeGlassSelects();
      }
    });

    applyAppearance(readSettings());
    if (window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if ((readSettings().themeMode || 'system') === 'system') {
          applyAppearance(readSettings());
        }
      });
    }

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      error.textContent = '';
      submit.disabled = true;
      const payload = {
        username: form.username.value.trim(),
        password: form.password.value,
        next
      };
      try {
        const res = await fetch('/api/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || data.err_no) {
          throw new Error(data.err_msg || '登录失败');
        }
        window.location.assign((data.data && data.data.redirect) || '/');
      } catch (err) {
        error.textContent = err.message || '登录失败';
        submit.disabled = false;
      }
    });
  </script>
</body>
</html>`
