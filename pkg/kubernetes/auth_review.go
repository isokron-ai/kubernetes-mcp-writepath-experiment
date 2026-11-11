package kubernetes

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AccessReview is the question and answer of ResourcesAccessReview.
type AccessReview struct {
	Verb        string `json:"verb"`
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
	User        string `json:"user,omitempty"`
	Allowed     bool   `json:"allowed"`
	Denied      bool   `json:"denied,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// ResourcesAccessReview asks the API server whether the server's identity (or, when
// user is set, that user with groups) may perform verb on a resource, like
// kubectl auth can-i. The resource is given as a GroupVersionKind (resolved through
// discovery) or directly as group and plural resource.
func (c *Core) ResourcesAccessReview(ctx context.Context, gvk *schema.GroupVersionKind, group, resource, subresource, verb, namespace, name, user string, groups []string) (*AccessReview, error) {
	if gvk != nil {
		gvr, err := c.resourceFor(gvk)
		if err != nil {
			return nil, err
		}
		group, resource = gvr.Group, gvr.Resource
		if namespaced, err := c.isNamespaced(gvk); err == nil && !namespaced {
			namespace = ""
		}
	}
	if resource == "" || verb == "" {
		return nil, fmt.Errorf("verb and a resource (apiVersion and kind, or resource) are required")
	}
	attributes := &authv1.ResourceAttributes{
		Namespace: namespace, Verb: verb, Group: group, Resource: resource, Subresource: subresource, Name: name,
	}
	review := &AccessReview{Verb: verb, Group: group, Resource: resource, Subresource: subresource, Namespace: namespace, Name: name, User: user}
	if user == "" {
		response, err := c.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes},
		}, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		review.Allowed, review.Denied, review.Reason = response.Status.Allowed, response.Status.Denied, response.Status.Reason
		return review, nil
	}
	response, err := c.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authv1.SubjectAccessReview{
		Spec: authv1.SubjectAccessReviewSpec{ResourceAttributes: attributes, User: user, Groups: groups},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	review.Allowed, review.Denied, review.Reason = response.Status.Allowed, response.Status.Denied, response.Status.Reason
	return review, nil
}
