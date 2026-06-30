package servers

import (
	"context"
	"net"
	"net/http"

	"github.com/bililive-go/bililive-go/src/pkg/internalapi"
)

type schedulerInternalAPIContextKey struct{}

func schedulerInternalAPIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rewrittenPath, ok := internalapi.RewriteSchedulerAPIPath(r.URL.Path)
		if !ok || !isLoopbackRemoteAddr(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}

		u := *r.URL
		u.Path = rewrittenPath
		u.RawPath = ""
		ctx := context.WithValue(r.Context(), schedulerInternalAPIContextKey{}, true)
		r2 := r.WithContext(ctx)
		r2.URL = &u
		next.ServeHTTP(w, r2)
	})
}

func schedulerInternalAPIProxy(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSchedulerInternalAPIRequest(r) {
			http.NotFound(w, r)
			return
		}
		router.ServeHTTP(w, r)
	})
}

func isSchedulerInternalAPIRequest(r *http.Request) bool {
	ok, _ := r.Context().Value(schedulerInternalAPIContextKey{}).(bool)
	return ok
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
