package source

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

type scriptedRunner struct {
	calls   []run.FakeCall
	outputs []run.Result
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: args})
	result := r.outputs[0]
	r.outputs = r.outputs[1:]
	return result
}

func TestManagerAddsPPAAndUpdatesOnce(t *testing.T) {
	run.OverrideElevation("sudo")
	t.Cleanup(func() { run.OverrideElevation("") })
	runner := &scriptedRunner{outputs: []run.Result{{}, {}, {}, {Stdout: []byte("https://ppa.launchpadcontent.net/neovim-ppa/stable/ubuntu")}}}
	manager := NewManager(runner, false)
	source := config.Source{Kind: "apt-ppa", Name: "ppa:neovim-ppa/stable"}
	if _, err := manager.Ensure(context.Background(), []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if runner.calls[1].Name != "sudo" || runner.calls[2].Name != "sudo" {
		t.Fatalf("commands = %v", runner.calls)
	}
}

func TestManagerDryRunDoesNotMutate(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}}}
	missing, err := NewManager(runner, true).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || len(runner.calls) != 1 {
		t.Fatalf("missing=%v calls=%v", missing, runner.calls)
	}
}

func TestManagerDryRunMutationBoundaryBlocksAdd(t *testing.T) {
	runner := &scriptedRunner{}
	manager := NewManager(runner, true)
	err := manager.add(context.Background(), config.Source{Kind: "brew-tap", Name: "user/tap"})
	if err == nil {
		t.Fatal("expected dry-run mutation boundary to reject source add")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("underlying runner must not be reached, calls=%v", runner.calls)
	}
}

func TestManagerRejectsUnsupportedSourceKind(t *testing.T) {
	runner := &scriptedRunner{}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "unknown", Name: "x"}})
	if err == nil {
		t.Fatal("expected unsupported source kind error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
}

func TestManagerRejectsIgnoredURLBeforeRunnerCall(t *testing.T) {
	for _, kind := range []string{"apt-ppa", "dnf-copr"} {
		t.Run(kind, func(t *testing.T) {
			configured := config.Source{Kind: kind, Name: "vendor/tools", URL: "https://example.test/tools"}
			for _, call := range []struct {
				name string
				run  func(*Manager) error
			}{
				{"Missing", func(m *Manager) error {
					_, err := m.Missing(context.Background(), []config.Source{configured})
					return err
				}},
				{"Add", func(m *Manager) error { return m.Add(context.Background(), configured) }},
			} {
				t.Run(call.name, func(t *testing.T) {
					runner := &scriptedRunner{}
					err := call.run(NewManager(runner, false))
					if err == nil || !strings.Contains(err.Error(), "URL is unsupported") {
						t.Fatalf("%s() = %v, want unsupported URL", call.name, err)
					}
					if len(runner.calls) != 0 {
						t.Fatalf("%s() reached Runner: %v", call.name, runner.calls)
					}
				})
			}
		})
	}
}

func TestManagerRejectsUnsafeGitSourceURLBeforeRunnerCall(t *testing.T) {
	for _, rawURL := range []string{
		"https://user@example.test/tools.git",
		"https://example.test/tools.git?token=secret",
		"https://example.test/tools.git#fragment",
	} {
		t.Run(rawURL, func(t *testing.T) {
			runner := &scriptedRunner{}
			configured := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: rawURL}
			_, err := NewManager(runner, false).Missing(context.Background(), []config.Source{configured})
			if err == nil || !strings.Contains(err.Error(), "credential-free") {
				t.Fatalf("Missing() error = %v, want unsafe source URL rejection", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe source URL reached Runner: %#v", runner.calls)
			}
		})
	}
}

func TestManagerPassesSupportedSourceURLToAdd(t *testing.T) {
	for _, kind := range []string{"scoop-bucket", "brew-tap"} {
		t.Run(kind, func(t *testing.T) {
			configured := config.Source{Kind: kind, Name: "vendor/tools", URL: "https://example.test/tools"}
			runner := &scriptedRunner{outputs: []run.Result{{}, {}}}
			if _, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{configured}); err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("calls = %v, want probe and add", runner.calls)
			}
			args := runner.calls[1].Args
			if len(args) == 0 || args[len(args)-1] != configured.URL {
				t.Fatalf("add args = %v, want source URL as final argument", args)
			}
		})
	}
}

func TestManagerVerifiesExistingGitBackedSourceOrigin(t *testing.T) {
	t.Run("brew tap", func(t *testing.T) {
		configured := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"}
		const revision = "0123456789012345678901234567890123456789"
		runner := &scriptedRunner{outputs: []run.Result{
			{Stdout: []byte("vendor/tools\n")},
			{Stdout: []byte(`[{"name":"vendor/tools","remote":"https://EXAMPLE.test/vendor/tools.git/"}]`)},
			{Stdout: []byte("/brew/taps/vendor/homebrew-tools\n")},
			{Stdout: []byte(revision + "\n")},
		}}
		manager := NewManager(runner, false)
		missing, err := manager.Missing(context.Background(), []config.Source{configured})
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 0 {
			t.Fatalf("missing = %v, want source verified present", missing)
		}
		if got := manager.SourceRevisions(); !reflect.DeepEqual(got, []SourceRevision{{Kind: "brew-tap", Name: "vendor/tools", Revision: revision}}) {
			t.Fatalf("SourceRevisions() = %#v", got)
		}
	})

	t.Run("scoop bucket", func(t *testing.T) {
		configured := config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor/bucket.git"}
		const revision = "1123456789012345678901234567890123456789"
		root := filepath.Join(t.TempDir(), "scoop-root")
		runner := &scriptedRunner{outputs: []run.Result{
			{Stdout: []byte("Name Source Updated Manifests\nvendor https://EXAMPLE.test/vendor/bucket.git/ 2026-09-26 42\n")},
			{Stdout: []byte(filepath.Join(root, "apps", "scoop", "current") + "\n")},
			{Stdout: []byte(revision + "\n")},
		}}
		manager := NewManager(runner, false)
		missing, err := manager.Missing(context.Background(), []config.Source{configured})
		if err != nil {
			t.Fatal(err)
		}
		if len(missing) != 0 {
			t.Fatalf("missing = %v, want source verified present", missing)
		}
		if got := manager.SourceRevisions(); !reflect.DeepEqual(got, []SourceRevision{{Kind: "scoop-bucket", Name: "vendor", Revision: revision}}) {
			t.Fatalf("SourceRevisions() = %#v", got)
		}
	})
}
func TestManagerCapturesRevisionAfterAddingExplicitGitSource(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"}
	runner := &scriptedRunner{outputs: []run.Result{
		{}, // brew tap <name> <url>
		{Stdout: []byte("/brew/taps/vendor/homebrew-tools\n")},
		{Stdout: []byte(revision + "\n")},
	}}
	manager := NewManager(runner, false)
	if err := manager.Add(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if got := manager.SourceRevisions(); !reflect.DeepEqual(got, []SourceRevision{{Kind: "brew-tap", Name: "vendor/tools", Revision: revision}}) {
		t.Fatalf("SourceRevisions() = %#v", got)
	}
}
func TestManagerRejectsExistingSourceWithDifferentOrigin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  config.Source
		outputs []run.Result
	}{
		{
			name:   "brew tap",
			source: config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"},
			outputs: []run.Result{
				{Stdout: []byte("vendor/tools\n")},
				{Stdout: []byte(`[{"name":"vendor/tools","remote":"https://mirror.test/vendor/tools.git"}]`)},
			},
		},
		{
			name:    "scoop bucket",
			source:  config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor/bucket.git"},
			outputs: []run.Result{{Stdout: []byte("vendor https://mirror.test/vendor/bucket.git 2026-09-26 42\n")}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRunner{outputs: tc.outputs}
			_, err := NewManager(runner, false).Missing(context.Background(), []config.Source{tc.source})
			if err == nil || !strings.Contains(err.Error(), "different origin") {
				t.Fatalf("Missing() error = %v, want origin mismatch", err)
			}
		})
	}
}

func TestManagerMatchesGitBackedSourceNamesExactly(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		name    string
		output  string
		present bool
	}{
		{kind: "brew-tap", name: "vendor/tools", output: "vendor/tools\n", present: true},
		{kind: "brew-tap", name: "tools", output: "vendor/tools\n", present: false},
		{kind: "brew-tap", name: "vendor/tools", output: "vendor/tools-extra\n", present: false},
		{kind: "scoop-bucket", name: "vendor", output: "Name Source Updated\nvendor https://example.test/vendor 2026-09-26\n", present: true},
		{kind: "scoop-bucket", name: "vendor", output: "Name Source Updated\nvendor-tools https://example.test/vendor 2026-09-26\n", present: false},
	} {
		t.Run(tc.kind+"/"+tc.name+"/"+tc.output, func(t *testing.T) {
			runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte(tc.output)}}}
			present, err := NewManager(runner, false).Present(context.Background(), config.Source{Kind: tc.kind, Name: tc.name})
			if err != nil {
				t.Fatal(err)
			}
			if present != tc.present {
				t.Fatalf("Present() = %t, want %t", present, tc.present)
			}
		})
	}
}

func TestCanonicalSourceURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "trim whitespace", raw: "  owner/repo  ", want: "owner/repo"},
		{name: "bare reference trailing slash", raw: "owner/repo/", want: "owner/repo"},
		{name: "scheme-less host reference", raw: "github.com/Owner/Repo", want: "github.com/Owner/Repo"},
		{name: "host case and trailing slash", raw: "HTTPS://GITHUB.COM/Owner/Repo/", want: "https://github.com/Owner/Repo"},
		{name: "github git suffix", raw: "https://github.com/Owner/Repo.git", want: "https://github.com/Owner/Repo"},
		{name: "gitlab git suffix", raw: "https://GITLAB.COM/group/repo.git/", want: "https://gitlab.com/group/repo"},
		{name: "codeberg git suffix", raw: "https://CODEBERG.ORG/group/repo.git", want: "https://codeberg.org/group/repo"},
		{name: "other host retains git suffix", raw: "https://example.test/group/repo.git/", want: "https://example.test/group/repo.git"},
		{name: "username userinfo preserved", raw: "https://User@GITHUB.COM/Owner/Repo.git", want: "https://User@github.com/Owner/Repo"},
		{name: "default port preserved", raw: "https://github.com:443/Owner/Repo.git", want: "https://github.com:443/Owner/Repo"},
		{name: "non-default port preserved", raw: "https://github.com:8443/Owner/Repo.git", want: "https://github.com:8443/Owner/Repo"},
		{name: "query and fragment preserved", raw: "https://github.com/Owner/Repo.git/?ref=main#section", want: "https://github.com/Owner/Repo?ref=main#section"},
		{name: "subpath preserved", raw: "https://github.com/Owner/Repo/releases/download/v1/file", want: "https://github.com/Owner/Repo/releases/download/v1/file"},
		{name: "scp reference unchanged apart from trailing slash", raw: "git@github.com:Owner/Repo.git/", want: "git@github.com:Owner/Repo.git"},
		{name: "malformed URL fallback", raw: "https://[::1/path/", want: "https://[::1/path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalSourceURL(tc.raw); got != tc.want {
				t.Fatalf("canonicalSourceURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSameSourceURLComparisonSemantics(t *testing.T) {
	for _, tc := range []struct {
		name  string
		left  string
		right string
		same  bool
	}{
		{name: "non-git host keeps git suffix significant", left: "https://EXAMPLE.test/vendor/tools.git/", right: "https://example.TEST/vendor/tools", same: false},
		{name: "github git suffix", left: "https://github.com/vendor/tools.git", right: "https://github.com/vendor/tools", same: true},
		{name: "gitlab git suffix", left: "https://gitlab.com/vendor/tools.git", right: "https://gitlab.com/vendor/tools", same: true},
		{name: "codeberg git suffix", left: "https://codeberg.org/vendor/tools.git", right: "https://codeberg.org/vendor/tools", same: true},
		{name: "scp trailing slash", left: "git@github.com:vendor/tools.git/", right: "git@github.com:vendor/tools.git", same: true},
		{name: "username userinfo remains significant", left: "https://user@example.test/vendor/tools", right: "https://example.test/vendor/tools", same: false},
		{name: "port remains significant", left: "https://example.test:443/vendor/tools", right: "https://example.test/vendor/tools", same: false},
		{name: "query remains significant", left: "https://example.test/vendor/tools?ref=main", right: "https://example.test/vendor/tools", same: false},
		{name: "fragment remains significant", left: "https://example.test/vendor/tools#main", right: "https://example.test/vendor/tools", same: false},
		{name: "subpath remains significant", left: "https://example.test/vendor/tools/releases", right: "https://example.test/vendor/tools", same: false},
		{name: "invalid inputs compare by fallback string", left: "https://[::1/path/", right: "https://[::1/path", same: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSourceURL(tc.left, tc.right); got != tc.same {
				t.Fatalf("sameSourceURL(%q, %q) = %t, want %t", tc.left, tc.right, got, tc.same)
			}
		})
	}
}

func TestManagerPropagatesCheckFailure(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Err: context.DeadlineExceeded, ExitCode: 1}}}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err == nil {
		t.Fatal("expected source check failure")
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != "brew" {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestManagerPropagatesAddFailure(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: nil}, {Err: context.DeadlineExceeded, ExitCode: 1}}}
	_, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{{Kind: "brew-tap", Name: "user/tap"}})
	if err == nil {
		t.Fatal("expected source add failure")
	}
	if len(runner.calls) != 2 || runner.calls[1].Name != "brew" {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestEnsureTrackedReportsOnlyConfirmedAdds(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}, {}}}
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != source {
		t.Fatalf("missing=%v want [%v]", result.Missing, source)
	}
	if len(result.Added) != 1 || result.Added[0] != source {
		t.Fatalf("added=%v want [%v]", result.Added, source)
	}
}

func TestEnsureTrackedDoesNotClaimFailedAdd(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{}, {Err: context.DeadlineExceeded, ExitCode: 1}}}
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil {
		t.Fatal("expected add failure")
	}
	if len(result.Missing) != 1 {
		t.Fatalf("missing=%v want source observed missing", result.Missing)
	}
	if len(result.Added) != 0 {
		t.Fatalf("added=%v; failed add must not be reported as confirmed", result.Added)
	}
	if result.Unconfirmed == nil || *result.Unconfirmed != source {
		t.Fatalf("unconfirmed=%v want %v", result.Unconfirmed, source)
	}
}

func TestManagerRemoveBrewTap(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte("vendor/tools\n")}, {}}}
	manager := NewManager(runner, false)
	if err := manager.Remove(context.Background(), []config.Source{{Kind: "brew-tap", Name: "vendor/tools"}}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%v want probe + removal", runner.calls)
	}
	if got := runner.calls[1]; got.Name != "brew" || len(got.Args) != 2 || got.Args[0] != "untap" || got.Args[1] != "vendor/tools" {
		t.Fatalf("remove call=%v", got)
	}
}

func TestManagerRemovePPARefreshesIndex(t *testing.T) {
	run.OverrideElevation("sudo")
	t.Cleanup(func() { run.OverrideElevation("") })
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("https://ppa.launchpadcontent.net/vendor/stable/ubuntu\n")},
		{},
		{},
	}}
	manager := NewManager(runner, false)
	if err := manager.Remove(context.Background(), []config.Source{{Kind: "apt-ppa", Name: "ppa:vendor/stable"}}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls=%v want probe + remove + refresh", runner.calls)
	}
	remove := runner.calls[1]
	if remove.Name != "sudo" || len(remove.Args) < 5 || remove.Args[0] != "add-apt-repository" || remove.Args[2] != "--remove" {
		t.Fatalf("remove call=%v", remove)
	}
	refresh := runner.calls[2]
	if refresh.Name != "sudo" || len(refresh.Args) != 2 || refresh.Args[0] != "apt-get" || refresh.Args[1] != "update" {
		t.Fatalf("refresh call=%v", refresh)
	}
}

func TestBrewTapRevisionVerification(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	for _, tc := range []struct {
		name    string
		commit  string
		wantErr bool
	}{
		{name: "match", commit: revision},
		{name: "mismatch", commit: "1123456789012345678901234567890123456789", wantErr: true},
		{name: "malformed", commit: "not-a-commit", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scriptedRunner{outputs: []run.Result{
				{Stdout: []byte("vendor/tools\n")},
				{Stdout: []byte(`[{"name":"vendor/tools","remote":"https://example.test/vendor/tools.git"}]`)},
				{Stdout: []byte("/opt/homebrew/Library/Taps/vendor/homebrew-tools\n")},
				{Stdout: []byte(tc.commit + "\n")},
			}}
			source := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", Revision: revision}
			present, err := NewManager(runner, false).Present(context.Background(), source)
			if (err != nil) != tc.wantErr || present != !tc.wantErr {
				t.Fatalf("Present() = (%t, %v), want present=%t error=%t", present, err, !tc.wantErr, tc.wantErr)
			}
			if len(runner.calls) != 4 {
				t.Fatalf("calls = %#v, want tap list, origin, repo path, and commit probes", runner.calls)
			}
			if got := runner.calls[2]; got.Name != "brew" || !reflect.DeepEqual(got.Args, []string{"--repo", source.Name}) {
				t.Fatalf("repo command = %#v", got)
			}
			if got := runner.calls[3]; got.Name != "git" || !reflect.DeepEqual(got.Args, []string{"-C", "/opt/homebrew/Library/Taps/vendor/homebrew-tools", "rev-parse", "--verify", "HEAD^{commit}"}) {
				t.Fatalf("revision command = %#v", got)
			}
		})
	}
}

func TestBrewTapRevisionVerifiesAfterAdd(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("\n")},
		{},
		{Stdout: []byte("/opt/homebrew/Library/Taps/vendor/homebrew-tools\n")},
		{Stdout: []byte(revision + "\n")},
	}}
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", Revision: revision}
	if _, err := NewManager(runner, false).Ensure(context.Background(), []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 || runner.calls[1].Name != "brew" || !reflect.DeepEqual(runner.calls[1].Args, []string{"tap", source.Name, source.URL}) {
		t.Fatalf("calls = %#v, want missing probe, add, path, and revision verification", runner.calls)
	}
}

func TestBrewTapRevisionMismatchRollsBackNewTap(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", Revision: revision}
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("\n")},
		{},
		{Stdout: []byte("/opt/homebrew/Library/Taps/vendor/homebrew-tools\n")},
		{Stdout: []byte("1123456789012345678901234567890123456789\n")},
		{},
	}}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") {
		t.Fatalf("EnsureTracked() error = %v, want revision mismatch", err)
	}
	if len(result.Added) != 0 || result.Unconfirmed == nil || *result.Unconfirmed != source {
		t.Fatalf("EnsureTracked() result = %+v, mismatched add must stay unconfirmed", result)
	}
	if len(runner.calls) != 5 {
		t.Fatalf("calls = %#v, want probe, add, path, revision, rollback", runner.calls)
	}
	if got := runner.calls[4]; got.Name != "brew" || !reflect.DeepEqual(got.Args, []string{"untap", source.Name}) {
		t.Fatalf("rollback command = %#v, want brew untap", got)
	}
}

func TestBrewTapRevisionRollbackFailureIsJoined(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	source := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git", Revision: revision}
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("\n")},
		{},
		{Stdout: []byte("/opt/homebrew/Library/Taps/vendor/homebrew-tools\n")},
		{Stdout: []byte("1123456789012345678901234567890123456789\n")},
		{Err: context.DeadlineExceeded, ExitCode: 1},
	}}
	_, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") || !strings.Contains(err.Error(), "remove newly added mismatched source") {
		t.Fatalf("EnsureTracked() error = %v, want mismatch plus rollback failure", err)
	}
}

func TestScoopBucketRevisionVerification(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	for _, tc := range []struct {
		name    string
		commit  string
		wantErr bool
	}{
		{name: "match", commit: revision},
		{name: "mismatch", commit: "1123456789012345678901234567890123456789", wantErr: true},
		{name: "malformed", commit: "not-a-commit", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "scoop-root")
			corePrefix := filepath.Join(root, "apps", "scoop", "current")
			runner := &scriptedRunner{outputs: []run.Result{
				{Stdout: []byte("corp-tools https://example.test/tools.git\n")},
				{Stdout: []byte(corePrefix + "\n")},
				{Stdout: []byte(tc.commit + "\n")},
			}}
			source := config.Source{Kind: "scoop-bucket", Name: "corp-tools", URL: "https://example.test/tools.git", Revision: revision}
			present, err := NewManager(runner, false).Present(context.Background(), source)
			if (err != nil) != tc.wantErr || present != !tc.wantErr {
				t.Fatalf("Present() = (%t, %v), want present=%t error=%t", present, err, !tc.wantErr, tc.wantErr)
			}
			if len(runner.calls) != 3 {
				t.Fatalf("calls = %#v, want bucket list, core prefix, and commit probes", runner.calls)
			}
			if got := runner.calls[1]; got.Name != "scoop" || !reflect.DeepEqual(got.Args, []string{"prefix", "scoop"}) {
				t.Fatalf("prefix command = %#v", got)
			}
			wantPath := filepath.Join(root, "buckets", source.Name)
			if got := runner.calls[2]; got.Name != "git" || !reflect.DeepEqual(got.Args, []string{"-C", wantPath, "rev-parse", "--verify", "HEAD^{commit}"}) {
				t.Fatalf("revision command = %#v", got)
			}
		})
	}
}

func TestScoopBucketRevisionAcceptsNoJunctionCorePrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "portable-scoop")
	prefix := filepath.Join(root, "apps", "scoop", "2026.09.29")
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte(prefix + "\n")}}}
	got, err := NewManager(runner, false).scoopBucketRepository(context.Background(), "corp-tools")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "buckets", "corp-tools"); got != want {
		t.Fatalf("repository = %q, want %q", got, want)
	}
}

func TestScoopBucketRevisionRejectsUnexpectedCorePrefix(t *testing.T) {
	root := t.TempDir()
	badPrefix := filepath.Join(root, "not-apps", "scoop", "current")
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte(badPrefix + "\n")}}}
	if _, err := NewManager(runner, false).scoopBucketRepository(context.Background(), "corp-tools"); err == nil || !strings.Contains(err.Error(), "unexpected core prefix") {
		t.Fatalf("scoopBucketRepository() error = %v, want unexpected prefix rejection", err)
	}
}

func TestScoopBucketRevisionMismatchAfterAddRollsBack(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	root := filepath.Join(t.TempDir(), "scoop-root")
	corePrefix := filepath.Join(root, "apps", "scoop", "current")
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("\n")},
		{},
		{Stdout: []byte(corePrefix + "\n")},
		{Stdout: []byte("1123456789012345678901234567890123456789\n")},
		{},
	}}
	source := config.Source{Kind: "scoop-bucket", Name: "corp-tools", URL: "https://example.test/tools.git", Revision: revision}
	result, err := NewManager(runner, false).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") {
		t.Fatalf("EnsureTracked() error = %v, want revision mismatch", err)
	}
	if len(result.Added) != 0 || result.Unconfirmed == nil || *result.Unconfirmed != source {
		t.Fatalf("EnsureTracked() result = %+v, mismatched add must stay unconfirmed", result)
	}
	if len(runner.calls) != 5 {
		t.Fatalf("calls = %#v, want probe, add, core prefix, revision check, and rollback", runner.calls)
	}
	if got := runner.calls[4]; got.Name != "scoop" || !reflect.DeepEqual(got.Args, []string{"bucket", "rm", source.Name}) {
		t.Fatalf("rollback command = %#v, want scoop bucket rm", got)
	}
}
