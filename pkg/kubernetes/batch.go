package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/containers/kubernetes-mcp-server/pkg/version"
)

// ResourcesSelect lists the objects of a kind that match a label selector, in the
// given (or configured) namespace. An empty selector is refused: a batch over a
// whole kind must be asked for by name.
func (c *Core) ResourcesSelect(ctx context.Context, gvk *schema.GroupVersionKind, namespace, labelSelector string) ([]unstructured.Unstructured, string, error) {
	if strings.TrimSpace(labelSelector) == "" {
		return nil, "", fmt.Errorf("labelSelector must not be empty")
	}
	if _, err := labels.Parse(labelSelector); err != nil {
		return nil, "", fmt.Errorf("invalid labelSelector: %w", err)
	}
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, "", err
	}
	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, "", err
	}
	list, err := c.DynamicClient().Resource(*gvr).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, "", err
	}
	return list.Items, namespace, nil
}

// ResourcesLabel sets (or, with a nil value, removes) labels on one object with a
// JSON merge patch on metadata.labels. Other labels are left alone.
func (c *Core) ResourcesLabel(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string, set map[string]*string) (*unstructured.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}
	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": set}})
	if err != nil {
		return nil, err
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).Patch(ctx, name, types.MergePatchType, body, metav1.PatchOptions{FieldManager: version.BinaryName})
}
