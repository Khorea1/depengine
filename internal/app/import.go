package app

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/spf13/cobra"
)

// importCandidateBudget is ADR-008 decision 7: a recipe above this many
// candidates is imported with a warning, never rejected (the limit is
// governance, not cost).
const importCandidateBudget = 8

// newImportCmd builds `depengine import`.
func newImportCmd() *cobra.Command {
	catalogRoots := new([]string)
	importForce := new(bool)
	importDryRun := new(bool)

	cmd := &cobra.Command{
		Use:     "import <recipe.toml>",
		Short:   ifPT("Importar uma receita declarativa para o catálogo", "Import a declarative recipe into the catalog"),
		GroupID: groupManage,
		Long: ifPT(
			"Valida uma receita antes de gravá-la no catálogo: pré-visualiza origem, integridade, candidatos e campos executáveis, rejeita hooks e código arbitrário, exige checksum e sinaliza 'sha256:auto' como TOFU.",
			"Validate a recipe before writing it to the catalog: preview origin, integrity, candidates and executable fields, reject hooks and arbitrary code, require a checksum and flag 'sha256:auto' as TOFU.",
		),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(args[0], *catalogRoots, *importDryRun, *importForce)
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(catalogRoots, "catalog", nil, ifPT(
		"raiz do catálogo de destino (repetível; a primeira raiz vence)",
		"destination catalog root (repeatable; the first root wins)",
	))
	f.BoolVar(importDryRun, "dry-run", false, ifPT("pré-visualizar sem gravar a receita", "preview the import without writing the recipe"))
	f.BoolVar(importForce, "force", false, ifPT("substituir uma receita existente com o mesmo nome", "replace an existing recipe with the same tool name"))
	return cmd
}

// importCandidate is one declared candidate as the preview shows it.
type importCandidate struct {
	Kind      string
	Label     string
	Origin    string
	Integrity string
}

func (c importCandidate) displayName() string {
	if c.Label != "" {
		return fmt.Sprintf("%s %q", c.Kind, c.Label)
	}
	return c.Kind
}

// importReport is the complete preview plus the blocking findings of a recipe
// import. It is computed before anything is written.
type importReport struct {
	Name        string
	Source      string
	Destination string
	Digest      string
	Candidates  []importCandidate
	Executable  []string
	Warnings    []string
	Errors      []string
}

// runImport validates, previews and (unless dry-run) writes one recipe file
// into the first configured catalog root.
func runImport(source string, catalogRoots []string, dryRun, force bool) error {
	return runImportTo(os.Stdout, os.Stderr, source, catalogRoots, dryRun, force)
}

func runImportTo(stdout, stderr io.Writer, source string, catalogRoots []string, dryRun, force bool) error {
	abs, err := filepath.Abs(source)
	if err != nil {
		writef(stderr, "error: resolve recipe path: %v\n", err)
		return exitWithCode(3)
	}
	raw, err := os.ReadFile(abs) // #nosec G304 -- the recipe path is the operator's explicit argument.
	if err != nil {
		writef(stderr, "error: read recipe: %v\n", err)
		return exitWithCode(3)
	}

	// Parse without host facts: a catalog recipe is host-independent, and the
	// preview must show every declared candidate in declared order.
	recipe, err := config.ParseRecipeBytes(abs, raw, nil)
	if err != nil {
		writef(stderr, "error: %v\n", err)
		return exitWithCode(2)
	}
	name, tool := singleRecipeTool(recipe)
	if tool == nil {
		writef(stderr, "error: %s: a recipe must declare exactly one tool\n", abs)
		return exitWithCode(2)
	}
	if err := config.ValidateCatalogName(name); err != nil {
		writef(stderr, "error: recipe name %q cannot be stored in a catalog: %v\n", name, err)
		return exitWithCode(2)
	}
	warnings, verr := config.Validate(recipe, exec.RegisteredKinds())
	if verr != nil {
		writef(stderr, "error: %s: %v\n", abs, verr)
		return exitWithCode(2)
	}

	roots, err := config.ResolveCatalogRoots(catalogRoots, false)
	if err != nil {
		writef(stderr, "error: %v\n", err)
		return exitWithCode(2)
	}
	if len(roots) == 0 {
		writeln(stderr, "error: no catalog root configured; pass --catalog or set DEPENGINE_CATALOG")
		return exitWithCode(2)
	}
	destRoot := roots[0]
	dest := filepath.Join(destRoot, name+".toml")

	report, err := inspectImportRecipe(abs, name, tool, raw)
	if err != nil {
		writef(stderr, "error: %v\n", err)
		return exitWithCode(2)
	}
	report.Destination = dest
	for _, warning := range warnings {
		report.Warnings = append(report.Warnings, strings.TrimPrefix(warning, "warning: "))
	}
	if _, statErr := os.Lstat(dest); statErr == nil && !force {
		report.Errors = append(report.Errors, fmt.Sprintf("%s already exists; pass --force to replace it", dest))
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		report.Errors = append(report.Errors, fmt.Sprintf("cannot inspect %s: %v", dest, statErr))
	}
	writeImportReport(stdout, report)

	if len(report.Errors) > 0 {
		for _, message := range report.Errors {
			writef(stderr, "error: %s\n", message)
		}
		return exitWithCode(2)
	}
	if dryRun {
		writeln(stdout, "dry run: recipe not written")
		return nil
	}
	if err := writeRecipeFile(destRoot, dest, raw); err != nil {
		writef(stderr, "error: write recipe: %v\n", err)
		return exitWithCode(3)
	}
	writef(stdout, "wrote %s\n", dest)
	return nil
}

// singleRecipeTool returns the only tool in a parsed recipe. ParseRecipe already
// rejects zero or multiple packages; this stays defensive so a future parser
// change cannot import a multi-tool file silently.
func singleRecipeTool(recipe *config.Schema) (string, *config.Tool) {
	if recipe == nil || len(recipe.Tools) != 1 {
		return "", nil
	}
	for name, tool := range recipe.Tools {
		if tool == nil {
			return "", nil
		}
		return name, tool
	}
	return "", nil
}

// inspectImportRecipe computes the preview and every blocking finding without
// touching the filesystem beyond the source read.
func inspectImportRecipe(source, name string, tool *config.Tool, raw []byte) (*importReport, error) {
	digest, err := config.CanonicalRecipeDigest(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	report := &importReport{
		Name:   name,
		Source: source,
		Digest: digest,
		Warnings: []string{
			"recipe origin is not verified by depengine: it does not sign or allowlist recipe sources, so review the recipe before importing",
		},
	}
	if len(tool.Methods) > importCandidateBudget {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"recipe declares %d candidates; the accepted budget is %d (warning only, ADR-008 decision 7)",
			len(tool.Methods), importCandidateBudget))
	}

	report.Executable = append(report.Executable, planner.ToolDeclarativeFindings(tool)...)

	externalOrigins := make(map[string]bool)
	for _, method := range tool.Methods {
		if method == nil {
			report.Errors = append(report.Errors, "recipe declares a nil candidate")
			continue
		}
		candidate := importCandidate{Kind: method.Kind, Label: method.Label}
		display := candidate.displayName()

		contract, known := methodkind.Lookup(method.Kind)
		if !known {
			report.Errors = append(report.Errors, fmt.Sprintf("candidate %s uses unknown method kind %q", display, method.Kind))
			continue
		}
		findings, planErr := planner.InspectDeclarative(tool, method)
		report.Executable = append(report.Executable, findings...)
		if planErr != nil {
			report.Errors = append(report.Errors, planErr.Error())
			continue
		}

		candidate.Origin = importCandidateOrigin(method)
		candidate.Integrity = inspectImportIntegrity(report, contract, method)
		if candidate.Origin != "" && !externalOrigins[candidate.Origin] {
			externalOrigins[candidate.Origin] = true
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("candidate %s resolves to %s; import does not verify that origin", display, candidate.Origin))
		}
		report.Candidates = append(report.Candidates, candidate)
	}

	if len(report.Executable) > 0 {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"recipe is not declarative: %s. Remove the executable fields or keep the recipe out of the catalog",
			strings.Join(dedupeStrings(report.Executable), "; ")))
	}
	return report, nil
}

// inspectImportIntegrity applies the D5 integrity floor to one candidate and
// returns the preview value.
func inspectImportIntegrity(report *importReport, contract *methodkind.Contract, method *config.MethodCandidate) string {
	display := method.Kind
	if method.Label != "" {
		display = fmt.Sprintf("%s %q", method.Kind, method.Label)
	}

	if method.ChecksumSecretRef != nil {
		// A secret-provided checksum is configured integrity; its value is
		// intentionally never read or printed here.
		return "checksum from secret reference"
	}
	if contract.Artifact == nil {
		return "n/a (no downloaded artifact)"
	}

	var checksums []string
	if configured, ok := method.Config["checksum"].(string); ok && configured != "" {
		checksums = append(checksums, configured)
	}
	// A host-independent parse leaves distribution selection unresolved, so
	// every declared distribution checksum counts toward the floor.
	for _, distribution := range method.Distributions {
		if distribution.HasChecksum && distribution.Checksum != "" {
			checksums = append(checksums, distribution.Checksum)
		}
	}
	checksums = uniqueSortedStrings(checksums)
	if len(checksums) == 0 {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"candidate %s downloads an artifact but declares no checksum; import requires an integrity floor (checksum or checksum_secret_ref)",
			display))
		return "missing"
	}
	for _, checksum := range checksums {
		if strings.HasSuffix(checksum, ":auto") {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"candidate %s uses %q: integrity is trust-on-first-use (TOFU) because the checksum is fetched with the artifact; verify it before relying on the import",
				display, checksum))
			return checksum + " (TOFU)"
		}
	}
	return strings.Join(checksums, ", ")
}

// importCandidateOrigin names the external origin a candidate reaches, without
// host facts. It is used only for a warning: depengine does not verify it.
func importCandidateOrigin(method *config.MethodCandidate) string {
	if repo := configString(method.Config, "repo"); repo != "" {
		return "github:" + repo
	}
	if raw := configString(method.Config, "url"); raw != "" {
		return urlOrigin(raw)
	}
	for _, source := range method.Sources {
		if source.URL != "" {
			return urlOrigin(source.URL)
		}
	}
	return ""
}

func urlOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	if parsed.Scheme == "" {
		return parsed.Host
	}
	return parsed.Scheme + "://" + parsed.Host
}

func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeln(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

func writeImportReport(w io.Writer, report *importReport) {
	writef(w, "Import %s -> %s\n", report.Source, report.Destination)
	writef(w, "  tool:       %s\n", report.Name)
	writef(w, "  digest:     %s\n", report.Digest)
	names := make([]string, 0, len(report.Candidates))
	origins := make([]string, 0, len(report.Candidates))
	for _, candidate := range report.Candidates {
		names = append(names, candidate.displayName())
		if candidate.Origin != "" {
			origins = append(origins, candidate.displayName()+": "+candidate.Origin)
		}
	}
	if len(names) > 0 {
		writef(w, "  candidates: %d (%s)\n", len(report.Candidates), strings.Join(names, ", "))
	} else {
		writef(w, "  candidates: 0\n")
	}
	if len(origins) > 0 {
		writef(w, "  origins:    %s\n", strings.Join(origins, "; "))
	}
	if len(report.Candidates) > 0 {
		parts := make([]string, 0, len(report.Candidates))
		for _, candidate := range report.Candidates {
			parts = append(parts, fmt.Sprintf("%s: %s", candidate.displayName(), candidate.Integrity))
		}
		writef(w, "  integrity:  %s\n", strings.Join(parts, "; "))
	}
	if len(report.Executable) > 0 {
		writef(w, "  executable: %s\n", strings.Join(dedupeStrings(report.Executable), "; "))
	} else {
		writeln(w, "  executable: none (no hooks, command fields or arbitrary-code plans)")
	}
	if len(report.Warnings) > 0 {
		writeln(w, "  warnings:")
		for _, warning := range report.Warnings {
			writef(w, "    - %s\n", warning)
		}
	}
}

// writeRecipeFile writes the recipe as authored, atomically, into the catalog
// root. The content is never re-serialized, so the imported digest matches the
// file the operator reviewed.
func writeRecipeFile(root, dest string, raw []byte) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(root, ".depengine-import-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}
