package plan

import "testing"

func TestValidateCredentialFreeReference(t *testing.T) {
	accepted := []string{
		"conda-forge",
		"https://github.com/org/repo.git",
		"git://github.com/org/repo.git",
		"ssh://git@github.com/org/repo.git",
		"git+ssh://git@github.com/org/repo.git",
		"git@github.com:org/repo.git",
		"localhost:5000/team/app",
		"/srv/repo.git",
		`C:\repos\x`,
		"https://{os}.example.com/{distro_family}/{pkg}.tar.gz",
	}
	for _, raw := range accepted {
		if err := validateCredentialFreeReference(raw); err != nil {
			t.Errorf("validateCredentialFreeReference(%q) = %v, want nil", raw, err)
		}
	}
	rejected := []string{
		"https://user:secret@example.test/repo.git",
		"https://ghp_token@example.test/repo.git",
		"ssh://git:secret@example.test/repo.git",
		"git+https://token@example.test/repo.git",
		"https://example.test/a.tar.gz?token=abc",
		"https://example.test/a.tar.gz?X-Amz-Signature=abc",
		" https://example.test/repo.git",
		"https://{token}@example.test/repo.git",
		// Host-less references parse with u.Host == "" but can still carry
		// userinfo or sensitive query material.
		"A://:@",
		"a://user:pass@/x",
		"file:///srv/key.gpg?token=abc",
		"/srv/repo.git#access_token=abc",
	}
	for _, raw := range rejected {
		if err := validateCredentialFreeReference(raw); err == nil {
			t.Errorf("validateCredentialFreeReference(%q) = nil, want error", raw)
		}
	}
}

func TestSourceRejectsSensitiveURLFragment(t *testing.T) {
	s := SourceReference{Role: SourceRegistry, URL: "https://example.test/index#access_token=secret"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected sensitive URL fragment to be rejected")
	}
}

func TestSourceRejectsSensitiveFragmentWithSemicolonOrEscapedKey(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/index#section=install;access_token=secret",
		"https://example.test/index#access%5ftoken=secret",
	} {
		s := SourceReference{Role: SourceRegistry, URL: raw}
		if err := s.Validate(); err == nil {
			t.Fatalf("expected sensitive fragment %q to be rejected", raw)
		}
	}
}

func TestSanitizeURLFragmentPreservesBenignFragmentSpelling(t *testing.T) {
	for _, raw := range []string{"v1.2.3", "section=install", "a=1&b=2"} {
		if got := sanitizeURLFragment(raw); got != raw {
			t.Fatalf("sanitizeURLFragment(%q) = %q", raw, got)
		}
	}
}
