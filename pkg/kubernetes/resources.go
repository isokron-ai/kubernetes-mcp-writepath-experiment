package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1beta1 "k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
)

const (
	AppKubernetesComponent = "app.kubernetes.io/component"
	AppKubernetesManagedBy = "app.kubernetes.io/managed-by"
	AppKubernetesName      = "app.kubernetes.io/name"
	AppKubernetesPartOf    = "app.kubernetes.io/part-of"
)

func (c *Core) ResourcesList(ctx context.Context, gvk *schema.GroupVersionKind, namespace string, options api.ListOptions) (runtime.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	// Check if operation is allowed for all namespaces (applicable for namespaced resources)
	isNamespaced, err := c.isNamespaced(gvk)
	if err != nil {
		return nil, err
	}
	// The access review only matters for an all-namespaces list; skip it when a namespace is given.
	if isNamespaced && namespace == "" && !c.canIUse(ctx, gvr, namespace, "list") {
		namespace = c.NamespaceOrDefault("")
	} else if !isNamespaced {
		// Cluster-scoped resources never have a namespace, even when the caller provides one.
		namespace = ""
	}
	if options.AsTable {
		return c.resourcesListAsTable(ctx, gvk, gvr, namespace, options)
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).List(ctx, options.ListOptions)
}

func (c *Core) ResourcesGet(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string) (*unstructured.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Core) ResourcesCreateOrUpdate(ctx context.Context, resource string) ([]*unstructured.Unstructured, error) {
	parsedResources, err := ParseManifests(resource)
	if err != nil {
		return nil, err
	}
	return c.resourcesCreateOrUpdate(ctx, parsedResources)
}

// ResourcesApplyObject server-side applies one object that the caller built as
// data, without parsing any manifest text, so that tool arguments placed in its
// fields cannot add documents to the apply.
func (c *Core) ResourcesApplyObject(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	applied, err := c.resourcesCreateOrUpdate(ctx, []*unstructured.Unstructured{obj})
	if err != nil {
		return nil, err
	}
	return applied[0], nil
}

// ParseManifests splits a YAML or JSON stream into Kubernetes objects the way
// kubectl's YAML reader does: a document separator is a line starting with
// "---", optionally followed by spaces and a comment, and documents that hold
// nothing (blank, comment-only, a trailing separator) are skipped. Every
// document is decoded before anything is sent to the cluster, so a document
// that cannot be decoded, or has no apiVersion or kind, fails the whole call
// without writing anything.
func ParseManifests(resource string) ([]*unstructured.Unstructured, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(strings.NewReader(resource)))
	var parsedResources []*unstructured.Unstructured
	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		// documents are numbered as the objects they hold: empty ones are not counted
		index := len(parsedResources) + 1
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", index, err)
		}
		obj := &unstructured.Unstructured{}
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(document), 4096).Decode(&obj.Object); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("document %d: %w", index, err)
		}
		if len(obj.Object) == 0 {
			// blank or comment-only document, or a trailing separator
			continue
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			return nil, fmt.Errorf("document %d: apiVersion and kind are required (nothing was applied)", index)
		}

		// remove the status from the resource, disallowing agent from directly editing (only controllers should be allowed to do this)
		delete(obj.Object, "status")

		parsedResources = append(parsedResources, obj)
	}
	if len(parsedResources) == 0 {
		return nil, errors.New("no Kubernetes object found in the resource argument")
	}
	return parsedResources, nil
}

func (c *Core) ResourcesDelete(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string, gracePeriodSeconds *int64) error {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return err
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: gracePeriodSeconds,
	})
}

func (c *Core) ResourcesScale(
	ctx context.Context,
	gvk *schema.GroupVersionKind,
	namespace, name string,
	desiredScale int64,
	shouldScale bool,
) (*unstructured.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	resourceClient := c.DynamicClient().Resource(*gvr).Namespace(namespace)

	scale, err := resourceClient.Get(ctx, name, metav1.GetOptions{}, "scale")
	if err != nil {
		return nil, err
	}
	// spec.replicas is omitted from the API's answer when it is 0; record it explicitly,
	// with the value before any change, so callers can state the change.
	previous, _, _ := unstructured.NestedInt64(scale.Object, "spec", "replicas")
	_ = unstructured.SetNestedField(scale.Object, previous, "spec", "replicas")
	scale.SetAnnotations(nil)

	if shouldScale {
		if err := unstructured.SetNestedField(scale.Object, desiredScale, "spec", "replicas"); err != nil {
			return scale, fmt.Errorf("failed to set .spec.replicas on scale object %v: %w", scale, err)
		}

		scale, err = resourceClient.Update(ctx, scale, metav1.UpdateOptions{}, "scale")
		if err != nil {
			return scale, fmt.Errorf("failed to update scale: %w", err)
		}
		_ = unstructured.SetNestedField(scale.Object, desiredScale, "spec", "replicas")
		scale.SetAnnotations(map[string]string{ScalePreviousReplicasAnnotation: strconv.FormatInt(previous, 10)})
	}

	return scale, nil
}

// ScalePreviousReplicasAnnotation is set (in the returned object only, never on the
// cluster) by ResourcesScale on a changed Scale to the replicas before the change.
const ScalePreviousReplicasAnnotation = "kubernetes-mcp-server/previous-replicas"

// resourcesListAsTable retrieves a list of resources in a table format.
// It's almost identical to the dynamic.DynamicClient implementation, but it uses a specific Accept header to request the table format.
// dynamic.DynamicClient does not provide a way to set the HTTP header (TODO: create an issue to request this feature)
func (c *Core) resourcesListAsTable(ctx context.Context, gvk *schema.GroupVersionKind, gvr *schema.GroupVersionResource, namespace string, options api.ListOptions) (runtime.Unstructured, error) {
	var url []string
	if len(gvr.Group) == 0 {
		url = append(url, "api")
	} else {
		url = append(url, "apis", gvr.Group)
	}
	url = append(url, gvr.Version)
	if len(namespace) > 0 {
		url = append(url, "namespaces", namespace)
	}
	url = append(url, gvr.Resource)
	var table metav1.Table
	err := c.CoreV1().RESTClient().
		Get().
		SetHeader("Accept", strings.Join([]string{
			fmt.Sprintf("application/json;as=Table;v=%s;g=%s", metav1.SchemeGroupVersion.Version, metav1.GroupName),
			fmt.Sprintf("application/json;as=Table;v=%s;g=%s", metav1beta1.SchemeGroupVersion.Version, metav1beta1.GroupName),
			"application/json",
		}, ",")).
		AbsPath(url...).
		SpecificallyVersionedParams(&options.ListOptions, ParameterCodec, schema.GroupVersion{Version: "v1"}).
		Do(ctx).Into(&table)
	if err != nil {
		return nil, err
	}
	// Add metav1.Table apiVersion and kind to the unstructured object (server may not return these fields)
	table.SetGroupVersionKind(metav1.SchemeGroupVersion.WithKind("Table"))
	// Add additional columns for fields that aren't returned by the server
	table.ColumnDefinitions = append([]metav1.TableColumnDefinition{
		{Name: "apiVersion", Type: "string"},
		{Name: "kind", Type: "string"},
	}, table.ColumnDefinitions...)
	for i := range table.Rows {
		row := &table.Rows[i]
		row.Cells = append([]interface{}{
			gvr.GroupVersion().String(),
			gvk.Kind,
		}, row.Cells...)
	}
	unstructuredObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&table)
	return &unstructured.Unstructured{Object: unstructuredObject}, err
}

// ApplyOutcome names one document of a multi-document apply.
type ApplyOutcome struct {
	Index     int
	Kind      string
	Namespace string
	Name      string
}

func (o ApplyOutcome) String() string {
	ref := o.Name
	if o.Namespace != "" {
		ref = o.Namespace + "/" + o.Name
	}
	return fmt.Sprintf("document %d (%s %s)", o.Index, o.Kind, ref)
}

// PartialApplyError is returned when a multi-document apply fails after some
// documents were already applied. Kubernetes has no multi-object
// transaction, so those stay in place; the error names them, the document
// that failed and the documents that were never sent.
type PartialApplyError struct {
	Total        int
	Applied      []ApplyOutcome
	Failed       ApplyOutcome
	NotAttempted []ApplyOutcome
	Err          error
}

func (e *PartialApplyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s of %d failed: %v", e.Failed, e.Total, e.Err)
	if len(e.Applied) > 0 {
		b.WriteString("; already applied and left in place: ")
		b.WriteString(joinOutcomes(e.Applied))
	}
	if len(e.NotAttempted) > 0 {
		b.WriteString("; not attempted: ")
		b.WriteString(joinOutcomes(e.NotAttempted))
	}
	return b.String()
}

func (e *PartialApplyError) Unwrap() error { return e.Err }

func joinOutcomes(outcomes []ApplyOutcome) string {
	parts := make([]string, len(outcomes))
	for i, o := range outcomes {
		parts[i] = o.String()
	}
	return strings.Join(parts, ", ")
}

func (c *Core) resourcesCreateOrUpdate(ctx context.Context, resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	// Resolve every document's resource and namespace before the first write, so an
	// unknown kind or apiVersion fails the call with nothing applied. A kind defined by a
	// CustomResourceDefinition earlier in the same input cannot resolve yet; it is
	// resolved after that CRD has been applied.
	gvrs := make([]*schema.GroupVersionResource, len(resources))
	namespaces := make([]string, len(resources))
	outcomes := make([]ApplyOutcome, len(resources))
	crdBefore := false
	unresolved := func(i int, err error) error {
		if len(resources) == 1 {
			return err
		}
		return fmt.Errorf("%s: %w (nothing was applied)", outcomes[i], err)
	}
	for i, obj := range resources {
		gvk := obj.GroupVersionKind()
		outcomes[i] = ApplyOutcome{Index: i + 1, Kind: gvk.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
		gvr, err := c.resourceFor(&gvk)
		if err != nil {
			if crdBefore && meta.IsNoMatchError(err) {
				continue
			}
			return nil, unresolved(i, err)
		}
		namespace, err := c.effectiveNamespace(&gvk, obj.GetNamespace())
		if err != nil {
			return nil, unresolved(i, err)
		}
		gvrs[i], namespaces[i] = gvr, namespace
		outcomes[i].Namespace = namespace
		if gvk.Kind == "CustomResourceDefinition" {
			crdBefore = true
		}
	}
	partial := func(i int, err error) error {
		if len(resources) == 1 {
			return err
		}
		return &PartialApplyError{
			Total:        len(resources),
			Applied:      outcomes[:i],
			Failed:       outcomes[i],
			NotAttempted: outcomes[i+1:],
			Err:          err,
		}
	}
	for i, obj := range resources {
		gvk := obj.GroupVersionKind()
		if gvrs[i] == nil {
			gvr, err := c.resourceFor(&gvk)
			if err != nil {
				return nil, partial(i, err)
			}
			namespace, err := c.effectiveNamespace(&gvk, obj.GetNamespace())
			if err != nil {
				return nil, partial(i, err)
			}
			gvrs[i], namespaces[i] = gvr, namespace
			outcomes[i].Namespace = namespace
		}
		obj.SetNamespace(namespaces[i])
		applied, err := c.DynamicClient().Resource(*gvrs[i]).Namespace(namespaces[i]).Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
			FieldManager: version.BinaryName,
			Force:        true,
		})
		if err != nil {
			return nil, partial(i, err)
		}
		resources[i] = applied
		// Clear the cache to ensure the next operation is performed on the latest exposed APIs (will change after the CRD creation)
		if gvk.Kind == "CustomResourceDefinition" {
			c.RESTMapper().Reset()
		}
	}
	return resources, nil
}

// APIResourceInfo describes one Kubernetes API resource type registered on
// the cluster: its preferred apiVersion, kind, plural resource name, and
// whether it's namespaced. Mirrors the fields `kubectl api-resources` shows.
type APIResourceInfo struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespaced bool   `json:"namespaced"`
}

// APIResourcesList returns the API resource types registered on the cluster,
// using the server's preferred version for each kind -- the same data
// `kubectl api-resources` shows. Unlike ResourcesList/ResourcesGet, callers
// don't need to already know a kind's apiVersion; this is how they discover
// it, including for CRDs the caller has no built-in knowledge of.
//
// When kindFilter is non-empty, only kinds whose name equals kindFilter
// (case-insensitive) are returned; an empty kindFilter returns every
// registered resource type. This function itself imposes no requirement on
// kindFilter -- the api_resources_list tool handler is what makes it
// mandatory, since an LLM caller dumping every API resource type on a
// cluster with many CRDs into its own context is a token cost and a source
// of confusion, not a useful default. The context-aware preferred discovery
// call can return partial results alongside a non-nil error (e.g. one API
// group temporarily unavailable, or a large CRD catalog slowing discovery -- see
// containers/kubernetes-mcp-server#283); this returns whatever resources
// were discovered together with that error rather than discarding them, so
// callers can decide whether partial results are still useful.
func (c *Core) APIResourcesList(ctx context.Context, kindFilter string) ([]APIResourceInfo, error) {
	// Use the raw discovery client because the cached discovery client can
	// remain stale after a CRD is installed outside this server. Resetting the
	// RESTMapper below refreshes resource mapping only; it does not invalidate
	// the discovery cache. Preferred discovery is intentional here: callers
	// need one server-preferred top-level version per kind. A future use case
	// that needs every served version or subresource can use a broader
	// discovery method instead.
	rawDiscoveryClient := c.RawDiscoveryClient()
	var (
		lists []*metav1.APIResourceList
		err   error
	)
	if contextual, ok := rawDiscoveryClient.(discovery.ServerResourcesInterfaceWithContext); ok {
		// Use the concrete client method when available: client-go wraps this
		// method with its standard discovery retry behavior.
		lists, err = contextual.ServerPreferredResourcesWithContext(ctx)
	} else if aggregated, ok := rawDiscoveryClient.(discovery.AggregatedDiscoveryInterface); ok {
		lists, err = discovery.ServerPreferredResourcesWithContext(
			ctx,
			discovery.ToAggregatedDiscoveryInterfaceWithContext(aggregated),
		)
	} else {
		lists, err = discovery.ServerPreferredResourcesWithContext(
			ctx,
			discovery.ToDiscoveryInterfaceWithContext(rawDiscoveryClient),
		)
	}
	resources := make([]APIResourceInfo, 0, len(lists))
	for _, list := range lists {
		if list == nil {
			continue
		}
		for _, r := range list.APIResources {
			if kindFilter != "" && !strings.EqualFold(r.Kind, kindFilter) {
				continue
			}
			resources = append(resources, APIResourceInfo{
				APIVersion: list.GroupVersion,
				Kind:       r.Kind,
				Name:       r.Name,
				Namespaced: r.Namespaced,
			})
		}
	}
	return resources, err
}

func (c *Core) resourceFor(gvk *schema.GroupVersionKind) (*schema.GroupVersionResource, error) {
	m, err := c.RESTMapper().RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
	if err != nil {
		if !meta.IsNoMatchError(err) {
			return nil, err
		}
		// The RESTMapper builds and caches its full group/resource snapshot on
		// first use and never refreshes it on its own -- resourcesCreateOrUpdate
		// resets it, but only when a CustomResourceDefinition is applied
		// through *this server's own* resources_create_or_update tool. A CRD
		// installed any other way (kubectl, an operator, Helm, a different MCP
		// client) after the mapper was already warmed left every other
		// resource tool permanently unable to find it without a server
		// restart. Reset once and retry so a fresh CRD resolves without one.
		c.RESTMapper().Reset()
		m, err = c.RESTMapper().RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
		if err != nil {
			return nil, err
		}
	}
	return &m.Resource, nil
}

func (c *Core) effectiveNamespace(gvk *schema.GroupVersionKind, namespace string) (string, error) {
	namespaced, err := c.isNamespaced(gvk)
	if err != nil {
		return "", err
	}
	if !namespaced {
		return "", nil
	}
	return c.NamespaceOrDefault(namespace), nil
}

func (c *Core) isNamespaced(gvk *schema.GroupVersionKind) (bool, error) {
	apiResourceList, err := c.DiscoveryClient().ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return false, err
	}
	for _, apiResource := range apiResourceList.APIResources {
		if apiResource.Kind == gvk.Kind {
			return apiResource.Namespaced, nil
		}
	}
	return false, nil
}

func (c *Core) supportsGroupVersion(groupVersion string) bool {
	if _, err := c.DiscoveryClient().ServerResourcesForGroupVersion(groupVersion); err != nil {
		return false
	}
	return true
}

func (c *Core) canIUse(ctx context.Context, gvr *schema.GroupVersionResource, namespace, verb string) bool {
	allowed, _ := CanI(ctx, c.AuthorizationV1(), gvr, namespace, "", verb)
	return allowed
}
