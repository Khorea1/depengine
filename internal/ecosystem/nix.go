package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// nixDefaultSource is the flake reference used when a candidate declares none.
const nixDefaultSource = "nixpkgs"

// nixExperimentalFeatures is enabled per invocation, never written to the
// user's nix.conf. `nix profile` needs nix-command and flake installables need
// flakes; without this every call fails on a default Nix install.
const nixExperimentalFeatures = "nix-command flakes"

var (
	// A Nix attribute path: dot-separated identifiers. Quoted attribute names
	// are intentionally unsupported because the value ends up in one argv
	// element and must not be able to alter the installable's structure.
	nixAttrPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_'+-]*(\.[A-Za-z0-9_][A-Za-z0-9_'+-]*)*$`)
	// Flake references are opaque to depengine, but must be a single argv
	// element that cannot be parsed as an option or as an installable fragment.
	nixSourceForbidden = regexp.MustCompile(`[\s#]`)
)

// NixAdapter manages packages in the user's Nix profile through `nix profile`.
//
// Identity: `pkg` is the flake attribute (for example "hello" or
// "python3Packages.requests") and `source` is the flake reference (default
// "nixpkgs"), so the installable is "<source>#<pkg>".
//
// Scope: user profile only. Packages installed through the legacy nix-env, or
// a system profile, are not visible to this adapter.
//
// Removal never guesses a target: it is derived from `nix profile list --json`
// as the profile element matching the candidate's source and attribute, and
// only that element is removed.
//
// UNVERIFIED against a real Nix installation: behavior follows the documented
// `nix profile` interface and is covered through a scripted runner only.
type NixAdapter struct{}

// NewNixAdapter returns the adapter for the "nix" kind.
func NewNixAdapter() *NixAdapter { return &NixAdapter{} }

func (a *NixAdapter) Kind() string { return "nix" }

func (a *NixAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "nix")
}

// nixTarget is the validated identity a candidate selects.
type nixTarget struct {
	source string
	attr   string
}

func (t nixTarget) installable() string { return t.source + "#" + t.attr }

func newNixTarget(tool *config.Tool, mc *config.MethodCandidate) (nixTarget, error) {
	if tool == nil || mc == nil {
		return nixTarget{}, errors.New("nix: tool and method are required")
	}
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	source, _ := mc.Config["source"].(string)
	return validateNixTarget(pkg[0], source)
}

func validateNixTarget(attr, source string) (nixTarget, error) {
	attr = strings.TrimSpace(attr)
	source = strings.TrimSpace(source)
	if source == "" {
		source = nixDefaultSource
	}
	if attr == "" {
		return nixTarget{}, errors.New("nix: no package attribute")
	}
	if !nixAttrPattern.MatchString(attr) {
		return nixTarget{}, fmt.Errorf("nix: invalid package attribute %q", attr)
	}
	if strings.HasPrefix(source, "-") || nixSourceForbidden.MatchString(source) {
		return nixTarget{}, fmt.Errorf("nix: invalid flake reference %q", source)
	}
	return nixTarget{source: source, attr: attr}, nil
}

func nixCommand(args ...string) []string {
	return append([]string{"--extra-experimental-features", nixExperimentalFeatures}, args...)
}

// nixProfileElement holds the fields of one `nix profile list --json` entry
// that identify where it came from. Elements installed without a flake (for
// example imported from nix-env) have no attrPath and never match.
type nixProfileElement struct {
	// Ref is how the running Nix version addresses the element for removal:
	// the element name on Nix >= 2.20, the list index before that.
	Ref         string
	Active      bool   `json:"active"`
	AttrPath    string `json:"attrPath"`
	OriginalURL string `json:"originalUrl"`
}

// parseNixProfileList accepts both layouts of `nix profile list --json`:
// `elements` as an array (older Nix) or as an object keyed by element name.
func parseNixProfileList(out []byte) ([]nixProfileElement, error) {
	var doc struct {
		Elements json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("nix: parse profile list: %w", err)
	}
	if len(doc.Elements) == 0 || string(doc.Elements) == "null" {
		return nil, nil
	}
	var byName map[string]nixProfileElement
	if err := json.Unmarshal(doc.Elements, &byName); err == nil {
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		elements := make([]nixProfileElement, 0, len(names))
		for _, name := range names {
			el := byName[name]
			el.Ref = name
			elements = append(elements, el)
		}
		return elements, nil
	}
	var list []nixProfileElement
	if err := json.Unmarshal(doc.Elements, &list); err != nil {
		return nil, fmt.Errorf("nix: parse profile elements: %w", err)
	}
	for i := range list {
		list[i].Ref = strconv.Itoa(i)
	}
	return list, nil
}

// matches reports whether the element is the target: same flake reference and
// an attribute path of the form <packages|legacyPackages>.<system>.<attr>.
func (e nixProfileElement) matches(t nixTarget) bool {
	sameSource := strings.TrimPrefix(e.OriginalURL, "flake:") == strings.TrimPrefix(t.source, "flake:")
	if e.AttrPath == t.attr {
		return sameSource
	}
	parts := strings.Split(e.AttrPath, ".")
	if len(parts) < 3 || (parts[0] != "packages" && parts[0] != "legacyPackages") {
		return false
	}
	if strings.Join(parts[2:], ".") != t.attr {
		return false
	}
	return sameSource
}

func activeNixProfileElements(elements []nixProfileElement) []nixProfileElement {
	active := make([]nixProfileElement, 0, len(elements))
	for _, element := range elements {
		if element.Active {
			active = append(active, element)
		}
	}
	return active
}

func (a *NixAdapter) listProfile(ctx context.Context, rn run.Runner) ([]nixProfileElement, error) {
	if !a.Available(ctx, rn) {
		return nil, errors.New("nix: nix binary not found")
	}
	res := rn.Run(ctx, "nix", nixCommand("profile", "list", "--json")...)
	if err := run.CheckResult(res, "nix: list profile"); err != nil {
		return nil, err
	}
	return parseNixProfileList(res.Stdout)
}

func (a *NixAdapter) find(ctx context.Context, rn run.Runner, t nixTarget) ([]nixProfileElement, error) {
	elements, err := a.listProfile(ctx, rn)
	if err != nil {
		return nil, err
	}
	var matched []nixProfileElement
	for _, el := range elements {
		if el.matches(t) {
			matched = append(matched, el)
		}
	}
	return matched, nil
}

// Check reports whether the target is in the user's profile.
func (a *NixAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	t, err := newNixTarget(tool, mc)
	if err != nil {
		return false
	}
	matched, err := a.find(ctx, rn, t)
	return err == nil && len(activeNixProfileElements(matched)) > 0
}

// ResolvePlan validates the candidate's identity and records it unchanged.
func (a *NixAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("nix: nil plan intent")
	}
	if _, err := newNixTarget(tool, mc); err != nil {
		return nil, err
	}
	resolved := intent.Clone()
	if resolved.Identity.Package == "" {
		return nil, errors.New("nix: no package attribute in plan intent")
	}
	return &resolved, nil
}

// Observe reports presence by matching the profile's flake elements. Nix does
// not expose a comparable version for a flake element, so only package and
// source are claimed.
func (a *NixAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	t, err := newNixTarget(tool, mc)
	if err != nil {
		return plan.Observation{}, err
	}
	known := []plan.IdentityField{plan.FieldPackage, plan.FieldSource}
	identity := plan.ObservedIdentity{Package: t.attr, Source: t.source}
	matched, err := a.find(ctx, rn, t)
	if err != nil {
		return plan.Observation{}, err
	}
	matched = activeNixProfileElements(matched)
	switch len(matched) {
	case 0:
		return plan.Observation{Presence: plan.PresenceAbsent, Identity: identity, KnownFields: known}, nil
	case 1:
		return plan.Observation{Presence: plan.PresencePresent, Identity: identity, KnownFields: known}, nil
	default:
		return plan.Observation{
			Presence:    plan.PresenceBroken,
			Identity:    identity,
			KnownFields: known,
			Detail:      fmt.Sprintf("nix: %d profile elements match %s", len(matched), t.installable()),
		}, nil
	}
}

// InstallResolved installs exactly the resolved package and source. Explicit
// operations have no Nix-specific interpretation and are rejected.
func (a *NixAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if rn == nil {
		return errors.New("nix: runner is required")
	}
	if resolved == nil {
		return errors.New("nix: nil resolved plan")
	}
	if err := validateResolvedInstallOperation("nix", resolved); err != nil {
		return err
	}
	t, err := validateNixTarget(resolved.Identity.Package, resolved.Identity.Source)
	if err != nil {
		return err
	}
	if !a.Available(ctx, rn) {
		return errors.New("nix: nix binary not found")
	}
	res := rn.Run(ctx, "nix", nixCommand("profile", "install", t.installable())...)
	return run.CheckResult(res, "nix: install")
}

func (a *NixAdapter) CanRemove() bool { return true }

// Remove removes the single profile element that matches the candidate. An
// absent target is a no-op; an ambiguous one is refused rather than guessed.
func (a *NixAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	t, err := newNixTarget(tool, mc)
	if err != nil {
		return err
	}
	matched, err := a.find(ctx, rn, t)
	if err != nil {
		return err
	}
	switch len(matched) {
	case 0:
		return nil
	case 1:
		if strings.HasPrefix(matched[0].Ref, "-") {
			return fmt.Errorf("nix: refusing profile element reference %q", matched[0].Ref)
		}
		res := rn.Run(ctx, "nix", nixCommand("profile", "remove", matched[0].Ref)...)
		return run.CheckResult(res, "nix: remove")
	default:
		return fmt.Errorf("nix: %d profile elements match %s; remove manually with `nix profile remove`", len(matched), t.installable())
	}
}

// CheckAvailable assumes availability: resolving an attribute needs an
// evaluation (and possibly the network), so an unknown attribute surfaces at
// install time instead.
func (a *NixAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no constraint beyond the nix binary existing.
func (a *NixAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*NixAdapter)(nil)
