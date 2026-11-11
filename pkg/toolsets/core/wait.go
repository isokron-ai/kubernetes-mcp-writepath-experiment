package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
)

// waitProperty is the schema of the optional `wait` argument (seconds).
var waitProperty = &jsonschema.Schema{
	Type:    "integer",
	Minimum: ptr.To(float64(0)),
	Maximum: ptr.To(float64(kubernetes.MaxWait / time.Second)),
	Description: "Optional: wait up to this many seconds (max 120; keep it under your client's request timeout) for the result: " +
		"a workload's rollout complete, a Pod Ready, a Job complete, a deleted object gone. The result then says whether that state was reached; " +
		"a timeout is reported, not treated as a failure of the write.",
}

func waitSeconds(p *api.Params) (time.Duration, error) {
	seconds := p.OptionalInt64("wait", 0)
	if seconds < 0 || time.Duration(seconds)*time.Second > kubernetes.MaxWait {
		return 0, fmt.Errorf("wait must be between 0 and %d seconds", int64(kubernetes.MaxWait/time.Second))
	}
	return time.Duration(seconds) * time.Second, nil
}

// waitForObjects waits for each written object to be ready and returns one line per object.
func waitForObjects(params api.ToolHandlerParams, objs []*unstructured.Unstructured, timeout time.Duration) string {
	core := kubernetes.NewCore(params)
	deadline := time.Now().Add(timeout)
	var lines []string
	for _, obj := range objs {
		gvk := obj.GroupVersionKind()
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		ok, state, err := core.WaitFor(params.Context, &gvk, obj.GetNamespace(), obj.GetName(), false, remaining)
		lines = append(lines, waitLine(gvk.Kind, obj.GetNamespace(), obj.GetName(), ok, state, err, "ready"))
	}
	return strings.Join(lines, "")
}

func waitForGone(params api.ToolHandlerParams, gvk *schema.GroupVersionKind, namespace, name string, timeout time.Duration) string {
	ok, state, err := kubernetes.NewCore(params).WaitFor(params.Context, gvk, namespace, name, true, timeout)
	return waitLine(gvk.Kind, namespace, name, ok, state, err, "gone")
}

func waitLine(kind, namespace, name string, ok bool, state string, err error, goal string) string {
	ref := name
	if namespace != "" {
		ref = namespace + "/" + name
	}
	switch {
	case err != nil:
		return fmt.Sprintf("# wait: %s %s: could not be read (%v)\n", kind, ref, err)
	case ok:
		return fmt.Sprintf("# wait: %s %s is %s (%s)\n", kind, ref, goal, state)
	default:
		return fmt.Sprintf("# wait: %s %s is NOT %s yet after the wait (%s); read it again later\n", kind, ref, goal, state)
	}
}
