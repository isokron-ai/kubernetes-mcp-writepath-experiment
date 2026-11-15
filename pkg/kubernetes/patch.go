package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/containers/kubernetes-mcp-server/pkg/version"
)

// PatchTypes maps the resources_patch patchType argument to the API's patch types.
var PatchTypes = map[string]types.PatchType{
	"strategic": types.StrategicMergePatchType,
	"merge":     types.MergePatchType,
	"json":      types.JSONPatchType,
}

// ResourcesPatch patches one object, like kubectl patch. If resourceVersion is set the
// patch carries it as a precondition: the API refuses the patch (409 Conflict) when the
// object has changed since that version was read.
func (c *Core) ResourcesPatch(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name, patchType, patch, resourceVersion string) (*unstructured.Unstructured, error) {
	record, err := c.ResourcesPatchRecorded(ctx, gvk, namespace, name, patchType, patch, resourceVersion, false)
	return record.Result, err
}

// ResourcesPatchRecorded is ResourcesPatch with a WriteRecord. With recordChanges the
// object is read just before the patch, so that the caller can report which fields the
// patch changed; a read that fails only leaves the earlier state unknown.
func (c *Core) ResourcesPatchRecorded(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name, patchType, patch, resourceVersion string, recordChanges bool) (WriteRecord, error) {
	var record WriteRecord
	pt, ok := PatchTypes[patchType]
	if !ok {
		return record, fmt.Errorf("patchType must be one of strategic, merge or json, got %q", patchType)
	}
	body, err := patchBody(pt, patch, resourceVersion)
	if err != nil {
		return record, err
	}
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return record, err
	}
	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return record, err
	}
	client := c.DynamicClient().Resource(*gvr).Namespace(namespace)
	if recordChanges {
		if current, getErr := client.Get(ctx, name, metav1.GetOptions{}); getErr == nil {
			record.Previous, record.PreviousKnown = current, true
		}
	}
	record.Result, err = client.Patch(ctx, name, pt, body, metav1.PatchOptions{FieldManager: version.BinaryName})
	return record, err
}

// patchBody converts a YAML or JSON patch to JSON and adds the resourceVersion
// precondition: in metadata for merge patches, as a leading test operation for JSON patches.
func patchBody(pt types.PatchType, patch, resourceVersion string) ([]byte, error) {
	raw, err := yaml.YAMLToJSON([]byte(patch))
	if err != nil {
		return nil, fmt.Errorf("patch is neither JSON nor YAML: %w", err)
	}
	if pt == types.JSONPatchType {
		var ops []map[string]any
		if err := json.Unmarshal(raw, &ops); err != nil {
			return nil, fmt.Errorf("a json patch must be an array of operations: %w", err)
		}
		if len(ops) == 0 {
			return nil, fmt.Errorf("a json patch needs at least one operation")
		}
		if resourceVersion != "" {
			ops = append([]map[string]any{{"op": "test", "path": "/metadata/resourceVersion", "value": resourceVersion}}, ops...)
		}
		return json.Marshal(ops)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("a %s patch must be an object", map[types.PatchType]string{types.MergePatchType: "merge", types.StrategicMergePatchType: "strategic"}[pt])
	}
	if resourceVersion != "" {
		metadata, _ := obj["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["resourceVersion"] = resourceVersion
		obj["metadata"] = metadata
	}
	return json.Marshal(obj)
}
