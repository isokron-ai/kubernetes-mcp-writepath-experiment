package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/containers/kubernetes-mcp-server/internal/test"
)

type ArgumentValidationSuite struct {
	BaseMcpSuite
}

func (s *ArgumentValidationSuite) TestUnknownArgumentIsRefusedBeforeAnyWrite() {
	s.InitMcpClient()
	result, err := s.CallTool("resources_create_or_update", map[string]interface{}{
		"resource": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-dryrun-typo\n  namespace: default\n",
		"dryRun":   true,
	})
	s.Require().NoError(err)
	s.Run("returns an error naming the argument and the accepted ones", func() {
		s.True(result.IsError)
		text := result.Content[0].(*mcp.TextContent).Text
		s.Contains(text, "unknown argument(s) dryRun")
		s.Contains(text, "dry_run")
	})
	s.Run("writes nothing", func() {
		_, getErr := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig()).CoreV1().ConfigMaps("default").Get(s.T().Context(), "cm-dryrun-typo", metav1.GetOptions{})
		s.True(apierrors.IsNotFound(getErr), "an ignored dryRun used to perform the real write")
	})
}

func (s *ArgumentValidationSuite) TestWrongTypeIsRefused() {
	s.InitMcpClient()
	result, err := s.CallTool("pods_run", map[string]interface{}{"image": "nginx", "port": "80"})
	s.Require().NoError(err)
	s.True(result.IsError)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "validating /properties/port: type")
}

func (s *ArgumentValidationSuite) TestRequiredArgumentsStillReportedByTheHandler() {
	s.InitMcpClient()
	result, err := s.CallTool("resources_create_or_update", map[string]interface{}{})
	s.Require().NoError(err)
	s.True(result.IsError)
	s.Equal("failed to create or update resources, missing argument resource", result.Content[0].(*mcp.TextContent).Text)
}

func TestArgumentValidation(t *testing.T) {
	suite.Run(t, new(ArgumentValidationSuite))
}
