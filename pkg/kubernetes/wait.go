package kubernetes

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// MaxWait bounds every wait, below the request timeouts MCP clients commonly use.
const MaxWait = 120 * time.Second

// WaitPollInterval is how often a wait re-reads the object.
var WaitPollInterval = time.Second

// Readiness says whether obj has reached the state a caller waits for after a write:
// a workload's rollout is complete, a Pod is Ready, a Job is Complete. Any other kind
// is ready once it exists.
func Readiness(obj *unstructured.Unstructured) (bool, string) {
	switch obj.GetKind() {
	case "Deployment", "StatefulSet", "DaemonSet":
		st := rolloutStatus(obj)
		return st.Complete, fmt.Sprintf("%d/%d updated, %d ready, %d available", st.UpdatedReplicas, st.Replicas, st.ReadyReplicas, st.AvailableReplicas)
	case "Pod":
		phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
		if phase == "Succeeded" {
			return true, "Succeeded"
		}
		return conditionTrue(obj, "Ready"), "phase " + phase
	case "Job":
		if conditionTrue(obj, "Failed") {
			return true, "Failed"
		}
		return conditionTrue(obj, "Complete"), "not complete"
	}
	return true, "exists"
}

func conditionTrue(obj *unstructured.Unstructured, conditionType string) bool {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		m, _ := c.(map[string]any)
		if m["type"] == conditionType && m["status"] == "True" {
			return true
		}
	}
	return false
}

// WaitFor re-reads an object until it is ready (gone == false) or gone (gone == true),
// or until timeout. It returns whether the state was reached and the last state seen.
func (c *Core) WaitFor(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string, gone bool, timeout time.Duration) (bool, string, error) {
	if timeout > MaxWait {
		timeout = MaxWait
	}
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return false, "", err
	}
	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return false, "", err
	}
	client := c.DynamicClient().Resource(*gvr).Namespace(namespace)
	deadline := time.Now().Add(timeout)
	state := ""
	for {
		obj, err := client.Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			if gone {
				return true, "gone", nil
			}
			state = "not found"
		case err != nil:
			return false, state, err
		case gone:
			state = "still exists"
			if obj.GetDeletionTimestamp() != nil {
				state = "terminating"
				if f := obj.GetFinalizers(); len(f) > 0 {
					state = fmt.Sprintf("held by finalizers %v", f)
				}
			}
		default:
			var ready bool
			ready, state = Readiness(obj)
			if ready {
				return true, state, nil
			}
		}
		if time.Now().Add(WaitPollInterval).After(deadline) {
			return false, state, nil
		}
		select {
		case <-ctx.Done():
			return false, state, ctx.Err()
		case <-time.After(WaitPollInterval):
		}
	}
}
