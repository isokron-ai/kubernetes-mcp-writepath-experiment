package helm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/yaml"

	"github.com/containers/kubernetes-mcp-server/pkg/klogutil"
)

type Kubernetes interface {
	genericclioptions.RESTClientGetter
	NamespaceOrDefault(namespace string) string
}

type Helm struct {
	kubernetes Kubernetes
	config     *Config
}

// NewHelm creates a new Helm instance
func NewHelm(kubernetes Kubernetes, config *Config) *Helm {
	return &Helm{kubernetes: kubernetes, config: config}
}

// InstallOptions are helm_install's waiting and failure options. The zero value is the
// historical behaviour: wait 5 minutes for every object to be ready, no rollback.
// Upstream #1334 proposes the same wait and timeout as toolset-level defaults.
type InstallOptions struct {
	// NoWait returns as soon as the objects are created (helm install without --wait).
	NoWait bool
	// Timeout of the wait and of hooks; 0 means 5 minutes.
	Timeout time.Duration
	// RollbackOnFailure uninstalls the release if the install fails or times out
	// (helm install --atomic), so the name is free for a retry. It implies waiting.
	RollbackOnFailure bool
}

func (h *Helm) Install(ctx context.Context, chart string, values map[string]interface{}, name string, namespace string) (string, error) {
	return h.InstallWithOptions(ctx, chart, values, name, namespace, InstallOptions{})
}

// InstallWithOptions installs a chart with explicit wait, timeout and rollback options.
func (h *Helm) InstallWithOptions(ctx context.Context, chart string, values map[string]interface{}, name string, namespace string, opts InstallOptions) (string, error) {
	if err := validateChartReference(chart, h.config); err != nil {
		return "", err
	}
	cfg, err := h.newAction(ctx, h.kubernetes.NamespaceOrDefault(namespace), false)
	if err != nil {
		return "", err
	}
	install := action.NewInstall(cfg)
	if name == "" {
		install.GenerateName = true
		install.ReleaseName, _, _ = install.NameAndChart([]string{chart})
	} else {
		install.ReleaseName = name
	}
	install.Namespace = h.kubernetes.NamespaceOrDefault(namespace)
	install.Wait = !opts.NoWait || opts.RollbackOnFailure
	install.Timeout = 5 * time.Minute
	if opts.Timeout > 0 {
		install.Timeout = opts.Timeout
	}
	install.Atomic = opts.RollbackOnFailure
	install.DryRun = false

	chartRequested, err := install.LocateChart(chart, cli.New())
	if err != nil {
		return "", err
	}
	chartLoaded, err := loader.Load(chartRequested)
	if err != nil {
		return "", err
	}

	installedRelease, err := install.RunWithContext(ctx, chartLoaded, values)
	if err != nil {
		return "", h.withReleaseState(ctx, install.ReleaseName, install.Namespace, err)
	}
	ret, err := yaml.Marshal(simplify(installedRelease))
	if err != nil {
		return "", err
	}
	return string(ret), nil
}

// List lists releases for the specified namespace, or across namespaces when allNamespaces is set.
// limit and offset page the releases returned after Helm has loaded them. Zero limit returns every loaded release.
func (h *Helm) List(ctx context.Context, namespace string, allNamespaces bool, limit, offset int) (string, error) {
	cfg, err := h.newAction(ctx, namespace, allNamespaces)
	if err != nil {
		return "", err
	}
	list := action.NewList(cfg)
	list.AllNamespaces = allNamespaces
	list.Limit = limit
	list.Offset = offset
	releases, err := list.Run()
	if err != nil {
		return "", err
	} else if len(releases) == 0 {
		return "No Helm releases found", nil
	}
	ret, err := yaml.Marshal(simplify(releases...))
	if err != nil {
		return "", err
	}
	out := string(ret)
	if note := helmPageNote(limit, offset, len(releases)); note != "" {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += note
	}
	return out, nil
}

// helmPageNote tells the caller the next offset when this page is full.
func helmPageNote(limit, offset, count int) string {
	if limit > 0 && count == limit {
		return fmt.Sprintf("# More releases are available. Call again with the same limit and offset=%d.\n", int64(offset)+int64(limit))
	}
	return ""
}

// withReleaseState adds the state the failed operation left the release in, so the
// caller knows whether the name is still taken and what to do next.
func (h *Helm) withReleaseState(ctx context.Context, name, namespace string, err error) error {
	if name == "" {
		return err
	}
	cfg, cfgErr := h.newAction(ctx, namespace, false)
	if cfgErr != nil {
		return err
	}
	rel, statusErr := action.NewStatus(cfg).Run(name)
	if statusErr != nil || rel == nil || rel.Info == nil {
		return err
	}
	return fmt.Errorf("%w (release %s is now in state %q in namespace %s; its objects may remain: helm_uninstall it before reusing the name, or retry with rollback_on_failure)", err, name, rel.Info.Status.String(), namespace)
}

func (h *Helm) Uninstall(ctx context.Context, name string, namespace string) (string, error) {
	return h.UninstallWithOptions(ctx, name, namespace, InstallOptions{})
}

// UninstallWithOptions uninstalls a release; NoWait and Timeout apply to the wait for its objects to be deleted.
func (h *Helm) UninstallWithOptions(ctx context.Context, name string, namespace string, opts InstallOptions) (string, error) {
	cfg, err := h.newAction(ctx, h.kubernetes.NamespaceOrDefault(namespace), false)
	if err != nil {
		return "", err
	}
	uninstall := action.NewUninstall(cfg)
	// Helm's IgnoreNotFound turns every error of the release-history lookup into
	// "not found", including a refused list of the release Secrets (RBAC). Only a
	// release that really does not exist is reported as not found (with success, as
	// before); any other lookup error is returned.
	uninstall.IgnoreNotFound = false
	uninstall.Wait = !opts.NoWait
	uninstall.Timeout = 5 * time.Minute
	if opts.Timeout > 0 {
		uninstall.Timeout = opts.Timeout
	}
	uninstalledRelease, err := uninstall.Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Sprintf("Release %s not found", name), nil
	} else if err != nil {
		return "", err
	} else if uninstalledRelease == nil {
		return fmt.Sprintf("Release %s not found", name), nil
	}
	return fmt.Sprintf("Uninstalled release %s %s", uninstalledRelease.Release.Name, uninstalledRelease.Info), nil
}

func (h *Helm) newAction(ctx context.Context, namespace string, allNamespaces bool) (*action.Configuration, error) {
	storageDriver := ""
	if h.config != nil {
		storageDriver = h.config.StorageDriver
	}
	cfg := new(action.Configuration)
	applicableNamespace := ""
	if !allNamespaces {
		applicableNamespace = h.kubernetes.NamespaceOrDefault(namespace)
	}
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}
	cfg.RegistryClient = registryClient
	logger := klogutil.FromContext(ctx)
	if err := cfg.Init(h.kubernetes, applicableNamespace, storageDriver, func(format string, v ...any) {
		if logger.V(5).Enabled() {
			logger.V(5).Info(fmt.Sprintf(format, v...))
		}
	}); err != nil {
		return cfg, err
	}
	// Init uses the namespace only for the release storage. The Helm kube client that
	// creates, waits for and deletes the chart's objects otherwise falls back to the
	// kubeconfig's namespace for every object without metadata.namespace, so a chart
	// installed with a namespace argument would land in another namespace than its
	// release record. The Helm CLI sets the same field from --namespace.
	if kc, ok := cfg.KubeClient.(*kube.Client); ok && applicableNamespace != "" {
		kc.Namespace = applicableNamespace
	}
	return cfg, nil
}

// validateChartReference blocks chart references using dangerous URL schemes.
// Only oci:// and https:// URLs are allowed. Non-URL references (e.g. "stable/grafana")
// are permitted as they resolve through Helm's local repo configuration.
// When a Config with AllowedRegistries is provided, URL-based chart references
// must prefix-match an entry in the allowlist, and non-URL references are rejected.
func validateChartReference(chart string, cfg *Config) error {
	u, err := url.Parse(chart)
	if err != nil || u.Scheme == "" || len(u.Scheme) == 1 {
		// Non-URL references (e.g. "stable/grafana", local paths, Windows drive letters like D:\...)
		if cfg != nil && len(cfg.AllowedRegistries) > 0 {
			return fmt.Errorf("chart reference %q is not allowed: only registry URLs from the allowed list are permitted when allowed_registries is configured", chart)
		}
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "oci", "https":
		if cfg != nil && len(cfg.AllowedRegistries) > 0 {
			cleaned := path.Clean(u.Path)
			if cleaned == "." {
				cleaned = ""
			}
			normalized := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + cleaned
			for _, allowed := range cfg.AllowedRegistries {
				if normalized == allowed || strings.HasPrefix(normalized, allowed+"/") {
					return nil
				}
			}
			return fmt.Errorf("chart reference %q is not allowed: does not match any entry in allowed_registries", chart)
		}
		return nil
	case "http":
		return fmt.Errorf("chart reference %q is not allowed: http:// scheme is blocked, use https:// or oci://", chart)
	case "file":
		return fmt.Errorf("chart reference %q is not allowed: file:// scheme is blocked", chart)
	default:
		return fmt.Errorf("chart reference %q is not allowed: only oci:// and https:// schemes are permitted", chart)
	}
}

func simplify(release ...*release.Release) []map[string]interface{} {
	ret := make([]map[string]interface{}, len(release))
	for i, r := range release {
		ret[i] = map[string]interface{}{
			"name":      r.Name,
			"namespace": r.Namespace,
			"revision":  r.Version,
		}
		if r.Chart != nil {
			ret[i]["chart"] = r.Chart.Metadata.Name
			ret[i]["chartVersion"] = r.Chart.Metadata.Version
			ret[i]["appVersion"] = r.Chart.Metadata.AppVersion
		}
		if r.Info != nil {
			ret[i]["status"] = r.Info.Status.String()
			if !r.Info.LastDeployed.IsZero() {
				ret[i]["lastDeployed"] = r.Info.LastDeployed.Format(time.RFC1123Z)
			}
		}
	}
	return ret
}
