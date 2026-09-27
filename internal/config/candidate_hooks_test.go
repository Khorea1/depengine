package config

import "testing"

func TestParseMethodHoistsCandidateHooksOutOfAdapterConfig(t *testing.T) {
	raw := map[string]any{
		"pkg":          "demo",
		"pre_install":  []any{map[string]any{"run": []any{"echo", "pre"}, "when": map[string]any{"os": []any{"linux"}}}},
		"post_install": "echo post",
	}
	method, err := parseMethod("native", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(method.PreInstall) != 1 || len(method.PostInstall) != 1 {
		t.Fatalf("candidate hooks = pre:%#v post:%#v", method.PreInstall, method.PostInstall)
	}
	if _, ok := method.Config["pre_install"]; ok {
		t.Fatal("pre_install leaked into adapter config")
	}
	if _, ok := method.Config["post_install"]; ok {
		t.Fatal("post_install leaked into adapter config")
	}
	if method.PreInstall[0].When == nil || len(method.PreInstall[0].When.OS) != 1 || method.PreInstall[0].When.OS[0] != "linux" {
		t.Fatalf("hook condition = %#v", method.PreInstall[0].When)
	}
}

func TestStrictValidationAcceptsCandidateHooks(t *testing.T) {
	errs := []string{}
	validateMethodValue(map[string]any{
		"pkg":         "demo",
		"pre_install": map[string]any{"run": []any{"echo", "pre"}},
	}, "tools.demo.native", "native", &errs)
	if len(errs) != 0 {
		t.Fatalf("candidate hook rejected by strict validation: %v", errs)
	}
}
