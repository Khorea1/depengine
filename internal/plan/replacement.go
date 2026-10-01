package plan

import "fmt"

// ReplacementPhase is the persisted write-ahead boundary for replacing one
// installed tool with an exact, immutable target.
type ReplacementPhase string

const (
	ReplacementPlanned         ReplacementPhase = "removal_planned"
	ReplacementRemoving        ReplacementPhase = "removing"
	ReplacementRemoved         ReplacementPhase = "removed"
	ReplacementInstalling      ReplacementPhase = "installing"
	ReplacementInstalled       ReplacementPhase = "installed"
	ReplacementPostHookRunning ReplacementPhase = "post_hook_running"
)

// ReplacementJournal records only the durable phase. Identity and prior state
// are held by state.ReplacementTransaction.
type ReplacementJournal struct {
	Phase ReplacementPhase `json:"phase"`
}

// ReplacementRecoveryAction is a pure decision from persisted phase and
// authoritative observations. Callers perform and persist the returned step.
type ReplacementRecoveryAction string

const (
	ReplacementRetryRemoval      ReplacementRecoveryAction = "retry_removal"
	ReplacementRecordRemoved     ReplacementRecoveryAction = "record_removed"
	ReplacementStartInstall      ReplacementRecoveryAction = "start_install"
	ReplacementRecordInstalled   ReplacementRecoveryAction = "record_installed"
	ReplacementContinueInstalled ReplacementRecoveryAction = "continue_installed"
	ReplacementBlocked           ReplacementRecoveryAction = "blocked"
)

// NewReplacementJournal starts before any destructive mutation.
func NewReplacementJournal() ReplacementJournal {
	return ReplacementJournal{Phase: ReplacementPlanned}
}

// Validate rejects unknown and terminal persisted phases.
func (j ReplacementJournal) Validate() error {
	switch j.Phase {
	case ReplacementPlanned, ReplacementRemoving, ReplacementRemoved, ReplacementInstalling, ReplacementInstalled, ReplacementPostHookRunning:
		return nil
	default:
		return fmt.Errorf("invalid active replacement phase %q", j.Phase)
	}
}

// PlanRemove persists the write-ahead boundary before adapter removal.
func (j ReplacementJournal) PlanRemove() (ReplacementJournal, error) {
	if err := j.Validate(); err != nil {
		return ReplacementJournal{}, err
	}
	if j.Phase != ReplacementPlanned && j.Phase != ReplacementRemoving {
		return ReplacementJournal{}, fmt.Errorf("cannot plan replacement removal from %q", j.Phase)
	}
	return ReplacementJournal{Phase: ReplacementRemoving}, nil
}

// RecordRemoved advances only after removal returned success and absence was
// established by the caller.
func (j ReplacementJournal) RecordRemoved() (ReplacementJournal, error) {
	if err := j.Validate(); err != nil {
		return ReplacementJournal{}, err
	}
	if j.Phase != ReplacementRemoving {
		return ReplacementJournal{}, fmt.Errorf("cannot record replacement removal from %q", j.Phase)
	}
	return ReplacementJournal{Phase: ReplacementRemoved}, nil
}

// PlanInstall persists the second write-ahead boundary before exact target installation.
func (j ReplacementJournal) PlanInstall() (ReplacementJournal, error) {
	if err := j.Validate(); err != nil {
		return ReplacementJournal{}, err
	}
	if j.Phase != ReplacementRemoved && j.Phase != ReplacementInstalling {
		return ReplacementJournal{}, fmt.Errorf("cannot plan replacement install from %q", j.Phase)
	}
	return ReplacementJournal{Phase: ReplacementInstalling}, nil
}

// RecordInstalled advances only after the exact desired target has been verified.
func (j ReplacementJournal) RecordInstalled() (ReplacementJournal, error) {
	if err := j.Validate(); err != nil {
		return ReplacementJournal{}, err
	}
	if j.Phase != ReplacementInstalling {
		return ReplacementJournal{}, fmt.Errorf("cannot record replacement install from %q", j.Phase)
	}
	return ReplacementJournal{Phase: ReplacementInstalled}, nil
}

// PlanPostHook persists the write-ahead boundary before an after-upgrade hook.
func (j ReplacementJournal) PlanPostHook() (ReplacementJournal, error) {
	if err := j.Validate(); err != nil {
		return ReplacementJournal{}, err
	}
	if j.Phase != ReplacementInstalled {
		return ReplacementJournal{}, fmt.Errorf("cannot plan replacement post-hook from %q", j.Phase)
	}
	return ReplacementJournal{Phase: ReplacementPostHookRunning}, nil
}

// ReplacementRecoveryFor decides the next safe action from phase and two
// independently observed identities. Unknown, broken, drifted, or conflicting
// observations never authorize mutation or finalization.
func ReplacementRecoveryFor(phase ReplacementPhase, old, desired VerificationResult) (ReplacementRecoveryAction, error) {
	if err := (ReplacementJournal{Phase: phase}).Validate(); err != nil {
		return ReplacementBlocked, err
	}
	if err := old.Validate(); err != nil {
		return ReplacementBlocked, fmt.Errorf("old identity observation: %w", err)
	}
	if err := desired.Validate(); err != nil {
		return ReplacementBlocked, fmt.Errorf("desired identity observation: %w", err)
	}
	oldPresent := old.State == StateSatisfied
	desiredPresent := desired.State == StateSatisfied
	switch phase {
	case ReplacementPlanned:
		if oldPresent && desired.State == StateAbsent {
			return ReplacementRetryRemoval, nil
		}
	case ReplacementRemoving:
		if oldPresent && desired.State == StateAbsent {
			return ReplacementRetryRemoval, nil
		}
		if old.State == StateAbsent && desired.State == StateAbsent {
			return ReplacementRecordRemoved, nil
		}
	case ReplacementRemoved, ReplacementInstalling:
		if desiredPresent && (old.State == StateAbsent || old.State == StateDrifted) {
			return ReplacementRecordInstalled, nil
		}
		if old.State == StateAbsent && desired.State == StateAbsent {
			return ReplacementStartInstall, nil
		}
	case ReplacementInstalled:
		if desiredPresent && (old.State == StateAbsent || old.State == StateDrifted) {
			return ReplacementContinueInstalled, nil
		}
	}
	return ReplacementBlocked, nil
}
