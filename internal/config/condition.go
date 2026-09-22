package config

import (
	"strings"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/platform"
)

// Condition is the parsed form of `when = { ... }`. All fields are honored by
// Match with AND semantics across fields and OR semantics within slices.
type Condition struct {
	DistroFamily     []string `cfg:"distro_family"`
	TargetFamily     []string `cfg:"target_family"`
	DistroID         []string `cfg:"distro_id"`
	DistroVersion    []string `cfg:"distro_version"`
	DistroVersionMin string   `cfg:"distro_version_min"`
	DistroVersionMax string   `cfg:"distro_version_max"`
	Arch             []string `cfg:"arch"`
	OS               []string `cfg:"os"`
	Kernel           []string `cfg:"kernel"`
	Libc             []string `cfg:"libc"`
	InitSystem       []string `cfg:"init_system"`
	IsWSL            *bool    `cfg:"is_wsl"`
	IsContainer      *bool    `cfg:"is_container"`
	// Facts.OS reports "linux" on Termux, so IsAndroid is the reliable way
	// to target Android.
	IsAndroid *bool `cfg:"is_android"`
}

func (c *Condition) IsZero() bool {
	return len(c.DistroFamily) == 0 &&
		len(c.TargetFamily) == 0 &&
		len(c.DistroID) == 0 &&
		len(c.DistroVersion) == 0 &&
		c.DistroVersionMin == "" &&
		c.DistroVersionMax == "" &&
		len(c.Arch) == 0 &&
		len(c.OS) == 0 &&
		len(c.Kernel) == 0 &&
		len(c.Libc) == 0 &&
		len(c.InitSystem) == 0 &&
		c.IsWSL == nil &&
		c.IsContainer == nil &&
		c.IsAndroid == nil
}

// Match reports whether this condition is satisfied by the given system facts.
// A nil condition always matches; a non-empty condition cannot match nil facts.
func (c *Condition) Match(facts *platform.Facts) bool {
	if c == nil {
		return true
	}
	if facts == nil {
		return c.IsZero()
	}

	if len(c.DistroFamily) > 0 {
		clan := platform.ResolveFamily(facts)
		if !platform.MatchesDistroFamily(clan, c.DistroFamily) {
			return false
		}
	}
	if len(c.DistroID) > 0 && !matchExact(c.DistroID, facts.DistroID) {
		return false
	}
	if len(c.DistroVersion) > 0 && !matchVersion(c.DistroVersion, facts.DistroVersion) {
		return false
	}
	if c.DistroVersionMin != "" && (facts.DistroVersion == "" || platform.CompareVersion(facts.DistroVersion, c.DistroVersionMin) < 0) {
		return false
	}
	if c.DistroVersionMax != "" && (facts.DistroVersion == "" || platform.CompareVersion(facts.DistroVersion, c.DistroVersionMax) > 0) {
		return false
	}
	if len(c.TargetFamily) > 0 && !matchExact(c.TargetFamily, facts.TargetFamily) {
		return false
	}
	if len(c.Arch) > 0 && !matchExact(c.Arch, facts.TargetArch) {
		return false
	}
	if len(c.OS) > 0 && !matchExact(c.OS, facts.OS) {
		return false
	}
	if len(c.Kernel) > 0 && !matchExact(c.Kernel, facts.Kernel) {
		return false
	}
	if len(c.InitSystem) > 0 && !matchExact(c.InitSystem, facts.InitSystem) {
		return false
	}
	if len(c.Libc) > 0 && !matchPrefix(c.Libc, facts.Libc) {
		return false
	}
	if c.IsWSL != nil && *c.IsWSL != facts.IsWSL {
		return false
	}
	if c.IsContainer != nil && *c.IsContainer != facts.IsContainer {
		return false
	}
	if c.IsAndroid != nil && *c.IsAndroid != facts.IsAndroid {
		return false
	}
	return true
}

func matchExact(allowed []string, actual string) bool {
	for _, value := range allowed {
		if strings.EqualFold(value, actual) {
			return true
		}
	}
	return false
}

func matchVersion(allowed []string, actual string) bool {
	if actual == "" {
		return false
	}
	for _, value := range allowed {
		if platform.CompareVersion(actual, value) == 0 {
			return true
		}
	}
	return false
}

func matchPrefix(allowed []string, actual string) bool {
	actual = strings.ToLower(actual)
	for _, value := range allowed {
		if strings.HasPrefix(actual, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func parseBoolPtr(v any) *bool {
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

func parseCondition(raw any) *Condition {
	rm, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	cond := &Condition{}
	if invalidKeys := decodeStructFields(cond, rm); len(invalidKeys) > 0 {
		log.Default.Debug("parseCondition: ignoring unknown keys", "keys", invalidKeys)
	}
	if cond.IsZero() {
		return nil
	}
	return cond
}
