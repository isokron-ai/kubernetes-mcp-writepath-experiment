package kubernetes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
)

type ResourcesDeleteOptionsSuite struct {
	suite.Suite
	mockServer *test.MockServer
	core       *Core
	sent       metav1.DeleteOptions
}

func (s *ResourcesDeleteOptionsSuite) SetupTest() {
	s.sent = metav1.DeleteOptions{}
	s.mockServer = test.NewMockServer()
	discovery := test.NewDiscoveryClientHandler(metav1.APIResourceList{
		GroupVersion: "batch/v1",
		APIResources: []metav1.APIResource{{Name: "jobs", Kind: "Job", Namespaced: true, Verbs: metav1.Verbs{"get", "delete"}}},
	})
	s.mockServer.Handle(discovery)
	s.mockServer.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/batch/v1/namespaces/ns/jobs/job-1" {
			return
		}
		if r.Method == http.MethodDelete {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &s.sent)
			test.WriteObject(w, &metav1.Status{Status: metav1.StatusSuccess})
			return
		}
		http.NotFound(w, r)
	}))
	k, err := NewKubernetes(s.T().Context(), config.BaseDefault(),
		clientcmd.NewDefaultClientConfig(*s.mockServer.Kubeconfig(), &clientcmd.ConfigOverrides{}), s.mockServer.Config())
	s.Require().NoError(err)
	s.T().Cleanup(k.close)
	s.core = NewCore(k)
}

func (s *ResourcesDeleteOptionsSuite) TearDownTest() { s.mockServer.Close() }

func (s *ResourcesDeleteOptionsSuite) TestDefaultPropagationIsBackground() {
	outcome, err := s.core.ResourcesDeleteWithOptions(context.Background(), &schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}, "ns", "job-1", DeleteOptions{})
	s.Require().NoError(err)
	s.True(outcome.Gone)
	s.Require().NotNil(s.sent.PropagationPolicy)
	s.Equal(metav1.DeletePropagationBackground, *s.sent.PropagationPolicy, "a Job's Pods must not be orphaned by default")
}

func (s *ResourcesDeleteOptionsSuite) TestPreconditionsAreSent() {
	_, err := s.core.ResourcesDeleteWithOptions(context.Background(), &schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}, "ns", "job-1",
		DeleteOptions{UID: "uid-1", ResourceVersion: "42", PropagationPolicy: metav1.DeletePropagationOrphan})
	s.Require().NoError(err)
	s.Require().NotNil(s.sent.Preconditions)
	s.Equal("uid-1", string(*s.sent.Preconditions.UID))
	s.Equal("42", *s.sent.Preconditions.ResourceVersion)
	s.Equal(metav1.DeletePropagationOrphan, *s.sent.PropagationPolicy)
}

func TestResourcesDeleteOptions(t *testing.T) {
	suite.Run(t, new(ResourcesDeleteOptionsSuite))
}
