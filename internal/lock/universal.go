package lock

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/plan"
)

// CurrentVersion is the current persisted depengine.lock format. Version 1
// remains readable and is upgraded when update writes a complete v2 document.
const CurrentVersion = 2

// SetProjection stores the universal plan document and marks the lock as v2.
// Existing method-specific pins remain alongside it so legacy readers can be
// migrated deliberately rather than silently discarded.
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
