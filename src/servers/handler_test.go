package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestBasicAuthMiddleware(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := basicAuthMiddleware(auth)(next)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusUnauthorized, unauthorized.Code)
	assert.Contains(t, unauthorized.Header().Get("WWW-Authenticate"), "Basic")

	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	handler.ServeHTTP(authorized, req)
	assert.Equal(t, http.StatusNoContent, authorized.Code)
}
