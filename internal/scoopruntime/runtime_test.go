package scoopruntime

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestOfficialObservesInstalledPackageSemantically(t *testing.T) {
	const inventory = "Installed apps:\nName Version Source Updated Info\n---- ------- ------ ------- ----\nNeovim 0.10.4 extras 2026-09-26 11:58:42 Global install\nOther 1.0 main 2026-09-26 11:58:42\n"
	for _, tc := range []struct {
		name  string
		scope string
		want  InstalledPackage
	}{
		{name: "global match", scope: "global", want: InstalledPackage{Present: true, Version: "0.10.4", Bucket: "extras", Scope: "global"}},
		{name: "different scope is absent", scope: "user"},
		{name: "all scopes", want: InstalledPackage{Present: true, Version: "0.10.4", Bucket: "extras", Scope: "global"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &run.FakeRunner{Stdout: inventory}
			got, err := NewOfficial().ObserveInstalled(context.Background(), runner, "neovim", tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("observation = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(runner.Calls[0].Args, []string{"list"}) {
				t.Fatalf("inventory args = %v, want full unscoped list", runner.Calls[0].Args)
			}
		})
	}
}

func TestOfficialRecognizesScoopListTitleOnlyNoMatch(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "Installed apps matching 'neovim':\nName Version Source Updated Info\n---- ------- ------ ------- ----\n"}
	got, err := NewOfficial().ObserveInstalled(context.Background(), runner, "neovim", "user")
	if err != nil || got.Present {
		t.Fatalf("title-only no-match = %+v, %v; want absent", got, err)
	}
}

func TestOfficialAcceptsOnlyDocumentedEmptyInventoryFailure(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 1, Stdout: "There aren't any apps installed.\n"}
	got, err := NewOfficial().ObserveInstalled(context.Background(), runner, "neovim", "user")
	if err != nil || got.Present {
		t.Fatalf("empty inventory = %+v, %v; want absent", got, err)
	}
	runner = &run.FakeRunner{ExitCode: 1, Stderr: "access denied"}
	if _, err := NewOfficial().ObserveInstalled(context.Background(), runner, "neovim", "user"); err == nil {
		t.Fatal("arbitrary exit 1 must not establish absence")
	}
}

func TestOfficialRejectsMalformedUnrelatedInstalledRow(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "Installed apps:\nName Version Source Updated Info\n---- ------- ------ ------- ----\nunknown row\n"}
	if _, err := NewOfficial().ObserveInstalled(context.Background(), runner, "neovim", "user"); err == nil {
		t.Fatal("malformed unrelated row must not establish complete inventory absence")
	}
}

func TestOfficialBucketListReturnsSemanticBuckets(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "Name Source Updated\nmain https://github.com/ScoopInstaller/Main 2026-09-26\nlegacy\nextras https://github.com/ScoopInstaller/Extras 2026-09-26\n"}
	got, err := NewOfficial().BucketList(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	want := []Bucket{{Name: "main", Location: "https://github.com/ScoopInstaller/Main"}, {Name: "legacy"}, {Name: "extras", Location: "https://github.com/ScoopInstaller/Extras"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buckets = %+v, want %+v", got, want)
	}
}

func TestOfficialBucketRepositoryInterpretsCorePrefix(t *testing.T) {
	root := t.TempDir()
	runner := &run.FakeRunner{Stdout: filepath.Join(root, "apps", "scoop", "current")}
	got, err := NewOfficial().BucketRepository(context.Background(), runner, "corp-tools")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "buckets", "corp-tools"); got != want {
		t.Fatalf("repository = %q, want %q", got, want)
	}
}

func TestOfficialBucketRepositoryRejectsMalformedCorePrefix(t *testing.T) {
	runner := &run.FakeRunner{Stdout: "/tmp/not-apps/scoop/current"}
	if _, err := NewOfficial().BucketRepository(context.Background(), runner, "corp-tools"); err == nil {
		t.Fatal("unexpected core prefix accepted")
	}
}

func TestMutationErrorsDoNotRetry(t *testing.T) {
	runner := &run.FakeRunner{ExitCode: 1, Stderr: "Scoop failed"}
	err := NewOfficial().InstallResolved(context.Background(), runner, InstallTarget{Package: "neovim"})
	if err == nil || !strings.Contains(err.Error(), "Scoop failed") {
		t.Fatalf("mutation error = %v", err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("mutation attempts = %d, want 1", len(runner.Calls))
	}
}

func TestCapabilityGatesRequireOnlyRequestedFeatures(t *testing.T) {
	full := Capabilities{ExactVersion: true, BucketSelection: true, Scope: true, Architecture: true, Removal: true, BucketRevisionLocation: true}
	target := InstallTarget{Package: "neovim", Version: "0.10.4", Bucket: "extras", Scope: "global", Architecture: "arm64"}
	if err := CheckInstallCapabilities(full, target); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		caps    Capabilities
		target  InstallTarget
		removal bool
	}{
		{name: "exact version", caps: Capabilities{BucketSelection: true, Scope: true, Architecture: true}, target: target},
		{name: "bucket", caps: Capabilities{ExactVersion: true, Scope: true, Architecture: true}, target: target},
		{name: "scope", caps: Capabilities{ExactVersion: true, BucketSelection: true, Architecture: true}, target: target},
		{name: "architecture", caps: Capabilities{ExactVersion: true, BucketSelection: true, Scope: true}, target: target},
		{name: "removal", caps: full, target: InstallTarget{Package: "neovim"}, removal: true},
	}
	cases[len(cases)-1].caps.Removal = false
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.removal {
				err = CheckRemoveCapabilities(tc.caps, tc.target)
			} else {
				err = CheckInstallCapabilities(tc.caps, tc.target)
			}
			if err == nil {
				t.Fatal("unsupported requested capability was accepted")
			}
		})
	}
	if err := CheckBucketAddCapabilities(Capabilities{BucketSelection: true}, "https://example.test/tools", false); err == nil {
		t.Fatal("custom location accepted without revision/location capability")
	}
	if err := CheckBucketAddCapabilities(Capabilities{BucketRevisionLocation: true}, "https://example.test/tools", false); err == nil {
		t.Fatal("custom location accepted without bucket selection")
	}
	if err := CheckBucketAddCapabilities(Capabilities{}, "", true); err == nil {
		t.Fatal("revision verification accepted without location capability")
	}
}
