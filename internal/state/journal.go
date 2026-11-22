package state

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

// JournalVersion is the on-disk format of one journal record. It is independent
// of the state, schema, and lock format versions.
const JournalVersion = 1

// JournalMaxBytes bounds the append-only journal. When the current file reaches
// this size it is rotated to journal.jsonl.1 and a fresh file starts; queries
// read both generations.
const JournalMaxBytes = 8 << 20

// Stable journal result values. Consumers classify by these, never by Detail.
const (
	JournalSuccess  = "success"
	JournalAlready  = "already"
	JournalFailed   = "failed"
	JournalSkipped  = "skipped"
	JournalUpdated  = "updated"
	JournalRemoved  = "removed"
	JournalReverted = "reverted"
)

// JournalResults returns every stable journal result value in documentation
// order. Queries validate --result against this closed vocabulary so a typo
// fails instead of silently matching nothing.
func JournalResults() []string {
	return []string{JournalSuccess, JournalAlready, JournalFailed, JournalSkipped, JournalUpdated, JournalRemoved, JournalReverted}
}

// IsJournalResult reports whether result is a stable journal result value.
func IsJournalResult(result string) bool {
	for _, candidate := range JournalResults() {
		if candidate == result {
			return true
		}
	}
	return false
}

// JournalIdentity is the concrete installed identity recorded with a mutation.
// Empty fields mean the adapter could not determine that dimension.
type JournalIdentity struct {
	Package  string `json:"package,omitempty"`
	Version  string `json:"version,omitempty"`
	Revision string `json:"revision,omitempty"`
	Source   string `json:"source,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

// JournalEntry is one append-only mutation record. Entries never contain secret
// material: every string is redacted before persistence.
type JournalEntry struct {
	FormatVersion int              `json:"format_version"`
	Timestamp     string           `json:"timestamp"`
	TraceID       string           `json:"trace_id,omitempty"`
	Command       string           `json:"command"`
	Tool          string           `json:"tool,omitempty"`
	Method        string           `json:"method,omitempty"`
	Provider      string           `json:"provider,omitempty"`
	Result        string           `json:"result"`
	ExitCode      int              `json:"exit_code,omitempty"`
	Elevated      bool             `json:"elevated,omitempty"`
	SchemaHash    string           `json:"schema_hash,omitempty"`
	Identity      *JournalIdentity `json:"identity,omitempty"`
	Detail        string           `json:"detail,omitempty"`
}

// DefaultJournalPath returns journal.jsonl next to the state file.
func DefaultJournalPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "journal.jsonl")
}

// SchemaHash returns a stable digest of the effective tool definitions. It is a
// decision record, not an identity boundary: lock and state keep their own
// hashes.
func SchemaHash(schema *config.Schema) string {
	if schema == nil {
		return ""
	}
	names := make([]string, 0, len(schema.Tools))
	for name := range schema.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(DefinitionHash(schema.Tools[name])))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// NewJournalEntry stamps an entry with the current time, trace id, and format
// version. Callers fill the command-specific fields.
func NewJournalEntry(command string) JournalEntry {
	return JournalEntry{
		FormatVersion: JournalVersion,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:       os.Getenv("DEPENGINE_TRACE_ID"),
		Command:       command,
	}
}

// AppendJournal appends entries to the default journal, creating it with
// owner-only permissions and rotating it once the retention bound is reached.
func AppendJournal(ctx context.Context, entries ...JournalEntry) error {
	return AppendJournalTo(ctx, DefaultJournalPath(), entries...)
}

// AppendJournalTo is AppendJournal with an explicit path, used by tests and
// callers that relocated the state directory.
func AppendJournalTo(ctx context.Context, path string, entries ...JournalEntry) error {
	return appendJournalTo(ctx, path, JournalMaxBytes, entries...)
}

// appendJournalTo is AppendJournalTo with an injectable rotation bound, so
// tests can exercise repeated rotation without writing 8 MiB.
func appendJournalTo(ctx context.Context, path string, limit int64, entries ...JournalEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create journal directory: %w", err)
	}
	unlock, err := lockFilePathContext(ctx, path+".lock", exclusiveLockMode, "acquire journal lock")
	if err != nil {
		return fmt.Errorf("lock journal: %w", err)
	}
	defer func() { _ = unlock.Close() }()

	if info, err := os.Stat(path); err == nil && info.Size() >= limit {
		if err := rotateJournal(path); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- journal path is derived from the private state directory.
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("tighten journal permissions: %w", err)
	}
	for _, entry := range entries {
		line, err := json.Marshal(redactJournalEntry(entry))
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("encode journal entry: %w", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			return fmt.Errorf("write journal entry: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync journal: %w", err)
	}
	return f.Close()
}

// journalExists reports whether either journal generation exists.
func journalExists(path string) (bool, error) {
	for _, candidate := range []string{path, path + ".1"} {
		_, err := os.Stat(candidate)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect journal: %w", err)
		}
	}
	return false, nil
}

// rotateJournal replaces journal.jsonl.1 with the current generation. os.Rename
// does not replace an existing destination on Windows (MoveFile fails there),
// so the previous generation is removed first. The append lock is held by the
// caller and queries take the shared lock, so neither a rotation nor a query
// observes the intermediate state.
func rotateJournal(path string) error {
	rotated := path + ".1"
	if err := os.Remove(rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rotate journal: remove previous generation: %w", err)
	}
	if err := os.Rename(path, rotated); err != nil {
		return fmt.Errorf("rotate journal: %w", err)
	}
	return nil
}

// JournalFilter narrows a journal query. Zero fields match everything.
type JournalFilter struct {
	Tool    string
	Command string
	Result  string
	Since   time.Time
	Until   time.Time
	// Limit keeps only the most recent N matching entries. Zero means all.
	Limit int
}

// QueryJournal reads the rotated generation and the current file, returning
// matching entries in chronological order. Malformed lines are skipped: a
// damaged record must not hide the rest of the audit trail.
//
// The query holds the shared journal lock for both reads, so a rotation cannot
// drop a whole generation between them and an in-flight append cannot expose a
// partial line. A missing journal returns no entries without creating the state
// directory just to answer a read-only query.
func QueryJournal(path string, filter JournalFilter) ([]JournalEntry, error) {
	exists, err := journalExists(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	unlock, err := lockSharedFilePathContext(context.Background(), path+".lock", "acquire journal query lock")
	if err != nil {
		return nil, fmt.Errorf("lock journal query: %w", err)
	}
	defer func() { _ = unlock.Close() }()

	var entries []JournalEntry
	for _, candidate := range []string{path + ".1", path} {
		batch, err := readJournalFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		entries = append(entries, batch...)
	}
	// Preserve the parsed instant alongside each entry: RFC3339Nano uses
	// variable-width fractional seconds, so string order is not time order.
	type timestampedEntry struct {
		entry JournalEntry
		stamp time.Time
	}
	var filtered []timestampedEntry
	for _, entry := range entries {
		if filter.Tool != "" && entry.Tool != filter.Tool {
			continue
		}
		if filter.Command != "" && entry.Command != filter.Command {
			continue
		}
		if filter.Result != "" && entry.Result != filter.Result {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if !filter.Since.IsZero() && stamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && stamp.After(filter.Until) {
			continue
		}
		filtered = append(filtered, timestampedEntry{entry: entry, stamp: stamp})
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].stamp.Before(filtered[j].stamp)
	})
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[len(filtered)-filter.Limit:]
	}
	result := entries[:0]
	for _, item := range filtered {
		result = append(result, item.entry)
	}
	return result, nil
}

func readJournalFile(path string) ([]JournalEntry, error) {
	f, err := os.Open(path) // #nosec G304 -- journal path is derived from the private state directory.
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var entries []JournalEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// A record written by an unknown format cannot be interpreted with the
		// current field semantics, so it is skipped like a damaged line instead
		// of being misreported as a v1 entry. JournalVersion is the only format
		// this reader understands.
		if entry.FormatVersion != JournalVersion {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	return entries, nil
}

func redactJournalEntry(entry JournalEntry) JournalEntry {
	entry.TraceID = run.RedactSensitiveText(entry.TraceID)
	entry.Tool = run.RedactSensitiveText(entry.Tool)
	entry.Method = run.RedactSensitiveText(entry.Method)
	entry.Provider = run.RedactSensitiveText(entry.Provider)
	entry.Detail = run.RedactSensitiveText(entry.Detail)
	if entry.Identity != nil {
		identity := *entry.Identity
		identity.Package = run.RedactSensitiveText(identity.Package)
		identity.Version = run.RedactSensitiveText(identity.Version)
		identity.Revision = run.RedactSensitiveText(identity.Revision)
		identity.Source = run.RedactSensitiveText(identity.Source)
		identity.Digest = run.RedactSensitiveText(identity.Digest)
		entry.Identity = &identity
	}
	return entry
}
