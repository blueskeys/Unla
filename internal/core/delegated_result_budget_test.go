package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/mcp/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDelegatedResultBudget(t *testing.T) {
	s := delegatedTestServer(t)
	s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	}))
	defer upstream.Close()
	for _, prefix := range []string{"/protected", "/open"} {
		t.Run(prefix, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", prefix+"/mcp", nil)
			c.Request.Header.Set("Authorization", "Bearer fixture-result-budget")
			conn := &fakeConnExec{meta: &session.Meta{Prefix: prefix}}
			tool := &config.ToolConfig{Name: "large", Method: "GET", Endpoint: upstream.URL, ResponseBody: "{{.Response.Body}}"}
			result, err := s.executeHTTPTool(c, conn, tool, map[string]any{}, nil)
			if prefix == "/protected" {
				require.ErrorContains(t, err, "1 MiB")
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
		})
	}
}
