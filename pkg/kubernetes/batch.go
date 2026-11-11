package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"

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

// ListableKind is one kind ResourcesListableNamespacedKinds returns.
type ListableKind struct {
	APIVersion string
	Kind       string
}

// ResourcesListableNamespacedKinds returns every namespaced kind (server-preferred
// version) that supports list, except Events, which are noise for "what is in this
// namespace" and have their own tool.
func (c *Core) ResourcesListableNamespacedKinds(ctx context.Context) ([]ListableKind, error) {
	lists, err := discovery.ServerPreferredNamespacedResources(c.RawDiscoveryClient())
	var kinds []ListableKind
	for _, list := range lists {
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") || r.Kind == "Event" || !slices.Contains(r.Verbs, "list") {
				continue
			}
			kinds = append(kinds, ListableKind{APIVersion: list.GroupVersion, Kind: r.Kind})
		}
	}
	slices.SortFunc(kinds, func(a, b ListableKind) int {
		return strings.Compare(a.APIVersion+"/"+a.Kind, b.APIVersion+"/"+b.Kind)
	})
	if len(kinds) == 0 && err != nil {
		return nil, err
	}
	return kinds, nil
}
