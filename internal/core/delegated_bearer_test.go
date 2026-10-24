package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amoylab/unla/internal/common/cnst"
	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/core/state"
	"github.com/amoylab/unla/internal/mcp/session"
	"github.com/amoylab/unla/pkg/mcp"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func delegatedTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := state.BuildStateFromConfig(context.Background(), []*config.MCPConfig{{
		Routers: []config.RouterConfig{
			{Prefix: "/protected", Server: "svc", Auth: &config.Auth{Mode: cnst.AuthModeDelegatedBearer}},
			{Prefix: "/other", Server: "svc", Auth: &config.Auth{Mode: cnst.AuthModeDelegatedBearer}},
			{Prefix: "/open", Server: "svc"},
		},
		Servers: []config.ServerConfig{{Name: "svc"}},
	}}, nil, zap.NewNop())
	require.NoError(t, err)
	return &Server{state: st, logger: zap.NewNop(), sessions: session.NewMemoryStore(zap.NewNop()), toolRespHandler: CreateResponseHandlerChain()}
}

func delegatedRequest(s *Server, method, path, token, sid, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		c.Request.Header.Set("Authorization", token)
	}
	if sid != "" {
		c.Request.Header.Set(mcp.HeaderMcpSessionID, sid)
	}
	s.handleRoot(c)
	return w
}

func TestDelegatedSessionBoundaries(t *testing.T) {
	s := delegatedTestServer(t)
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	w := delegatedRequest(s, "POST", "/protected/mcp", "Bearer fixture-token-A", "", initBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sid := w.Header().Get(mcp.HeaderMcpSessionID)
	require.NotEmpty(t, sid)
	conn, err := s.sessions.Get(context.Background(), sid)
	require.NoError(t, err)
	encoded, err := json.Marshal(conn.Meta())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-token-A")
	require.NotEmpty(t, conn.Meta().BearerDigest)
	for _, endpoint := range []struct{ method, path string }{{"POST", "/mcp"}, {"GET", "/mcp"}, {"DELETE", "/mcp"}, {"POST", "/message?sessionId=" + sid}} {
		for _, tc := range []struct {
			name, prefix, token string
			status              int
		}{
			{"missing", "/protected", "", 401},
			{"changed", "/protected", "Bearer fixture-token-B", 403},
			{"prefix", "/other", "Bearer fixture-token-A", 403},
			{"open-prefix", "/open", "Bearer fixture-token-A", 403},
		} {
			t.Run(endpoint.method+endpoint.path+tc.name, func(t *testing.T) {
				got := delegatedRequest(s, endpoint.method, tc.prefix+endpoint.path, tc.token, sid, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
				require.Equal(t, tc.status, got.Code, got.Body.String())
			})
		}
	}
	w = delegatedRequest(s, "POST", "/protected/mcp", "Bearer fixture-token-A", sid, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = delegatedRequest(s, "POST", "/open/mcp", "", "", initBody)
	require.Equal(t, http.StatusOK, w.Code)
	w = delegatedRequest(s, "DELETE", "/protected/mcp", "Bearer fixture-token-A", sid, "")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestDelegatedCurrentBearer(t *testing.T) {
	for _, value := range []string{"", "Basic fixture", "Bearer ", "Bearer fixture extra", "Bearer fixture,other", "Bearer fixture\tother"} {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Authorization", value)
		auth, digest := currentBearer(req)
		require.Empty(t, auth)
		require.Empty(t, digest)
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Add("Authorization", "Bearer fixture-A")
	req.Header.Add("Authorization", "Bearer fixture-B")
	auth, _ := currentBearer(req)
	require.Empty(t, auth)
}

func TestDelegatedSSEAndStream(t *testing.T) {
	s := delegatedTestServer(t)
	for _, token := range []string{"", "Basic fixture"} {
		w := delegatedRequest(s, "GET", "/protected/sse", token, "", "")
		require.Equal(t, 401, w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/protected/sse", nil).WithContext(ctx)
	c.Request.Header.Set("Authorization", "Bearer fixture-sse")
	s.handleRoot(c)
	require.Equal(t, 200, w.Code)
	conns, err := s.sessions.List(context.Background())
	require.NoError(t, err)
	require.Len(t, conns, 1)
	meta := conns[0].Meta()
	require.NotEmpty(t, meta.BearerDigest)
	require.Empty(t, meta.Request.Headers)
	w = delegatedRequest(s, "POST", "/protected/message?sessionId="+meta.ID, "Bearer fixture-sse", "", `{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	require.Equal(t, 202, w.Code)
	w = delegatedRequest(s, "POST", "/protected/message?sessionId="+meta.ID, "Bearer fixture-other", "", `{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	require.Equal(t, 403, w.Code)
	// A valid stream is allowed to open and closes on request cancellation.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/protected/mcp", nil).WithContext(ctx)
	c.Request.Header.Set("Authorization", "Bearer fixture-sse")
	c.Request.Header.Set("Accept", "text/event-stream")
	c.Request.Header.Set(mcp.HeaderMcpSessionID, meta.ID)
	s.handleRoot(c)
	require.Equal(t, 200, w.Code)
}

func TestDelegatedRedirectRefused(t *testing.T) {
	s := delegatedTestServer(t)
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer upstream.Close()
	s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/protected/mcp", nil)
	c.Request.Header.Set("Authorization", "Bearer fixture-redirect")
	conn := &fakeConnExec{meta: &session.Meta{Prefix: "/protected"}}
	_, err := s.executeHTTPTool(c, conn, &config.ToolConfig{Name: "query", Method: "GET", Endpoint: upstream.URL}, map[string]any{}, nil)
	require.ErrorContains(t, err, "redirect refused")
	require.Zero(t, targetCalls)
}

func TestDelegatedToolCredentialAndPolicy(t *testing.T) {
	s := delegatedTestServer(t)
	logs, observed := observer.New(zap.DebugLevel)
	s.logger = zap.New(logs)
	s.forwardConfig.Enabled = true
	s.forwardConfig.McpArg.KeyForHeader = "_headers"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer fixture-token-A", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired"}`))
	}))
	defer upstream.Close()
	s.internalNetACL, _ = parseInternalNetworkAllowlist([]string{"127.0.0.0/8"})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/protected/mcp", nil)
	c.Request.Header.Set("Authorization", "Bearer fixture-token-A")
	c.Request.Header.Set("Cookie", "secret=cookie")
	meta := &session.Meta{ID: "sid", Prefix: "/protected"}
	s.bindDelegatedSession(c.Request, meta)
	conn := &fakeConnExec{meta: meta}
	tool := &config.ToolConfig{Name: "query", Method: "GET", Endpoint: upstream.URL, Headers: map[string]string{"Authorization": "Bearer configured-forgery"}}
	args := map[string]any{"_headers": map[string]any{"Authorization": "Bearer model-forgery"}, "__forwardHeaders": map[string]any{"Authorization": "Bearer other-forgery"}}
	result, err := s.executeHTTPTool(c, conn, tool, args, nil)
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Equal(t, 1, calls)
	data, err := json.Marshal(observed.All())
	require.NoError(t, err)
	require.NotContains(t, string(data), "fixture-token-A")
	require.NotContains(t, string(data), "model-forgery")
	c.Request.Header.Del("Authorization")
	_, err = s.executeHTTPTool(c, conn, tool, args, nil)
	require.ErrorContains(t, err, "current bearer")
	require.Equal(t, 1, calls)
	for _, invalid := range []config.ToolConfig{
		{Method: "POST", Endpoint: upstream.URL},
		{Method: "GET", Endpoint: "{{.Args.url}}"},
		{Method: "GET", Endpoint: upstream.URL, Args: []config.ArgConfig{{Name: "Authorization", Position: "header"}}},
	} {
		require.Error(t, validateDelegatedTool(&invalid))
	}
}
