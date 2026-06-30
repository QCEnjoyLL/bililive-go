package servers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/pkg/internalapi"
)

func TestSchedulerInternalAPIBypassesWebAuthForLoopbackToken(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}

	var seenPath string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	handler := schedulerInternalAPIMiddleware(webAuthMiddleware(auth)(next))

	req := httptest.NewRequest(http.MethodGet, internalapi.SchedulerPathPrefix()+"/api/info", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "/api/info", seenPath)
}

func TestSchedulerInternalAPIProxyReachesAPIRoute(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}

	router := mux.NewRouter()
	router.Use(schedulerInternalAPIMiddleware)
	router.Use(webAuthMiddleware(auth))
	apiRoute := router.PathPrefix(apiRouterPrefix).Subrouter()
	apiRoute.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodGet)
	router.PathPrefix(internalapi.SchedulerPathPrefix() + apiRouterPrefix).Handler(schedulerInternalAPIProxy(router))

	req := httptest.NewRequest(http.MethodGet, internalapi.SchedulerPathPrefix()+"/api/info", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestSchedulerInternalAPIDoesNotBypassWebAuthForInvalidToken(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := schedulerInternalAPIMiddleware(webAuthMiddleware(auth)(next))

	req := httptest.NewRequest(http.MethodGet, "/internal/scheduler-api/bad-token/api/info", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSchedulerInternalAPIDoesNotBypassWebAuthForRemoteAddress(t *testing.T) {
	auth := configs.RPCAuth{
		Enable:   true,
		Username: "admin",
		Password: "secret",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := schedulerInternalAPIMiddleware(webAuthMiddleware(auth)(next))

	req := httptest.NewRequest(http.MethodGet, internalapi.SchedulerPathPrefix()+"/api/info", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
