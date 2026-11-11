package core

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/output"
)

func initRollout() []api.ServerTool {
	return []api.ServerTool{{
		Tool: api.Tool{
			Name: "resources_rollout",
			Description: "Manage the rollout of a Deployment, StatefulSet or DaemonSet, like kubectl rollout: " +
				"'restart' restarts its Pods (rolling, by updating the Pod template's restartedAt annotation); " +
				"'undo' rolls a Deployment back to the previous revision, or to to_revision, by restoring that revision's Pod template; " +
				"'status' says whether the rollout is complete (updated, ready and available replicas, conditions).",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"action":      {Type: "string", Enum: []any{"restart", "undo", "status"}, Description: "restart, undo or status"},
					"kind":        {Type: "string", Enum: []any{"Deployment", "StatefulSet", "DaemonSet"}, Description: "Kind of the workload (apps/v1)"},
					"namespace":   {Type: "string", Description: "Namespace of the workload (configured namespace if not provided)"},
					"name":        {Type: "string", Description: "Name of the workload"},
					"to_revision": {Type: "integer", Description: "undo only: the revision to roll back to (default: the previous one)"},
				},
				Required: []string{"action", "kind", "name"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Resources: Rollout",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		},
		RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs:     []string{"get", "patch"},
			Target:    api.RBACTarget{Resource: &api.RBACResourceTarget{APIGroup: "apps", Resource: "deployments"}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}, api.RBACRequirement{
			Verbs:     []string{"list"},
			Target:    api.RBACTarget{Resource: &api.RBACResourceTarget{APIGroup: "apps", Resource: "replicasets"}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}),
		Handler: resourcesRollout,
	}}
}

func resourcesRollout(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	action := p.RequiredString("action")
	kind := p.RequiredString("kind")
	name := p.RequiredString("name")
	ns := p.OptionalString("namespace", "")
	toRevision := p.OptionalInt64("to_revision", 0)
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to %s rollout: %w", action, err)), nil
	}
	core := kubernetes.NewCore(params)
	switch action {
	case "status":
		st, err := core.RolloutStatusOf(params, kind, ns, name)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to read rollout status: %w", err)), nil
		}
		verdict := "in progress"
		if st.Complete {
			verdict = "complete"
		}
		body, _ := output.MarshalYaml(st)
		return api.NewToolCallResultFull(fmt.Sprintf("# %s %s/%s rollout %s: %d/%d updated, %d ready, %d available\n", kind, st.Namespace, name, verdict, st.UpdatedReplicas, st.Replicas, st.ReadyReplicas, st.AvailableReplicas)+body, st, nil), nil
	case "restart":
		obj, err := core.RolloutRestart(params, kind, ns, name)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to restart rollout: %w", err)), nil
		}
		return api.NewToolCallResult(fmt.Sprintf("# %s %s/%s restart requested (generation %d); call resources_rollout status to follow it\n", kind, obj.GetNamespace(), name, obj.GetGeneration()), nil), nil
	case "undo":
		obj, from, to, err := core.RolloutUndo(params, kind, ns, name, toRevision)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to undo rollout: %w", err)), nil
		}
		return api.NewToolCallResult(fmt.Sprintf("# %s %s/%s rolled back from revision %d to the Pod template of revision %d (the rollout creates a new revision); call resources_rollout status to follow it\n", kind, obj.GetNamespace(), name, from, to), nil), nil
	}
	return api.NewToolCallResult("", fmt.Errorf("action must be restart, undo or status, got %q", action)), nil
}
