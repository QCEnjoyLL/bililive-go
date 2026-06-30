package internalapi

import (
	"crypto/rand"
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"time"
)

const schedulerBasePath = "/internal/scheduler-api"

var (
	schedulerTokenOnce sync.Once
	schedulerToken     string
)

func SchedulerPathPrefix() string {
	return schedulerBasePath + "/" + SchedulerToken()
}

func SchedulerToken() string {
	schedulerTokenOnce.Do(func() {
		var b [32]byte
		if _, err := rand.Read(b[:]); err == nil {
			schedulerToken = base64.RawURLEncoding.EncodeToString(b[:])
			return
		}
		schedulerToken = strconv.FormatInt(time.Now().UnixNano(), 36)
	})
	return schedulerToken
}

func RewriteSchedulerAPIPath(path string) (string, bool) {
	prefix := SchedulerPathPrefix()
	if path == prefix+"/api" {
		return "/api", true
	}
	if strings.HasPrefix(path, prefix+"/api/") {
		return strings.TrimPrefix(path, prefix), true
	}
	return "", false
}
