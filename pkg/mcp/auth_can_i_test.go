package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
)

type AuthCanISuite struct {
	BaseMcpSuite
}

func (s *AuthCanISuite) TestAuthCanI() {
	s.InitMcpClient()
	s.Run("allowed action by kind", func() {
		result, err := s.CallTool("auth_can_i", map[string]interface{}{
			"verb": "create", "apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "namespace": "default",
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Contains(result.Content[0].(*mcp.TextContent).Text, "yes: this server's identity may create networkpolicies.networking.k8s.io in namespace default")
		s.Equal(true, result.StructuredContent.(map[string]any)["allowed"])
	})
	s.Run("another user by resource name", func() {
		result, err := s.CallTool("auth_can_i", map[string]interface{}{
			"verb": "delete", "resource": "pods", "namespace": "default", "user": "nobody-in-particular",
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Contains(result.Content[0].(*mcp.TextContent).Text, "no: user nobody-in-particular may not delete pods in namespace default")
	})
	s.Run("a target is required", func() {
		result, err := s.CallTool("auth_can_i", map[string]interface{}{"verb": "get"})
		s.Require().NoError(err)
		s.True(result.IsError)
	})
}

func TestAuthCanI(t *testing.T) {
	suite.Run(t, new(AuthCanISuite))
}
