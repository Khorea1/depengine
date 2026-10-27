package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// whyReport is the audit contract of `depengine why`. The human `--fields`
// output and the documented `--json` object are both rendered from it, so the
// two surfaces can never disagree about origins, candidates or identity.
type whyReport struct {
	Tool            string             `json:"tool"`
	Recipe          *whyRecipe         `json:"recipe,omitempty"`
	FieldProvenance []whyFieldProv     `json:"field_provenance,omitempty"`
	CatalogUpdate   *whyCatalogUpdate  `json:"catalog_update,omitempty"`
	Candidates      []whyCandidate     `json:"candidates"`
	Selected        *whySelectedMethod `json:"selected,omitempty"`
}

// whyRecipe identifies the catalog recipe a tool was resolved from. Layer is
// always "catalog" today; it stays explicit so import can name its own layer.
type whyRecipe struct {
	Name      string `json:"name"`
	Layer     string `json:"layer"`
	RootIndex int    `json:"root_index"`
	Root      string `json:"root"`
	Path      string `json:"path"`
	Digest    string `json:"digest"`
}

// whyFieldProv answers, per field, which layer supplied the effective value,
// which declared values were replaced, and what the effective value is.
type whyFieldProv struct {
	Field      string            `json:"field"`
	Origin     string            `json:"origin"`
	Effective  string            `json:"effective"`
	Layers     map[string]string `json:"layers,omitempty"`
	Overridden []string          `json:"overridden,omitempty"`
}

// whyCatalogUpdate states what a catalog update would change for this tool.
// Fields still sourced from the catalog are affected; fields overridden by the
// project schema or personal manifest are shielded from the recipe.
type whyCatalogUpdate struct {
	AffectsTool bool     `json:"affects_tool"`
	Fields      []string `json:"fields,omitempty"`
	Overridden  []string `json:"overridden_fields,omitempty"`
}

// whyCandidate is one considered candidate with a stable machine-readable
// reason. Detail is human text; ReasonCode is the contract.
type whyCandidate struct {
	Kind           string                    `json:"kind"`
	Label          string                    `json:"label,omitempty"`
	Candidate      int                       `json:"candidate"`
	CandidateKnown bool                      `json:"candidate_known"`
	Status         string                    `json:"status"`
	ReasonCode     string                    `json:"reason_code,omitempty"`
	Detail         string                    `json:"detail,omitempty"`
	Intent         map[string]string         `json:"intent,omitempty"`
	PlanIntent     *plan.ResolvedInstallPlan `json:"plan_intent,omitempty"`
}

// whySelectedMethod names the candidate the executor would use and why.
type whySelectedMethod struct {
	Kind      string `json:"kind"`
	Label     string `json:"label,omitempty"`
	Candidate int    `json:"candidate"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
}

// secretProvenanceFields never print their value. A reference is identity, not
// content, but keeping it out of provenance avoids leaking a command or env
// name that embeds a credential.
var secretProvenanceFields = map[string]bool{
	"SecretRef":          true,
	"ChecksumSecretRef":  true,
	"SignatureSecretRef": true,
}

// buildWhyReport assembles the audit report from already-resolved project state
// and explanations. It performs no I/O and no catalog access, so `why` remains
// a local, single-tool query.
func buildWhyReport(toolName string, tool *config.Tool, provenance []config.FieldSource, attempts []exec.MethodAttempt) *whyReport {
	report := &whyReport{
		Tool:       toolName,
		Candidates: make([]whyCandidate, 0, len(attempts)),
	}
	if tool != nil && tool.Recipe != nil {
		report.Recipe = &whyRecipe{
			Name:      tool.Recipe.Name,
			Layer:     config.LayerCatalog,
			RootIndex: tool.Recipe.RootIndex,
			Root:      tool.Recipe.Root,
			Path:      tool.Recipe.Path,
			Digest:    tool.Recipe.Digest,
		}
	}
	report.FieldProvenance = collapseFieldProvenance(provenance)
	report.CatalogUpdate = catalogUpdateImpact(report.FieldProvenance)

	for _, attempt := range attempts {
		report.Candidates = append(report.Candidates, whyCandidate{
			Kind:           attempt.Kind,
			Label:          attempt.Label,
			Candidate:      attempt.Candidate,
			CandidateKnown: attempt.CandidateKnown,
			Status:         attempt.Status,
			ReasonCode:     attempt.ReasonCode,
			Detail:         run.RedactSensitiveText(attempt.Error),
			Intent:         attempt.Intent,
			PlanIntent:     attempt.PlanIntent,
		})
	}
	for _, candidate := range report.Candidates {
		if candidate.Status != "would_install" && candidate.Status != "already_installed" {
			continue
		}
		reason := "first candidate that would install"
		if candidate.Status == "already_installed" {
			reason = "first candidate whose desired state is already satisfied"
		}
		report.Selected = &whySelectedMethod{
			Kind:      candidate.Kind,
			Label:     candidate.Label,
			Candidate: candidate.Candidate,
			Status:    candidate.Status,
			Reason:    reason,
		}
		break
	}
	return report
}

// collapseFieldProvenance keeps the last entry per field, which carries the
// final effective origin after every layer merged. Entries stay in the order
// the merged tool declares its fields.
func collapseFieldProvenance(provenance []config.FieldSource) []whyFieldProv {
	if len(provenance) == 0 {
		return nil
	}
	latest := make(map[string]config.FieldSource, len(provenance))
	order := make([]string, 0, len(provenance))
	for _, source := range provenance {
		if _, seen := latest[source.Field]; !seen {
			order = append(order, source.Field)
		}
		latest[source.Field] = source
	}
	out := make([]whyFieldProv, 0, len(order))
	for _, field := range order {
		source := latest[field]
		layers := make(map[string]string, len(source.Values))
		for layer, value := range source.Values {
			layers[layer] = formatProvenanceValue(field, value)
		}
		if len(layers) == 0 {
			layers = nil
		}
		overridden := append([]string(nil), source.Overridden...)
		sort.Strings(overridden)
		out = append(out, whyFieldProv{
			Field:      field,
			Origin:     source.Source,
			Effective:  formatProvenanceValue(field, source.Merged),
			Layers:     layers,
			Overridden: overridden,
		})
	}
	return out
}

// catalogUpdateImpact reports which fields a recipe change would still reach.
// It is nil when the tool has no provenance at all.
func catalogUpdateImpact(provenance []whyFieldProv) *whyCatalogUpdate {
	if len(provenance) == 0 {
		return nil
	}
	impact := &whyCatalogUpdate{}
	for _, field := range provenance {
		if !containsLayer(field.Layers, config.LayerCatalog) {
			continue
		}
		if containsLayerName(field.Overridden, config.LayerCatalog) {
			impact.Overridden = append(impact.Overridden, field.Field)
			continue
		}
		impact.Fields = append(impact.Fields, field.Field)
	}
	sort.Strings(impact.Fields)
	sort.Strings(impact.Overridden)
	impact.AffectsTool = len(impact.Fields) > 0
	return impact
}

func containsLayer(layers map[string]string, want string) bool {
	_, ok := layers[want]
	return ok
}

func containsLayerName(layers []string, want string) bool {
	for _, layer := range layers {
		if layer == want {
			return true
		}
	}
	return false
}

// formatProvenanceValue renders a merged or declared value without dumping
// command bodies, secret references or full method configuration.
func formatProvenanceValue(field string, value any) string {
	if secretProvenanceFields[field] {
		if value == nil {
			return ""
		}
		return "<secret reference>"
	}
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return run.RedactSensitiveText(typed)
	case []string:
		return run.RedactSensitiveText(fmt.Sprintf("%v", typed))
	case bool:
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case []config.Hook:
		return fmt.Sprintf("[%d hooks]", len(typed))
	case []*config.MethodCandidate:
		return fmt.Sprintf("[%d methods]", len(typed))
	case map[string]*config.Condition:
		return fmt.Sprintf("[%d conditions]", len(typed))
	default:
		return run.RedactSensitiveText(fmt.Sprintf("%v", value))
	}
}

// renderWhyAudit writes the human audit sections behind `why --fields`.
func renderWhyAudit(w io.Writer, report *whyReport) {
	if report.Recipe != nil {
		_, _ = fmt.Fprintf(w, "Recipe: %s (layer %s, root #%d)\n", report.Recipe.Name, report.Recipe.Layer, report.Recipe.RootIndex)
		_, _ = fmt.Fprintf(w, "  root:   %s\n", report.Recipe.Root)
		_, _ = fmt.Fprintf(w, "  path:   %s\n", report.Recipe.Path)
		_, _ = fmt.Fprintf(w, "  digest: %s\n", report.Recipe.Digest)
	} else {
		_, _ = fmt.Fprintln(w, "Recipe: none (tool is defined by the project schema and personal manifest)")
	}
	if report.Selected != nil {
		_, _ = fmt.Fprintf(w, "Selected: %s #%d (%s; %s)\n", report.Selected.displayName(), report.Selected.Candidate, report.Selected.Status, report.Selected.Reason)
	} else {
		_, _ = fmt.Fprintln(w, "Selected: none (no candidate is able to install on this host)")
	}
	if report.CatalogUpdate != nil {
		switch {
		case report.CatalogUpdate.AffectsTool:
			_, _ = fmt.Fprintf(w, "Catalog update: affects %s", strings.Join(report.CatalogUpdate.Fields, ", "))
			if len(report.CatalogUpdate.Overridden) > 0 {
				_, _ = fmt.Fprintf(w, "; shielded by more specific layers: %s", strings.Join(report.CatalogUpdate.Overridden, ", "))
			}
			_, _ = fmt.Fprintln(w)
		case len(report.CatalogUpdate.Overridden) > 0:
			_, _ = fmt.Fprintf(w, "Catalog update: no effect (shielded by more specific layers: %s)\n", strings.Join(report.CatalogUpdate.Overridden, ", "))
		default:
			_, _ = fmt.Fprintln(w, "Catalog update: no effect (the tool has no catalog-sourced field)")
		}
	}
	if len(report.FieldProvenance) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "\nField provenance:")
	for _, field := range report.FieldProvenance {
		origin := field.Origin
		if origin == "" {
			origin = "none"
		}
		_, _ = fmt.Fprintf(w, "  %s: origin=%s effective=%s", field.Field, origin, field.Effective)
		if len(field.Overridden) > 0 {
			_, _ = fmt.Fprintf(w, " overridden=%s", strings.Join(field.Overridden, ","))
		}
		if len(field.Layers) > 0 {
			_, _ = fmt.Fprintf(w, " layers: %s", formatLayerValues(field.Layers))
		}
		_, _ = fmt.Fprintln(w)
	}
}

func (s *whySelectedMethod) displayName() string {
	if s.Label != "" {
		return s.Label
	}
	return s.Kind
}

// formatLayerValues renders the per-layer declared values in a deterministic
// order: catalog, manifest, schema, then any future layer name.
func formatLayerValues(layers map[string]string) string {
	names := make([]string, 0, len(layers))
	for name := range layers {
		names = append(names, name)
	}
	rank := map[string]int{config.LayerCatalog: 0, config.LayerManifest: 1, config.LayerSchema: 2}
	sort.Slice(names, func(i, j int) bool {
		ri, iok := rank[names[i]]
		rj, jok := rank[names[j]]
		if iok != jok {
			return iok
		}
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+layers[name])
	}
	return strings.Join(parts, " ")
}
