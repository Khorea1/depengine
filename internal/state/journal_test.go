package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
)

func journalPathForTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "journal.jsonl")
}

func fixedJournalEntry(command, tool, result, stamp string) JournalEntry {
	entry := NewJournalEntry(command)
	entry.Timestamp = stamp
	entry.Tool = tool
	entry.Result = result
	return entry
}

func TestJournalAppendQueryFiltersAndLimit(t *testing.T) {
	path := journalPathForTest(t)
	entries := []JournalEntry{
		fixedJournalEntry("install", "jq", JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedJournalEntry("install", "rg", JournalFailed, "2026-01-02T10:00:00Z"),
		fixedJournalEntry("remove", "jq", JournalRemoved, "2026-01-03T10:00:00Z"),
	}
	if err := AppendJournalTo(context.Background(), path, entries...); err != nil {
		t.Fatalf("append: %v", err)
	}
	all, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("entries = %d, want 3", len(all))
	}
	if all[0].Command != "install" || all[2].Command != "remove" {
		t.Fatalf("entries are not chronological: %+v", all)
	}

	byTool, err := QueryJournal(path, JournalFilter{Tool: "jq"})
	if err != nil || len(byTool) != 2 {
		t.Fatalf("tool filter = %d entries, err=%v", len(byTool), err)
	}
	failed, err := QueryJournal(path, JournalFilter{Result: JournalFailed})
	if err != nil || len(failed) != 1 || failed[0].Tool != "rg" {
		t.Fatalf("result filter = %+v, err=%v", failed, err)
	}
	since, _ := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	windowed, err := QueryJournal(path, JournalFilter{Since: since})
	if err != nil || len(windowed) != 2 {
		t.Fatalf("since filter = %d entries, err=%v", len(windowed), err)
	}
	limited, err := QueryJournal(path, JournalFilter{Limit: 1})
	if err != nil || len(limited) != 1 || limited[0].Tool != "jq" || limited[0].Command != "remove" {
		t.Fatalf("limit filter kept the wrong entry: %+v, err=%v", limited, err)
	}
}

func TestJournalFractionalTimestampsSortChronologicallyBeforeLimit(t *testing.T) {
	path := journalPathForTest(t)
	entries := []JournalEntry{
		fixedJournalEntry("install", "mid", JournalSuccess, "2026-01-01T10:00:00.123Z"),
		fixedJournalEntry("install", "early", JournalSuccess, "2026-01-01T10:00:00.12Z"),
		fixedJournalEntry("install", "late", JournalSuccess, "2026-01-01T10:00:00.5Z"),
	}
	if err := AppendJournalTo(context.Background(), path, entries...); err != nil {
		t.Fatalf("append: %v", err)
	}
	for _, tc := range []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "all", want: []string{"early", "mid", "late"}},
		{name: "latest two", limit: 2, want: []string{"mid", "late"}},
		{name: "latest one", limit: 1, want: []string{"late"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := QueryJournal(path, JournalFilter{Limit: tc.limit})
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("entries = %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if got[i].Tool != want {
					t.Errorf("entry %d: tool = %q, want %q", i, got[i].Tool, want)
				}
			}
		})
	}
}

func TestJournalRedactsSecretsAndSkipsMalformedLines(t *testing.T) {
	path := journalPathForTest(t)
	entry := fixedJournalEntry("install", "jq", JournalFailed, "2026-01-01T10:00:00Z")
	entry.Detail = "clone https://user:token-secret@example.test/repo failed"
	if err := AppendJournalTo(context.Background(), path, entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- test temp file.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json}\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	entries, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (malformed line skipped)", len(entries))
	}
	if strings.Contains(entries[0].Detail, "token-secret") {
		t.Fatalf("detail kept a secret: %q", entries[0].Detail)
	}
}

func TestJournalReadsRotatedGeneration(t *testing.T) {
	path := journalPathForTest(t)
	older := fixedJournalEntry("install", "older", JournalSuccess, "2026-01-01T10:00:00Z")
	newer := fixedJournalEntry("install", "newer", JournalSuccess, "2026-01-02T10:00:00Z")
	if err := AppendJournalTo(context.Background(), path+".1", older); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournalTo(context.Background(), path, newer); err != nil {
		t.Fatal(err)
	}
	entries, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 || entries[0].Tool != "older" || entries[1].Tool != "newer" {
		t.Fatalf("rotated generations not merged: %+v", entries)
	}
}

func TestJournalConcurrentAppendsStayParseable(t *testing.T) {
	path := journalPathForTest(t)
	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entry := fixedJournalEntry("install", "worker", JournalSuccess, "2026-01-01T10:00:00Z")
			entry.Detail = strings.Repeat("x", i)
			if err := AppendJournalTo(context.Background(), path, entry); err != nil {
				t.Errorf("append: %v", err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != workers {
		t.Fatalf("entries = %d, want %d", len(entries), workers)
	}
}

func TestJournalRotatesRepeatedlyReplacingPreviousGeneration(t *testing.T) {
	path := journalPathForTest(t)
	// A one-byte bound rotates on every append after the first, so this
	// exercises the second rotation exactly where Windows used to fail: the
	// destination journal.jsonl.1 already exists.
	const limit = int64(1)
	entries := []JournalEntry{
		fixedJournalEntry("install", "first", JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedJournalEntry("install", "second", JournalSuccess, "2026-01-02T10:00:00Z"),
		fixedJournalEntry("install", "third", JournalSuccess, "2026-01-03T10:00:00Z"),
	}
	for i, entry := range entries {
		if err := appendJournalTo(context.Background(), path, limit, entry); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	query, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// The third append replaces the rotated generation, which then holds the
	// second entry; the oldest generation is intentionally discarded.
	if len(query) != 2 || query[0].Tool != "second" || query[1].Tool != "third" {
		t.Fatalf("generations after repeated rotation = %+v, want second, third", query)
	}
}

func TestQueryJournalWaitsForJournalWriter(t *testing.T) {
	path := journalPathForTest(t)
	if err := AppendJournalTo(context.Background(), path, fixedJournalEntry("install", "jq", JournalSuccess, "2026-01-01T10:00:00Z")); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	holder, err := lockFilePathContext(context.Background(), path+".lock", exclusiveLockMode, "hold journal lock")
	if err != nil {
		t.Fatalf("hold journal lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := QueryJournal(path, JournalFilter{})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("query returned while the journal writer lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := holder.Close(); err != nil {
		t.Fatalf("release journal lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("query: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query did not resume after the writer released the lock")
	}
}

func TestQueryJournalDuringRotationNeverFailsOrReturnsPartialEntries(t *testing.T) {
	path := journalPathForTest(t)
	const limit = int64(512)
	payload := strings.Repeat("x", 400)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 40; i++ {
			entry := fixedJournalEntry("install", "rotating", JournalSuccess, "2026-01-01T10:00:00Z")
			entry.Detail = payload
			if err := appendJournalTo(context.Background(), path, limit, entry); err != nil {
				t.Errorf("append: %v", err)
				return
			}
		}
	}()
	for {
		select {
		case <-stop:
			wg.Wait()
			return
		default:
		}
		entries, err := QueryJournal(path, JournalFilter{})
		if err != nil {
			t.Fatalf("query during rotation: %v", err)
		}
		for _, entry := range entries {
			if entry.FormatVersion != JournalVersion || entry.Tool != "rotating" || entry.Detail != payload {
				t.Fatalf("query returned a partial entry during rotation: %+v", entry)
			}
		}
		time.Sleep(time.Millisecond)
	}
}

func TestJournalSkipsUnsupportedFormatVersions(t *testing.T) {
	path := journalPathForTest(t)
	lines := []string{
		`{"format_version":0,"timestamp":"2026-01-01T10:00:00Z","command":"install","tool":"zero","result":"success"}`,
		`{"format_version":1,"timestamp":"2026-01-02T10:00:00Z","command":"install","tool":"one","result":"success"}`,
		`{"format_version":2,"timestamp":"2026-01-03T10:00:00Z","command":"install","tool":"future","result":"success"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	entries, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 || entries[0].Tool != "one" {
		t.Fatalf("entries = %+v, want only the v1 record", entries)
	}
}

func TestQueryJournalMissingFileReturnsEmptyWithoutCreatingStateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "journal.jsonl")
	entries, err := QueryJournal(path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none", entries)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("query created %s: %v", dir, err)
	}
}

func TestSchemaHashIsStableAndTracksDefinitions(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"b": {Name: "b"},
		"a": {Name: "a"},
	}}
	first := SchemaHash(schema)
	if first == "" || !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("hash = %q", first)
	}
	if again := SchemaHash(schema); again != first {
		t.Fatalf("hash not stable: %q != %q", again, first)
	}
	schema.Tools["a"].Tags = []string{"cli"}
	if changed := SchemaHash(schema); changed == first {
		t.Fatal("hash did not change with the tool definition")
	}
	if SchemaHash(nil) != "" {
		t.Fatal("nil schema must hash to empty")
	}
}
