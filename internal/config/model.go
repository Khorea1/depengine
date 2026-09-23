package config

import (
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/platform"
)

// Schema is the fully-normalized in-memory form of schema.toml after parsing.
type Schema struct {
	Version       int
	Defaults      Defaults
	Tools         map[string]*Tool
	AllowNewTools bool                     `json:"-"`
	Provenance    map[string][]FieldSource `json:"-"`
	// ProjectRoot is the absolute directory containing the project schema.
	// It is runtime-only metadata used to resolve project-relative inputs such
	// as vendored/local artifacts; it is never serialized into plans or state.
	ProjectRoot string `json:"-"`
}

// Defaults mirrors the [defaults] table. Omitted fields keep engine-safe
// defaults baked into the parser.
type Defaults struct {
	Manager     string
	AurHelper   string
	MethodOrder []string
	// ArchMap/OSMap override the built-in {arch}/{os} spelling table for
	// every method unless the method declares its own map.
	ArchMap map[string]string
	OSMap   map[string]string
}

// DefaultBuckets maps ecosystem names to lists of method kinds.
var DefaultBuckets = methodkind.DefaultBuckets

// Tool is one entry under [tools].
type Tool struct {
	Name           string                `merge:"overwrite"`
	PreInstall     []Hook                `merge:"overwrite"`
	PostInstall    []Hook                `merge:"overwrite"`
	RequiresWhen   map[string]*Condition `merge:"overwrite"`
	Requires       []string              `merge:"overwrite"`
	Methods        []*MethodCandidate    `merge:"methods"`
	MethodPrefer   []string              `merge:"overwrite"`
	MethodOnly     []string              `merge:"overwrite"`
	IsSimple       bool                  `merge:"overwrite"`
	Tags           []string              `merge:"union"`
	Ecosystem      string                `merge:"overwrite"`
	DependencyOnly bool                  `merge:"overwrite"`
}

// Hook is an explicitly tokenized command. Run[0] is the executable and the
// remaining elements are passed unchanged as argv.
type Hook struct {
	Run  []string
	When *Condition
}

// FilteredTools clones tools with Requires reduced to the dependencies that
// apply under facts. Tools without gated dependencies are reused.
func FilteredTools(tools map[string]*Tool, facts *platform.Facts) map[string]*Tool {
	if tools == nil || facts == nil {
		return tools
	}
	out := make(map[string]*Tool, len(tools))
	for name, tool := range tools {
		if len(tool.RequiresWhen) == 0 {
			out[name] = tool
			continue
		}
		clone := cloneTool(tool)
		clone.Requires = tool.EffectiveRequires(facts)
		out[name] = clone
	}
	return out
}

// EffectiveRequires returns dependencies whose conditions match facts. Nil
// facts disable filtering.
func (t *Tool) EffectiveRequires(facts *platform.Facts) []string {
	if facts == nil || len(t.RequiresWhen) == 0 {
		return t.Requires
	}
	out := make([]string, 0, len(t.Requires))
	for _, dependency := range t.Requires {
		if condition, gated := t.RequiresWhen[dependency]; gated && !condition.Match(facts) {
			continue
		}
		out = append(out, dependency)
	}
	return out
}

func cloneTool(tool *Tool) *Tool {
	if tool == nil {
		return nil
	}
	out := *tool
	out.PreInstall = cloneHooks(tool.PreInstall)
	out.PostInstall = cloneHooks(tool.PostInstall)
	out.Requires = append([]string{}, tool.Requires...)
	if tool.RequiresWhen != nil {
		out.RequiresWhen = make(map[string]*Condition, len(tool.RequiresWhen))
		for dependency, condition := range tool.RequiresWhen {
			out.RequiresWhen[dependency] = condition
		}
	}
	out.Tags = append([]string{}, tool.Tags...)
	out.MethodPrefer = append([]string{}, tool.MethodPrefer...)
	out.MethodOnly = append([]string{}, tool.MethodOnly...)
	out.Methods = cloneMethods(tool.Methods)
	return &out
}

func cloneHooks(hooks []Hook) []Hook {
	out := make([]Hook, len(hooks))
	for i, hook := range hooks {
		out[i] = hook
		out[i].Run = append([]string(nil), hook.Run...)
		if hook.When != nil {
			condition := *hook.When
			out[i].When = &condition
		}
	}
	return out
}

func cloneMethods(methods []*MethodCandidate) []*MethodCandidate {
	out := make([]*MethodCandidate, len(methods))
	for i, method := range methods {
		out[i] = cloneMethod(method)
	}
	return out
}

func cloneMethod(method *MethodCandidate) *MethodCandidate {
	if method == nil {
		return nil
	}
	out := *method
	out.Requires = append([]string(nil), method.Requires...)
	out.Sources = cloneSources(method.Sources)
	out.Config = make(map[string]any, len(method.Config))
	for key, value := range method.Config {
		out.Config[key] = value
	}
	if method.When != nil {
		condition := *method.When
		out.When = &condition
	}
	return &out
}

func cloneSources(sources []Source) []Source {
	out := append([]Source(nil), sources...)
	for i := range out {
		if out[i].SecretRef != nil {
			ref := *out[i].SecretRef
			out[i].SecretRef = &ref
		}
	}
	return out
}

// MethodCandidate is one way to install the parent Tool.
type MethodCandidate struct {
	Kind     string
	Label    string
	Inferred bool // synthesized by depengine rather than explicitly declared
	// ProjectRoot is inherited from the final project schema after layering.
	// Adapters may use it for project-relative resources, but must never persist
	// the machine-specific absolute value.
	ProjectRoot string `json:"-"`
	When        *Condition
	Config      map[string]any
	Err         error
	ArchMap     map[string]string
	OSMap       map[string]string
	Requires    []string
	Sources     []Source
}

// Source is repository configuration scoped to a single method candidate.
type Source struct {
	Kind      string
	Name      string
	URL       string
	SecretRef *SecretReference
}

// SecretReference names external secret material without storing its value.
type SecretReference struct {
	Provider string
	Name     string
}

func (t *Tool) GraphDependencies() []string { return t.Requires }

func (t *Tool) GraphTags() []string { return t.Tags }

func (t *Tool) GraphConditionalDependencies() map[string][]string {
	dependencies := make(map[string][]string)
	for _, method := range t.Methods {
		label := method.Kind
		if method.Label != "" {
			label = method.Label
		}
		for _, dependency := range method.Requires {
			dependencies[label] = append(dependencies[label], dependency)
		}
	}
	return dependencies
}
