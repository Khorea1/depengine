package config

import (
	"sort"
	"strconv"
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

// String returns a deterministic, human-readable condition expression.
//
// Slice values are sorted because their matching semantics are OR-based; this
// makes equivalent conditions render identically regardless of declaration
// order. Quoted values keep the representation unambiguous for graph labels.
func (c *Condition) String() string {
	if c == nil {
		return ""
	}

	parts := make([]string, 0, 14)
	appendList := func(name string, values []string) {
		if len(values) == 0 {
			return
		}
		ordered := append([]string(nil), values...)
		sort.Strings(ordered)
		for i := range ordered {
			ordered[i] = strconv.Quote(ordered[i])
		}
		parts = append(parts, name+" in ["+strings.Join(ordered, ",")+"]")
	}
	appendBool := func(name string, value *bool) {
		if value != nil {
			parts = append(parts, name+" == "+strconv.FormatBool(*value))
		}
	}

	appendList("distro_family", c.DistroFamily)
	appendList("target_family", c.TargetFamily)
	appendList("distro_id", c.DistroID)
	appendList("distro_version", c.DistroVersion)
	if c.DistroVersionMin != "" {
		parts = append(parts, "distro_version >= "+strconv.Quote(c.DistroVersionMin))
	}
	if c.DistroVersionMax != "" {
		parts = append(parts, "distro_version <= "+strconv.Quote(c.DistroVersionMax))
	}
	appendList("arch", c.Arch)
	appendList("os", c.OS)
	appendList("kernel", c.Kernel)
	appendList("libc", c.Libc)
	appendList("init_system", c.InitSystem)
	appendBool("is_wsl", c.IsWSL)
	appendBool("is_container", c.IsContainer)
	appendBool("is_android", c.IsAndroid)

	return strings.Join(parts, " && ")
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
