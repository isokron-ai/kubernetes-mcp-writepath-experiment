package tekton

import (
	"context"
	"fmt"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/google/jsonschema-go/jsonschema"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// emptySchema is an empty JSON Schema (equivalent to JSON Schema `true`) that allows any additional properties.
var emptySchema = &jsonschema.Schema{}

// GroupVersionResource definitions for Tekton resources
var (
	pipelineGVR = schema.GroupVersionResource{
		Group:    "tekton.dev",
		Version:  "v1",
		Resource: "pipelines",
	}
	pipelineRunGVR = schema.GroupVersionResource{
		Group:    "tekton.dev",
		Version:  "v1",
		Resource: "pipelineruns",
	}
	taskGVR = schema.GroupVersionResource{
		Group:    "tekton.dev",
		Version:  "v1",
		Resource: "tasks",
	}
	taskRunGVR = schema.GroupVersionResource{
		Group:    "tekton.dev",
		Version:  "v1",
		Resource: "taskruns",
	}
	pacRepositoryGVR = schema.GroupVersionResource{
		Group:    "pipelinesascode.tekton.dev",
		Version:  "v1alpha1",
		Resource: "repositories",
	}
	tektonConfigGVR = schema.GroupVersionResource{
		Group:    "operator.tekton.dev",
		Version:  "v1alpha1",
		Resource: "tektonconfigs",
	}
	pipelineRunGVK = schema.GroupVersionKind{
		Group:   "tekton.dev",
		Version: "v1",
		Kind:    "PipelineRun",
	}
)

func hasPipelineRun(inspector api.ClusterInspector) api.TargetCompatibilityFilter {
	return func(ctx context.Context) bool {
		return inspector == nil || api.AnyTargetHasGVK(ctx, inspector, pipelineRunGVK)
	}
}

// paramsArgument returns the "params" tool argument as a map. A missing or null
// argument gives nil; any other type is an error, so a wrongly typed value is
// refused instead of being dropped and starting the run without parameters.
func paramsArgument(args map[string]any) (map[string]interface{}, error) {
	raw, ok := args["params"]
	if !ok || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("params must be an object mapping each parameter name to its value, got %T", raw)
	}
	return m, nil
}

// parseParams converts a map[string]interface{} from a tool call argument into Tekton Params.
// Each map entry becomes a Param whose value type is inferred from the Go value:
//   - string  → ParamTypeString
//   - []interface{} → ParamTypeArray  (each element is coerced to string)
//   - map[string]interface{} → ParamTypeObject (each value is coerced to string)
func parseParams(raw map[string]interface{}) ([]tektonv1.Param, error) {
	params := make([]tektonv1.Param, 0, len(raw))
	for k, v := range raw {
		pv, err := toParamValue(k, v)
		if err != nil {
			return nil, err
		}
		params = append(params, tektonv1.Param{Name: k, Value: pv})
	}
	return params, nil
}

func toParamValue(name string, v interface{}) (tektonv1.ParamValue, error) {
	switch val := v.(type) {
	case string:
		return tektonv1.ParamValue{
			Type:      tektonv1.ParamTypeString,
			StringVal: val,
		}, nil

	case []interface{}:
		arr := make([]string, 0, len(val))
		for i, elem := range val {
			s, ok := elem.(string)
			if !ok {
				return tektonv1.ParamValue{}, fmt.Errorf("param %q: array element %d must be a string, got %T", name, i, elem)
			}
			arr = append(arr, s)
		}
		return tektonv1.ParamValue{
			Type:     tektonv1.ParamTypeArray,
			ArrayVal: arr,
		}, nil

	case map[string]interface{}:
		obj := make(map[string]string, len(val))
		for k, elem := range val {
			s, ok := elem.(string)
			if !ok {
				return tektonv1.ParamValue{}, fmt.Errorf("param %q: object key %q value must be a string, got %T", name, k, elem)
			}
			obj[k] = s
		}
		return tektonv1.ParamValue{
			Type:      tektonv1.ParamTypeObject,
			ObjectVal: obj,
		}, nil

	default:
		return tektonv1.ParamValue{}, fmt.Errorf("param %q: unsupported value type %T (expected string, array, or object)", name, v)
	}
}
