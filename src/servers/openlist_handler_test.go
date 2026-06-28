package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOpenListConfigReadyForUpload(t *testing.T) {
	openListServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/public/settings":
			_, _ = w.Write([]byte(`{"code":200}`))
		case "/api/admin/storage/list":
			assert.Equal(t, "token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"content":[{"id":1,"mount_path":"/115","driver":"115","status":"work","disabled":false}]}}`))
		case "/api/fs/list":
			assert.Equal(t, "token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer openListServer.Close()

	body := `{"external_url":"` + openListServer.URL + `","external_token":"token","storage_name":"115"}`
	req := httptest.NewRequest(http.MethodPost, "/api/openlist/validate", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	validateOpenListConfig(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp OpenListValidateResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.True(t, resp.ReadyForUpload)
	assert.True(t, resp.ServiceReady)
	assert.True(t, resp.AuthOK)
	assert.True(t, resp.StorageOK)
	assert.Equal(t, "验证通过，可用于云上传。", resp.Message)
}
