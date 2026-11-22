package app

import (
	"encoding/hex"
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
	importJSON := new(bool)
	importFormat := new(string)
	expectedDigest := new(string)

	cmd := &cobra.Command{
		Use:     "import <recipe.toml>",
		Short:   ifPT("Importar uma receita declarativa para o catálogo", "Import a declarative recipe into the catalog"),
		GroupID: groupManage,
		Long: ifPT(
			"Valida uma receita antes de gravá-la no catálogo: apresenta origens declaradas, integridade e candidatos; rejeita código arbitrário e referências a credenciais; exige checksum e alerta sobre TOFU. Use --expected-digest para conferir uma identidade obtida por canal confiável.",
			"Validate a recipe before writing it to the catalog: preview declared origins, integrity and candidates; reject arbitrary code and credential references; require a checksum and warn about TOFU. Use --expected-digest to verify a separately trusted recipe identity.",
		),
		Example: `  depengine import ./fd.toml --dry-run   # preview origin, integrity and candidates
  depengine import ./fd.toml --dry-run --json  # inspect the preview in scripts
  depengine import ./fd.toml --force     # replace an existing recipe
  depengine import ./fd.toml --expected-digest sha256:YOUR_TRUSTED_DIGEST  # verify an out-of-band pin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(importFormat, importJSON); err != nil {
				writef(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitWithCode(2)
			}
			return runImportToFormatPinned(cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], *catalogRoots, *importDryRun, *importForce, *importFormat, *expectedDigest, cmd.Flags().Changed("expected-digest"))
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(catalogRoots, "catalog", nil, ifPT(
		"raiz do catálogo de destino (repetível; a primeira raiz vence)",
		"destination catalog root (repeatable; the first root wins)",
	))
	f.BoolVar(importDryRun, "dry-run", false, ifPT("pré-visualizar sem gravar a receita", "preview the import without writing the recipe"))
	f.BoolVar(importForce, "force", false, ifPT("substituir uma receita existente com o mesmo nome", "replace an existing recipe with the same tool name"))
	f.BoolVar(importJSON, "json", false, "shorthand for --format=json")
	f.StringVar(importFormat, "format", "text", "output format: text or json")
	f.StringVar(expectedDigest, "expected-digest", "", "require the canonical sha256:<64 hex> recipe digest from a trusted source")
	return cmd
}

// importCandidate is one declared candidate as the preview shows it.
type importCandidate struct {
	Kind      string   `json:"kind"`
	Label     string   `json:"label,omitempty"`
	Origin    string   `json:"origin,omitempty"` // Legacy primary origin; use Origins for the full list.
	Origins   []string `json:"origins,omitempty"`
	Integrity string   `json:"integrity"`
}

func (c importCandidate) displayName() string {
	if c.Label != "" {
		return fmt.Sprintf("%s %q", c.Kind, c.Label)
	}
	return c.Kind
}

// importReport is the complete static preview plus the blocking findings of a
// recipe import. Runtime redirects and external manager defaults are unknown.
// It is computed before anything is written.
type importReport struct {
	Outcome              string            `json:"outcome"`
	WouldReplace         bool              `json:"would_replace"`
	Name                 string            `json:"name"`
	Source               string            `json:"source"`
	Destination          string            `json:"destination"`
	Digest               string            `json:"digest"`
	Candidates           []importCandidate `json:"candidates"`
	Executable           []string          `json:"executable"`
	CredentialReferences []string          `json:"credential_references,omitempty"`
	Warnings             []string          `json:"warnings"`
	Errors               []string          `json:"errors"`
}

func runImportTo(stdout, stderr io.Writer, source string, catalogRoots []string, dryRun, force bool) error {
	return runImportToFormat(stdout, stderr, source, catalogRoots, dryRun, force, "text")
}

func runImportToFormat(stdout, stderr io.Writer, source string, catalogRoots []string, dryRun, force bool, format string) error {
	return runImportToFormatPinned(stdout, stderr, source, catalogRoots, dryRun, force, format, "", false)
}

// runImportToFormatPinned optionally verifies an out-of-band recipe digest. The
// digest pins recipe bytes, not publisher identity or artifact safety.
func runImportToFormatPinned(stdout, stderr io.Writer, source string, catalogRoots []string, dryRun, force bool, format, expectedDigest string, requireDigest bool) error {
	preflight := importReport{
		Outcome:    "rejected",
		Source:     source,
		Candidates: []importCandidate{},
		Executable: []string{},
		Warnings:   []string{},
		Errors:     []string{},
	}
	reject := func(code int, message string) error {
		preflight.Errors = append(preflight.Errors, message)
		if format == "json" {
			if err := writeMachineOutput(stdout, "import", preflight); err != nil {
				writef(stderr, "error: encode import result: %v\n", err)
				return exitWithCode(3)
			}
		}
		writef(stderr, "error: %s\n", message)
		return exitWithCode(code)
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return reject(3, fmt.Sprintf("resolve recipe path: %v", err))
	}
	preflight.Source = abs
	raw, err := os.ReadFile(abs) // #nosec G304 -- the recipe path is the operator's explicit argument.
	if err != nil {
		return reject(3, fmt.Sprintf("read recipe: %v", err))
	}

	if requireDigest {
		value, hasPrefix := strings.CutPrefix(expectedDigest, "sha256:")
		_, decodeErr := hex.DecodeString(value)
		if !hasPrefix || len(value) != 64 || value != strings.ToLower(value) || decodeErr != nil {
			return reject(2, "invalid --expected-digest: expected sha256:<64 lowercase hex characters>")
		}
		actual, err := config.CanonicalRecipeDigest(raw)
		if err != nil {
			return reject(2, fmt.Sprintf("canonicalize recipe: %v", err))
		}
		preflight.Digest = actual
		if actual != expectedDigest {
			return reject(2, fmt.Sprintf("recipe digest mismatch: expected %s, got %s", expectedDigest, actual))
		}
	}

	// Parse without host facts: a catalog recipe is host-independent, and the
	// preview must show every declared candidate in declared order.
	recipe, err := config.ParseRecipeBytes(abs, raw, nil)
	if err != nil {
		return reject(2, err.Error())
	}
	name, tool := singleRecipeTool(recipe)
	if tool == nil {
		return reject(2, fmt.Sprintf("%s: a recipe must declare exactly one tool", abs))
	}
	preflight.Name = name
	if err := config.ValidateCatalogName(name); err != nil {
		return reject(2, fmt.Sprintf("recipe name %q cannot be stored in a catalog: %v", name, err))
	}
	warnings, verr := config.Validate(recipe, exec.RegisteredKinds())
	if verr != nil {
		return reject(2, fmt.Sprintf("%s: %v", abs, verr))
	}

	roots, err := config.ResolveCatalogRoots(catalogRoots, false)
	if err != nil {
		return reject(2, err.Error())
	}
	if len(roots) == 0 {
		return reject(2, "no catalog root configured; pass --catalog or set DEPENGINE_CATALOG")
	}
	destRoot := roots[0]
	dest := filepath.Join(destRoot, name+".toml")
	preflight.Destination = dest

	report, err := inspectImportRecipe(abs, name, tool, raw)
	if err != nil {
		return reject(2, err.Error())
	}
	report.Destination = dest
	if report.Candidates == nil {
		report.Candidates = []importCandidate{}
	}
	if report.Executable == nil {
		report.Executable = []string{}
	}
	if report.Errors == nil {
		report.Errors = []string{}
	}
	for _, warning := range warnings {
		report.Warnings = append(report.Warnings, strings.TrimPrefix(warning, "warning: "))
	}
	if _, statErr := os.Lstat(dest); statErr == nil {
		report.WouldReplace = true
		if dryRun {
			hint := "use --force to replace it"
			if force {
				hint = "omit --dry-run to replace it"
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf("would replace %s (preview only; %s)", dest, hint))
		} else if !force {
			report.Errors = append(report.Errors, fmt.Sprintf("%s already exists; pass --force to replace it", dest))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		report.Errors = append(report.Errors, fmt.Sprintf("cannot inspect %s: %v", dest, statErr))
	}
	writeReport := func() error {
		if format != "json" {
			writeImportReport(stdout, report)
			return nil
		}
		if err := writeMachineOutput(stdout, "import", report); err != nil {
			writef(stderr, "error: encode import result: %v\n", err)
			return exitWithCode(3)
		}
		return nil
	}

	if len(report.Errors) > 0 {
		report.Outcome = "rejected"
		if err := writeReport(); err != nil {
			return err
		}
		for _, message := range report.Errors {
			writef(stderr, "error: %s\n", message)
		}
		return exitWithCode(2)
	}
	if dryRun {
		report.Outcome = "previewed"
		if err := writeReport(); err != nil {
			return err
		}
		if format != "json" {
			writeln(stdout, "dry run: recipe not written")
		}
		return nil
	}
	if err := writeRecipeFile(destRoot, dest, raw); err != nil {
		report.Outcome = "failed"
		report.Errors = append(report.Errors, fmt.Sprintf("write recipe: %v", err))
		if format == "json" {
			if outputErr := writeReport(); outputErr != nil {
				return outputErr
			}
		}
		writef(stderr, "error: write recipe: %v\n", err)
		return exitWithCode(3)
	}
	report.Outcome = "written"
	if err := writeReport(); err != nil {
		return err
	}
	if format != "json" {
		writef(stdout, "wrote %s\n", dest)
	}
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
		for _, finding := range planner.CandidateCredentialFindings(method) {
			report.CredentialReferences = append(report.CredentialReferences, finding)
			report.Errors = append(report.Errors, fmt.Sprintf("candidate %s is not allowed in a shared catalog recipe", finding))
		}
		if planErr != nil {
			report.Errors = append(report.Errors, planErr.Error())
			continue
		}

		candidate.Origin = importCandidateOrigin(method) // Retain the original JSON field and value.
		candidate.Origins = importCandidateOrigins(method)
		candidate.Integrity = inspectImportIntegrity(report, contract, method)
		for _, origin := range candidate.Origins {
			if externalOrigins[origin] {
				continue
			}
			externalOrigins[origin] = true
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("candidate %s references %s; import does not verify that origin", display, origin))
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
		// The reference is blocked by the catalog credential policy, but the
		// preview still describes why it would otherwise satisfy the floor.
		// The value is intentionally never read or printed here.
		return "checksum from secret reference"
	}
	if contract.Artifact == nil {
		return "n/a (no downloaded artifact)"
	}

	baseChecksum, _ := method.Config["checksum"].(string)
	var checksums []string
	var missing []string
	check := func(checksum, location string) {
		if checksum == "" {
			missing = append(missing, location)
			return
		}
		checksums = append(checksums, checksum)
	}
	if len(method.Distributions) == 0 {
		check(baseChecksum, "")
	} else {
		// Distributions are exhaustive and their checksum overrides are
		// independent. A checksum on one sibling cannot protect another.
		for i, distribution := range method.Distributions {
			effective := baseChecksum
			if distribution.HasChecksum {
				effective = distribution.Checksum
			}
			check(effective, fmt.Sprintf(" distributions[%d]", i))
		}
	}
	for _, location := range missing {
		report.Errors = append(report.Errors, fmt.Sprintf(
			"candidate %s%s downloads an artifact but declares no checksum; import requires an integrity floor (checksum or checksum_secret_ref)",
			display, location))
	}
	if len(missing) > 0 {
		return "missing"
	}
	checksums = uniqueSortedStrings(checksums)
	for i, checksum := range checksums {
		if strings.HasSuffix(checksum, ":auto") {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"candidate %s uses %q: integrity is trust-on-first-use (TOFU) because the checksum is fetched with the artifact; verify it before relying on the import",
				display, checksum))
			checksums[i] += " (TOFU)"
		}
	}
	return strings.Join(checksums, ", ")
}

// importCandidateOrigin keeps the original primary-origin field stable for
// existing JSON consumers. New clients should use importCandidate.Origins.
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

// importCandidateOrigins inventories declared source selectors, network origins
// and verification sidecars in stable order. Runtime redirects, defaults and
// external package-manager network requests are not discoverable at import.
func importCandidateOrigins(method *config.MethodCandidate) []string {
	var origins []string
	add := func(origin string) {
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if repo := configString(method.Config, "repo"); repo != "" {
		add("github:" + repo)
	}
	if raw := configString(method.Config, "url"); raw != "" {
		add(urlOrigin(raw))
	}
	// These fields select other package registries, remotes or sources. Some
	// carry URLs while others contain only an external manager's source name.
	for _, key := range []string{"source", "registry", "remote", "bucket", "index", "index_url", "git", "backend"} {
		if value := configString(method.Config, key); value != "" {
			add(key + " @ " + importOriginValue(value))
		}
	}
	if channels, ok := method.Config["channels"].([]string); ok {
		for _, channel := range channels {
			add("channel @ " + importOriginValue(channel))
		}
	}
	for _, source := range method.Sources {
		identity := source.Kind
		if source.Name != "" {
			identity += ":" + source.Name
		}
		if source.URL != "" {
			add("source " + identity + " @ " + urlOrigin(source.URL))
		} else if identity != "" {
			add("source " + identity)
		}
	}
	for _, key := range []string{"checksum_url", "signature_url"} {
		if raw := configString(method.Config, key); raw != "" {
			add(key + " @ " + urlOrigin(raw))
		}
	}
	for i, distribution := range method.Distributions {
		if distribution.HasChecksumURL && distribution.ChecksumURL != "" {
			add(fmt.Sprintf("distributions[%d] checksum_url @ %s", i, urlOrigin(distribution.ChecksumURL)))
		}
		if distribution.HasSignatureURL && distribution.SignatureURL != "" {
			add(fmt.Sprintf("distributions[%d] signature_url @ %s", i, urlOrigin(distribution.SignatureURL)))
		}
	}
	return dedupeStrings(origins)
}

func importOriginValue(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return urlOrigin(raw) // Strip userinfo, path, query and fragment.
	}
	return raw
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
		for _, origin := range candidate.Origins {
			origins = append(origins, candidate.displayName()+": "+origin)
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
	if len(report.CredentialReferences) > 0 {
		writef(w, "  credential references: %s\n", strings.Join(dedupeStrings(report.CredentialReferences), "; "))
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
