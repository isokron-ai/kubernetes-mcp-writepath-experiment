package core

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

// kindList is one kind's part of a multi-kind list: the names found, or the error.
type kindList struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Count      int      `json:"count"`
	Names      []string `json:"names,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// resourcesListKinds lists several kinds (or every listable namespaced kind) in one
// call, with names only and each kind's error inline instead of failing the call.
func resourcesListKinds(params api.ToolHandlerParams, ns string, allKinds bool) (*api.ToolCallResult, error) {
	core := kubernetes.NewCore(params)
	var kinds []kubernetes.ListableKind
	if allKinds {
		if ns == "" {
			return api.NewToolCallResult("", fmt.Errorf("failed to list resources: all_kinds needs a namespace")), nil
		}
		found, err := core.ResourcesListableNamespacedKinds(params.Context)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to list resources: %w", err)), nil
		}
		kinds = found
	} else {
		raw, ok := params.GetArguments()["kinds"].([]any)
		if !ok || len(raw) == 0 {
			return api.NewToolCallResult("", fmt.Errorf("failed to list resources: kinds must be a non-empty array of {apiVersion, kind}")), nil
		}
		for i, item := range raw {
			m, _ := item.(map[string]any)
			apiVersion, _ := m["apiVersion"].(string)
			kind, _ := m["kind"].(string)
			if apiVersion == "" || kind == "" {
				return api.NewToolCallResult("", fmt.Errorf("failed to list resources: kinds[%d] needs apiVersion and kind", i)), nil
			}
			kinds = append(kinds, kubernetes.ListableKind{APIVersion: apiVersion, Kind: kind})
		}
	}
	selector, _ := params.GetArguments()["labelSelector"].(string)
	results := make([]kindList, 0, len(kinds))
	total := 0
	for _, k := range kinds {
		entry := kindList{APIVersion: k.APIVersion, Kind: k.Kind, Names: []string{}}
		gv, err := schema.ParseGroupVersion(k.APIVersion)
		if err != nil {
			entry.Error = "invalid apiVersion"
			results = append(results, entry)
			continue
		}
		gvk := gv.WithKind(k.Kind)
		ret, err := core.ResourcesList(params, &gvk, ns, api.ListOptions{ListOptions: listOptions(selector)})
		if err != nil {
			entry.Error = ListErr(err).Error()
			results = append(results, entry)
			continue
		}
		if list, ok := ret.(*unstructured.UnstructuredList); ok {
			for _, item := range list.Items {
				name := item.GetName()
				if ns == "" && item.GetNamespace() != "" {
					name = item.GetNamespace() + "/" + name
				}
				entry.Names = append(entry.Names, name)
			}
		}
		entry.Count = len(entry.Names)
		total += entry.Count
		if allKinds && entry.Count == 0 {
			continue // with all_kinds only kinds that have objects are shown
		}
		results = append(results, entry)
	}
	where := "all namespaces"
	if ns != "" {
		where = "namespace " + ns
	}
	header := fmt.Sprintf("# %d objects of %d kinds in %s (names only; use resources_get for an object)\n", total, len(kinds), where)
	if allKinds {
		header = fmt.Sprintf("# %d objects in %s across %d listable kinds; kinds with no objects are left out (Events too: use events_list)\n", total, where, len(kinds))
	}
	body, err := output.MarshalYaml(results)
	if err != nil {
		return api.NewToolCallResult("", err), nil
	}
	return api.NewToolCallResultFull(header+body, map[string]any{"kinds": results}, nil), nil
}

// resourcesGetMany reads several named objects of one kind in one call, each whole or
// projected to the requested fields, with each object's error inline.
func resourcesGetMany(params api.ToolHandlerParams, gvk *schema.GroupVersionKind, ns string, names, fields []string) (*api.ToolCallResult, error) {
	core := kubernetes.NewCore(params)
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		obj, err := core.ResourcesGet(params, gvk, ns, name)
		if err != nil {
			items = append(items, map[string]any{"name": name, "error": err.Error()})
			continue
		}
		obj.SetManagedFields(nil)
		if len(fields) > 0 {
			items = append(items, map[string]any{"name": name, "fields": projectFields(obj.Object, fields)})
		} else {
			items = append(items, obj.Object)
		}
	}
	body, err := output.MarshalYaml(items)
	if err != nil {
		return api.NewToolCallResult("", err), nil
	}
	header := fmt.Sprintf("# %d %s objects\n", len(names), gvk.Kind)
	if len(fields) > 0 {
		header = fmt.Sprintf("# %d %s objects, fields %s\n", len(names), gvk.Kind, strings.Join(fields, ", "))
	}
	return api.NewToolCallResultFull(header+body, map[string]any{"items": items}, nil), nil
}

// resourcesListFields lists one kind and returns, for each object, its name and the
// requested fields only.
func resourcesListFields(params api.ToolHandlerParams, gvk *schema.GroupVersionKind, ns string, options api.ListOptions, fields []string) (*api.ToolCallResult, error) {
	options.AsTable = false
	ret, err := kubernetes.NewCore(params).ResourcesList(params, gvk, ns, options)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list resources: %w", ListErr(err))), nil
	}
	rows := []map[string]any{}
	if list, ok := ret.(*unstructured.UnstructuredList); ok {
		for _, item := range list.Items {
			row := map[string]any{"name": item.GetName()}
			if ns == "" && item.GetNamespace() != "" {
				row["namespace"] = item.GetNamespace()
			}
			for k, v := range projectFields(item.Object, fields) {
				row[k] = v
			}
			rows = append(rows, row)
		}
	}
	body, err := output.MarshalYaml(rows)
	if err != nil {
		return api.NewToolCallResult("", err), nil
	}
	text, structured := WithListPage(fmt.Sprintf("# %d %s objects, fields %s\n", len(rows), gvk.Kind, strings.Join(fields, ", "))+body, map[string]any{"items": rows}, ret)
	return api.NewToolCallResultFull(text, structured, nil), nil
}

func listOptions(labelSelector string) metav1.ListOptions {
	return metav1.ListOptions{LabelSelector: labelSelector}
}
