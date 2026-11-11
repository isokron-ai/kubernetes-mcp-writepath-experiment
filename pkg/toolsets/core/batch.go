package core

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

// batchItem is one object's outcome in a batch call.
type batchItem struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// batchResult renders a best-effort batch: each object is handled on its own, a
// failure does not stop the others, and every object gets its own result line.
func batchResult(action string, gvk *schema.GroupVersionKind, namespace, selector string, items []batchItem, dryRun bool) *api.ToolCallResult {
	counts := map[string]int{}
	failed := 0
	for _, item := range items {
		if item.Error != "" {
			failed++
		} else {
			counts[item.Result]++
		}
	}
	where := ""
	if namespace != "" {
		where = " in namespace " + namespace
	}
	var summary []string
	for _, key := range []string{"deleted", "deletion pending", "scaled", "unchanged", "labelled", "would be deleted", "would be scaled", "would be labelled"} {
		if counts[key] > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", counts[key], key))
		}
	}
	summary = append(summary, fmt.Sprintf("%d failed", failed))
	mode := "best_effort: each object is handled on its own; a failure does not stop or undo the others"
	if dryRun {
		mode = "dry run: nothing was changed"
	}
	header := fmt.Sprintf("# %s %s%s with labelSelector %q: %d matched; %s (%s)\n", action, gvk.Kind, where, selector, len(items), strings.Join(summary, ", "), mode)
	if len(items) == 0 {
		return api.NewToolCallResult(header, nil)
	}
	body, err := output.MarshalYaml(items)
	var callErr error
	if failed > 0 {
		callErr = fmt.Errorf("%s%s", header, body)
		return api.NewToolCallResult("", callErr)
	}
	return api.NewToolCallResult(header+body, err)
}

func resourcesDeleteBySelector(params api.ToolHandlerParams, gvk *schema.GroupVersionKind, ns, selector string, opts kubernetes.DeleteOptions, dryRun bool) (*api.ToolCallResult, error) {
	core := kubernetes.NewCore(params)
	objs, namespace, err := core.ResourcesSelect(params, gvk, ns, selector)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to delete resources: %w", err)), nil
	}
	items := make([]batchItem, 0, len(objs))
	for _, obj := range objs {
		item := batchItem{Name: obj.GetName()}
		if dryRun {
			item.Result = "would be deleted"
			items = append(items, item)
			continue
		}
		perObject := opts
		perObject.UID = string(obj.GetUID()) // never delete an object recreated after the list
		outcome, deleteErr := core.ResourcesDeleteWithOptions(params, gvk, namespace, obj.GetName(), perObject)
		switch {
		case deleteErr != nil:
			item.Result, item.Error = "failed", deleteErr.Error()
		case outcome.Pending:
			item.Result = "deletion pending"
		default:
			item.Result = "deleted"
		}
		items = append(items, item)
	}
	return batchResult("Delete", gvk, namespace, selector, items, dryRun), nil
}

func resourcesScaleBySelector(params api.ToolHandlerParams, gvk *schema.GroupVersionKind, ns, selector string, desired int64, dryRun bool) (*api.ToolCallResult, error) {
	core := kubernetes.NewCore(params)
	objs, namespace, err := core.ResourcesSelect(params, gvk, ns, selector)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to scale resources: %w", err)), nil
	}
	items := make([]batchItem, 0, len(objs))
	for _, obj := range objs {
		item := batchItem{Name: obj.GetName()}
		if dryRun {
			item.Result = "would be scaled"
			items = append(items, item)
			continue
		}
		scale, scaleErr := core.ResourcesScale(params.Context, gvk, namespace, obj.GetName(), desired, true)
		if scaleErr != nil {
			item.Result, item.Error = "failed", scaleErr.Error()
		} else {
			item.Result = "scaled"
			item.Name = fmt.Sprintf("%s: %s -> %d replicas", obj.GetName(), scale.GetAnnotations()[kubernetes.ScalePreviousReplicasAnnotation], desired)
		}
		items = append(items, item)
	}
	return batchResult("Scale", gvk, namespace, selector, items, dryRun), nil
}

func resourcesLabel(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	gvk, err := parseGroupVersionKind(params.GetArguments())
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to label resources, %s", err)), nil
	}
	p := api.WrapParams(params)
	ns := p.OptionalString("namespace", "")
	name := p.OptionalString("name", "")
	selector := p.OptionalString("labelSelector", "")
	dryRun := p.OptionalBool("dry_run", false)
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to label resources: %w", err)), nil
	}
	if (name == "") == (selector == "") {
		return api.NewToolCallResult("", fmt.Errorf("failed to label resources: pass either name or labelSelector")), nil
	}
	raw, ok := params.GetArguments()["labels"].(map[string]any)
	if !ok || len(raw) == 0 {
		return api.NewToolCallResult("", fmt.Errorf("failed to label resources: labels must be a non-empty object of label: value (null removes)")), nil
	}
	set := make(map[string]*string, len(raw))
	for k, v := range raw {
		switch value := v.(type) {
		case nil:
			set[k] = nil
		case string:
			set[k] = &value
		default:
			return api.NewToolCallResult("", fmt.Errorf("failed to label resources: label %q must be a string or null, got %T", k, v)), nil
		}
	}
	core := kubernetes.NewCore(params)
	if name != "" {
		labelled, err := core.ResourcesLabel(params, gvk, ns, name, set)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to label resource: %w", err)), nil
		}
		text, err := output.MarshalYaml(map[string]any{"name": labelled.GetName(), "labels": labelled.GetLabels(), "resourceVersion": labelled.GetResourceVersion()})
		return api.NewToolCallResult(fmt.Sprintf("# %s %s labelled\n", gvk.Kind, name)+text, err), nil
	}
	objs, namespace, err := core.ResourcesSelect(params, gvk, ns, selector)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to label resources: %w", err)), nil
	}
	items := make([]batchItem, 0, len(objs))
	for _, obj := range objs {
		item := batchItem{Name: obj.GetName()}
		if dryRun {
			item.Result = "would be labelled"
		} else if _, err := core.ResourcesLabel(params, gvk, namespace, obj.GetName(), set); err != nil {
			item.Result, item.Error = "failed", err.Error()
		} else {
			item.Result = "labelled"
		}
		items = append(items, item)
	}
	return batchResult("Label", gvk, namespace, selector, items, dryRun), nil
}
