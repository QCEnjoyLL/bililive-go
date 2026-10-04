package servers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/stretchr/testify/require"
)

func setWebAuthTestConfig(t *testing.T, auth configs.RPCAuth) {
	t.Helper()
	previous := configs.GetCurrentConfig()
	t.Cleanup(func() { configs.SetCurrentConfig(previous) })
	cfg := configs.NewConfig()
	cfg.RPC.Auth = auth
	configs.SetCurrentConfig(cfg)
}

func TestWebAuthAppliesConfigChangesWithoutRebuildingMiddleware(t *testing.T) {
	setWebAuthTestConfig(t, configs.RPCAuth{})
	handler := webAuthMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(username, password string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
		if username != "" {
			req.SetBasicAuth(username, password)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}
	require.Equal(t, http.StatusNoContent, request("", ""))
	_, err := configs.UpdateTransient(func(c *configs.Config) error {
		c.RPC.Auth = configs.RPCAuth{Enable: true, Username: "admin", Password: "old-password"}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, request("", ""))
	require.Equal(t, http.StatusNoContent, request("admin", "old-password"))

	_, err = configs.UpdateTransient(func(c *configs.Config) error {
		c.RPC.Auth.Username = "new-admin"
		c.RPC.Auth.Password = "new-password"
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, request("admin", "old-password"))
	require.Equal(t, http.StatusNoContent, request("new-admin", "new-password"))

	_, err = configs.UpdateTransient(func(c *configs.Config) error {
		c.RPC.Auth.Enable = false
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, request("", ""))
	configs.SetCurrentConfig(nil)
	require.Equal(t, http.StatusServiceUnavailable, request("", ""))
}
