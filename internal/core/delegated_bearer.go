package core

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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

var delegatedPathPlaceholder = regexp.MustCompile(`^\{\{\.Args\.([A-Za-z_][A-Za-z0-9_]*)\}\}$`)
var delegatedPathValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validateDelegatedTool(tool *config.ToolConfig) error {
	if tool.Method != http.MethodGet && tool.Method != http.MethodHead {
		return fmt.Errorf("delegated tool must use GET or HEAD")
	}
	u, err := url.Parse(tool.Endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(u.Host+u.RawQuery, "{}") {
		return fmt.Errorf("delegated tool requires a fixed HTTP(S) origin and query")
	}
	path := u.Path
	if u.RawPath != "" {
		path = u.RawPath
	}
	pathArgs := make(map[string]bool)
	for _, segment := range strings.Split(path, "/") {
		if match := delegatedPathPlaceholder.FindStringSubmatch(segment); match != nil {
			pathArgs[match[1]] = false
		} else if strings.ContainsAny(segment, "{}") {
			return fmt.Errorf("delegated path template must be a complete segment using .Args.identifier")
		}
	}
	if tool.RequestBody != "" || tool.Proxy != nil {
		return fmt.Errorf("delegated tool body and proxy are forbidden")
	}
	for _, arg := range tool.Args {
		if declared, used := pathArgs[arg.Name]; used {
			if declared || arg.Position != "path" || arg.Type != "string" || !arg.Required {
				return fmt.Errorf("delegated path argument %q must have one required string path declaration", arg.Name)
			}
			pathArgs[arg.Name] = true
		} else if arg.Position != "query" {
			return fmt.Errorf("delegated tool only supports query arguments and declared path placeholders")
		}
	}
	for name, declared := range pathArgs {
		if !declared {
			return fmt.Errorf("delegated path argument %q is undeclared", name)
		}
	}
	return nil
}

func validateDelegatedPathArgs(tool *config.ToolConfig, args map[string]any) error {
	for _, arg := range tool.Args {
		if arg.Position != "path" {
			continue
		}
		value, ok := args[arg.Name].(string)
		if !ok || !delegatedPathValue.MatchString(value) {
			return fmt.Errorf("delegated path argument %q requires a 1-128 character ASCII identifier", arg.Name)
		}
	}
	return nil
}
