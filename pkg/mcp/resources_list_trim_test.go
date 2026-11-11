package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
)

type ResourcesListTrimSuite struct {
	BaseMcpSuite
}

func (s *ResourcesListTrimSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.Cfg.ListOutput.SetForTest("table")
}

func (s *ResourcesListTrimSuite) rows(result *mcp.CallToolResult) []any {
	structured, ok := result.StructuredContent.(map[string]any)
	s.Require().True(ok, "expected structured content, got %T", result.StructuredContent)
	rows, ok := structured["items"].([]any)
	s.Require().True(ok, "expected items, got %v", structured)
	return rows
}

func (s *ResourcesListTrimSuite) TestRowsOmitWhatTheRequestFixes() {
	s.InitMcpClient()
	result, err := s.CallTool("resources_list", map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "namespace": "ns-1"})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	row := s.rows(result)[0].(map[string]any)
	s.Contains(row, "Name")
	s.Contains(row, "Status")
	for _, dropped := range []string{"apiVersion", "kind", "Namespace", "IP", "Node", "Nominated Node", "Readiness Gates"} {
		s.NotContainsf(row, dropped, "%s must not be in the default rows", dropped)
	}
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "IP", "the text table is unchanged")
}

func (s *ResourcesListTrimSuite) TestWideKeepsTheWideColumns() {
	s.InitMcpClient()
	result, err := s.CallTool("pods_list_in_namespace", map[string]interface{}{"namespace": "ns-1", "wide": true})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	row := s.rows(result)[0].(map[string]any)
	s.Contains(row, "IP")
	s.Contains(row, "Node")
}

func (s *ResourcesListTrimSuite) TestEmptyListSaysSo() {
	s.InitMcpClient()
	result, err := s.CallTool("resources_list", map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod", "namespace": "ns-1", "labelSelector": "app=none-such",
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	s.Equal("# No Pod found in namespace ns-1 matching labelSelector \"app=none-such\"\n", result.Content[0].(*mcp.TextContent).Text)
	result, err = s.CallTool("pods_list_in_namespace", map[string]interface{}{"namespace": "ns-1", "labelSelector": "app=none-such"})
	s.Require().NoError(err)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "# No Pod found in namespace ns-1")
}

func TestResourcesListTrim(t *testing.T) {
	suite.Run(t, new(ResourcesListTrimSuite))
}
