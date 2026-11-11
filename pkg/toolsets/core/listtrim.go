package core

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/jsonschema-go/jsonschema"
)

// wideProperty is the schema of the optional `wide` argument of list tools.
var wideProperty = &jsonschema.Schema{
	Type: "boolean",
	Description: "Include the columns kubectl shows only with -o wide (for Pods: IP, Node, Nominated Node, Readiness Gates; " +
		"for Deployments: Containers, Images, Selector) in the structured rows. Default false.",
}

// trimTableRows removes from structured list rows what the request already fixes or
// what kubectl prints only with -o wide: apiVersion and kind (the request names the
// kind), Namespace when a namespace was given, and columns with priority > 0 unless
// wide is set. The text table is unchanged.
func trimTableRows(ret runtime.Unstructured, structured any, namespaceGiven, wide bool) any {
	drop := map[string]bool{"apiVersion": true, "kind": true}
	if namespaceGiven {
		drop["Namespace"] = true
	}
	if !wide && ret != nil {
		columns, _, _ := unstructured.NestedSlice(ret.UnstructuredContent(), "columnDefinitions")
		for _, c := range columns {
			col, _ := c.(map[string]any)
			name, _ := col["name"].(string)
			if priority, ok := col["priority"].(int64); ok && priority > 0 {
				drop[name] = true
			}
		}
	}
	trim := func(rows []map[string]any) []map[string]any {
		for _, row := range rows {
			for k := range drop {
				delete(row, k)
			}
		}
		return rows
	}
	switch s := structured.(type) {
	case []map[string]any:
		return trim(s)
	case map[string]any:
		if rows, ok := s["items"].([]map[string]any); ok {
			s["items"] = trim(rows)
			if columns, ok := s["columns"].([]string); ok {
				kept := make([]string, 0, len(columns))
				for _, c := range columns {
					if !drop[c] {
						kept = append(kept, c)
					}
				}
				s["columns"] = kept
			}
		}
	}
	return structured
}

// listLength counts the objects in a list or table result.
func listLength(ret runtime.Unstructured) int {
	if list, ok := ret.(*unstructured.UnstructuredList); ok {
		return len(list.Items)
	}
	if ret == nil {
		return 0
	}
	rows, _, _ := unstructured.NestedSlice(ret.UnstructuredContent(), "rows")
	return len(rows)
}

// emptyListText says plainly that a list found nothing, and where it looked.
func emptyListText(kind, namespace, labelSelector, fieldSelector string) string {
	where := "in any namespace"
	if namespace != "" {
		where = "in namespace " + namespace
	}
	filters := ""
	if labelSelector != "" {
		filters += fmt.Sprintf(" matching labelSelector %q", labelSelector)
	}
	if fieldSelector != "" {
		filters += fmt.Sprintf(" matching fieldSelector %q", fieldSelector)
	}
	return fmt.Sprintf("# No %s found %s%s\n", kind, where, filters)
}
