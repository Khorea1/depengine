package config

import "testing"

func TestBuildMethodsInfersNativeOnlyForShorthand(t *testing.T) {
	tests := []struct {
		name       string
		decl       map[string]any
		wantKinds  []string
		wantNative bool
		inferred   bool
	}{
		{
			name:       "go string shorthand",
			decl:       map[string]any{"go": "example.com/tool"},
			wantKinds:  []string{"native", "go"},
			wantNative: true,
			inferred:   true,
		},
		{
			name:       "cargo bool shorthand",
			decl:       map[string]any{"cargo": true},
			wantKinds:  []string{"native", "cargo"},
			wantNative: true,
			inferred:   true,
		},
		{
			name:      "explicit go table",
			decl:      map[string]any{"go": map[string]any{"pkg": "example.com/tool", "version": "v1.2.3"}},
			wantKinds: []string{"go"},
		},
		{
			name:      "explicit cargo table",
			decl:      map[string]any{"cargo": map[string]any{"pkg": "ripgrep", "version": "14.1.1"}},
			wantKinds: []string{"cargo"},
		},
		{
			name:      "explicit apt table",
			decl:      map[string]any{"apt": map[string]any{"pkg": "fd-find"}},
			wantKinds: []string{"apt"},
		},
		{
			name:       "native manager shorthand is explicit native intent",
			decl:       map[string]any{"brew": "ripgrep"},
			wantKinds:  []string{"native"},
			wantNative: true,
			inferred:   false,
		},
		{
			name:       "explicit native table",
			decl:       map[string]any{"native": map[string]any{"pkg": "ripgrep"}},
			wantKinds:  []string{"native"},
			wantNative: true,
			inferred:   false,
		},
		{
			name:       "mixed shorthand and explicit table keeps convenience fallback",
			decl:       map[string]any{"go": "example.com/tool", "github": map[string]any{"repo": "owner/tool"}},
			wantKinds:  []string{"native", "github", "go"},
			wantNative: true,
			inferred:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			methods := buildMethods("tool", tt.decl)
			if len(methods) != len(tt.wantKinds) {
				t.Fatalf("got %d methods, want %d: %+v", len(methods), len(tt.wantKinds), methods)
			}
			for i, want := range tt.wantKinds {
				if methods[i].Kind != want {
					t.Fatalf("method[%d].Kind = %q, want %q", i, methods[i].Kind, want)
				}
			}
			var native *MethodCandidate
			for _, method := range methods {
				if method.Kind == "native" {
					native = method
					break
				}
			}
			if (native != nil) != tt.wantNative {
				t.Fatalf("native present = %v, want %v", native != nil, tt.wantNative)
			}
			if native != nil && native.Inferred != tt.inferred {
				t.Fatalf("native.Inferred = %v, want %v", native.Inferred, tt.inferred)
			}
		})
	}
}

func TestMethodOnlyStillExcludesInferredNativeFallback(t *testing.T) {
	methods := buildMethods("tool", map[string]any{"go": "example.com/tool"})
	tool := &Tool{Methods: methods, MethodOnly: []string{"go"}}
	selected := SelectMethods(tool, []string{"native", "go"}, "native")
	if len(selected) != 1 || selected[0].Kind != "go" {
		t.Fatalf("SelectMethods = %+v, want only go", selected)
	}
}
