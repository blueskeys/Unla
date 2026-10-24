package core

import (
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

func TestLiveopsBetaConfig(t *testing.T) {
	data, err := os.ReadFile("../../deploy/ubuntu243/liveops-beta.yaml")
	require.NoError(t, err)
	var cfg config.MCPConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.NoError(t, config.ValidateMCPConfigs([]*config.MCPConfig{&cfg}))
	require.Len(t, cfg.Tools, 9)
	require.Equal(t, "delegated_bearer", string(cfg.Routers[0].Auth.Mode))
	fixtures := map[string]string{
		"liveops_list_boards":            `{"items":[{"id":"b1","name":"fixture","goods_count":1}],"total":1,"page":1,"page_size":5}`,
		"liveops_get_board_goods":        `{"board":{"id":"b1","name":"fixture","private":"DO_NOT_EXPOSE"},"goods":[{"id":"g1","sku":"SKU1","price":8,"cost":"DO_NOT_EXPOSE","image":"DO_NOT_EXPOSE"}],"archived_goods":[{"sku":"DO_NOT_EXPOSE"}],"trash":["DO_NOT_EXPOSE"]}`,
		"liveops_get_pallet_workspace":   `{"id":"p1","board_id":"b1","migration_status":"ready","version":2,"private":"DO_NOT_EXPOSE"}`,
		"liveops_list_pallet_sessions":   `[{"id":"s1","business_date":"2026-10-24","status":"planned","private":"DO_NOT_EXPOSE"}]`,
		"liveops_get_session_assortment": `{"planned_session_id":"s1","assortment_version":0,"generation_status":"not_generated","items":[{"goods_id":"g1","order_index":1,"private":"DO_NOT_EXPOSE"}]}`,
		"liveops_get_pallet_product":     `{"goods_id":"g1","lifecycle_type":"new","version":1,"private":"DO_NOT_EXPOSE"}`,
		"liveops_list_replays":           `{"items":[{"session_key":"r1","latest":{"summary":{"watch_ucnt":10,"raw_status":"DO_NOT_EXPOSE"},"urls":"DO_NOT_EXPOSE"}}]}`,
		"liveops_list_products":          `{"items":[{"sku":"SKU1","product_name":"fixture","cost":"DO_NOT_EXPOSE"}],"total":1,"page":1,"page_size":20,"total_pages":1,"summary":"DO_NOT_EXPOSE"}`,
		"liveops_get_product_workspace":  `{"single_items":[{"sku":"SKU1","cost":"DO_NOT_EXPOSE"}],"sets":[{"id":"set1","name":"fixture","created_by":"DO_NOT_EXPOSE"}]}`,
	}
	for _, tool := range cfg.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			require.Contains(t, cfg.Servers[0].AllowedTools, tool.Name)
			require.Equal(t, http.MethodGet, tool.Method)
			require.True(t, strings.HasPrefix(tool.Endpoint, "http://100.92.212.67:9731/api/"))
			require.NoError(t, validateDelegatedTool(&tool))
			fixture, ok := fixtures[tool.Name]
			require.True(t, ok)
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))}
			result, err := CreateResponseHandlerChain().Handle(resp, &tool, template.NewContext())
			require.NoError(t, err)
			content := result.Content[0].(*mcp.TextContent)
			require.True(t, json.Valid([]byte(content.Text)), content.Text)
			require.NotContains(t, content.Text, "DO_NOT_EXPOSE")
			require.False(t, result.IsError)
			resp.StatusCode = http.StatusForbidden
			resp.Body = io.NopCloser(strings.NewReader(`{"detail":"DO_NOT_EXPOSE"}`))
			result, err = CreateResponseHandlerChain().Handle(resp, &tool, template.NewContext())
			if err == nil {
				require.True(t, result.IsError)
			} else {
				require.NotContains(t, err.Error(), "DO_NOT_EXPOSE")
			}
		})
	}
	// Preserve the existing tool contract for old published ExAI versions.
	require.Equal(t, "liveops_list_boards", cfg.Tools[0].Name)
	require.Equal(t, "http://100.92.212.67:9731/api/boards?kind=boards&page=1&page_size=5", cfg.Tools[0].Endpoint)
	require.Len(t, cfg.Tools[0].Args, 1)
}
