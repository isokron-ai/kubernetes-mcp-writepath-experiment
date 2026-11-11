package kubernetes

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
)

// ResourcesApplyRollbackSuite drives a multi-document apply against a mock API server
// whose real write of one document fails after the dry run accepted it (as a quota or
// a webhook racing with the call can do).
type ResourcesApplyRollbackSuite struct {
	suite.Suite
	mockServer *test.MockServer
	core       *Core
	mu         sync.Mutex
	stored     map[string]bool
	deleted    []string
}

func (s *ResourcesApplyRollbackSuite) SetupTest() {
	s.stored = map[string]bool{"existing": true}
	s.deleted = nil
	s.mockServer = test.NewMockServer()
	discovery := test.NewDiscoveryClientHandler()
	discovery.APIResourceLists[0].APIResources = append(discovery.APIResourceLists[0].APIResources,
		metav1.APIResource{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: metav1.Verbs{"get", "patch", "delete"}})
	s.mockServer.Handle(discovery)
	s.mockServer.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/namespaces/ns/configmaps/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		s.mu.Lock()
		defer s.mu.Unlock()
		cm := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]interface{}{"name": name, "namespace": "ns"},
		}}
		switch r.Method {
		case http.MethodGet:
			if !s.stored[name] {
				http.NotFound(w, r)
				return
			}
			test.WriteObject(w, cm)
		case http.MethodPatch:
			if name == "fails" && r.URL.Query().Get("dryRun") == "" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"injected failure","code":500}`))
				return
			}
			if r.URL.Query().Get("dryRun") == "" {
				s.stored[name] = true
			}
			test.WriteObject(w, cm)
		case http.MethodDelete:
			s.deleted = append(s.deleted, name)
			delete(s.stored, name)
			test.WriteObject(w, &metav1.Status{Status: metav1.StatusSuccess})
		}
	}))
	k, err := NewKubernetes(s.T().Context(), config.BaseDefault(),
		clientcmd.NewDefaultClientConfig(*s.mockServer.Kubeconfig(), &clientcmd.ConfigOverrides{}), s.mockServer.Config())
	s.Require().NoError(err)
	s.T().Cleanup(k.close)
	s.core = NewCore(k)
}

func (s *ResourcesApplyRollbackSuite) TearDownTest() {
	s.mockServer.Close()
}

func manifest(names ...string) string {
	docs := make([]string, 0, len(names))
	for _, n := range names {
		docs = append(docs, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: "+n+"\n  namespace: ns\n")
	}
	return strings.Join(docs, "---\n")
}

func (s *ResourcesApplyRollbackSuite) TestRollbackDeletesOnlyWhatTheCallCreated() {
	_, err := s.core.ResourcesApply(context.Background(), manifest("new-1", "existing", "fails", "never"), ApplyOptions{ValidateFirst: true, RollbackOnFailure: true})
	var pe *PartialApplyError
	s.Require().True(errors.As(err, &pe), "expected a PartialApplyError, got %v", err)
	s.Run("deletes the object the call created", func() {
		s.Equal([]string{"new-1"}, s.deleted)
		s.Contains(err.Error(), "created by this call and deleted again (rollback): document 1 (ConfigMap ns/new-1)")
	})
	s.Run("says the changed object was not restored", func() {
		s.Contains(err.Error(), "existed before and were changed, NOT restored: document 2 (ConfigMap ns/existing)")
	})
	s.Run("never sends the documents after the failure", func() {
		s.False(s.stored["never"])
	})
}

func (s *ResourcesApplyRollbackSuite) TestStopLeavesWrittenObjects() {
	_, err := s.core.ResourcesApply(context.Background(), manifest("new-2", "fails"), ApplyOptions{ValidateFirst: true})
	s.Require().Error(err)
	s.Empty(s.deleted)
	s.True(s.stored["new-2"])
	s.Contains(err.Error(), "already applied and left in place: document 1 (ConfigMap ns/new-2)")
}

func TestResourcesApplyRollback(t *testing.T) {
	suite.Run(t, new(ResourcesApplyRollbackSuite))
}
