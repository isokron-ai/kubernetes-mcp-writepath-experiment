package mcp

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/containers/kubernetes-mcp-server/internal/test"
)

type ResourcesPatchSuite struct {
	BaseMcpSuite
}

func (s *ResourcesPatchSuite) TestResourcesPatch() {
	s.InitMcpClient()
	client := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	cm, err := client.CoreV1().ConfigMaps("default").Create(s.T().Context(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm-to-patch", Annotations: map[string]string{"stale": "yes", "keep": "yes"}},
		Data:       map[string]string{"LOG_LEVEL": "info", "MAX_CONNECTIONS": "100"},
	}, metav1.CreateOptions{FieldManager: "someone-else"})
	s.Require().NoError(err)
	get := func() *corev1.ConfigMap {
		current, getErr := client.CoreV1().ConfigMaps("default").Get(s.T().Context(), "cm-to-patch", metav1.GetOptions{})
		s.Require().NoError(getErr)
		return current
	}

	s.Run("merge patch changes one key and keeps the others", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "merge", "patch": `{"data":{"LOG_LEVEL":"debug"}}`,
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Equal(map[string]string{"LOG_LEVEL": "debug", "MAX_CONNECTIONS": "100"}, get().Data)
	})
	s.Run("merge patch with null removes a key set by another manager", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "merge", "patch": "metadata:\n  annotations:\n    stale: null\n",
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Equal(map[string]string{"keep": "yes"}, get().Annotations)
	})
	s.Run("a stale resourceVersion is refused and changes nothing", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "merge", "patch": `{"data":{"MAX_CONNECTIONS":"1"}}`, "resourceVersion": cm.ResourceVersion,
		})
		s.Require().NoError(err)
		s.True(result.IsError)
		s.Contains(result.Content[0].(*mcp.TextContent).Text, "the object changed after resourceVersion")
		s.Equal("100", get().Data["MAX_CONNECTIONS"])
	})
	s.Run("json patch with the current resourceVersion succeeds", func() {
		current := get()
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "json", "patch": `[{"op":"remove","path":"/data/MAX_CONNECTIONS"}]`, "resourceVersion": current.ResourceVersion,
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Equal(map[string]string{"LOG_LEVEL": "debug"}, get().Data)
	})
	s.Run("returns a receipt", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "merge", "patch": `{"data":{"A":"b"}}`,
		})
		s.Require().NoError(err)
		text := result.Content[0].(*mcp.TextContent).Text
		s.Contains(text, "# ConfigMap cm-to-patch patched (merge patch)")
		s.Contains(text, "resourceVersion:")
		s.NotContains(text, "LOG_LEVEL")
		s.Run("naming the field the patch changed and its new value", func() {
			var decoded []unstructured.Unstructured
			s.Require().NoError(yaml.Unmarshal([]byte(strings.SplitN(text, "\n", 2)[1]), &decoded))
			s.Require().Len(decoded, 1)
			s.Equal(map[string]interface{}{"data.A": "b"}, decoded[0].Object["changed"])
		})
	})
	s.Run("returns a compact receipt with output: compact", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "merge", "patch": `{"data":{"A":"c"}}`, "output": "compact",
		})
		s.Require().NoError(err)
		text := result.Content[0].(*mcp.TextContent).Text
		s.Contains(text, "resourceVersion:")
		s.NotContains(text, "changed")
	})
	s.Run("an unknown patchType is refused", func() {
		result, err := s.CallTool("resources_patch", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": "default", "name": "cm-to-patch",
			"patchType": "apply", "patch": `{}`,
		})
		s.Require().NoError(err)
		s.True(result.IsError)
	})
}

func (s *ResourcesPatchSuite) TestStrategicPatchMergesContainersByName() {
	s.InitMcpClient()
	client := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := client.AppsV1().Deployments("default").Create(s.T().Context(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deployment-to-patch"},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "p"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "app", Image: "nginx:1"}, {Name: "sidecar", Image: "busybox:1"},
				}},
			},
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	result, err := s.CallTool("resources_patch", map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment", "namespace": "default", "name": "deployment-to-patch",
		"patchType": "strategic", "patch": `{"spec":{"template":{"spec":{"containers":[{"name":"app","image":"nginx:2"}]}}}}`,
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	d, err := client.AppsV1().Deployments("default").Get(s.T().Context(), "deployment-to-patch", metav1.GetOptions{})
	s.Require().NoError(err)
	s.Require().Len(d.Spec.Template.Spec.Containers, 2)
	s.Equal("nginx:2", d.Spec.Template.Spec.Containers[0].Image)
	s.Equal("busybox:1", d.Spec.Template.Spec.Containers[1].Image)
}

func TestResourcesPatch(t *testing.T) {
	suite.Run(t, new(ResourcesPatchSuite))
}
