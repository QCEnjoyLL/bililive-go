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
	case "/login", "/api/auth/login":
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
  <style>
    :root {
      color-scheme: light;
      --ink: #17202a;
      --muted: #657383;
      --line: #d7dee8;
      --panel: #ffffff;
      --soft: #f6f8fb;
      --brand: #246bfe;
      --accent: #12a594;
      --danger: #c43d4b;
      --shadow: 0 18px 50px rgba(25, 35, 55, 0.16);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
      color: var(--ink);
      background:
        linear-gradient(145deg, rgba(36, 107, 254, 0.10), rgba(18, 165, 148, 0.10)),
        var(--soft);
      display: grid;
      place-items: center;
      padding: 28px;
    }
    .shell {
      width: min(960px, 100%);
      min-height: 560px;
      display: grid;
      grid-template-columns: minmax(0, 1fr) 400px;
      background: var(--panel);
      border: 1px solid rgba(215, 222, 232, 0.88);
      border-radius: 8px;
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .brand {
      position: relative;
      padding: 42px;
      background:
        linear-gradient(160deg, rgba(23, 32, 42, 0.92), rgba(34, 66, 88, 0.88)),
        #17202a;
      color: #fff;
      display: flex;
      flex-direction: column;
      justify-content: space-between;
    }
    .mark {
      width: 48px;
      height: 48px;
      border-radius: 8px;
      display: grid;
      place-items: center;
      background: #fff;
      color: var(--brand);
      font-weight: 800;
      font-size: 24px;
    }
    h1 {
      margin: 28px 0 14px;
      font-size: 34px;
      line-height: 1.18;
      font-weight: 750;
      letter-spacing: 0;
    }
    .brand p {
      margin: 0;
      max-width: 420px;
      line-height: 1.8;
      color: rgba(255,255,255,0.78);
      font-size: 15px;
    }
    .meta {
      display: grid;
      gap: 10px;
      font-size: 13px;
      color: rgba(255,255,255,0.66);
    }
    .status {
      width: fit-content;
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 8px 10px;
      border: 1px solid rgba(255,255,255,0.18);
      border-radius: 8px;
      background: rgba(255,255,255,0.08);
    }
    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--accent);
      box-shadow: 0 0 0 4px rgba(18, 165, 148, 0.18);
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
      font-size: 24px;
      line-height: 1.25;
      font-weight: 730;
      letter-spacing: 0;
    }
    .form-subtitle {
      margin: 0 0 28px;
      color: var(--muted);
      font-size: 14px;
      line-height: 1.6;
    }
    label {
      display: block;
      margin: 18px 0 8px;
      color: #334255;
      font-size: 14px;
      font-weight: 650;
    }
    input {
      width: 100%;
      height: 44px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #fff;
      color: var(--ink);
      padding: 0 12px;
      font-size: 15px;
      outline: none;
      transition: border-color .16s ease, box-shadow .16s ease;
    }
    input:focus {
      border-color: var(--brand);
      box-shadow: 0 0 0 3px rgba(36, 107, 254, 0.14);
    }
    button {
      width: 100%;
      height: 44px;
      margin-top: 24px;
      border: 0;
      border-radius: 8px;
      background: var(--brand);
      color: #fff;
      font-size: 15px;
      font-weight: 720;
      cursor: pointer;
      transition: transform .14s ease, background .14s ease, opacity .14s ease;
    }
    button:hover { background: #1f5ee8; }
    button:active { transform: translateY(1px); }
    button:disabled { cursor: wait; opacity: .72; }
    .error {
      min-height: 22px;
      margin-top: 14px;
      color: var(--danger);
      font-size: 13px;
      line-height: 1.6;
    }
    @media (max-width: 760px) {
      body { padding: 16px; }
      .shell { min-height: auto; grid-template-columns: 1fr; }
      .brand { padding: 28px; gap: 34px; }
      h1 { font-size: 28px; }
      .form-pane { padding: 28px; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="brand" aria-label="BiliLive-go">
      <div>
        <div class="mark">B</div>
        <h1>BiliLive-go</h1>
        <p>录制管理入口已启用访问保护。</p>
      </div>
      <div class="meta">
        <span class="status"><span class="dot"></span>WebUI 访问保护已开启</span>
      </div>
    </section>
    <section class="form-pane">
      <form id="login-form" autocomplete="on">
        <h2 class="form-title">登录 WebUI</h2>
        <p class="form-subtitle">使用 ` + "`rpc.auth`" + ` 中配置的账号登录。</p>
        <label for="username">用户名</label>
        <input id="username" name="username" type="text" autocomplete="username" required autofocus>
        <label for="password">密码</label>
        <input id="password" name="password" type="password" autocomplete="current-password" required>
        <button id="submit" type="submit">登录</button>
        <div id="error" class="error" role="alert" aria-live="polite"></div>
      </form>
    </section>
  </main>
  <script>
    const form = document.getElementById('login-form');
    const submit = document.getElementById('submit');
    const error = document.getElementById('error');
    const params = new URLSearchParams(window.location.search);
    const next = params.get('next') || '/';
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
