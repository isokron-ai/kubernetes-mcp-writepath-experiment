package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/internal/test"
)

type ResourcesMultiSuite struct {
	BaseMcpSuite
	namespace string
}

func (s *ResourcesMultiSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	client := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	ns, err := client.CoreV1().Namespaces().Create(s.T().Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "multi-"}}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.namespace = ns.Name
	for _, d := range []struct{ name, image, memory string }{{"orders", "nginx:2", "128Mi"}, {"search", "nginx:1", "256Mi"}, {"cart", "nginx:1", "128Mi"}} {
		_, err = client.AppsV1().Deployments(s.namespace).Create(s.T().Context(), &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: d.name},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": d.name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": d.name}},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: d.image, Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(d.memory)},
					}}}},
				},
			},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
	}
	_, err = client.CoreV1().ConfigMaps(s.namespace).Create(s.T().Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings"}}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
}

func (s *ResourcesMultiSuite) TestGetManyWithFields() {
	result, err := s.CallTool("resources_get", map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment", "namespace": s.namespace,
		"names":  []interface{}{"orders", "search", "missing"},
		"fields": []interface{}{"spec.template.spec.containers[*].image", "spec.template.spec.containers[0].resources.limits.memory"},
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	s.Contains(text, "- fields:\n    spec.template.spec.containers[*].image:\n    - nginx:2")
	s.Contains(text, "spec.template.spec.containers[0].resources.limits.memory: 256Mi")
	s.Contains(text, "name: missing")
	s.Contains(text, "not found")
	s.NotContains(text, "managedFields")
}

func (s *ResourcesMultiSuite) TestListSeveralKinds() {
	result, err := s.CallTool("resources_list", map[string]interface{}{
		"namespace": s.namespace,
		"kinds": []interface{}{
			map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment"},
			map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap"},
			map[string]interface{}{"apiVersion": "example.com/v1", "kind": "Nope"},
		},
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	s.Contains(text, "kind: Deployment")
	s.Contains(text, "- cart")
	s.Contains(text, "- settings")
	s.Contains(text, "kind: Nope", "an unknown kind is reported inline")
}

func (s *ResourcesMultiSuite) TestListAllKinds() {
	result, err := s.CallTool("resources_list", map[string]interface{}{"namespace": s.namespace, "all_kinds": true})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	s.Contains(text, "kind: Deployment")
	s.Contains(text, "kind: ConfigMap")
	s.NotContains(text, "kind: Secret\n", "kinds with no objects are left out")
}

func (s *ResourcesMultiSuite) TestListWithFields() {
	result, err := s.CallTool("resources_list", map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment", "namespace": s.namespace, "fields": []interface{}{"spec.replicas"},
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	s.Contains(text, "# 3 Deployment objects, fields spec.replicas")
	s.Contains(text, "spec.replicas: 1")
}

func TestResourcesMulti(t *testing.T) {
	suite.Run(t, new(ResourcesMultiSuite))
}
