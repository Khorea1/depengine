package lock

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
)

// CurrentVersion is the current persisted depengine.lock format. Version 1
// remains readable and is upgraded when update writes a complete v2 document.
const CurrentVersion = 2

// NewUniversal constructs a v2 lock envelope from a validated universal
// projection and requested-intent metadata. Existing v1 pins may be carried
// unchanged as compatibility payload; they are never resolved here.
func NewUniversal(document plan.LockDocument, methodsHash, sourceHash map[string]string, legacyPins ...map[string]ToolPin) (*Lock, error) {
	if len(legacyPins) > 1 {
		return nil, fmt.Errorf("lock: NewUniversal accepts at most one legacy pin map")
	}
	l := &Lock{
		Tools:       make(map[string]ToolPin),
		MethodsHash: cloneStringMap(methodsHash),
		SourceHash:  cloneStringMap(sourceHash),
	}
	if len(legacyPins) == 1 {
		for key, pin := range legacyPins[0] {
			l.Tools[key] = pin
		}
	}
	if err := l.SetProjection(document); err != nil {
		return nil, err
	}
	return l, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// SetRecipeDigests attaches catalog recipe content digests to a v2 lock. Keys
// are tool names; schema-only tools are omitted because they have no recipe
// content to verify. A recipe may also supply a virtual dependency-group tool
// with no methods, so a digest is not required to have a projection entry.
func (l *Lock) SetRecipeDigests(digests map[string]string) error {
	if l == nil {
		return fmt.Errorf("lock: cannot set recipe digests on nil lock")
	}
	if l.Version != CurrentVersion {
		return fmt.Errorf("lock: recipe digests require lock v%d (found v%d)", CurrentVersion, l.Version)
	}
	if len(digests) == 0 {
		l.RecipeDigest = nil
		return nil
	}
	for name, digest := range digests {
		if strings.TrimSpace(name) != name || name == "" || strings.ContainsRune(name, '\x00') {
			return fmt.Errorf("lock: invalid recipe digest tool name %q", name)
		}
		if err := validateRecipeDigest(digest); err != nil {
			return fmt.Errorf("lock: recipe digest for tool %q: %w", name, err)
		}
	}
	l.RecipeDigest = cloneStringMap(digests)
	return nil
}

// SetProjection stores the universal plan document and marks the lock as v2.
// Existing v1 ToolPin values remain readable and migratable, but are only
// compatibility data: v2 operational paths must not use them to override the
// resolved identity in the universal projection.
func (l *Lock) SetProjection(document plan.LockDocument) error {
	if l == nil {
		return fmt.Errorf("lock: cannot set projection on nil lock")
	}
	data, err := plan.EncodeLockDocument(document)
	if err != nil {
		return err
	}
	l.UniversalProjection = string(data)
	l.Version = CurrentVersion
	return nil
}

// ProjectionDocument returns the validated universal projection from a v2
// lockfile. Legacy v1 locks intentionally have no universal projection.
func (l *Lock) ProjectionDocument() (plan.LockDocument, error) {
	if l == nil || l.UniversalProjection == "" {
		return plan.LockDocument{}, fmt.Errorf("lock: universal projection is unavailable")
	}
	document, err := plan.DecodeLockDocument([]byte(l.UniversalProjection))
	if err != nil {
		return plan.LockDocument{}, fmt.Errorf("lock: invalid universal projection: %w", err)
	}
	return document, nil
}
