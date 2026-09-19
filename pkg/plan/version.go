package plan

import (
	"errors"
	"fmt"
	"strings"
)

// VersionMode identifies the semantic form of requested version/revision
// intent. Modes are explicit so adapters cannot reinterpret an exact request
// as latest, or confuse a branch/tag with a package version.
type VersionMode string

const (
	VersionLatest       VersionMode = "latest"
	VersionExact        VersionMode = "exact"
	VersionConstraint   VersionMode = "constraint"
	VersionChannel      VersionMode = "channel"
	VersionGitTag       VersionMode = "git_tag"
	VersionGitBranch    VersionMode = "git_branch"
	VersionGitRevision  VersionMode = "git_revision"
	VersionContainerTag VersionMode = "container_tag"
	VersionDigest       VersionMode = "digest"
)

// VersionPortability documents whether a version mode has common semantics
// across unrelated installation methods or is meaningful only to a family of
// methods. This is planner metadata, not a claim that every method supports a
// portable mode.
type VersionPortability string

const (
	VersionPortable       VersionPortability = "portable"
	VersionMethodSpecific VersionPortability = "method_specific"
)

// ChannelSelector represents channel-style version intent without flattening
// distinct concepts such as Snap track/risk into an ambiguous string.
type ChannelSelector struct {
	Name  string `json:"name,omitempty"`
	Track string `json:"track,omitempty"`
	Risk  string `json:"risk,omitempty"`
}

// VersionIntent is the requested version/revision semantics before a method
// resolves them to the concrete fields in ResolvedIdentity.
type VersionIntent struct {
	Mode    VersionMode      `json:"mode"`
	Value   string           `json:"value,omitempty"`
	Channel *ChannelSelector `json:"channel,omitempty"`
}

// Validate rejects ambiguous or incomplete version intent. It deliberately
// does not parse ecosystem-specific version grammars; those remain method
// resolver responsibilities.
func (v VersionIntent) Validate() error {
	if v.Mode == "" {
		return errors.New("version mode is required")
	}
	if v.Value != strings.TrimSpace(v.Value) {
		return errors.New("version value must not have surrounding whitespace")
	}
	if v.Channel != nil {
		if v.Channel.Name != strings.TrimSpace(v.Channel.Name) ||
			v.Channel.Track != strings.TrimSpace(v.Channel.Track) ||
			v.Channel.Risk != strings.TrimSpace(v.Channel.Risk) {
			return errors.New("channel fields must not have surrounding whitespace")
		}
	}

	switch v.Mode {
	case VersionLatest:
		if v.Value != "" || v.Channel != nil {
			return errors.New("latest version intent must not include a value or channel")
		}
	case VersionExact, VersionConstraint, VersionGitTag, VersionGitBranch, VersionGitRevision, VersionContainerTag, VersionDigest:
		if v.Value == "" {
			return fmt.Errorf("%s version intent requires a value", v.Mode)
		}
		if v.Channel != nil {
			return fmt.Errorf("%s version intent must not include channel fields", v.Mode)
		}
	case VersionChannel:
		if v.Value != "" {
			return errors.New("channel version intent uses channel fields, not value")
		}
		if v.Channel == nil || (v.Channel.Name == "" && v.Channel.Track == "" && v.Channel.Risk == "") {
			return errors.New("channel version intent requires at least one channel field")
		}
	default:
		return fmt.Errorf("unsupported version mode %q", v.Mode)
	}
	return nil
}

// Portability classifies the semantic scope of a version mode. Exact package
// versions, constraints, and latest intent have cross-method meaning; channel,
// Git, container-tag and digest forms require method-family interpretation.
func (v VersionIntent) Portability() VersionPortability {
	switch v.Mode {
	case VersionLatest, VersionExact, VersionConstraint:
		return VersionPortable
	default:
		return VersionMethodSpecific
	}
}
