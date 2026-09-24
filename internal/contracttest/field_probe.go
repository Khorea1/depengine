// Package contracttest contains reusable fixtures for behavioral conformance
// tests derived from methodkind.Contracts. It deliberately owns only test
// stimuli; production field metadata remains in methodkind.
package contracttest

import "maps"

// FieldPair returns two valid values for a field. Exact kind.field fixtures
// win over shared field-name fixtures when an adapter has narrower semantics.
func FieldPair(kind, field string) ([2]any, bool) {
	if pair, ok := fieldPairs[kind+"."+field]; ok {
		return pair, true
	}
	pair, ok := fieldPairs[field]
	return pair, ok
}

// BaseConfig returns the minimal valid config for probing kind.field. Some
// fields require companion configuration; those use an exact base override.
func BaseConfig(kind, field string) (map[string]any, bool) {
	key := kind
	if override, ok := baseOverrides[kind+"."+field]; ok {
		key = override
	}
	base, ok := baseConfigs[key]
	if !ok {
		return nil, false
	}
	return maps.Clone(base), true
}

var fieldPairs = map[string][2]any{
	"url":          {"https://example.test/a.tar.gz", "https://example.test/b.tar.gz"},
	"git.url":      {"https://example.test/a.git", "https://example.test/b.git"},
	"appimage.url": {"https://example.test/a.AppImage", "https://example.test/b.AppImage"},
	"android.url":  {"https://example.test/a.apk", "https://example.test/b.apk"},
	"cargo.git":    {"https://example.test/a.git", "https://example.test/b.git"},
	"repo":         {"org/a", "org/b"},
	"asset":        {"a.tar.gz", "b.tar.gz"},
	"branch":       {"edge-a", "edge-b"},
	"tag":          {"tag-a", "tag-b"},
	"rev":          {"rev-a", "rev-b"},
	"registry":     {"reg-a", "reg-b"},
	"source":       {"src-a", "src-b"},
	"index_url":    {"https://index-a.example/simple", "https://index-b.example/simple"},
	"index":        {"https://index-a.example/simple", "https://index-b.example/simple"},
	"channels":     {[]any{"chan-a"}, []any{"chan-b"}},
	"local_path":   {"vendor/a.tar.gz", "vendor/b.tar.gz"},
	"checksum": {
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"sha256:ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb",
	},
	"scope": {"user", "system"},
	"secret_ref": {
		map[string]any{"provider": "env", "name": "TOKEN_A"},
		map[string]any{"provider": "env", "name": "TOKEN_B"},
	},
	"checksum_secret_ref": {
		map[string]any{"provider": "env", "name": "CHECKSUM_TOKEN_A"},
		map[string]any{"provider": "env", "name": "CHECKSUM_TOKEN_B"},
	},
	"signature_secret_ref": {
		map[string]any{"provider": "env", "name": "SIGNATURE_TOKEN_A"},
		map[string]any{"provider": "env", "name": "SIGNATURE_TOKEN_B"},
	},
	"cargo.branch": {"edge-a", "edge-b"},
	"cargo.tag":    {"tag-a", "tag-b"},
	"cargo.rev":    {"rev-a", "rev-b"},
}

var baseOverrides = map[string]string{
	"cargo.branch":   "cargo+git",
	"cargo.tag":      "cargo+git",
	"cargo.rev":      "cargo+git",
	"http.asset":     "http+repo",
	"msi.asset":      "msi+repo",
	"appimage.asset": "appimage+repo",
	"android.asset":  "android+repo",
}

var baseConfigs = map[string]map[string]any{
	"native":        {"pkg": "demo"},
	"winget":        {"pkg": "demo"},
	"cargo":         {"pkg": "demo"},
	"cargo+git":     {"pkg": "demo", "git": "https://example.test/demo.git"},
	"pipx":          {"pkg": "demo"},
	"uv":            {"pkg": "demo"},
	"pip":           {"pkg": "demo"},
	"npm":           {"pkg": "demo"},
	"bun":           {"pkg": "demo"},
	"gem":           {"pkg": "demo"},
	"conda":         {"pkg": "demo"},
	"git":           {"url": "https://example.test/demo.git"},
	"local":         {"local_path": "vendor/tool.tar.gz"},
	"github":        {"repo": "org/demo", "asset": "demo.tar.gz"},
	"appimage":      {"url": "https://example.test/tool.AppImage"},
	"appimage+repo": {"repo": "org/demo", "asset": "demo.tar.gz"},
	"android":       {"url": "https://example.test/tool.apk"},
	"android+repo":  {"repo": "org/demo", "asset": "demo.tar.gz"},
	"http":          {"url": "https://example.test/tool.tar.gz"},
	"http+repo":     {"repo": "org/demo", "asset": "demo.tar.gz"},
	"msi":           {"url": "https://example.test/tool.msi", "product_name": "Demo"},
	"msi+repo":      {"repo": "org/demo", "asset": "demo.tar.gz", "product_name": "Demo"},
}
