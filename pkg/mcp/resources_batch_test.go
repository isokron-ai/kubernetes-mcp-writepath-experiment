package mcp

import (
	"fmt"
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

type ResourcesBatchSuite struct {
	BaseMcpSuite
	client    *kubernetes.Clientset
	namespace string
}

func (s *ResourcesBatchSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.client = kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	ns, err := s.client.CoreV1().Namespaces().Create(s.T().Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "batch-"}}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.namespace = ns.Name
	for i := 0; i < 3; i++ {
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(s.T().Context(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("stale-%d", i), Labels: map[string]string{"cache": "stale"}},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
	}
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(s.T().Context(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh", Labels: map[string]string{"cache": "fresh"}},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
}

func (s *ResourcesBatchSuite) remaining() []string {
	list, err := s.client.CoreV1().ConfigMaps(s.namespace).List(s.T().Context(), metav1.ListOptions{LabelSelector: "cache"})
	s.Require().NoError(err)
	var names []string
	for _, cm := range list.Items {
		names = append(names, cm.Name)
	}
	return names
}

func (s *ResourcesBatchSuite) TestDeleteBySelector() {
	s.Run("dry_run lists the matches and deletes nothing", func() {
		result, err := s.CallTool("resources_delete", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "labelSelector": "cache=stale", "dry_run": true,
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Contains(result.Content[0].(*mcp.TextContent).Text, "3 matched; 3 would be deleted, 0 failed (dry run: nothing was changed)")
		s.Len(s.remaining(), 4)
	})
	s.Run("deletes every match with one result per object", func() {
		result, err := s.CallTool("resources_delete", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "labelSelector": "cache=stale",
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		text := result.Content[0].(*mcp.TextContent).Text
		s.Contains(text, "3 matched; 3 deleted, 0 failed (best_effort")
		s.Contains(text, "- name: stale-0\n  result: deleted")
		s.Equal([]string{"fresh"}, s.remaining())
	})
	s.Run("an empty selector is refused", func() {
		result, err := s.CallTool("resources_delete", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "labelSelector": " ",
		})
		s.Require().NoError(err)
		s.True(result.IsError)
		s.Equal([]string{"fresh"}, s.remaining())
	})
	s.Run("name and labelSelector together are refused", func() {
		result, err := s.CallTool("resources_delete", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "name": "fresh", "labelSelector": "cache=fresh",
		})
		s.Require().NoError(err)
		s.True(result.IsError)
	})
}

func (s *ResourcesBatchSuite) TestLabel() {
	s.Run("labels one object by name, keeping its other labels", func() {
		result, err := s.CallTool("resources_label", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "name": "fresh", "labels": map[string]interface{}{"tier": "cache"},
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(s.T().Context(), "fresh", metav1.GetOptions{})
		s.Require().NoError(err)
		s.Equal(map[string]string{"cache": "fresh", "tier": "cache"}, cm.Labels)
	})
	s.Run("removes a label from every match", func() {
		result, err := s.CallTool("resources_label", map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap", "namespace": s.namespace, "labelSelector": "cache=stale", "labels": map[string]interface{}{"cache": nil, "archived": "true"},
		})
		s.Require().NoError(err)
		s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
		s.Contains(result.Content[0].(*mcp.TextContent).Text, "3 matched; 3 labelled, 0 failed")
		list, err := s.client.CoreV1().ConfigMaps(s.namespace).List(s.T().Context(), metav1.ListOptions{LabelSelector: "archived=true,!cache"})
		s.Require().NoError(err)
		s.Len(list.Items, 3)
	})
}

func (s *ResourcesBatchSuite) TestScaleBySelector() {
	for _, name := range []string{"web-1", "web-2"} {
		_, err := s.client.AppsV1().Deployments(s.namespace).Create(s.T().Context(), &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"tier": "storefront"}},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
				},
			},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
	}
	result, err := s.CallTool("resources_scale", map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment", "namespace": s.namespace, "labelSelector": "tier=storefront", "scale": 0,
	})
	s.Require().NoError(err)
	s.Require().Falsef(result.IsError, "call tool failed: %v", result.Content)
	text := result.Content[0].(*mcp.TextContent).Text
	s.Contains(text, "2 matched; 2 scaled, 0 failed")
	s.Contains(text, "web-1: 1 -> 0 replicas")
	for _, name := range []string{"web-1", "web-2"} {
		d, err := s.client.AppsV1().Deployments(s.namespace).Get(s.T().Context(), name, metav1.GetOptions{})
		s.Require().NoError(err)
		s.Equal(int32(0), *d.Spec.Replicas)
	}
}

func TestResourcesBatch(t *testing.T) {
	suite.Run(t, new(ResourcesBatchSuite))
}
