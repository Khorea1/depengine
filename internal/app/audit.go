package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// auditCheckOrder fixes the order checks appear in text and machine output.
var auditCheckOrder = []string{"schema", "layers", "lock", "state"}

var auditCheckLabel = map[string]string{
	"schema": "schema",
	"layers": "layer merge",
	"lock":   "lock",
	"state":  "state/host",
}

// newAuditCmd builds `depengine audit`: one read-only report that reconciles
// the effective schema, the lockfile, tracked state and observed host identity.
func newAuditCmd() *cobra.Command {
	auditSchema := new(string)
	auditLockPath := new(string)
	auditManifest := new(string)
	auditNoManifest := new(bool)
	auditCatalog := new([]string)
	auditNoCatalog := new(bool)
	auditFormat := new(string)
	auditJSON := new(bool)
	auditStrict := new(bool)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: ifPT("Auditar schema, lock, estado e host em um relatório", "Audit schema, lock, state and host in one report"),
		Long:  ifPT("Reconcilia o schema efetivo, o lockfile, o estado rastreado e a identidade observada no host. Cada divergência traz a remediação; o código de saída agrega o resultado (0 sem erros, 1 com erros, 2 schema inválido, 3 falha de runtime).", "Reconciles the effective schema, the lockfile, tracked state and observed host identity. Every divergence carries a remediation, and one exit code aggregates the result (0 without errors, 1 with errors, 2 invalid schema, 3 runtime failure)."),
		Example: `  depengine audit                       # full consistency report
  depengine audit --strict              # warnings also fail the gate
  depengine audit --json                # machine-readable report
  depengine audit --schema ci/schema.toml --no-manifest`,
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(auditFormat, auditJSON); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			catalog, err := catalogFromCommand(cmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			return runAudit(cmd.Context(), auditSchema, auditManifest, auditNoManifest, auditFormat, auditStrict, catalog, *auditLockPath)
		},
	}
	f := cmd.Flags()
	f.StringVar(auditSchema, "schema", "", "path to schema.toml (default: the path recorded in state)")
	f.StringVar(auditLockPath, "lock", "", "lockfile to audit (default: derived from schema path)")
	f.StringVar(auditManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(auditNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	addCatalogFlags(cmd, auditCatalog, auditNoCatalog)
	f.StringVar(auditFormat, "format", "text", "output format: text or json")
	f.BoolVar(auditJSON, "json", false, "shorthand for --format=json")
	f.BoolVar(auditStrict, "strict", false, "treat warnings as errors")
	return cmd
}

// auditFinding is one divergence (or informational fact) in the report.
type auditFinding struct {
	Check       string `json:"check"`
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Tool        string `json:"tool,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type auditSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
}

type auditReport struct {
	Schema      string         `json:"schema"`
	LockPath    string         `json:"lock_path,omitempty"`
	LockVersion int            `json:"lock_version,omitempty"`
	Findings    []auditFinding `json:"findings"`
	Summary     auditSummary   `json:"summary"`
}

func (r *auditReport) add(check, severity, code, tool, message, remediation string) {
	r.Findings = append(r.Findings, auditFinding{
		Check: check, Severity: severity, Code: code, Tool: tool,
		Message: message, Remediation: remediation,
	})
}

// runAudit collects the four consistency surfaces and renders one report.
func runAudit(ctx context.Context, auditSchema, auditManifest *string, auditNoManifest *bool, auditFormat *string, auditStrict *bool, catalog *config.Catalog, lockOverride ...string) error {
	jsonOut := *auditFormat == "json"
	report := &auditReport{Findings: []auditFinding{}}

	ls, err := state.LoadSharedContext(ctx)
	if err != nil {
		log.Default.Error("state lock", "error", err)
		return exitWithCode(3)
	}
	defer func() { _ = ls.Close() }()
	st := ls.State()

	schemaPath := st.SchemaPath
	if *auditSchema != "" {
		schemaPath = *auditSchema
	}
	if schemaPath == "" {
		report.add("schema", "error", "E_SCHEMA_NOT_FOUND", "",
			"no schema given and none recorded in state",
			"pass --schema or run 'depengine init'")
		return writeAuditReport(report, jsonOut, 2)
	}

	noManifest := *auditNoManifest
	manifestPath := *auditManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	project, err := loadProject(schemaPath, projectLoadOptions{ManifestPath: manifestPath, ManifestAuto: manifestAuto, Catalog: catalog})
	if err != nil {
		finding, code := classifyAuditLoadError(err)
		report.add("schema", finding.Severity, finding.Code, "", finding.Message, finding.Remediation)
		return writeAuditReport(report, jsonOut, code)
	}
	report.Schema = project.SchemaPath
	if project.ManifestCount > 0 && manifestAuto {
		report.add("layers", "info", "I_MANIFEST_MERGED", "",
			fmt.Sprintf("merged %d tools from %s", project.ManifestCount, project.ManifestPath), "")
	}
	if project.CatalogCount > 0 {
		report.add("layers", "info", "I_CATALOG_RESOLVED", "",
			fmt.Sprintf("resolved %d catalog recipes", project.CatalogCount), "")
	}
	for _, warning := range project.Warnings {
		report.add("layers", "warning", "W_LAYER", warning.Tool, warning.Message, "")
	}
	if st.SchemaPath != "" && st.SchemaPath != project.SchemaPath {
		report.add("state", "warning", "W_STATE_SCHEMA_PATH", "",
			fmt.Sprintf("state was recorded against %s", st.SchemaPath),
			"run 'depengine install' with the current schema to refresh tracked state")
	}

	lk := auditLock(report, project, lockOverride...)
	auditState(ctx, report, project, lk, st.Tools)

	code := 0
	severity := report.summarize()
	if severity.Errors > 0 || (*auditStrict && severity.Warnings > 0) {
		code = 1
	}
	return writeAuditReport(report, jsonOut, code)
}

// auditLock validates the lockfile against the effective schema and records
// findings. A readable lock is returned for state reconciliation.
func auditLock(report *auditReport, project *loadedProject, override ...string) *lock.Lock {
	lockPath := lock.Path(project.SchemaPath, override...)
	report.LockPath = lockPath
	lk, err := lock.Load(lockPath)
	if err != nil {
		report.add("lock", "error", "E_LOCK_UNREADABLE", "",
			fmt.Sprintf("lockfile %s cannot be read: %v", lockPath, err),
			"run 'depengine update' to regenerate the lockfile")
		return nil
	}
	if lk == nil {
		report.add("lock", "warning", "W_LOCK_MISSING", "",
			"no lockfile: mutable references are not pinned",
			"run 'depengine update'")
		return nil
	}
	report.LockVersion = lk.Version
	if lk.Version == lock.CurrentVersion {
		if _, err := lk.ProjectionDocument(); err != nil {
			report.add("lock", "error", "E_LOCK_INVALID", "",
				fmt.Sprintf("lock projection cannot be decoded: %v", err),
				"run 'depengine update' to regenerate the lockfile")
			return nil
		}
	}
	if err := lock.ValidateFrozen(project.Schema, lk); err != nil {
		report.add("lock", "error", "E_LOCK_STALE", "",
			fmt.Sprintf("lockfile does not match the schema: %v", err),
			"run 'depengine update' and review the new pins")
	}
	return lk
}

// auditState reconciles tracked state against schema and host identity.
func auditState(ctx context.Context, report *auditReport, project *loadedProject, lk *lock.Lock, installed map[string]state.ToolState) {
	rows, err := statusRows(ctx, project, lk, installed, false)
	if err != nil {
		report.add("state", "error", "E_STATE_PROBE_FAILED", "", err.Error(),
			"check that the state file and lockfile are readable")
		return
	}
	for _, row := range rows {
		tool := project.Schema.Tools[row.Name]
		if row.Status == "missing" && (tool == nil || len(tool.Methods) == 0) {
			// Virtual tools have nothing to install.
			continue
		}
		switch row.Status {
		case "missing":
			report.add("state", "warning", "W_TOOL_MISSING", row.Name,
				"schema expects this tool but state has no installation",
				"run 'depengine install --only "+row.Name+"'")
		case "outdated":
			report.add("state", "warning", "W_TOOL_OUTDATED", row.Name,
				"installed identity no longer matches the schema or pinned version",
				"run 'depengine upgrade --only "+row.Name+"'")
		case "orphaned":
			report.add("state", "warning", "W_TOOL_ORPHANED", row.Name,
				"tracked in state but absent from the schema",
				"add it to the schema or run 'depengine forget "+row.Name+"'")
		case "broken":
			detail := "installed target is broken"
			if row.Verification != nil && row.Verification.Detail != "" {
				detail = "installed target is broken: " + row.Verification.Detail
			}
			report.add("state", "error", "E_TOOL_BROKEN", row.Name, detail,
				"run 'depengine install --only "+row.Name+"'")
		case "unknown":
			detail := "host identity could not be verified"
			if row.Verification != nil && row.Verification.Detail != "" {
				detail = "host identity could not be verified: " + row.Verification.Detail
			}
			report.add("state", "warning", "W_TOOL_UNKNOWN", row.Name, detail,
				"run 'depengine check "+row.Name+" --live'")
		}
	}
}

// summarize counts severities and orders findings for stable output.
func (r *auditReport) summarize() auditSummary {
	rank := make(map[string]int, len(auditCheckOrder))
	for i, check := range auditCheckOrder {
		rank[check] = i
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if rank[r.Findings[i].Check] != rank[r.Findings[j].Check] {
			return rank[r.Findings[i].Check] < rank[r.Findings[j].Check]
		}
		return r.Findings[i].Tool < r.Findings[j].Tool
	})
	summary := auditSummary{}
	for _, finding := range r.Findings {
		switch finding.Severity {
		case "error":
			summary.Errors++
		case "warning":
			summary.Warnings++
		default:
			summary.Info++
		}
	}
	r.Summary = summary
	return summary
}

func writeAuditReport(report *auditReport, jsonOut bool, code int) error {
	report.summarize()
	if jsonOut {
		if err := writeMachineOutput(os.Stdout, "audit", report); err != nil {
			return exitWithCode(3)
		}
	} else {
		renderAuditText(report)
	}
	if code == 0 {
		return nil
	}
	return exitWithCode(code)
}

func renderAuditText(report *auditReport) {
	c := newCLIStyle(os.Stderr)
	header := "depengine audit"
	if report.Schema != "" {
		header += " · " + report.Schema
	}
	if report.LockPath != "" {
		lockLabel := report.LockPath
		if report.LockVersion != 0 {
			lockLabel += fmt.Sprintf(" (v%d)", report.LockVersion)
		}
		header += " · " + lockLabel
	}
	_, _ = fmt.Fprintln(c.w, c.bold(header))

	for _, check := range auditCheckOrder {
		findings := make([]auditFinding, 0, len(report.Findings))
		for _, finding := range report.Findings {
			if finding.Check == check {
				findings = append(findings, finding)
			}
		}
		if len(findings) == 0 {
			c.ok("%s: ok", auditCheckLabel[check])
			continue
		}
		for _, finding := range findings {
			label := finding.Tool
			if label == "" {
				label = auditCheckLabel[check]
			}
			switch finding.Severity {
			case "error":
				c.fail("%s: %s", label, finding.Message)
			case "warning":
				c.warn("%s: %s", label, finding.Message)
			default:
				c.skip("%s: %s", label, finding.Message)
			}
			if finding.Remediation != "" {
				_, _ = fmt.Fprintf(c.w, "      fix: %s\n", finding.Remediation)
			}
		}
	}

	counts := []string{
		plural(report.Summary.Errors, "error"),
		plural(report.Summary.Warnings, "warning"),
	}
	if report.Summary.Info > 0 {
		counts = append(counts, fmt.Sprintf("%d info", report.Summary.Info))
	}
	_, _ = fmt.Fprintln(c.w, c.dim(strings.Join(counts, " · ")))
}

// classifyAuditLoadError maps a project load failure to a finding and exit
// code. Missing or invalid schemas are configuration errors (2); anything else
// is an environment failure (3).
func classifyAuditLoadError(err error) (auditFinding, int) {
	var missing *schemaNotFoundError
	if errors.As(err, &missing) || errors.Is(err, os.ErrNotExist) {
		return auditFinding{
			Severity:    "error",
			Code:        "E_SCHEMA_NOT_FOUND",
			Message:     err.Error(),
			Remediation: "pass --schema or run 'depengine init'",
		}, 2
	}
	var schemaErr *config.ParseSchemaError
	var codeErr *config.SchemaCodeError
	var catalogErr *config.CatalogError
	if errors.As(err, &schemaErr) || errors.As(err, &codeErr) || errors.As(err, &catalogErr) {
		return auditFinding{
			Severity:    "error",
			Code:        "E_SCHEMA_INVALID",
			Message:     err.Error(),
			Remediation: "run 'depengine validate' for field-level diagnostics",
		}, 2
	}
	return auditFinding{
		Severity:    "error",
		Code:        "E_RUNTIME",
		Message:     err.Error(),
		Remediation: "run 'depengine doctor' to inspect the host",
	}, 3
}
