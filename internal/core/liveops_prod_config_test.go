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
	fixtureData, err := os.ReadFile("testdata/liveops_beta_expansion.json")
	require.NoError(t, err)
	var expansion map[string]string
	require.NoError(t, json.Unmarshal(fixtureData, &expansion))
	for name, fixture := range expansion {
		fixtures[name] = fixture
	}
	fixtures["liveops_get_pallet_product"] = `{"goods_id":"g1","lifecycle_type":"new","version":1,"private":"DO_NOT_EXPOSE"}`
	fixtures["liveops_list_products"] = `{"items":[{"sku":"SKU1","product_name":"fixture","cost":"DO_NOT_EXPOSE"}],"total":1,"page":1,"page_size":20,"total_pages":1,"summary":"DO_NOT_EXPOSE"}`
	fixtures["liveops_get_product_workspace"] = `{"single_items":[{"sku":"SKU1","cost":"DO_NOT_EXPOSE"}],"sets":[{"id":"set1","name":"fixture","created_by":"DO_NOT_EXPOSE"}]}`
	data, err = os.ReadFile("../../deploy/ubuntu243/liveops-beta.yaml")
	require.NoError(t, err)
	var beta config.MCPConfig
	require.NoError(t, yaml.Unmarshal(data, &beta))
	baseline := make(map[string]config.ToolConfig)
	for _, tool := range beta.Tools {
		baseline[tool.Name] = tool
	}
	require.Len(t, cfg.Tools, 59)
	require.Len(t, cfg.Servers[0].AllowedTools, 59)
	require.NotContains(t, cfg.Servers[0].AllowedTools, "liveops_get_board_goods")
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
			if tool.Name == "liveops_get_hot_order_goods_fact" {
				contract.Args = append([]config.ArgConfig(nil), tool.Args...)
				for i := range contract.Args {
					contract.Args[i].Description = original.Args[i].Description
				}
			}
			require.Equal(t, original, contract)
			fixture, ok := fixtures[tool.Name]
			require.True(t, ok)
			render := func(target config.ToolConfig) interface{} {
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))}
				result, err := CreateResponseHandlerChain().Handle(resp, &target, template.NewContext())
				require.NoError(t, err)
				require.False(t, result.IsError)
				text := result.Content[0].(*mcp.TextContent).Text
				require.NotContains(t, text, "DO_NOT_EXPOSE")
				var output interface{}
				require.NoError(t, json.Unmarshal([]byte(text), &output))
				return output
			}
			actual := render(tool).(map[string]interface{})
			expected := render(original)
			require.Equal(t, "production", actual["environment"])
			if projected, wrapped := actual["data"]; wrapped {
				require.Equal(t, expected, projected)
			} else {
				delete(actual, "environment")
				delete(expected.(map[string]interface{}), "environment")
				require.Equal(t, expected, actual)
			}
			resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"detail":"DO_NOT_EXPOSE"}`))}
			result, err := CreateResponseHandlerChain().Handle(resp, &tool, template.NewContext())
			if err == nil {
				require.True(t, result.IsError)
				encoded, marshalErr := json.Marshal(result)
				require.NoError(t, marshalErr)
				require.NotContains(t, string(encoded), "DO_NOT_EXPOSE")
			} else {
				require.NotContains(t, err.Error(), "DO_NOT_EXPOSE")
			}
		})
	}
}

func TestLiveopsProdRejectsNestedFields(t *testing.T) {
	data, err := os.ReadFile("../../deploy/ubuntu243/liveops-prod.yaml")
	require.NoError(t, err)
	var cfg config.MCPConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	fixtures := map[string][]string{
		"liveops_list_replays":          {`{"items":[{"latest":{"summary":{"watch_ucnt":{"secret":"DO_NOT_EXPOSE"}}}}]}`, `{"items":[{"latest":{"summary":{"fans":["DO_NOT_EXPOSE"]}}}]}`},
		"liveops_list_products":         {`{"items":[{"sku":{"secret":"DO_NOT_EXPOSE"}}]}`, `{"items":[],"total":{"secret":"DO_NOT_EXPOSE"}}`, `{"items":[` + strings.Repeat(`{},`, 20) + `{}` + `]}`},
		"liveops_get_product_workspace": {`{"single_items":[],"sets":[{"name":{"secret":"DO_NOT_EXPOSE"}}]}`, `{"single_items":[],"sets":[` + strings.Repeat(`{},`, 1000) + `{}` + `]}`},
	}
	for _, tool := range cfg.Tools {
		for _, fixture := range fixtures[tool.Name] {
			t.Run(tool.Name, func(t *testing.T) {
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture))}
				result, err := CreateResponseHandlerChain().Handle(resp, &tool, template.NewContext())
				if err != nil {
					require.NotContains(t, err.Error(), "DO_NOT_EXPOSE")
				} else {
					require.True(t, result.IsError)
					encoded, marshalErr := json.Marshal(result)
					require.NoError(t, marshalErr)
					require.NotContains(t, string(encoded), "DO_NOT_EXPOSE")
				}
			})
		}
	}
}
