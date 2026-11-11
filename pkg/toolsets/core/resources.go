package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"github.com/containers/kubernetes-mcp-server/pkg/klogutil"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/mcpapps"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

func initResources(ctx context.Context, inspector api.ClusterInspector, targetCompatibilityFiltersEnabled bool) []api.ServerTool {
	// commonApiVersion lists example apiVersion/kind pairs that are appended to the
	// resources_* tool descriptions as hints for the model. It is extended with
	// target-specific kinds (e.g. OpenShift Route) when a target cluster exposes them,
	// so the examples the model sees match the cluster it is talking to.
	commonApiVersion := "v1 Pod, v1 Service, v1 Node, apps/v1 Deployment, networking.k8s.io/v1 Ingress"
	if !targetCompatibilityFiltersEnabled {
		commonApiVersion += ", route.openshift.io/v1 Route"
	} else {
		if api.AnyTargetHasGVK(ctx, inspector, schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}) {
			commonApiVersion += ", route.openshift.io/v1 Route"
		}
	}
	commonApiVersion = fmt.Sprintf("(common apiVersion and kind include: %s)", commonApiVersion)
	return []api.ServerTool{
		{Tool: api.Tool{
			Name:        "resources_list",
			Description: "List Kubernetes resources and objects in the current cluster by providing their apiVersion and kind and optionally the namespace and label selector." + ListPageSentence + "\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: PageProperties(map[string]*jsonschema.Schema{
					"apiVersion": {
						Type:        "string",
						Description: "apiVersion of the resources (examples of valid apiVersion are: v1, apps/v1, networking.k8s.io/v1)",
					},
					"kind": {
						Type:        "string",
						Description: "kind of the resources (examples of valid kind are: Pod, Service, Deployment, Ingress)",
					},
					"namespace": {
						Type:        "string",
						Description: "Optional Namespace to retrieve the namespaced resources from (ignored in case of cluster scoped resources). If not provided, will list resources from all namespaces",
					},
					"labelSelector": {
						Type:        "string",
						Description: "Optional Kubernetes label selector (e.g. 'app=myapp,env=prod' or 'app in (myapp,yourapp)'), use this option when you want to filter the resources by label",
						Pattern:     REGEX_LABELSELECTOR_VALID_CHARS,
					},
					"fieldSelector": {
						Type:        "string",
						Description: "Optional Kubernetes field selector to filter resources by field values (e.g. 'status.phase=Running', 'metadata.name=myresource'). Supported fields vary by resource type. For Pods: metadata.name, metadata.namespace, spec.nodeName, spec.restartPolicy, spec.schedulerName, spec.serviceAccountName, status.phase (Pending/Running/Succeeded/Failed/Unknown), status.podIP, status.nominatedNodeName. See https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/",
						Pattern:     REGEX_FIELDSELECTOR,
					},
				}),
				Required: []string{"apiVersion", "kind"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: List",
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"list"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}), Handler: resourcesList,
			App: mcpapps.Table("ui://kubernetes-mcp-server/resources-list", "Resources list", mcpapps.WithDescription("Interactive table of Kubernetes resources"))},
		{Tool: api.Tool{
			Name:        "resources_get",
			Description: "Get a Kubernetes resource in the current cluster by providing its apiVersion, kind, optionally the namespace, and its name\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"apiVersion": {
						Type:        "string",
						Description: "apiVersion of the resource (examples of valid apiVersion are: v1, apps/v1, networking.k8s.io/v1)",
					},
					"kind": {
						Type:        "string",
						Description: "kind of the resource (examples of valid kind are: Pod, Service, Deployment, Ingress)",
					},
					"namespace": {
						Type:        "string",
						Description: "Optional Namespace to retrieve the namespaced resource from (ignored in case of cluster scoped resources). If not provided, will get resource from configured namespace",
					},
					"name": {
						Type:        "string",
						Description: "Name of the resource",
					},
				},
				Required: []string{"apiVersion", "kind", "name"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Get",
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"get"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace:    &api.RBACNamespace{Argument: "namespace"},
			ResourceName: &api.RBACResourceName{Argument: "name"},
		}), Handler: resourcesGet,
			App: mcpapps.Resource("ui://kubernetes-mcp-server/resources-get", "Resource details", mcpapps.WithDescription("Interactive Kubernetes resource details"))},
		{Tool: api.Tool{
			Name:        "resources_create_or_update",
			Description: "Create or update a Kubernetes resource via Server-Side Apply. The manifest is the complete desired state: any field this tool previously set and the new manifest omits is removed. To edit an existing resource, fetch it with resources_get, modify it, then re-apply the full resource.\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"resource": {
						Type:        "string",
						Description: "Complete YAML or JSON representation of the Kubernetes resource (full desired state, not a partial patch). Include apiVersion, kind, metadata, and the full spec.",
					},
					"output": outputProperty,
					"force": {
						Type:        "boolean",
						Description: "Take over fields that other field managers own (server-side apply force). Default false: if the manifest sets a field another writer owns, nothing is written and the error names the fields and their managers, so a concurrent change is not silently reverted.",
					},
					"mode": {
						Type:        "string",
						Enum:        []any{"apply", "create"},
						Description: "'apply' (default) creates the object or updates the fields it sets. 'create' only creates: an existing object of the same name is left untouched and the error names it and its field managers.",
					},
					"dry_run": {
						Type:        "boolean",
						Description: "Validate every document with a server-side dry run (admission, schema, field ownership) and write nothing. The result lists each document as the API would store it. A multi-document manifest is always validated this way before its first write.",
					},
					"on_failure": {
						Type:        "string",
						Enum:        []any{"stop", "rollback"},
						Description: "For multi-document manifests, if a document fails after earlier ones were written: 'stop' (default) leaves the written ones and lists them; 'rollback' deletes the ones this call created. Objects that existed before are not restored. This is a compensating action, not a transaction: the result lists exactly what was rolled back and what was not.",
					},
				},
				Required: []string{"resource"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Create or Update",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs:  []string{"create", "patch"},
			Target: api.RBACTarget{Manifest: &api.RBACManifestTarget{Argument: "resource"}},
		}), Handler: resourcesCreateOrUpdate},
		{Tool: api.Tool{
			Name: "resources_patch",
			Description: "Patch one Kubernetes resource, like kubectl patch: change, add or remove individual fields without sending the whole object. " +
				"Use it for one-field edits, to remove a label, annotation or finalizer, or to delete a map key that another tool or person set (server-side apply cannot remove fields it does not own). " +
				"Pass resourceVersion (from a read) to make the patch fail instead of overwriting a change made since.\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"apiVersion": {Type: "string", Description: "apiVersion of the resource (examples of valid apiVersion are: v1, apps/v1, networking.k8s.io/v1)"},
					"kind":       {Type: "string", Description: "kind of the resource (examples of valid kind are: Pod, Service, Deployment, Ingress)"},
					"namespace":  {Type: "string", Description: "Optional Namespace of the resource (ignored for cluster scoped resources). If not provided, the configured namespace is used"},
					"name":       {Type: "string", Description: "Name of the resource"},
					"patchType": {
						Type: "string",
						Enum: []any{"merge", "strategic", "json"},
						Description: "'merge' (JSON merge patch, RFC 7386: an object; null removes a key), 'strategic' (built-in kinds only: merges lists by key, e.g. containers by name), " +
							"'json' (JSON patch, RFC 6902: an array of operations such as {\"op\":\"remove\",\"path\":\"/metadata/finalizers\"})",
					},
					"patch": {Type: "string", Description: "The patch, as JSON or YAML"},
					"resourceVersion": {
						Type:        "string",
						Description: "Optional precondition: the metadata.resourceVersion you read. The patch is refused with a conflict if the object has changed since.",
					},
					"output": outputProperty,
				},
				Required: []string{"apiVersion", "kind", "name", "patchType", "patch"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Patch",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"patch"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace:    &api.RBACNamespace{Argument: "namespace"},
			ResourceName: &api.RBACResourceName{Argument: "name"},
		}), Handler: resourcesPatch},
		{Tool: api.Tool{
			Name: "resources_label",
			Description: "Add, change or remove labels on one object (name) or on every object of a kind that matches a label selector, without sending the objects. " +
				"Other labels are left alone. With labelSelector each object is handled on its own (best effort) and gets its own result line.\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"apiVersion": {Type: "string", Description: "apiVersion of the resources (examples of valid apiVersion are: v1, apps/v1, networking.k8s.io/v1)"},
					"kind":       {Type: "string", Description: "kind of the resources (examples of valid kind are: Pod, Service, Deployment, ConfigMap)"},
					"namespace":  {Type: "string", Description: "Optional Namespace (ignored for cluster scoped resources). If not provided, the configured namespace is used"},
					"name":       {Type: "string", Description: "Name of one object to label (or use labelSelector)"},
					"labelSelector": {
						Type:        "string",
						Description: "Instead of name: label every object of this kind in the namespace whose labels match (e.g. 'app=web'). Must not be empty.",
						Pattern:     REGEX_LABELSELECTOR_VALID_CHARS,
					},
					"labels": {
						Type:                 "object",
						Description:          "Labels to set, as key: value; a null value removes the label (e.g. {\"tier\": \"cache\", \"stale\": null})",
						AdditionalProperties: &jsonschema.Schema{Types: []string{"string", "null"}},
					},
					"dry_run": {Type: "boolean", Description: "With labelSelector: list the objects that would be labelled and change nothing."},
				},
				Required: []string{"apiVersion", "kind", "labels"},
			},
			Annotations: api.ToolAnnotations{
				Title: "Resources: Label",
				// changing or removing labels can move objects out of selectors (Services, ReplicaSets)
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"list", "patch"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}), Handler: resourcesLabel},
		{Tool: api.Tool{
			Name:        "resources_delete",
			Description: "Delete a Kubernetes resource in the current cluster by providing its apiVersion, kind, optionally the namespace, and its name\n" + commonApiVersion,
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"apiVersion": {
						Type:        "string",
						Description: "apiVersion of the resource (examples of valid apiVersion are: v1, apps/v1, networking.k8s.io/v1)",
					},
					"kind": {
						Type:        "string",
						Description: "kind of the resource (examples of valid kind are: Pod, Service, Deployment, Ingress)",
					},
					"namespace": {
						Type:        "string",
						Description: "Optional Namespace to delete the namespaced resource from (ignored in case of cluster scoped resources). If not provided, will delete resource from configured namespace",
					},
					"name": {
						Type:        "string",
						Description: "Name of the resource",
					},
					"gracePeriodSeconds": {
						Type:        "integer",
						Description: "Optional duration in seconds before the object should be deleted. Value must be non-negative integer. The value zero indicates delete immediately. If this value is nil, the default grace period for the specified type will be used",
					},
					"propagationPolicy": {
						Type:        "string",
						Enum:        []any{"Background", "Foreground", "Orphan"},
						Description: "What happens to dependents (for example a Job's Pods): Background (default, like kubectl delete) deletes them after the object, Foreground before it, Orphan leaves them.",
					},
					"uid": {
						Type:        "string",
						Description: "Optional precondition: the metadata.uid you read. The delete is refused if the object was deleted and recreated under the same name since.",
					},
					"resourceVersion": {
						Type:        "string",
						Description: "Optional precondition: the metadata.resourceVersion you read. The delete is refused if the object changed since.",
					},
					"labelSelector": {
						Type:        "string",
						Description: "Instead of name: act on every object of this kind in the namespace whose labels match (e.g. 'cache=stale'). Must not be empty. Each object is handled on its own (best effort) and gets its own result line.",
						Pattern:     REGEX_LABELSELECTOR_VALID_CHARS,
					},
					"dry_run": {
						Type:        "boolean",
						Description: "With labelSelector: list the objects that would be affected and change nothing.",
					},
				},
				Required: []string{"apiVersion", "kind"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Delete",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"list"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}, api.RBACRequirement{
			Verbs: []string{"delete"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace:    &api.RBACNamespace{Argument: "namespace"},
			ResourceName: &api.RBACResourceName{Argument: "name"},
		}), Handler: resourcesDelete},
		{Tool: api.Tool{
			Name:        "resources_scale",
			Description: "Get or update the scale of a Kubernetes resource in the current cluster by providing its apiVersion, kind, name, and optionally the namespace. If the scale is set in the tool call, the scale will be updated to that value. Always returns the current scale of the resource",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"apiVersion": {
						Type:        "string",
						Description: "apiVersion of the resource (examples of valid apiVersion are apps/v1)",
					},
					"kind": {
						Type:        "string",
						Description: "kind of the resource (examples of valid kind are: StatefulSet, Deployment)",
					},
					"namespace": {
						Type:        "string",
						Description: "Optional Namespace to get/update the namespaced resource scale from (ignored in case of cluster scoped resources). If not provided, will get/update resource scale from configured namespace",
					},
					"name": {
						Type:        "string",
						Description: "Name of the resource",
					},
					"scale": {
						Type:        "integer",
						Description: "Optional scale to update the resources scale to. If not provided, will return the current scale of the resource, and not update it",
					},
					"output": outputProperty,
					"labelSelector": {
						Type:        "string",
						Description: "Instead of name: act on every object of this kind in the namespace whose labels match (e.g. 'cache=stale'). Must not be empty. Each object is handled on its own (best effort) and gets its own result line.",
						Pattern:     REGEX_LABELSELECTOR_VALID_CHARS,
					},
					"dry_run": {
						Type:        "boolean",
						Description: "With labelSelector: list the objects that would be affected and change nothing.",
					},
				},
				Required: []string{"apiVersion", "kind"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Scale",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs: []string{"list"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
			}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}, api.RBACRequirement{
			Verbs: []string{"get", "update"},
			Target: api.RBACTarget{GVK: &api.RBACGVKTarget{
				APIVersionArgument: "apiVersion",
				KindArgument:       "kind",
				Subresource:        "scale",
			}},
			Namespace:    &api.RBACNamespace{Argument: "namespace"},
			ResourceName: &api.RBACResourceName{Argument: "name"},
		}), Handler: resourcesScale},
		{Tool: api.Tool{
			Name:        "api_resources_list",
			Description: "Look up the Kubernetes API resource type(s) matching a specific kind you already have in mind (apiVersion, plural resource name, whether namespaced), using the server's preferred version for each match -- the same data 'kubectl api-resources' shows for that kind. Use this to discover the correct apiVersion for a kind before calling resources_get, resources_list, resources_create_or_update, resources_delete, or resources_scale, especially for CRDs whose apiVersion isn't already known.",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"kind": {
						Type:        "string",
						MinLength:   ptr.To(1),
						Description: "Required case-insensitive exact filter on kind (e.g. 'Deployment', 'Route'). Matches the same Kind across every API group and version, so the result can still contain multiple API resource entries.",
					},
				},
				Required: []string{"kind"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "API Resources: List by Kind",
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACUnbounded(
			"requires Kubernetes API discovery non-resource URL access across all API groups; current RBAC metadata cannot express non-resource URLs",
		), Handler: apiResourcesList},
	}
}

// apiResourcesListResult wraps APIResourceInfo in an object (rather than
// returning a bare slice) per the MCP spec's requirement that
// structuredContent be a JSON object, and carries a warning when discovery
// returned partial results instead of silently dropping them.
type apiResourcesListResult struct {
	Resources []kubernetes.APIResourceInfo `json:"resources"`
	Warning   string                       `json:"warning,omitempty"`
}

func apiResourcesList(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	kindFilter := strings.TrimSpace(p.RequiredString("kind"))
	if err := p.Err(); err != nil {
		return api.NewToolCallResultStructured(nil, fmt.Errorf("failed to list API resources: %w", err)), nil
	}
	if kindFilter == "" {
		return api.NewToolCallResultStructured(nil, errors.New("failed to list API resources: kind parameter must not be empty")), nil
	}
	if params.Config == nil {
		return api.NewToolCallResultStructured(nil, errors.New("failed to list API resources: config cannot be nil")), nil
	}

	resources, err := kubernetes.NewCore(params).APIResourcesList(params.Context, kindFilter)
	if err != nil && len(resources) == 0 {
		return api.NewToolCallResultStructured(nil, fmt.Errorf("failed to list API resources: %w", err)), nil
	}
	resources = filterDeniedAPIResources(params.Context, resources, params.Config.DeniedResources.Get())

	result := apiResourcesListResult{Resources: resources}
	if err != nil {
		// Partial discovery failure (e.g. one API group unavailable): still
		// return what was found instead of discarding it -- see
		// kubernetes.Core.APIResourcesList's doc comment.
		result.Warning = fmt.Sprintf("some API groups could not be queried and are omitted from this list: %v", err)
	}
	return api.NewToolCallResultStructured(result, nil), nil
}

func filterDeniedAPIResources(ctx context.Context, resources []kubernetes.APIResourceInfo, denied []config.GroupVersionKind) []kubernetes.APIResourceInfo {
	if len(denied) == 0 {
		return resources
	}

	filtered := make([]kubernetes.APIResourceInfo, 0, len(resources))
	for _, resource := range resources {
		if config.IsDeniedResource(resource.APIVersion, resource.Kind, denied) {
			klogutil.LogInfo(
				klogutil.FromContext(ctx).V(4),
				"Omitting denied API resource from discovery",
				klogutil.Field("apiVersion", resource.APIVersion),
				klogutil.Field("kind", resource.Kind),
				klogutil.Field("resource", resource.Name),
			)
			continue
		}
		filtered = append(filtered, resource)
	}
	return filtered
}

func resourcesList(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	namespace := params.GetArguments()["namespace"]
	if namespace == nil {
		namespace = ""
	}
	labelSelector := params.GetArguments()["labelSelector"]
	resourceListOptions := api.ListOptions{
		AsTable: params.ListOutput.AsTable(),
	}

	if labelSelector != nil {
		l, ok := labelSelector.(string)
		if !ok {
			return api.NewToolCallResult("", fmt.Errorf("labelSelector is not a string")), nil
		}
		resourceListOptions.LabelSelector = l
	}
	fieldSelector := params.GetArguments()["fieldSelector"]
	if fieldSelector != nil {
		f, ok := fieldSelector.(string)
		if !ok {
			return api.NewToolCallResult("", fmt.Errorf("fieldSelector is not a string")), nil
		}
		resourceListOptions.FieldSelector = f
	}
	if err := ApplyListPage(api.WrapParams(params), &resourceListOptions.ListOptions); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list resources: %w", err)), nil
	}
	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list resources, %s", err)), nil
	}

	ns, ok := namespace.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("namespace is not a string")), nil
	}

	ret, err := kubernetes.NewCore(params).ResourcesList(params, gvk, ns, resourceListOptions)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list resources: %w", ListErr(err))), nil
	}
	printed, err := params.ListOutput.PrintObjStructured(ret)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to format resources: %w", err)), nil
	}
	// Empty Table responses have no structured output from the printer. Supply
	// an explicit empty list for Apps without altering nonempty or non-Apps results.
	if params.Config.AppsEnabled.Get() && printed.Structured == nil && ret.GetObjectKind().GroupVersionKind() == metav1.SchemeGroupVersion.WithKind("Table") {
		if rows, _, err := unstructured.NestedSlice(ret.UnstructuredContent(), "rows"); err == nil && len(rows) == 0 {
			printed.Structured = map[string]any{"items": []map[string]any{}}
		}
	}
	text, structured := WithListPage(printed.Text, printed.Structured, ret)
	return api.NewToolCallResultFull(text, structured, nil), nil
}

func resourcesGet(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	namespace := params.GetArguments()["namespace"]
	if namespace == nil {
		namespace = ""
	}
	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get resource, %s", err)), nil
	}
	name := params.GetArguments()["name"]
	if name == nil {
		return api.NewToolCallResult("", errors.New("failed to get resource, missing argument name")), nil
	}

	ns, ok := namespace.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("namespace is not a string")), nil
	}

	n, ok := name.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("name is not a string")), nil
	}

	ret, err := kubernetes.NewCore(params).ResourcesGet(params, gvk, ns, n)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get resource: %w", err)), nil
	}
	printed, err := output.Yaml.PrintObjStructured(ret)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to format resource: %w", err)), nil
	}
	return api.NewToolCallResultFull(printed.Text, printed.Structured, nil), nil
}

func resourcesCreateOrUpdate(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	resource := params.GetArguments()["resource"]
	if resource == nil || resource == "" {
		return api.NewToolCallResult("", errors.New("failed to create or update resources, missing argument resource")), nil
	}

	r, ok := resource.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("resource is not a string")), nil
	}

	full, err := wantFullOutput(api.WrapParams(params))
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to create or update resources: %w", err)), nil
	}

	p := api.WrapParams(params)
	opts := kubernetes.ApplyOptions{
		Force:         p.OptionalBool("force", false),
		DryRun:        p.OptionalBool("dry_run", false),
		ValidateFirst: true,
	}
	switch onFailure := p.OptionalString("on_failure", "stop"); onFailure {
	case "stop":
	case "rollback":
		opts.RollbackOnFailure = true
	default:
		return api.NewToolCallResult("", fmt.Errorf("failed to create or update resources: on_failure must be \"stop\" or \"rollback\", got %q", onFailure)), nil
	}
	switch mode := p.OptionalString("mode", "apply"); mode {
	case "apply":
	case "create":
		opts.CreateOnly = true
	default:
		return api.NewToolCallResult("", fmt.Errorf("failed to create or update resources: mode must be \"apply\" or \"create\", got %q", mode)), nil
	}
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to create or update resources: %w", err)), nil
	}

	resources, err := kubernetes.NewCore(params).ResourcesApply(params, r, opts)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to create or update resources: %w", err)), nil
	}
	marshalledYaml, err := writeResultYaml(resources, full)
	if err != nil {
		err = fmt.Errorf("failed to create or update resources: %w", err)
	}
	note := receiptNote
	if full {
		note = ""
	}
	if opts.DryRun {
		return api.NewToolCallResult("# Dry run (server-side, dryRun=All): every document was accepted; nothing was written\n"+note+marshalledYaml, err), nil
	}
	return api.NewToolCallResult("# The following resources (YAML) have been created or updated successfully\n"+note+marshalledYaml, err), nil
}

func resourcesPatch(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to patch resource, %s", err)), nil
	}
	p := api.WrapParams(params)
	ns := p.OptionalString("namespace", "")
	name := p.RequiredString("name")
	patchType := p.RequiredString("patchType")
	patch := p.RequiredString("patch")
	resourceVersion := p.OptionalString("resourceVersion", "")
	full, outputErr := wantFullOutput(p)
	if err := errors.Join(p.Err(), outputErr); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to patch resource: %w", err)), nil
	}
	patched, err := kubernetes.NewCore(params).ResourcesPatch(params, gvk, ns, name, patchType, patch, resourceVersion)
	if err != nil {
		if resourceVersion != "" && apierrors.IsConflict(err) {
			err = fmt.Errorf("%w (the object changed after resourceVersion %s was read: read it again and rebuild the patch)", err, resourceVersion)
		}
		return api.NewToolCallResult("", fmt.Errorf("failed to patch resource: %w", err)), nil
	}
	text, err := writeResultYaml([]*unstructured.Unstructured{patched}, full)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to patch resource: %w", err)), nil
	}
	return api.NewToolCallResult(fmt.Sprintf("# %s %s patched (%s patch)\n", gvk.Kind, name, patchType)+text, nil), nil
}

func resourcesDelete(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	namespace := params.GetArguments()["namespace"]
	if namespace == nil {
		namespace = ""
	}
	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to delete resource, %s", err)), nil
	}
	name := params.GetArguments()["name"]
	if selector, ok := params.GetArguments()["labelSelector"].(string); ok && name == nil {
		ns, _ := namespace.(string)
		p := api.WrapParams(params)
		opts := kubernetes.DeleteOptions{PropagationPolicy: metav1.DeletionPropagation(p.OptionalString("propagationPolicy", string(metav1.DeletePropagationBackground)))}
		dryRun := p.OptionalBool("dry_run", false)
		if err := p.Err(); err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to delete resources: %w", err)), nil
		}
		return resourcesDeleteBySelector(params, gvk, ns, selector, opts, dryRun)
	}
	if name == nil {
		return api.NewToolCallResult("", errors.New("failed to delete resource, missing argument name")), nil
	}
	if _, ok := params.GetArguments()["labelSelector"]; ok {
		return api.NewToolCallResult("", errors.New("failed to delete resource, pass either name or labelSelector, not both")), nil
	}

	ns, ok := namespace.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("namespace is not a string")), nil
	}

	n, ok := name.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("name is not a string")), nil
	}

	var gracePeriodSecondsPtr *int64
	if value, ok := params.GetArguments()["gracePeriodSeconds"]; ok {
		gracePeriodSeconds, err := api.ParseInt64(value)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to delete resource, invalid argument gracePeriodSeconds")), nil
		}
		gracePeriodSecondsPtr = &gracePeriodSeconds
	}

	p := api.WrapParams(params)
	opts := kubernetes.DeleteOptions{
		GracePeriodSeconds: gracePeriodSecondsPtr,
		PropagationPolicy:  metav1.DeletionPropagation(p.OptionalString("propagationPolicy", string(metav1.DeletePropagationBackground))),
		UID:                p.OptionalString("uid", ""),
		ResourceVersion:    p.OptionalString("resourceVersion", ""),
	}
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to delete resource: %w", err)), nil
	}
	switch opts.PropagationPolicy {
	case metav1.DeletePropagationBackground, metav1.DeletePropagationForeground, metav1.DeletePropagationOrphan:
	default:
		return api.NewToolCallResult("", fmt.Errorf("failed to delete resource: propagationPolicy must be Background, Foreground or Orphan, got %q", opts.PropagationPolicy)), nil
	}
	outcome, err := kubernetes.NewCore(params).ResourcesDeleteWithOptions(params, gvk, ns, n, opts)
	if err != nil {
		if (opts.UID != "" || opts.ResourceVersion != "") && apierrors.IsConflict(err) {
			err = fmt.Errorf("%w (precondition failed: the object was recreated or changed since you read it; nothing was deleted)", err)
		}
		return api.NewToolCallResult("", fmt.Errorf("failed to delete resource: %w", err)), nil
	}
	return api.NewToolCallResult(deleteOutcomeText(gvk.Kind, ns, n, outcome), nil), nil
}

// deleteOutcomeText states whether the object is gone or still pending.
func deleteOutcomeText(kind, namespace, name string, outcome *kubernetes.DeleteOutcome) string {
	ref := name
	if namespace != "" {
		ref = namespace + "/" + name
	}
	switch {
	case outcome.Gone && outcome.RecreatedUID != "":
		return fmt.Sprintf("Resource deleted successfully; an object of the same name already exists again (%s %s, uid %s), created by someone else", kind, ref, outcome.RecreatedUID)
	case outcome.Gone:
		return "Resource deleted successfully"
	case outcome.Pending && len(outcome.Finalizers) > 0:
		return fmt.Sprintf("Resource deletion pending: %s %s is marked for deletion (deletionTimestamp %s) but still exists, held by finalizers %s; it disappears when they are removed", kind, ref, outcome.DeletionTimestamp, strings.Join(outcome.Finalizers, ", "))
	case outcome.Pending:
		grace := ""
		if outcome.GracePeriod != nil {
			grace = fmt.Sprintf(", grace period %ds", *outcome.GracePeriod)
		}
		return fmt.Sprintf("Resource deletion pending: %s %s is terminating (deletionTimestamp %s%s) and disappears when that ends", kind, ref, outcome.DeletionTimestamp, grace)
	default:
		return "Resource delete accepted; its current state could not be read back"
	}
}

func resourcesScale(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	namespace := params.GetArguments()["namespace"]
	if namespace == nil {
		namespace = ""
	}

	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get/update resource scale, %w", err)), nil
	}

	name := params.GetArguments()["name"]
	if selector, ok := params.GetArguments()["labelSelector"].(string); ok && name == nil {
		ns, _ := namespace.(string)
		scaleVal, hasScale := params.GetArguments()["scale"]
		if !hasScale {
			return api.NewToolCallResult("", errors.New("failed to scale resources: scale is required with labelSelector")), nil
		}
		desired, err := parseScaleValue(scaleVal)
		if err != nil {
			return api.NewToolCallResult("", err), nil
		}
		p := api.WrapParams(params)
		dryRun := p.OptionalBool("dry_run", false)
		if err := p.Err(); err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to scale resources: %w", err)), nil
		}
		return resourcesScaleBySelector(params, gvk, ns, selector, desired, dryRun)
	}
	if name == nil {
		return api.NewToolCallResult("", errors.New("failed to get/update resource scale, missing argument name")), nil
	}
	if _, ok := params.GetArguments()["labelSelector"]; ok {
		return api.NewToolCallResult("", errors.New("failed to get/update resource scale, pass either name or labelSelector, not both")), nil
	}

	ns, ok := namespace.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("namespace is not a string")), nil
	}

	n, ok := name.(string)
	if !ok {
		return api.NewToolCallResult("", fmt.Errorf("name is not a string")), nil
	}

	var desiredScale int64
	scaleVal, shouldScale := params.GetArguments()["scale"]
	if shouldScale {
		desiredScale, err = parseScaleValue(scaleVal)
		if err != nil {
			return api.NewToolCallResult("", err), nil
		}
	}

	full, err := wantFullOutput(api.WrapParams(params))
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get/update resource scale: %w", err)), nil
	}

	scale, err := kubernetes.NewCore(params).ResourcesScale(params.Context, gvk, ns, n, desiredScale, shouldScale)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get/update resource scale: %w", err)), nil
	}

	text, err := scaleResultText(gvk, scale, full)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to marshall scale to yaml format: %v", scale)), nil
	}

	return api.NewToolCallResult(text, nil), nil
}

func parseScaleValue(desiredScale interface{}) (int64, error) {
	v, err := api.ParseInt64(desiredScale)
	if err != nil {
		return 0, fmt.Errorf("failed to parse scale parameter: %w", err)
	}
	return v, nil
}

func parseGroupVersionKind(arguments map[string]interface{}) (*schema.GroupVersionKind, error) {
	apiVersion := arguments["apiVersion"]
	if apiVersion == nil {
		return nil, errors.New("missing argument apiVersion")
	}
	kind := arguments["kind"]
	if kind == nil {
		return nil, errors.New("missing argument kind")
	}

	a, ok := apiVersion.(string)
	if !ok {
		return nil, fmt.Errorf("apiVersion is not a string")
	}

	gv, err := schema.ParseGroupVersion(a)
	if err != nil {
		return nil, errors.New("invalid argument apiVersion")
	}
	k, ok := kind.(string)
	if !ok {
		return nil, fmt.Errorf("kind is not a string")
	}
	return &schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: k}, nil
}
