package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bililive-go/bililive-go/src/pkg/internalapi"
)

func TestSchedulerAPIURLUsesInternalPathWhenAuthEnabled(t *testing.T) {
	apiURL := schedulerAPIURL("http://localhost:8080/", true)

	assert.True(t, strings.HasPrefix(apiURL, "http://localhost:8080/internal/scheduler-api/"))
	assert.Equal(t, "http://localhost:8080"+internalapi.SchedulerPathPrefix(), apiURL)
}

func TestSchedulerAPIURLKeepsPlainBaseWhenAuthDisabled(t *testing.T) {
	assert.Equal(t, "http://localhost:8080", schedulerAPIURL("http://localhost:8080/", false))
}
