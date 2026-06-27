package servers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectExternalThemeHTML(t *testing.T) {
	html := []byte("<html><head><title>x</title></head><body>ok</body></html>")
	injected := string(injectExternalThemeHTML(html))

	assert.Contains(t, injected, "bgo-external-theme-bridge")
	assert.Contains(t, injected, "bg-white")
	assert.Contains(t, injected, "bg-card")
	assert.Contains(t, injected, "text-gray-900")
	assert.Contains(t, injected, "text-muted-foreground")
	assert.Contains(t, injected, "text-white")
	assert.Less(t, strings.Index(injected, "bgo-external-theme-bridge"), strings.Index(injected, "</head>"))

	injectedAgain := string(injectExternalThemeHTML([]byte(injected)))
	assert.Equal(t, 1, strings.Count(injectedAgain, "bgo-external-theme-bridge"))
}

func TestInjectExternalThemeResponseSkipsNonHTML(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(strings.NewReader("console.log(1);")),
	}

	require.NoError(t, injectExternalThemeResponse(resp))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "console.log(1);", string(body))
}
