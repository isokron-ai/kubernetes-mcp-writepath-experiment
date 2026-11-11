package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ProjectionSuite struct {
	suite.Suite
}

func (s *ProjectionSuite) TestProjectFields() {
	obj := map[string]any{
		"metadata": map[string]any{"name": "web"},
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{
			map[string]any{"name": "app", "image": "nginx:1", "resources": map[string]any{"limits": map[string]any{"memory": "128Mi"}}},
			map[string]any{"name": "sidecar", "image": "busybox"},
		}}}},
	}
	got := projectFields(obj, []string{
		"metadata.name",
		"spec.template.spec.containers[*].image",
		"spec.template.spec.containers[0].resources.limits.memory",
		"spec.template.spec.containers[5].image",
		"status.replicas",
	})
	s.Equal("web", got["metadata.name"])
	s.Equal([]any{"nginx:1", "busybox"}, got["spec.template.spec.containers[*].image"])
	s.Equal("128Mi", got["spec.template.spec.containers[0].resources.limits.memory"])
	s.Nil(got["spec.template.spec.containers[5].image"])
	s.Nil(got["status.replicas"])
}

func TestProjection(t *testing.T) {
	suite.Run(t, new(ProjectionSuite))
}
