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
	all, err := QueryJournal(context.Background(), path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("entries = %d, want 3", len(all))
	}
	if all[0].Command != "install" || all[2].Command != "remove" {
		t.Fatalf("entries are not chronological: %+v", all)
	}

	byTool, err := QueryJournal(context.Background(), path, JournalFilter{Tool: "jq"})
	if err != nil || len(byTool) != 2 {
		t.Fatalf("tool filter = %d entries, err=%v", len(byTool), err)
	}
	failed, err := QueryJournal(context.Background(), path, JournalFilter{Result: JournalFailed})
	if err != nil || len(failed) != 1 || failed[0].Tool != "rg" {
		t.Fatalf("result filter = %+v, err=%v", failed, err)
	}
	since, _ := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	windowed, err := QueryJournal(context.Background(), path, JournalFilter{Since: since})
	if err != nil || len(windowed) != 2 {
		t.Fatalf("since filter = %d entries, err=%v", len(windowed), err)
	}
	limited, err := QueryJournal(context.Background(), path, JournalFilter{Limit: 1})
	if err != nil || len(limited) != 1 || limited[0].Tool != "jq" || limited[0].Command != "remove" {
		t.Fatalf("limit filter kept the wrong entry: %+v, err=%v", limited, err)
	}
}

func TestJournalRedactsSecretsAndSkipsMalformedLines(t *testing.T) {
	path := journalPathForTest(t)
	entry := fixedJournalEntry("install", "jq", JournalFailed, "2026-01-01T10:00:00Z")
	entry.Detail = "clone https://user:token-secret@example.test/repo failed"
	if err := AppendJournalTo(context.Background(), path, entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json}\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	entries, err := QueryJournal(context.Background(), path, JournalFilter{})
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
	entries, err := QueryJournal(context.Background(), path, JournalFilter{})
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
	entries, err := QueryJournal(context.Background(), path, JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != workers {
		t.Fatalf("entries = %d, want %d", len(entries), workers)
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
