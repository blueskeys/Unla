package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/template"
	"github.com/amoylab/unla/pkg/mcp"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLiveopsProdConfig(t *testing.T) {
	data, err := os.ReadFile("../../deploy/ubuntu243/liveops-prod.yaml")
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(data)), "beta")
	require.NotContains(t, string(data), "9731")
	var cfg config.MCPConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&cfg))
	require.NoError(t, config.ValidateMCPConfigs([]*config.MCPConfig{&cfg}))
	require.Equal(t, "liveops-prod", cfg.Name)
	require.Equal(t, "default", cfg.Tenant)
	require.Len(t, cfg.Routers, 1)
	require.Equal(t, "/gateway/liveops-prod", cfg.Routers[0].Prefix)
	require.Equal(t, "delegated_bearer", string(cfg.Routers[0].Auth.Mode))
	require.Len(t, cfg.Servers, 1)
	fixtures := map[string]string{
		"liveops_list_boards":            `{"items":[{"id":"b1","name":"fixture","goods_count":1,"private":"DO_NOT_EXPOSE"}],"total":1,"page":1,"page_size":5}`,
		"liveops_get_pallet_workspace":   `{"id":"p1","board_id":"b1","migration_status":"blocked","version":2,"private":"DO_NOT_EXPOSE"}`,
		"liveops_list_pallet_sessions":   `[{"id":"s1","business_date":"2026-10-25","status":"planned","private":"DO_NOT_EXPOSE"}]`,
		"liveops_get_session_assortment": `{"planned_session_id":"s1","assortment_version":0,"generation_status":"not_generated","items":[],"private":"DO_NOT_EXPOSE"}`,
		"liveops_list_replays":           `{"items":[{"session_key":"r1","latest":{"summary":{"watch_ucnt":10,"private":"DO_NOT_EXPOSE"}}},{"session_key":"r2","latest":null}]}`,
	}
	data, err = os.ReadFile("../../deploy/ubuntu243/liveops-beta.yaml")
	require.NoError(t, err)
	var beta config.MCPConfig
	require.NoError(t, yaml.Unmarshal(data, &beta))
	baseline := make(map[string]config.ToolConfig)
	for _, tool := range beta.Tools {
		baseline[tool.Name] = tool
	}
	require.Len(t, cfg.Tools, 5)
	require.Len(t, cfg.Servers[0].AllowedTools, 5)
	seen := make(map[string]bool)
	for _, tool := range cfg.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			require.False(t, seen[tool.Name])
			seen[tool.Name] = true
			require.Contains(t, cfg.Servers[0].AllowedTools, tool.Name)
			require.Contains(t, tool.Description, "production")
			require.NoError(t, validateDelegatedTool(&tool))
			original := baseline[tool.Name]
			require.Equal(t, strings.Replace(original.Endpoint, "http://100.92.212.67:9731", "https://ops.api.kindgem.net", 1), tool.Endpoint)
			contract := tool
			contract.Endpoint, contract.Description, contract.ResponseBody = original.Endpoint, original.Description, original.ResponseBody
			require.Equal(t, original, contract)
			fixture, ok := fixtures[tool.Name]
			require.True(t, ok)
			render := func(target config.ToolConfig) map[string]interface{} {
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))}
				result, err := CreateResponseHandlerChain().Handle(resp, &target, template.NewContext())
				require.NoError(t, err)
				require.False(t, result.IsError)
				text := result.Content[0].(*mcp.TextContent).Text
				require.NotContains(t, text, "DO_NOT_EXPOSE")
				var output map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(text), &output))
				return output
			}
			actual, expected := render(tool), render(original)
			require.Equal(t, "production", actual["environment"])
			delete(actual, "environment")
			delete(expected, "environment")
			require.Equal(t, expected, actual)
		})
	}
}
