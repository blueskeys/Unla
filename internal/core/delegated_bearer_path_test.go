package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/mcp/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func delegatedPathTool(endpoint string) *config.ToolConfig {
	return &config.ToolConfig{
		Name: "board", Method: http.MethodGet, Endpoint: endpoint,
		Args: []config.ArgConfig{{Name: "board_id", Position: "path", Type: "string", Required: true, Default: "default-board"}},
	}
}

func TestDelegatedPathStructure(t *testing.T) {
	const endpoint = "https://example.com:8443/api/boards/{{.Args.board_id}}"
	for _, valid := range []string{endpoint, endpoint + "/items?fixed=1", endpoint + "/{{.Args.board_id}}", "https://example.com/api/a%20b/{{.Args.board_id}}"} {
		require.NoError(t, validateDelegatedTool(delegatedPathTool(valid)))
	}
	for _, invalid := range []string{
		"{{.Args.scheme}}://example.com/api/boards/{{.Args.board_id}}",
		"https://{{.Args.board_id}}/api/boards",
		"https://example.com:{{.Args.board_id}}/api/boards",
		endpoint + "?id={{.Args.board_id}}",
		endpoint + "?{{.Args.board_id}}=1",
		endpoint + "#{{.Args.board_id}}",
		"https://example.com/api/boards/prefix-{{.Args.board_id}}",
		endpoint + ".json",
		endpoint + "{{.Args.board_id}}",
		"https://example.com/api/{{ .Args.board_id }}",
		"https://example.com/api/{{.Args.board_id | urlquery}}",
		`https://example.com/api/{{printf "%s" .Args.board_id}}`,
		"https://example.com/api/{{index .Args `board_id`}}",
		"https://example.com/api/{{.Config.board_id}}",
		"https://example.com/api/{{.Request.Headers.Authorization}}",
		"https://example.com/api/{{.Args.board_id.child}}",
		"https://example.com/api/{{if .Args.board_id}}ok{{end}}",
		"https://example.com/api/{{/* comment */}}{{.Args.board_id}}",
		"https://example.com/api/{{- .Args.board_id -}}",
		"https://example.com/api/{.Args.board_id}",
		"https://example.com/api/%7B{.Args.board_id}}",
	} {
		t.Run(invalid, func(t *testing.T) {
			require.Error(t, validateDelegatedTool(delegatedPathTool(invalid)))
		})
	}
	for name, mutate := range map[string]func(*config.ToolConfig){
		"undeclared": func(tool *config.ToolConfig) { tool.Args = nil },
		"optional":   func(tool *config.ToolConfig) { tool.Args[0].Required = false },
		"number":     func(tool *config.ToolConfig) { tool.Args[0].Type = "number" },
		"query":      func(tool *config.ToolConfig) { tool.Args[0].Position = "query" },
		"header":     func(tool *config.ToolConfig) { tool.Args[0].Position = "header" },
		"duplicate":  func(tool *config.ToolConfig) { tool.Args = append(tool.Args, tool.Args[0]) },
		"unused":     func(tool *config.ToolConfig) { tool.Endpoint = "https://example.com/api/boards" },
		"post":       func(tool *config.ToolConfig) { tool.Method = http.MethodPost },
		"body":       func(tool *config.ToolConfig) { tool.RequestBody = "body" },
		"proxy":      func(tool *config.ToolConfig) { tool.Proxy = &config.ProxyConfig{} },
	} {
		t.Run(name, func(t *testing.T) {
			tool := delegatedPathTool(endpoint)
			mutate(tool)
			require.Error(t, validateDelegatedTool(tool))
		})
	}
}

func TestDelegatedPathExecution(t *testing.T) {
	s := delegatedTestServer(t)
	s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
	s.forwardConfig.Enabled = true
	s.forwardConfig.McpArg.KeyForHeader = "_headers"
	type receivedRequest struct{ method, uri, authorization, cookie string }
	received := make(chan receivedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- receivedRequest{r.Method, r.RequestURI, r.Header.Get("Authorization"), r.Header.Get("Cookie")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/protected/mcp", nil)
	c.Request.Header.Set("Authorization", "Bearer fixture-path-current")
	c.Request.Header.Set("Cookie", "session=fixture")
	meta := &session.Meta{ID: "path-session", Prefix: "/protected"}
	s.bindDelegatedSession(c.Request, meta)
	conn := &fakeConnExec{meta: meta}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, id := range []string{"a", "0", "Board_01-abc", strings.Repeat("a", 128)} {
			t.Run(method+"/"+id, func(t *testing.T) {
				tool := delegatedPathTool(upstream.URL + "/api/boards/{{.Args.board_id}}/items?fixed=yes")
				tool.Method = method
				tool.Headers = map[string]string{"Authorization": "Bearer configured-forgery"}
				tool.Args = append(tool.Args, config.ArgConfig{Name: "search", Position: "query"})
				args := map[string]any{"board_id": id, "search": "a/b?x", "_headers": map[string]any{"Authorization": "Bearer model-forgery"}}
				_, err := s.executeHTTPTool(c, conn, tool, args, nil)
				require.NoError(t, err)
				got := <-received
				require.Equal(t, receivedRequest{method, "/api/boards/" + id + "/items?fixed=yes&search=a%2Fb%3Fx", "Bearer fixture-path-current", ""}, got)
			})
		}
	}
	tool := &config.ToolConfig{Name: "legacy", Method: "GET", Endpoint: upstream.URL + "/api/boards?fixed=yes", Args: []config.ArgConfig{{Name: "search", Position: "query", Default: "default-query"}}}
	_, err := s.executeHTTPTool(c, conn, tool, map[string]any{}, nil)
	require.NoError(t, err)
	require.Equal(t, "/api/boards?fixed=yes&search=default-query", (<-received).uri)
}

func TestDelegatedPathRejectsInputBeforeRequest(t *testing.T) {
	s := delegatedTestServer(t)
	s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/protected/mcp", nil)
	c.Request.Header.Set("Authorization", "Bearer fixture-path-current")
	conn := &fakeConnExec{meta: &session.Meta{Prefix: "/protected"}}
	tool := delegatedPathTool(upstream.URL + "/api/boards/{{.Args.board_id}}")
	for i, value := range []any{
		nil, "", ".", "..", "a.b", "a/b", `a\b`, "a%2Fb", "a?b", "a#b", "a b", "a\n", "é", "看板", "_a", "-a",
		strings.Repeat("a", 129), 123, float64(123), true, []any{"abc"}, map[string]any{"id": "abc"},
		`"abc"`, `{"id":"abc"}`, "{{.Config.id}}",
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, err := s.executeHTTPTool(c, conn, tool, map[string]any{"board_id": value}, nil)
			require.ErrorContains(t, err, "delegated path argument")
		})
	}
	for _, args := range []map[string]any{nil, {}, {"unrelated": "abc"}} {
		_, err := s.executeHTTPTool(c, conn, tool, args, nil)
		require.ErrorContains(t, err, "delegated path argument")
		require.NotContains(t, args, "board_id")
	}
	tool.Endpoint = upstream.URL + "/api/{{.Args.board_id | printf `%s`}}"
	_, err := s.executeHTTPTool(c, conn, tool, map[string]any{"board_id": "safe"}, nil)
	require.Error(t, err)
	require.Zero(t, calls.Load())
}
