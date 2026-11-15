package core

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

const (
	outputReceipt = "receipt"
	outputCompact = "compact"
	outputFull    = "full"
)

// outputProperty is the schema of the optional `output` argument of write tools.
var outputProperty = &jsonschema.Schema{
	Type: "string",
	Enum: []any{outputReceipt, outputCompact, outputFull},
	Description: "What the result shows of each written object: 'receipt' (default) gives apiVersion, kind, " +
		"namespace, name, uid, resourceVersion and generation, and under 'changed' each field this call changed with its new value " +
		"(null: removed); 'compact' gives the identity and version only; 'full' gives the whole object as stored, as earlier versions did.",
}

// outputMode reads the `output` argument.
func outputMode(p *api.Params) (string, error) {
	switch v := p.OptionalString("output", outputReceipt); v {
	case outputReceipt, outputCompact, outputFull:
		return v, p.Err()
	default:
		return outputReceipt, fmt.Errorf("output must be %q, %q or %q, got %q", outputReceipt, outputCompact, outputFull, v)
	}
}

// receiptObject keeps an object's identity and version: what a caller needs to refer
// to it again or to send a version precondition.
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

const (
	// receiptMaxFields bounds the fields a receipt lists; the rest are counted.
	receiptMaxFields = 50
	// receiptMaxValue bounds a string value shown in a receipt, in characters.
	receiptMaxValue = 256
)

// receiptWithChanges is a receipt plus the fields the write changed, with their new values:
//   - the object existed: every field whose value differs between the read made just before
//     the write and the API's answer ("changed"; null for a removed field);
//   - the write created it: every field the call sent, with the value stored ("changed",
//     and "created: true");
//   - the earlier state could not be read: every field the call sent, with the value
//     stored ("set"), since which of them changed is not known.
//
// Server-maintained metadata (resourceVersion, generation, managedFields, uid,
// creationTimestamp) and status are left out: the receipt's identity carries the first.
func receiptWithChanges(r kubernetes.WriteRecord) map[string]any {
	receipt := receiptObject(r.Result)
	fields := map[string]any{}
	key := "changed"
	switch {
	case r.PreviousKnown && r.Previous != nil:
		diffFields("", r.Previous.Object, r.Result.Object, fields)
	case r.PreviousKnown && r.Sent != nil:
		receipt["created"] = true
		sentFields("", r.Sent.Object, r.Result.Object, fields)
	case r.Sent != nil:
		key = "set"
		sentFields("", r.Sent.Object, r.Result.Object, fields)
	default:
		return receipt
	}
	if len(fields) > receiptMaxFields {
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names[receiptMaxFields:] {
			delete(fields, name)
		}
		receipt[key+"NotShown"] = len(names) - receiptMaxFields
	}
	receipt[key] = fields
	return receipt
}

// receiptSkipped reports the paths a receipt never lists.
func receiptSkipped(path string) bool {
	switch path {
	case "apiVersion", "kind", "status", "metadata.name", "metadata.namespace", "metadata.resourceVersion",
		"metadata.generation", "metadata.managedFields", "metadata.uid", "metadata.creationTimestamp", "metadata.selfLink":
		return true
	}
	return false
}

// fieldPath appends a map key to a field path: dotted, or ["key"] when the key holds
// a dot, bracket, quote or space (label keys such as app.kubernetes.io/name, ConfigMap keys).
func fieldPath(path, key string) string {
	if key == "" || strings.ContainsAny(key, ".[]\" \t") {
		return path + "[" + strconv.Quote(key) + "]"
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

func indexPath(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}

// receiptValue shortens long strings; other values are shown as stored.
func receiptValue(v any) any {
	if s, ok := v.(string); ok {
		if r := []rune(s); len(r) > receiptMaxValue {
			return string(r[:receiptMaxValue]) + fmt.Sprintf("… (%d characters in all)", len(r))
		}
	}
	return v
}

// diffFields records in out each leaf field whose value differs between before and after.
func diffFields(path string, before, after any, out map[string]any) {
	if receiptSkipped(path) {
		return
	}
	switch a := after.(type) {
	case map[string]any:
		b, isMap := before.(map[string]any)
		if len(a) == 0 && !isMap {
			out[path] = a
			return
		}
		for k, v := range a {
			diffFields(fieldPath(path, k), b[k], v, out)
		}
		for k := range b {
			if _, kept := a[k]; !kept && !receiptSkipped(fieldPath(path, k)) {
				out[fieldPath(path, k)] = nil
			}
		}
	case []any:
		b, isList := before.([]any)
		if len(a) == 0 && (!isList || len(b) > 0) {
			out[path] = a
			return
		}
		for i, v := range a {
			var old any
			if i < len(b) {
				old = b[i]
			}
			diffFields(indexPath(path, i), old, v, out)
		}
		for i := len(a); i < len(b); i++ {
			out[indexPath(path, i)] = nil
		}
	default:
		if !reflect.DeepEqual(before, after) {
			out[path] = receiptValue(after)
		}
	}
}

// sentFields records in out each leaf field of sent, with the value the API stored there
// (the sent value if the answer does not hold that path). Null and empty values are left
// out: objects built from Go types carry many (status: {}, resources: {}).
func sentFields(path string, sent, stored any, out map[string]any) {
	if sent == nil || receiptSkipped(path) {
		return
	}
	switch s := sent.(type) {
	case map[string]any:
		st, _ := stored.(map[string]any)
		for k, v := range s {
			sentFields(fieldPath(path, k), v, st[k], out)
		}
	case []any:
		st, _ := stored.([]any)
		for i, v := range s {
			var at any
			if i < len(st) {
				at = st[i]
			}
			sentFields(indexPath(path, i), v, at, out)
		}
	default:
		if stored != nil {
			out[path] = receiptValue(stored)
		} else {
			out[path] = receiptValue(s)
		}
	}
}

// writeResultYaml renders written objects: receipts with the changed fields by default,
// identity and version only with output: compact, whole objects with output: full.
func writeResultYaml(records []kubernetes.WriteRecord, mode string) (string, error) {
	if mode == outputFull {
		return output.MarshalYaml(kubernetes.Results(records))
	}
	receipts := make([]map[string]any, 0, len(records))
	for _, r := range records {
		if mode == outputCompact {
			receipts = append(receipts, receiptObject(r.Result))
		} else {
			receipts = append(receipts, receiptWithChanges(r))
		}
	}
	return output.MarshalYaml(receipts)
}

// receiptNote is the line above a write result that says what the result shows.
func receiptNote(mode string) string {
	switch mode {
	case outputCompact:
		return "# Receipt: identity and version of each object; pass output: full for the whole objects\n"
	case outputReceipt:
		return "# Receipt: identity, version and changed fields (new values; null = removed); output: full for whole objects\n"
	}
	return ""
}

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
