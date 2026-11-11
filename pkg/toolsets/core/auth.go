package core

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
)

func initAuth() []api.ServerTool {
	return []api.ServerTool{{
		Tool: api.Tool{
			Name: "auth_can_i",
			Description: "Check whether this server's Kubernetes identity may perform an action, like 'kubectl auth can-i': " +
				"e.g. verb 'create' on apiVersion 'networking.k8s.io/v1' kind 'NetworkPolicy' in a namespace. Use it before a write that may be refused, " +
				"or to explain a 403. With user (and groups) it asks about another subject, which needs permission to create SubjectAccessReviews.",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"verb":        {Type: "string", Description: "get, list, watch, create, update, patch, delete, deletecollection, or another verb"},
					"apiVersion":  {Type: "string", Description: "apiVersion of the kind (e.g. v1, apps/v1)"},
					"kind":        {Type: "string", Description: "kind (e.g. Pod, Deployment, NetworkPolicy)"},
					"resource":    {Type: "string", Description: "Instead of apiVersion and kind: the plural resource name (e.g. pods), with group for non-core resources"},
					"group":       {Type: "string", Description: "API group of resource (empty for the core group)"},
					"subresource": {Type: "string", Description: "Optional subresource (e.g. log, exec, scale)"},
					"namespace":   {Type: "string", Description: "Namespace to ask about; empty asks about all namespaces (or a cluster-scoped kind)"},
					"name":        {Type: "string", Description: "Optional name of one object"},
					"user":        {Type: "string", Description: "Optional other user to ask about (SubjectAccessReview)"},
					"groups":      {Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: "Groups of user"},
				},
				Required: []string{"verb"},
			},
			Annotations: api.ToolAnnotations{
				Title: "Auth: Can I",
				// an access review is answered and not stored
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		},
		RBAC:    api.RBACUnbounded("creates SelfSubjectAccessReviews (allowed for every authenticated user) and, with user, SubjectAccessReviews"),
		Handler: authCanI,
	}}
}

func authCanI(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	verb := p.RequiredString("verb")
	apiVersion := p.OptionalString("apiVersion", "")
	kind := p.OptionalString("kind", "")
	resource := p.OptionalString("resource", "")
	group := p.OptionalString("group", "")
	subresource := p.OptionalString("subresource", "")
	namespace := p.OptionalString("namespace", "")
	name := p.OptionalString("name", "")
	user := p.OptionalString("user", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to check access: %w", err)), nil
	}
	groups, err := stringList(params.GetArguments(), "groups")
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to check access: %w", err)), nil
	}
	var gvk *schema.GroupVersionKind
	switch {
	case kind != "" && apiVersion != "":
		gv, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			return api.NewToolCallResult("", fmt.Errorf("failed to check access: invalid apiVersion %q", apiVersion)), nil
		}
		k := gv.WithKind(kind)
		gvk = &k
	case resource == "":
		return api.NewToolCallResult("", fmt.Errorf("failed to check access: give apiVersion and kind, or resource")), nil
	}
	review, err := kubernetes.NewCore(params).ResourcesAccessReview(params, gvk, group, resource, subresource, verb, namespace, name, user, groups)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to check access: %w", err)), nil
	}
	answer := "no"
	if review.Allowed {
		answer = "yes"
	}
	who := "this server's identity"
	if user != "" {
		who = "user " + user
	}
	target := review.Resource
	if review.Group != "" {
		target += "." + review.Group
	}
	if review.Subresource != "" {
		target += "/" + review.Subresource
	}
	if review.Name != "" {
		target += " " + review.Name
	}
	where := "in all namespaces"
	if review.Namespace != "" {
		where = "in namespace " + review.Namespace
	}
	text := fmt.Sprintf("%s: %s may %s %s %s", answer, who, verb, target, where)
	if !review.Allowed {
		text = fmt.Sprintf("no: %s may not %s %s %s", who, verb, target, where)
	}
	if review.Reason != "" {
		text += " (" + review.Reason + ")"
	}
	return api.NewToolCallResultFull(text, review, nil), nil
}
