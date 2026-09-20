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
	}
	for _, raw := range rejected {
		if err := validateCredentialFreeReference(raw); err == nil {
			t.Errorf("validateCredentialFreeReference(%q) = nil, want error", raw)
		}
	}
}
