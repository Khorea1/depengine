package lock

import (
	"fmt"

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
