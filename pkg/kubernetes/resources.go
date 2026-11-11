package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1beta1 "k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

const (
	AppKubernetesComponent = "app.kubernetes.io/component"
	AppKubernetesManagedBy = "app.kubernetes.io/managed-by"
	AppKubernetesName      = "app.kubernetes.io/name"
	AppKubernetesPartOf    = "app.kubernetes.io/part-of"
)

func (c *Core) ResourcesList(ctx context.Context, gvk *schema.GroupVersionKind, namespace string, options api.ListOptions) (runtime.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	// Check if operation is allowed for all namespaces (applicable for namespaced resources)
	isNamespaced, err := c.isNamespaced(gvk)
	if err != nil {
		return nil, err
	}
	// The access review only matters for an all-namespaces list; skip it when a namespace is given.
	if isNamespaced && namespace == "" && !c.canIUse(ctx, gvr, namespace, "list") {
		namespace = c.NamespaceOrDefault("")
	} else if !isNamespaced {
		// Cluster-scoped resources never have a namespace, even when the caller provides one.
		namespace = ""
	}
	if options.AsTable {
		return c.resourcesListAsTable(ctx, gvk, gvr, namespace, options)
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).List(ctx, options.ListOptions)
}

func (c *Core) ResourcesGet(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string) (*unstructured.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	return c.DynamicClient().Resource(*gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Core) ResourcesCreateOrUpdate(ctx context.Context, resource string) ([]*unstructured.Unstructured, error) {
	return c.ResourcesApply(ctx, resource, ApplyOptions{Force: true})
}

// ApplyOptions controls how ResourcesApply writes each document.
type ApplyOptions struct {
	// Force takes over fields owned by other field managers (server-side apply force).
	// Without it, a field owned by another manager makes the API refuse the write with a
	// conflict, which is returned as a FieldConflictError.
	Force bool
	// CreateOnly creates each object (POST) instead of applying it: an existing object of
	// the same name is never modified, and the call fails with an AlreadyExistsError.
	CreateOnly bool
	// DryRun sends every document as a server-side dry run (dryRun=All) and writes nothing.
	DryRun bool
	// ValidateFirst dry-runs every document of a multi-document input before the first
	// write; if any is refused, nothing is written.
	ValidateFirst bool
	// RollbackOnFailure deletes, when a later document fails, the objects this call created.
	// It is a compensating action, not a transaction: objects that existed before and were
	// changed are not restored, and a delete can itself fail. Both are reported.
	RollbackOnFailure bool
}

// ResourcesApply parses a YAML or JSON manifest stream and writes every document.
func (c *Core) ResourcesApply(ctx context.Context, resource string, opts ApplyOptions) ([]*unstructured.Unstructured, error) {
	parsedResources, err := ParseManifests(resource)
	if err != nil {
		return nil, err
	}
	return c.resourcesApply(ctx, parsedResources, opts)
}

// ResourcesApplyObject server-side applies one object that the caller built as
// data, without parsing any manifest text, so that tool arguments placed in its
// fields cannot add documents to the apply.
func (c *Core) ResourcesApplyObject(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	applied, err := c.resourcesCreateOrUpdate(ctx, []*unstructured.Unstructured{obj})
	if err != nil {
		return nil, err
	}
	return applied[0], nil
}

// ParseManifests splits a YAML or JSON stream into Kubernetes objects the way
// kubectl's YAML reader does: a document separator is a line starting with
// "---", optionally followed by spaces and a comment, and documents that hold
// nothing (blank, comment-only, a trailing separator) are skipped. Every
// document is decoded before anything is sent to the cluster, so a document
// that cannot be decoded, or has no apiVersion or kind, fails the whole call
// without writing anything.
func ParseManifests(resource string) ([]*unstructured.Unstructured, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(strings.NewReader(resource)))
	var parsedResources []*unstructured.Unstructured
	for {
		document, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		// documents are numbered as the objects they hold: empty ones are not counted
		index := len(parsedResources) + 1
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", index, err)
		}
		obj := &unstructured.Unstructured{}
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(document), 4096).Decode(&obj.Object); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("document %d: %w", index, err)
		}
		if len(obj.Object) == 0 {
			// blank or comment-only document, or a trailing separator
			continue
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			return nil, fmt.Errorf("document %d: apiVersion and kind are required (nothing was applied)", index)
		}

		// remove the status from the resource, disallowing agent from directly editing (only controllers should be allowed to do this)
		delete(obj.Object, "status")

		parsedResources = append(parsedResources, obj)
	}
	if len(parsedResources) == 0 {
		return nil, errors.New("no Kubernetes object found in the resource argument")
	}
	return parsedResources, nil
}

func (c *Core) ResourcesDelete(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string, gracePeriodSeconds *int64) error {
	_, err := c.ResourcesDeleteWithOptions(ctx, gvk, namespace, name, DeleteOptions{GracePeriodSeconds: gracePeriodSeconds})
	return err
}

// DeleteOptions are the options of ResourcesDeleteWithOptions.
type DeleteOptions struct {
	GracePeriodSeconds *int64
	// PropagationPolicy for dependents; empty means Background, as kubectl delete does
	// (the API's own default for some kinds, such as batch/v1 Job, orphans them).
	PropagationPolicy metav1.DeletionPropagation
	// UID and ResourceVersion are optional preconditions: the delete is refused (409)
	// if the object no longer has that uid (it was deleted and recreated) or that version.
	UID             string
	ResourceVersion string
}

// DeleteOutcome says what a delete left behind, read right after the delete call.
type DeleteOutcome struct {
	// Gone is true when the object no longer exists.
	Gone bool
	// Pending is set when the object still exists with a deletionTimestamp: finalizers
	// or a graceful termination are holding it.
	Pending           bool
	DeletionTimestamp string
	Finalizers        []string
	GracePeriod       *int64
	// RecreatedUID is set when an object of the same name with another uid exists.
	RecreatedUID string
}

// ResourcesDeleteWithOptions deletes one object and reads it again to report whether
// it is gone or still pending.
func (c *Core) ResourcesDeleteWithOptions(ctx context.Context, gvk *schema.GroupVersionKind, namespace, name string, opts DeleteOptions) (*DeleteOutcome, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	policy := opts.PropagationPolicy
	if policy == "" {
		policy = metav1.DeletePropagationBackground
	}
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: opts.GracePeriodSeconds,
		PropagationPolicy:  &policy,
	}
	if opts.UID != "" || opts.ResourceVersion != "" {
		deleteOptions.Preconditions = &metav1.Preconditions{}
		if opts.UID != "" {
			deleteOptions.Preconditions.UID = ptr.To(types.UID(opts.UID))
		}
		if opts.ResourceVersion != "" {
			deleteOptions.Preconditions.ResourceVersion = ptr.To(opts.ResourceVersion)
		}
	}
	client := c.DynamicClient().Resource(*gvr).Namespace(namespace)
	before, _ := client.Get(ctx, name, metav1.GetOptions{})
	if err := client.Delete(ctx, name, deleteOptions); err != nil {
		return nil, err
	}
	after, err := client.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &DeleteOutcome{Gone: true}, nil
	}
	if err != nil {
		// the delete was accepted; only the read-back failed
		return &DeleteOutcome{}, nil
	}
	if before != nil && after.GetUID() != before.GetUID() {
		return &DeleteOutcome{Gone: true, RecreatedUID: string(after.GetUID())}, nil
	}
	outcome := &DeleteOutcome{Pending: after.GetDeletionTimestamp() != nil, Finalizers: after.GetFinalizers(), GracePeriod: after.GetDeletionGracePeriodSeconds()}
	if ts := after.GetDeletionTimestamp(); ts != nil {
		outcome.DeletionTimestamp = ts.UTC().Format(time.RFC3339)
	}
	return outcome, nil
}

func (c *Core) ResourcesScale(
	ctx context.Context,
	gvk *schema.GroupVersionKind,
	namespace, name string,
	desiredScale int64,
	shouldScale bool,
) (*unstructured.Unstructured, error) {
	gvr, err := c.resourceFor(gvk)
	if err != nil {
		return nil, err
	}

	namespace, err = c.effectiveNamespace(gvk, namespace)
	if err != nil {
		return nil, err
	}
	resourceClient := c.DynamicClient().Resource(*gvr).Namespace(namespace)

	scale, err := resourceClient.Get(ctx, name, metav1.GetOptions{}, "scale")
	if err != nil {
		return nil, err
	}
	// spec.replicas is omitted from the API's answer when it is 0; record it explicitly,
	// with the value before any change, so callers can state the change.
	previous, _, _ := unstructured.NestedInt64(scale.Object, "spec", "replicas")
	_ = unstructured.SetNestedField(scale.Object, previous, "spec", "replicas")
	scale.SetAnnotations(nil)

	if shouldScale {
		if err := unstructured.SetNestedField(scale.Object, desiredScale, "spec", "replicas"); err != nil {
			return scale, fmt.Errorf("failed to set .spec.replicas on scale object %v: %w", scale, err)
		}

		scale, err = resourceClient.Update(ctx, scale, metav1.UpdateOptions{}, "scale")
		if err != nil {
			return scale, fmt.Errorf("failed to update scale: %w", err)
		}
		_ = unstructured.SetNestedField(scale.Object, desiredScale, "spec", "replicas")
		scale.SetAnnotations(map[string]string{ScalePreviousReplicasAnnotation: strconv.FormatInt(previous, 10)})
	}

	return scale, nil
}

// ScalePreviousReplicasAnnotation is set (in the returned object only, never on the
// cluster) by ResourcesScale on a changed Scale to the replicas before the change.
const ScalePreviousReplicasAnnotation = "kubernetes-mcp-server/previous-replicas"

// resourcesListAsTable retrieves a list of resources in a table format.
// It's almost identical to the dynamic.DynamicClient implementation, but it uses a specific Accept header to request the table format.
// dynamic.DynamicClient does not provide a way to set the HTTP header (TODO: create an issue to request this feature)
func (c *Core) resourcesListAsTable(ctx context.Context, gvk *schema.GroupVersionKind, gvr *schema.GroupVersionResource, namespace string, options api.ListOptions) (runtime.Unstructured, error) {
	var url []string
	if len(gvr.Group) == 0 {
		url = append(url, "api")
	} else {
		url = append(url, "apis", gvr.Group)
	}
	url = append(url, gvr.Version)
	if len(namespace) > 0 {
		url = append(url, "namespaces", namespace)
	}
	url = append(url, gvr.Resource)
	var table metav1.Table
	err := c.CoreV1().RESTClient().
		Get().
		SetHeader("Accept", strings.Join([]string{
			fmt.Sprintf("application/json;as=Table;v=%s;g=%s", metav1.SchemeGroupVersion.Version, metav1.GroupName),
			fmt.Sprintf("application/json;as=Table;v=%s;g=%s", metav1beta1.SchemeGroupVersion.Version, metav1beta1.GroupName),
			"application/json",
		}, ",")).
		AbsPath(url...).
		SpecificallyVersionedParams(&options.ListOptions, ParameterCodec, schema.GroupVersion{Version: "v1"}).
		Do(ctx).Into(&table)
	if err != nil {
		return nil, err
	}
	// Add metav1.Table apiVersion and kind to the unstructured object (server may not return these fields)
	table.SetGroupVersionKind(metav1.SchemeGroupVersion.WithKind("Table"))
	// Add additional columns for fields that aren't returned by the server
	table.ColumnDefinitions = append([]metav1.TableColumnDefinition{
		{Name: "apiVersion", Type: "string"},
		{Name: "kind", Type: "string"},
	}, table.ColumnDefinitions...)
	for i := range table.Rows {
		row := &table.Rows[i]
		row.Cells = append([]interface{}{
			gvr.GroupVersion().String(),
			gvk.Kind,
		}, row.Cells...)
	}
	unstructuredObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&table)
	return &unstructured.Unstructured{Object: unstructuredObject}, err
}

// ApplyOutcome names one document of a multi-document apply.
type ApplyOutcome struct {
	Index     int
	Kind      string
	Namespace string
	Name      string
}

func (o ApplyOutcome) String() string {
	ref := o.Name
	if o.Namespace != "" {
		ref = o.Namespace + "/" + o.Name
	}
	return fmt.Sprintf("document %d (%s %s)", o.Index, o.Kind, ref)
}

// PartialApplyError is returned when a multi-document apply fails after some
// documents were already applied. Kubernetes has no multi-object
// transaction, so those stay in place; the error names them, the document
// that failed and the documents that were never sent.
type PartialApplyError struct {
	Total        int
	Applied      []ApplyOutcome
	Failed       ApplyOutcome
	NotAttempted []ApplyOutcome
	Err          error
	// Set when ApplyOptions.RollbackOnFailure was requested.
	RollbackRequested bool
	RolledBack        []ApplyOutcome
	NotRestored       []ApplyOutcome
	RollbackFailed    []string
}

func (e *PartialApplyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s of %d failed: %v", e.Failed, e.Total, e.Err)
	if e.RollbackRequested {
		if len(e.RolledBack) > 0 {
			b.WriteString("; created by this call and deleted again (rollback): ")
			b.WriteString(joinOutcomes(e.RolledBack))
		}
		if len(e.NotRestored) > 0 {
			b.WriteString("; existed before and were changed, NOT restored: ")
			b.WriteString(joinOutcomes(e.NotRestored))
		}
		if len(e.RollbackFailed) > 0 {
			b.WriteString("; rollback delete FAILED for: ")
			b.WriteString(strings.Join(e.RollbackFailed, ", "))
		}
	} else if len(e.Applied) > 0 {
		b.WriteString("; already applied and left in place: ")
		b.WriteString(joinOutcomes(e.Applied))
	}
	if len(e.NotAttempted) > 0 {
		b.WriteString("; not attempted: ")
		b.WriteString(joinOutcomes(e.NotAttempted))
	}
	return b.String()
}

func (e *PartialApplyError) Unwrap() error { return e.Err }

func joinOutcomes(outcomes []ApplyOutcome) string {
	parts := make([]string, len(outcomes))
	for i, o := range outcomes {
		parts[i] = o.String()
	}
	return strings.Join(parts, ", ")
}

func (c *Core) resourcesCreateOrUpdate(ctx context.Context, resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	return c.resourcesApply(ctx, resources, ApplyOptions{Force: true})
}

func (c *Core) resourcesApply(ctx context.Context, resources []*unstructured.Unstructured, opts ApplyOptions) ([]*unstructured.Unstructured, error) {
	// Resolve every document's resource and namespace before the first write, so an
	// unknown kind or apiVersion fails the call with nothing applied. A kind defined by a
	// CustomResourceDefinition earlier in the same input cannot resolve yet; it is
	// resolved after that CRD has been applied.
	gvrs := make([]*schema.GroupVersionResource, len(resources))
	namespaces := make([]string, len(resources))
	outcomes := make([]ApplyOutcome, len(resources))
	crdBefore := false
	unresolved := func(i int, err error) error {
		if len(resources) == 1 {
			return err
		}
		return fmt.Errorf("%s: %w (nothing was applied)", outcomes[i], err)
	}
	for i, obj := range resources {
		gvk := obj.GroupVersionKind()
		outcomes[i] = ApplyOutcome{Index: i + 1, Kind: gvk.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
		gvr, err := c.resourceFor(&gvk)
		if err != nil {
			if crdBefore && meta.IsNoMatchError(err) {
				continue
			}
			return nil, unresolved(i, err)
		}
		namespace, err := c.effectiveNamespace(&gvk, obj.GetNamespace())
		if err != nil {
			return nil, unresolved(i, err)
		}
		gvrs[i], namespaces[i] = gvr, namespace
		outcomes[i].Namespace = namespace
		if gvk.Kind == "CustomResourceDefinition" {
			crdBefore = true
		}
	}

	// Validate first: a server-side dry run of every document before the first write,
	// so that one refused document stops the call with nothing written.
	if opts.DryRun || (opts.ValidateFirst && len(resources) > 1) {
		dryRun, err := c.validateDocuments(ctx, resources, gvrs, namespaces, outcomes, opts)
		if err != nil {
			return nil, err
		}
		if opts.DryRun {
			return dryRun, nil
		}
	}

	// existed[i] is known only when a rollback may be needed: it decides which objects
	// this call created (and may delete again) and which it changed (and cannot restore).
	existed := make([]bool, len(resources))
	partial := func(i int, err error) error {
		if len(resources) == 1 {
			return err
		}
		pe := &PartialApplyError{
			Total:        len(resources),
			Applied:      outcomes[:i],
			Failed:       outcomes[i],
			NotAttempted: outcomes[i+1:],
			Err:          err,
		}
		if opts.RollbackOnFailure {
			pe.RollbackRequested = true
			for j := i - 1; j >= 0; j-- {
				if existed[j] {
					pe.NotRestored = append(pe.NotRestored, outcomes[j])
					continue
				}
				delErr := c.DynamicClient().Resource(*gvrs[j]).Namespace(namespaces[j]).Delete(ctx, outcomes[j].Name, metav1.DeleteOptions{
					PropagationPolicy: ptr.To(metav1.DeletePropagationBackground),
				})
				if delErr != nil && !apierrors.IsNotFound(delErr) {
					pe.RollbackFailed = append(pe.RollbackFailed, fmt.Sprintf("%s: %v", outcomes[j], delErr))
				} else {
					pe.RolledBack = append(pe.RolledBack, outcomes[j])
				}
			}
		}
		return pe
	}
	for i, obj := range resources {
		gvk := obj.GroupVersionKind()
		if gvrs[i] == nil {
			gvr, err := c.resourceFor(&gvk)
			if err != nil {
				return nil, partial(i, err)
			}
			namespace, err := c.effectiveNamespace(&gvk, obj.GetNamespace())
			if err != nil {
				return nil, partial(i, err)
			}
			gvrs[i], namespaces[i] = gvr, namespace
			outcomes[i].Namespace = namespace
		}
		obj.SetNamespace(namespaces[i])
		client := c.DynamicClient().Resource(*gvrs[i]).Namespace(namespaces[i])
		if opts.RollbackOnFailure {
			_, getErr := client.Get(ctx, obj.GetName(), metav1.GetOptions{})
			if getErr != nil && !apierrors.IsNotFound(getErr) {
				return nil, partial(i, getErr)
			}
			existed[i] = getErr == nil
		}
		applied, err := c.writeDocument(ctx, client, obj, outcomes[i], opts, false)
		if err != nil {
			return nil, partial(i, err)
		}
		resources[i] = applied
		// Clear the cache to ensure the next operation is performed on the latest exposed APIs (will change after the CRD creation)
		if gvk.Kind == "CustomResourceDefinition" {
			c.RESTMapper().Reset()
		}
	}
	return resources, nil
}

// writeDocument creates or applies one object, as a server-side dry run if dryRun is set.
func (c *Core) writeDocument(ctx context.Context, client dynamic.ResourceInterface, obj *unstructured.Unstructured, o ApplyOutcome, opts ApplyOptions, dryRun bool) (*unstructured.Unstructured, error) {
	var dry []string
	if dryRun {
		dry = []string{metav1.DryRunAll}
	}
	if opts.CreateOnly {
		created, err := client.Create(ctx, obj, metav1.CreateOptions{FieldManager: version.BinaryName, DryRun: dry})
		if apierrors.IsAlreadyExists(err) {
			err = c.alreadyExists(ctx, client, o, err)
		}
		return created, err
	}
	applied, err := client.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: version.BinaryName,
		Force:        opts.Force,
		DryRun:       dry,
	})
	if !opts.Force && apierrors.IsConflict(err) {
		err = fieldConflict(o, err)
	}
	return applied, err
}

// validateDocuments dry-runs every document and returns the dry-run results, or a
// ValidationError with the outcome of each document. A document whose kind or
// namespace is created by an earlier document of the same input cannot be checked
// before that document exists; it is reported as checked at write time.
func (c *Core) validateDocuments(ctx context.Context, resources []*unstructured.Unstructured, gvrs []*schema.GroupVersionResource, namespaces []string, outcomes []ApplyOutcome, opts ApplyOptions) ([]*unstructured.Unstructured, error) {
	newNamespaces := map[string]bool{}
	results := make([]*unstructured.Unstructured, len(resources))
	verdicts := make([]string, len(resources))
	failed := false
	for i, obj := range resources {
		if gvrs[i] == nil {
			verdicts[i] = "not checked: its kind is defined by an earlier document"
			continue
		}
		copied := obj.DeepCopy()
		copied.SetNamespace(namespaces[i])
		client := c.DynamicClient().Resource(*gvrs[i]).Namespace(namespaces[i])
		result, err := c.writeDocument(ctx, client, copied, outcomes[i], opts, true)
		switch {
		case err == nil:
			results[i] = result
			verdicts[i] = "ok"
		case apierrors.IsNotFound(err) && newNamespaces[namespaces[i]]:
			verdicts[i] = "not checked: its namespace is created by an earlier document"
		default:
			failed = true
			verdicts[i] = "refused: " + err.Error()
		}
		if obj.GetKind() == "Namespace" && obj.GetAPIVersion() == "v1" {
			newNamespaces[obj.GetName()] = true
		}
	}
	if failed {
		return nil, &ValidationError{Outcomes: outcomes, Verdicts: verdicts}
	}
	return results, nil
}

// ValidationError reports a server-side dry run that refused at least one document:
// nothing was written. It lists every document with its verdict.
type ValidationError struct {
	Outcomes []ApplyOutcome
	Verdicts []string
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("validation (server-side dry run) refused the input, nothing was written:")
	for i, o := range e.Outcomes {
		fmt.Fprintf(&b, "\n- %s: %s", o, e.Verdicts[i])
	}
	return b.String()
}

// FieldConflictError is a server-side apply refused because fields in the manifest
// are owned by other field managers (someone else set them since, or set them first).
type FieldConflictError struct {
	Object    ApplyOutcome
	Conflicts []string // "<field> (manager <name>)"
	Err       error
}

func (e *FieldConflictError) Error() string {
	return fmt.Sprintf("%s was not applied: it sets fields owned by other field managers: %s. Send only the fields you mean to change "+
		"(fields you leave out keep their values), re-reading the object first if needed, or pass force: true to take these fields over",
		e.Object.ref(), strings.Join(e.Conflicts, "; "))
}

func (e *FieldConflictError) Unwrap() error { return e.Err }

var conflictCause = regexp.MustCompile(`^conflict with "([^"]*)"`)

func fieldConflict(o ApplyOutcome, err error) error {
	var statusErr apierrors.APIStatus
	if !errors.As(err, &statusErr) || statusErr.Status().Details == nil {
		return err
	}
	var conflicts []string
	for _, cause := range statusErr.Status().Details.Causes {
		if cause.Type != metav1.CauseTypeFieldManagerConflict {
			continue
		}
		manager := "unknown"
		if m := conflictCause.FindStringSubmatch(cause.Message); m != nil {
			manager = m[1]
		}
		conflicts = append(conflicts, fmt.Sprintf("%s (manager %s)", cause.Field, manager))
	}
	if len(conflicts) == 0 {
		return err
	}
	return &FieldConflictError{Object: o, Conflicts: conflicts, Err: err}
}

// AlreadyExistsError is a create-only write refused because the object exists.
type AlreadyExistsError struct {
	Object   ApplyOutcome
	Managers []string
	Err      error
}

func (e *AlreadyExistsError) Error() string {
	owners := ""
	if len(e.Managers) > 0 {
		owners = " (its fields are managed by " + strings.Join(e.Managers, ", ") + ")"
	}
	return fmt.Sprintf("%s already exists%s and was not modified (mode: create): choose another name, or apply over it with mode: apply", e.Object.ref(), owners)
}

func (e *AlreadyExistsError) Unwrap() error { return e.Err }

func (c *Core) alreadyExists(ctx context.Context, client dynamic.ResourceInterface, o ApplyOutcome, err error) error {
	existing, getErr := client.Get(ctx, o.Name, metav1.GetOptions{})
	var managers []string
	if getErr == nil {
		for _, mf := range existing.GetManagedFields() {
			if !slices.Contains(managers, mf.Manager) {
				managers = append(managers, mf.Manager)
			}
		}
	}
	return &AlreadyExistsError{Object: o, Managers: managers, Err: err}
}

func (o ApplyOutcome) ref() string {
	ref := o.Name
	if o.Namespace != "" {
		ref = o.Namespace + "/" + o.Name
	}
	return o.Kind + " " + ref
}

// APIResourceInfo describes one Kubernetes API resource type registered on
// the cluster: its preferred apiVersion, kind, plural resource name, and
// whether it's namespaced. Mirrors the fields `kubectl api-resources` shows.
type APIResourceInfo struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespaced bool   `json:"namespaced"`
}

// APIResourcesList returns the API resource types registered on the cluster,
// using the server's preferred version for each kind -- the same data
// `kubectl api-resources` shows. Unlike ResourcesList/ResourcesGet, callers
// don't need to already know a kind's apiVersion; this is how they discover
// it, including for CRDs the caller has no built-in knowledge of.
//
// When kindFilter is non-empty, only kinds whose name equals kindFilter
// (case-insensitive) are returned; an empty kindFilter returns every
// registered resource type. This function itself imposes no requirement on
// kindFilter -- the api_resources_list tool handler is what makes it
// mandatory, since an LLM caller dumping every API resource type on a
// cluster with many CRDs into its own context is a token cost and a source
// of confusion, not a useful default. The context-aware preferred discovery
// call can return partial results alongside a non-nil error (e.g. one API
// group temporarily unavailable, or a large CRD catalog slowing discovery -- see
// containers/kubernetes-mcp-server#283); this returns whatever resources
// were discovered together with that error rather than discarding them, so
// callers can decide whether partial results are still useful.
func (c *Core) APIResourcesList(ctx context.Context, kindFilter string) ([]APIResourceInfo, error) {
	// Use the raw discovery client because the cached discovery client can
	// remain stale after a CRD is installed outside this server. Resetting the
	// RESTMapper below refreshes resource mapping only; it does not invalidate
	// the discovery cache. Preferred discovery is intentional here: callers
	// need one server-preferred top-level version per kind. A future use case
	// that needs every served version or subresource can use a broader
	// discovery method instead.
	rawDiscoveryClient := c.RawDiscoveryClient()
	var (
		lists []*metav1.APIResourceList
		err   error
	)
	if contextual, ok := rawDiscoveryClient.(discovery.ServerResourcesInterfaceWithContext); ok {
		// Use the concrete client method when available: client-go wraps this
		// method with its standard discovery retry behavior.
		lists, err = contextual.ServerPreferredResourcesWithContext(ctx)
	} else if aggregated, ok := rawDiscoveryClient.(discovery.AggregatedDiscoveryInterface); ok {
		lists, err = discovery.ServerPreferredResourcesWithContext(
			ctx,
			discovery.ToAggregatedDiscoveryInterfaceWithContext(aggregated),
		)
	} else {
		lists, err = discovery.ServerPreferredResourcesWithContext(
			ctx,
			discovery.ToDiscoveryInterfaceWithContext(rawDiscoveryClient),
		)
	}
	resources := make([]APIResourceInfo, 0, len(lists))
	for _, list := range lists {
		if list == nil {
			continue
		}
		for _, r := range list.APIResources {
			if kindFilter != "" && !strings.EqualFold(r.Kind, kindFilter) {
				continue
			}
			resources = append(resources, APIResourceInfo{
				APIVersion: list.GroupVersion,
				Kind:       r.Kind,
				Name:       r.Name,
				Namespaced: r.Namespaced,
			})
		}
	}
	return resources, err
}

func (c *Core) resourceFor(gvk *schema.GroupVersionKind) (*schema.GroupVersionResource, error) {
	m, err := c.RESTMapper().RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
	if err != nil {
		if !meta.IsNoMatchError(err) {
			return nil, err
		}
		// The RESTMapper builds and caches its full group/resource snapshot on
		// first use and never refreshes it on its own -- resourcesCreateOrUpdate
		// resets it, but only when a CustomResourceDefinition is applied
		// through *this server's own* resources_create_or_update tool. A CRD
		// installed any other way (kubectl, an operator, Helm, a different MCP
		// client) after the mapper was already warmed left every other
		// resource tool permanently unable to find it without a server
		// restart. Reset once and retry so a fresh CRD resolves without one.
		c.RESTMapper().Reset()
		m, err = c.RESTMapper().RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
		if err != nil {
			return nil, err
		}
	}
	return &m.Resource, nil
}

func (c *Core) effectiveNamespace(gvk *schema.GroupVersionKind, namespace string) (string, error) {
	namespaced, err := c.isNamespaced(gvk)
	if err != nil {
		return "", err
	}
	if !namespaced {
		return "", nil
	}
	return c.NamespaceOrDefault(namespace), nil
}

func (c *Core) isNamespaced(gvk *schema.GroupVersionKind) (bool, error) {
	apiResourceList, err := c.DiscoveryClient().ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return false, err
	}
	for _, apiResource := range apiResourceList.APIResources {
		if apiResource.Kind == gvk.Kind {
			return apiResource.Namespaced, nil
		}
	}
	return false, nil
}

func (c *Core) supportsGroupVersion(groupVersion string) bool {
	if _, err := c.DiscoveryClient().ServerResourcesForGroupVersion(groupVersion); err != nil {
		return false
	}
	return true
}

func (c *Core) canIUse(ctx context.Context, gvr *schema.GroupVersionResource, namespace, verb string) bool {
	allowed, _ := CanI(ctx, c.AuthorizationV1(), gvr, namespace, "", verb)
	return allowed
}
