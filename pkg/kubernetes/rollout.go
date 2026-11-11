package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/containers/kubernetes-mcp-server/pkg/version"
)

const deploymentRevisionAnnotation = "deployment.kubernetes.io/revision"

// RolloutStatus summarises a workload's rollout, like kubectl rollout status.
type RolloutStatus struct {
	Kind               string   `json:"kind"`
	Name               string   `json:"name"`
	Namespace          string   `json:"namespace"`
	Generation         int64    `json:"generation"`
	ObservedGeneration int64    `json:"observedGeneration"`
	Replicas           int64    `json:"replicas"`
	UpdatedReplicas    int64    `json:"updatedReplicas"`
	ReadyReplicas      int64    `json:"readyReplicas"`
	AvailableReplicas  int64    `json:"availableReplicas"`
	Revision           string   `json:"revision,omitempty"`
	Complete           bool     `json:"complete"`
	Conditions         []string `json:"conditions,omitempty"`
}

func rolloutGVK(kind string) (*schema.GroupVersionKind, error) {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		return &schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: kind}, nil
	}
	return nil, fmt.Errorf("kind must be Deployment, StatefulSet or DaemonSet, got %q", kind)
}

// RolloutStatusOf reads a Deployment, StatefulSet or DaemonSet and says whether its
// rollout is complete.
func (c *Core) RolloutStatusOf(ctx context.Context, kind, namespace, name string) (*RolloutStatus, error) {
	gvk, err := rolloutGVK(kind)
	if err != nil {
		return nil, err
	}
	obj, err := c.ResourcesGet(ctx, gvk, namespace, name)
	if err != nil {
		return nil, err
	}
	return rolloutStatus(obj), nil
}

func rolloutStatus(obj *unstructured.Unstructured) *RolloutStatus {
	i := func(path ...string) int64 { v, _, _ := unstructured.NestedInt64(obj.Object, path...); return v }
	st := &RolloutStatus{
		Kind: obj.GetKind(), Name: obj.GetName(), Namespace: obj.GetNamespace(),
		Generation: obj.GetGeneration(), ObservedGeneration: i("status", "observedGeneration"),
		Revision: obj.GetAnnotations()[deploymentRevisionAnnotation],
	}
	switch obj.GetKind() {
	case "DaemonSet":
		st.Replicas = i("status", "desiredNumberScheduled")
		st.UpdatedReplicas = i("status", "updatedNumberScheduled")
		st.ReadyReplicas = i("status", "numberReady")
		st.AvailableReplicas = i("status", "numberAvailable")
	default:
		st.Replicas, _, _ = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		st.UpdatedReplicas = i("status", "updatedReplicas")
		st.ReadyReplicas = i("status", "readyReplicas")
		st.AvailableReplicas = i("status", "availableReplicas")
		if obj.GetKind() == "StatefulSet" {
			st.AvailableReplicas = i("status", "availableReplicas")
		}
	}
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		m, _ := c.(map[string]any)
		st.Conditions = append(st.Conditions, fmt.Sprintf("%v=%v (%v) %v", m["type"], m["status"], m["reason"], m["message"]))
	}
	st.Complete = st.ObservedGeneration >= st.Generation && st.UpdatedReplicas == st.Replicas && st.AvailableReplicas == st.Replicas
	return st
}

// RolloutRestart restarts a workload's Pods the way kubectl rollout restart does: by
// setting the kubectl.kubernetes.io/restartedAt annotation on the Pod template.
func (c *Core) RolloutRestart(ctx context.Context, kind, namespace, name string) (*unstructured.Unstructured, error) {
	gvk, err := rolloutGVK(kind)
	if err != nil {
		return nil, err
	}
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
		"annotations": map[string]any{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339)},
	}}}})
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}
	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{FieldManager: version.BinaryName})
}

// RolloutUndo rolls a Deployment back to an earlier revision (the previous one, or
// toRevision) the way kubectl rollout undo does: its Pod template is replaced by the
// template of the ReplicaSet that carried that revision. Returns the revisions from and to.
func (c *Core) RolloutUndo(ctx context.Context, kind, namespace, name string, toRevision int64) (*unstructured.Unstructured, int64, int64, error) {
	if kind != "Deployment" {
		return nil, 0, 0, fmt.Errorf("undo supports Deployments only (StatefulSets and DaemonSets keep their history in ControllerRevisions)")
	}
	gvk, _ := rolloutGVK(kind)
	deployment, err := c.ResourcesGet(ctx, gvk, namespace, name)
	if err != nil {
		return nil, 0, 0, err
	}
	namespace = deployment.GetNamespace()
	current, _ := strconv.ParseInt(deployment.GetAnnotations()[deploymentRevisionAnnotation], 10, 64)
	rsList, err := c.DynamicClient().Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, 0, 0, err
	}
	type revisionTemplate struct {
		revision int64
		template map[string]any
	}
	var history []revisionTemplate
	for _, rs := range rsList.Items {
		owned := false
		for _, ref := range rs.GetOwnerReferences() {
			if ref.UID == deployment.GetUID() {
				owned = true
			}
		}
		revision, err := strconv.ParseInt(rs.GetAnnotations()[deploymentRevisionAnnotation], 10, 64)
		if !owned || err != nil {
			continue
		}
		template, _, _ := unstructured.NestedMap(rs.Object, "spec", "template")
		history = append(history, revisionTemplate{revision, template})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].revision > history[j].revision })
	var target *revisionTemplate
	for i := range history {
		if (toRevision == 0 && history[i].revision < current) || (toRevision != 0 && history[i].revision == toRevision) {
			target = &history[i]
			break
		}
	}
	if target == nil {
		if toRevision != 0 {
			return nil, current, 0, fmt.Errorf("revision %d of Deployment %s/%s not found", toRevision, namespace, name)
		}
		return nil, current, 0, fmt.Errorf("Deployment %s/%s has no revision before %d to roll back to", namespace, name, current)
	}
	// the ReplicaSet's pod-template-hash label belongs to the ReplicaSet, not the template
	if labels, ok, _ := unstructured.NestedStringMap(target.template, "metadata", "labels"); ok {
		delete(labels, "pod-template-hash")
		_ = unstructured.SetNestedStringMap(target.template, labels, "metadata", "labels")
	}
	patch, _ := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": deployment.GetResourceVersion()},
		{"op": "replace", "path": "/spec/template", "value": target.template},
	})
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, 0, 0, err
	}
	updated, err := c.DynamicClient().Resource(*gvr).Namespace(namespace).Patch(ctx, name, types.JSONPatchType, patch, metav1.PatchOptions{FieldManager: version.BinaryName})
	return updated, current, target.revision, err
}
