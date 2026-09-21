package source

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/plan"
)

const resourceKeyVersion = "v1"

// ResourceIdentity returns the canonical, credential-free ownership identity
// for a host package source. Source URLs are intentionally excluded: host
// managers remove and address these resources by kind+name, and persisting a
// URL here would both duplicate mutable metadata and risk credential leakage.
func ResourceIdentity(source config.Source) (plan.ResourceIdentity, error) {
	kind := strings.ToLower(strings.TrimSpace(source.Kind))
	name := strings.ToLower(strings.TrimSpace(source.Name))
	if kind == "" || name == "" {
		return plan.ResourceIdentity{}, fmt.Errorf("source identity requires non-empty kind and name")
	}
	if strings.ContainsRune(kind, '\x00') || strings.ContainsRune(name, '\x00') {
		return plan.ResourceIdentity{}, fmt.Errorf("source identity must not contain NUL")
	}
	switch kind {
	case "apt-ppa", "dnf-copr", "scoop-bucket", "brew-tap":
	default:
		return plan.ResourceIdentity{}, fmt.Errorf("source: unsupported kind %q", source.Kind)
	}
	key := resourceKeyVersion + "?kind=" + url.QueryEscape(kind) + "&name=" + url.QueryEscape(name)
	identity := plan.ResourceIdentity{Kind: plan.ResourceSource, Key: key}
	if err := identity.Validate(); err != nil {
		return plan.ResourceIdentity{}, fmt.Errorf("source resource identity: %w", err)
	}
	return identity, nil
}

// FromResourceIdentity reconstructs the minimum source descriptor required for
// host cleanup. It accepts only canonical keys emitted by ResourceIdentity.
func FromResourceIdentity(identity plan.ResourceIdentity) (config.Source, error) {
	if err := identity.Validate(); err != nil {
		return config.Source{}, err
	}
	if identity.Kind != plan.ResourceSource {
		return config.Source{}, fmt.Errorf("resource kind %q is not a source", identity.Kind)
	}
	prefix := resourceKeyVersion + "?"
	if !strings.HasPrefix(identity.Key, prefix) {
		return config.Source{}, fmt.Errorf("unsupported source resource key %q", identity.Key)
	}
	values, err := url.ParseQuery(strings.TrimPrefix(identity.Key, prefix))
	if err != nil {
		return config.Source{}, fmt.Errorf("parse source resource key: %w", err)
	}
	if len(values) != 2 || len(values["kind"]) != 1 || len(values["name"]) != 1 {
		return config.Source{}, fmt.Errorf("invalid source resource key %q", identity.Key)
	}
	source := config.Source{Kind: values.Get("kind"), Name: values.Get("name")}
	canonical, err := ResourceIdentity(source)
	if err != nil {
		return config.Source{}, err
	}
	if canonical != identity {
		return config.Source{}, fmt.Errorf("source resource key %q is not canonical; use %q", identity.Key, canonical.Key)
	}
	return source, nil
}
