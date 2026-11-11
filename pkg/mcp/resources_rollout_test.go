package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/internal/test"
)

type ResourcesRolloutSuite struct {
	BaseMcpSuite
	client *kubernetes.Clientset
}

func podTemplate(image, version string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "pricing"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "app", Image: image, Env: []corev1.EnvVar{{Name: "VERSION", Value: version}},
		}}},
	}
}

func (s *ResourcesRolloutSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.client = kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	d, err := s.client.AppsV1().Deployments("ns-1").Create(s.T().Context(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "pricing", Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(2)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pricing"}},
			Template: podTemplate("pricing:bad", "2"),
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	// envtest runs no controllers: write the two ReplicaSets the deployment controller would have made
	for _, rs := range []struct{ revision, image, version string }{{"1", "pricing:good", "1"}, {"2", "pricing:bad", "2"}} {
		template := podTemplate(rs.image, rs.version)
		template.Labels["pod-template-hash"] = "h" + rs.revision
		_, err = s.client.AppsV1().ReplicaSets("ns-1").Create(s.T().Context(), &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "pricing-" + rs.revision,
				Annotations:     map[string]string{"deployment.kubernetes.io/revision": rs.revision},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "pricing", UID: d.UID, Controller: ptr.To(true)}},
			},
			Spec: appsv1.ReplicaSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: template.Labels},
				Template: template,
			},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
	}
	s.InitMcpClient()
}

func (s *ResourcesRolloutSuite) TearDownTest() {
	_ = s.client.AppsV1().Deployments("ns-1").Delete(s.T().Context(), "pricing", metav1.DeleteOptions{})
	for _, rs := range []string{"pricing-1", "pricing-2"} {
		_ = s.client.AppsV1().ReplicaSets("ns-1").Delete(s.T().Context(), rs, metav1.DeleteOptions{})
	}
	s.BaseMcpSuite.TearDownTest()
}

func (s *ResourcesRolloutSuite) TestUndoRestoresThePreviousTemplate() {
	result, err := s.CallTool("resources_rollout", map[string]interface{}{"action": "undo", "kind": "Deployment", "namespace": "ns-1", "name": "pricing"})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "rolled back from revision 2 to the Pod template of revision 1")
	d, err := s.client.AppsV1().Deployments("ns-1").Get(s.T().Context(), "pricing", metav1.GetOptions{})
	s.Require().NoError(err)
	s.Equal("pricing:good", d.Spec.Template.Spec.Containers[0].Image)
	s.Equal("1", d.Spec.Template.Spec.Containers[0].Env[0].Value)
	s.NotContains(d.Spec.Template.Labels, "pod-template-hash")
}

func (s *ResourcesRolloutSuite) TestRestartAndStatus() {
	result, err := s.CallTool("resources_rollout", map[string]interface{}{"action": "restart", "kind": "Deployment", "namespace": "ns-1", "name": "pricing"})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	d, err := s.client.AppsV1().Deployments("ns-1").Get(s.T().Context(), "pricing", metav1.GetOptions{})
	s.Require().NoError(err)
	s.NotEmpty(d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"])

	result, err = s.CallTool("resources_rollout", map[string]interface{}{"action": "status", "kind": "Deployment", "namespace": "ns-1", "name": "pricing"})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "rollout in progress: 0/2 updated")
}

func TestResourcesRollout(t *testing.T) {
	suite.Run(t, new(ResourcesRolloutSuite))
}
