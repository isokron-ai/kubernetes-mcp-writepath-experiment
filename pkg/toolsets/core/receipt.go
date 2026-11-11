package core

import (
	"fmt"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

const (
	outputReceipt = "receipt"
	outputFull    = "full"
)

// outputProperty is the schema of the optional `output` argument of write tools.
var outputProperty = &jsonschema.Schema{
	Type: "string",
	Enum: []any{outputReceipt, outputFull},
	Description: "What the result shows of each written object: 'receipt' (default) gives apiVersion, kind, " +
		"namespace, name, uid, resourceVersion and generation; 'full' gives the whole object as stored, as earlier versions did.",
}

// wantFullOutput reads the `output` argument.
func wantFullOutput(p *api.Params) (bool, error) {
	switch v := p.OptionalString("output", outputReceipt); v {
	case outputReceipt:
		return false, p.Err()
	case outputFull:
		return true, p.Err()
	default:
		return false, fmt.Errorf("output must be %q or %q, got %q", outputReceipt, outputFull, v)
	}
}

// receiptObject keeps an object's identity and version: what a caller needs to refer
// to it again or to send a version precondition. Receipts carry no spec, data or status.
func receiptObject(obj *unstructured.Unstructured) map[string]any {
	metadata := map[string]any{"name": obj.GetName()}
	if ns := obj.GetNamespace(); ns != "" {
		metadata["namespace"] = ns
	}
	if uid := obj.GetUID(); uid != "" {
		metadata["uid"] = string(uid)
	}
	if rv := obj.GetResourceVersion(); rv != "" {
		metadata["resourceVersion"] = rv
	}
	if g := obj.GetGeneration(); g != 0 {
		metadata["generation"] = g
	}
	if ts := obj.GetDeletionTimestamp(); ts != nil {
		metadata["deletionTimestamp"] = ts.UTC().Format("2006-01-02T15:04:05Z")
	}
	return map[string]any{
		"apiVersion": obj.GetAPIVersion(),
		"kind":       obj.GetKind(),
		"metadata":   metadata,
	}
}

// writeResultYaml renders written objects: receipts by default, whole objects with output: full.
func writeResultYaml(objs []*unstructured.Unstructured, full bool) (string, error) {
	if full {
		return output.MarshalYaml(objs)
	}
	receipts := make([]map[string]any, 0, len(objs))
	for _, obj := range objs {
		receipts = append(receipts, receiptObject(obj))
	}
	return output.MarshalYaml(receipts)
}

const receiptNote = "# Receipt: identity and version of each object; pass output: full for the whole objects\n"

// scaleResultText renders a resources_scale result. A change is stated as from -> to;
// the status shown was read with the change, before the controller acted on it.
func scaleResultText(gvk *schema.GroupVersionKind, scale *unstructured.Unstructured, full bool) (string, error) {
	desired, _, _ := unstructured.NestedInt64(scale.Object, "spec", "replicas")
	header := "# Current resource scale (YAML) is below\n"
	if previous, ok := scale.GetAnnotations()[kubernetes.ScalePreviousReplicasAnnotation]; ok {
		from, _ := strconv.ParseInt(previous, 10, 64)
		ref := scale.GetName()
		if scale.GetNamespace() != "" {
			ref = scale.GetNamespace() + "/" + scale.GetName()
		}
		header = fmt.Sprintf("# %s %s scaled: spec.replicas %d -> %d. The status below was read with the change, before the controller acted on it; read again to see the result\n",
			gvk.Kind, ref, from, desired)
	}
	scale.SetAnnotations(nil)
	if full {
		body, err := output.MarshalYaml(scale)
		return header + body, err
	}
	trimmed := receiptObject(scale)
	trimmed["spec"] = map[string]any{"replicas": desired}
	if replicas, found, _ := unstructured.NestedInt64(scale.Object, "status", "replicas"); found {
		trimmed["status"] = map[string]any{"replicas": replicas}
	} else {
		trimmed["status"] = map[string]any{"replicas": int64(0)}
	}
	body, err := output.MarshalYaml(trimmed)
	return header + body, err
}
