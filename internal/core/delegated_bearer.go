package core

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/amoylab/unla/internal/common/cnst"
	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/mcp/session"
	"github.com/amoylab/unla/pkg/mcp"
	"github.com/gin-gonic/gin"
)

func (s *Server) delegatedBearer(prefix string) bool {
	if s.state == nil {
		return false
	}
	auth := s.state.GetAuth(prefix)
	return auth != nil && auth.Mode == cnst.AuthModeDelegatedBearer
}

func currentBearer(req *http.Request) (string, string) {
	values := req.Header.Values("Authorization")
	if len(values) != 1 {
		return "", ""
	}
	parts := strings.Split(values[0], " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ""
	}
	for _, ch := range parts[1] {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~+/=", ch)) {
			return "", ""
		}
	}
	sum := sha256.Sum256([]byte(parts[1]))
	return "Bearer " + parts[1], hex.EncodeToString(sum[:])
}

func (s *Server) bindDelegatedSession(req *http.Request, meta *session.Meta) {
	if !s.delegatedBearer(meta.Prefix) {
		return
	}
	_, meta.BearerDigest = currentBearer(req)
	// Protected sessions retain no initialization credentials or cookies.
	meta.Request = &session.RequestInfo{}
}

func (s *Server) authorizeDelegatedRequest(c *gin.Context, prefix string) bool {
	protected := s.delegatedBearer(prefix)
	_, digest := currentBearer(c.Request)
	if protected && digest == "" {
		c.Header("WWW-Authenticate", `Bearer realm="delegated"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "current_bearer_required"})
		return false
	}
	if protected && s.state.GetProtoType(prefix) != cnst.BackendProtoHttp {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "delegated_http_only"})
		return false
	}
	ids := []string{c.GetHeader(mcp.HeaderMcpSessionID), c.Query("sessionId")}
	for _, id := range ids {
		if id == "" || s.sessions == nil {
			continue
		}
		conn, err := s.sessions.Get(c.Request.Context(), id)
		if err != nil || conn == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "session_not_found"})
			return false
		}
		meta := conn.Meta()
		if !protected && meta.BearerDigest == "" && !s.delegatedBearer(meta.Prefix) {
			continue
		}
		if meta.Prefix != prefix || digest == "" || meta.BearerDigest == "" || subtle.ConstantTimeCompare([]byte(digest), []byte(meta.BearerDigest)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "session_binding_mismatch"})
			return false
		}
	}
	return true
}

func validateDelegatedTool(tool *config.ToolConfig) error {
	if tool.Method != http.MethodGet && tool.Method != http.MethodHead {
		return fmt.Errorf("delegated tool must use GET or HEAD")
	}
	u, err := url.Parse(tool.Endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(tool.Endpoint, "{}") {
		return fmt.Errorf("delegated tool requires a fixed HTTP(S) endpoint")
	}
	if tool.RequestBody != "" || tool.Proxy != nil {
		return fmt.Errorf("delegated tool body and proxy are forbidden")
	}
	for _, arg := range tool.Args {
		if arg.Position != "query" {
			return fmt.Errorf("delegated tool only supports query arguments")
		}
	}
	return nil
}
