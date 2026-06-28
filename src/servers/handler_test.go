package servers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bililive-go/bililive-go/src/configs"
)

func TestGetSoopLiveAuthConfigDoesNotExposeSavedPassword(t *testing.T) {
	cfg := configs.NewConfig()
	cfg.SoopLiveAuth.Username = "tester"
	cfg.SoopLiveAuth.Password = "secret"
	configs.SetCurrentConfig(cfg)

	recorder := httptest.NewRecorder()
	getSoopLiveAuthConfig(recorder, nil)

	assert.Equal(t, 200, recorder.Code)

	var resp commonResp
	err := json.Unmarshal(recorder.Body.Bytes(), &resp)
	assert.NoError(t, err)

	data, ok := resp.Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "tester", data["username"])
	assert.Equal(t, true, data["has_saved_credentials"])
	_, exists := data["password"]
	assert.False(t, exists)
}

func TestWebAuthMiddleware(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := webAuthMiddleware(auth)(next)

	page := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.Header.Set("Accept", "text/html")
	handler.ServeHTTP(page, pageReq)
	assert.Equal(t, http.StatusFound, page.Code)
	assert.Equal(t, "/login?next=%2F", page.Header().Get("Location"))
	assert.Empty(t, page.Header().Get("WWW-Authenticate"))

	api := httptest.NewRecorder()
	apiReq := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	apiReq.Header.Set("Accept", "application/json")
	handler.ServeHTTP(api, apiReq)
	assert.Equal(t, http.StatusUnauthorized, api.Code)
	assert.Empty(t, api.Header().Get("WWW-Authenticate"))

	favicon := httptest.NewRecorder()
	faviconReq := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	handler.ServeHTTP(favicon, faviconReq)
	assert.Equal(t, http.StatusNoContent, favicon.Code)

	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	handler.ServeHTTP(authorized, req)
	assert.Equal(t, http.StatusNoContent, authorized.Code)
}

func TestServeWebAppAssetUsesExplicitContentType(t *testing.T) {
	dir := t.TempDir()
	body := []byte{0x89, 0x50, 0x4e, 0x47}
	err := os.WriteFile(filepath.Join(dir, "favicon.ico"), body, 0644)
	assert.NoError(t, err)

	handler := serveWebAppAsset(http.Dir(dir), "favicon.ico", "image/png")
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)

	handler(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
	assert.Equal(t, body, recorder.Body.Bytes())
}

func TestLoginWebUICreatesSessionCookie(t *testing.T) {
	webAuthSessions = &webAuthSessionStore{sessions: map[string]webAuthSession{}}
	cfg := configs.NewConfig()
	cfg.RPC.Auth = configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}
	configs.SetCurrentConfig(cfg)

	body := bytes.NewBufferString(`{"username":"admin","password":"secret","next":"/config"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set(contentType, contentTypeJSON)
	recorder := httptest.NewRecorder()

	loginWebUI(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	cookies := recorder.Result().Cookies()
	assert.Len(t, cookies, 1)
	assert.Equal(t, webAuthCookieName, cookies[0].Name)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, webAuthSessions.valid(cookies[0].Value))

	var resp commonResp
	err := json.Unmarshal(recorder.Body.Bytes(), &resp)
	assert.NoError(t, err)
	data, ok := resp.Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "/config", data["redirect"])
}
