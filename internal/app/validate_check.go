package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/validate"
	"github.com/spf13/cobra"
)

type jsonOutput struct {
	Errors   []validate.ValidationError `json:"errors"`
	Warnings []validate.ValidationError `json:"warnings"`
}

// newValidateCmd builds `depengine validate`.
func newValidateCmd() *cobra.Command {
	validateSchema := new(string)
	validateManifest := new(string)
	validateNoManifest := new(bool)
	validateCheckEnv := new(bool)
	validateFormat := new(string)
	validateStrict := new(bool)

	cmd := &cobra.Command{
		Use:     "validate",
		Short:   ifPT("Validar schema.toml", "Validate schema.toml"),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.Context(), validateSchema, validateManifest, validateNoManifest, validateCheckEnv, validateFormat, validateStrict)
		},
	}
	f := cmd.Flags()
	f.StringVar(validateSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(validateManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(validateNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.BoolVar(validateCheckEnv, "check-env", false, "check system environment for required tools")
	f.StringVar(validateFormat, "format", "text", "output format: text or json")
	f.BoolVar(validateStrict, "strict", false, "treat warnings as errors")
	return cmd
}

func runValidate(ctx context.Context, validateSchema, validateManifest *string, validateNoManifest, validateCheckEnv *bool, validateFormat *string, validateStrict *bool) error {

	s, err := config.ParseProjectSchema(*validateSchema, map[string]string{})
	if err != nil {
		var sce *config.SchemaCodeError
		if errors.As(err, &sce) && *validateFormat == "json" {
			out := jsonOutput{
				Errors: []validate.ValidationError{{
					Code:    validate.ErrorCode(sce.Code),
					Field:   "tools",
					Message: sce.Msg,
				}},
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(out); err != nil {
				fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
				return exitWithCode(3)
			}
			return exitWithCode(2)
		}
		// Fallback: plain text on stderr (existing behavior)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}

	noManifest := *validateNoManifest
	manifestPath := *validateManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}
	if manifestPath != "" {
		var count int
		s, count, err = mergeManifest(s, manifestPath, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", err)
			return exitWithCode(2)
		}
		if count > 0 && manifestAuto {
			fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
		}
	}

	knownKinds := exec.RegisteredKinds()
	result := validate.ValidateSchema(s, knownKinds)

	if *validateCheckEnv {
		envResult := validate.CheckEnv(ctx, run.OSExecRunner{})
		for _, ch := range envResult.Checks {
			if !ch.Found {
				result.Add(validate.ValidationError{
					Code:    validate.WarnEnvMissing,
					Field:   "environment",
					Message: ch.Message,
				})
			}
		}
	}

	if *validateFormat == "json" {
		out := jsonOutput{
			Errors:   result.Errors,
			Warnings: result.Warnings,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
			return exitWithCode(3)
		}
	} else {
		c := newCLIStyle(os.Stderr)
		if len(result.Errors) == 0 && len(result.Warnings) == 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.green("✓ schema is valid"))
		} else {
			if len(result.Errors) == 0 {
				// Warnings without errors still mean the schema is usable —
				// say so explicitly instead of leaving the ✗/⚠ section as
				// the only closing signal.
				fmt.Fprintf(c.w, "\n%s\n", c.green("✓ schema is valid"))
			}
			if len(result.Errors) > 0 {
				fmt.Fprintf(c.w, "\n%s\n", c.red(fmt.Sprintf("✗ %d error(s)", len(result.Errors))))
				for _, e := range result.Errors {
					// [E_CODE] field — message; the code is what to grep for,
					// the field is where to fix.
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.red(string(e.Code)), c.dim(string(e.Field)+":"), e.Message)
				}
			}
			if len(result.Warnings) > 0 {
				fmt.Fprintf(c.w, "\n%s\n", c.yellow(fmt.Sprintf("⚠ %d warning(s)", len(result.Warnings))))
				for _, w := range result.Warnings {
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow(string(w.Code)), c.dim(string(w.Field)+":"), w.Message)
				}
			}
		}
	}

	exitCode := 0
	if result.HasErrors() {
		exitCode = 2
	} else if *validateStrict && len(result.Warnings) > 0 {
		exitCode = 1
	}
	return exitWithCode(exitCode)
}

// newCheckCmd builds `depengine check`.
func newCheckCmd() *cobra.Command {
	checkSchema := new(string)
	checkManifest := new(string)
	checkNoManifest := new(bool)
	checkJSON := new(bool)
	checkFormat := new(string)
	checkLive := new(bool)

	cmd := &cobra.Command{
		Use:     "check <tool>",
		Short:   ifPT("Verificar se uma ferramenta está instalada", "Check whether a tool is installed"),
		GroupID: groupInspect,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd.Context(), args[0], checkSchema, checkManifest, checkNoManifest, checkJSON, checkFormat, checkLive)
		},
	}
	f := cmd.Flags()
	f.StringVar(checkSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(checkManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(checkNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.BoolVar(checkJSON, "json", false, "JSON output")
	f.StringVar(checkFormat, "format", "", "output format (json)")
	f.BoolVar(checkLive, "live", false, "check via adapter (may run subprocesses)")
	return cmd
}

// runCheck reports whether a single tool is installed. Body unchanged from
// the pre-Cobra version — Cobra's cobra.ExactArgs(1) now enforces the
// argument count that the old manual length check did, and toolName arrives
// as a plain argument instead of remain[0].
func runCheck(ctx context.Context, toolName string, checkSchema, checkManifest *string, checkNoManifest, checkJSON *bool, checkFormat *string, checkLive *bool) error {
	noManifest := *checkNoManifest
	manifestPath := *checkManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	s, clan, _, manifestCount, err := loadSchemaWithManifest(*checkSchema, manifestPath)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		return exitWithCode(exitCodeForError(err))
	}
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}

	tool, ok := s.Tools[toolName]
	if !ok {
		log.Default.Error("tool not found", "tool", toolName)
		return exitWithCode(1)
	}
	useJSON := *checkJSON || *checkFormat == "json"

	for _, method := range tool.Methods {
		if method.When != nil && len(method.When.DistroFamily) > 0 {
			if !engine.MatchesDistroFamily(clan, method.When.DistroFamily) {
				continue
			}
		}
		adapter := exec.Lookup(method.Kind)
		if adapter == nil {
			continue
		}
		if !*checkLive && !adapter.Available(ctx, run.OSExecRunner{}) {
			continue
		}
		if checkAdapterInstalled(ctx, adapter, tool, method) {
			if useJSON {
				json.NewEncoder(os.Stdout).Encode(map[string]string{
					"tool":   toolName,
					"status": "installed",
					"method": method.Kind,
				})
			} else {
				newCLIStyle(os.Stderr).ok("%s is installed (via %s)", toolName, method.Kind)
			}
			return nil
		}
	}
	if useJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]string{
			"tool":   toolName,
			"status": "not-installed",
		})
	} else {
		newCLIStyle(os.Stderr).fail("%s is not installed", toolName)
	}
	return exitWithCode(1)
}

func checkAdapterInstalled(ctx context.Context, adapter exec.Adapter, tool *config.Tool, method *config.MethodCandidate) bool {
	if observer, ok := adapter.(exec.AdapterV2); ok {
		return checkLiveAdapterV2(ctx, observer, tool, method)
	}
	return adapter.Check(ctx, run.OSExecRunner{}, tool, method)
}

// checkLiveAdapterV2 preserves the direct check command's probe-only
// semantics while honoring the plan-aware adapter contract. Resolution is
// performed once before observation; no executor or install path is invoked.
func checkLiveAdapterV2(ctx context.Context, adapter exec.AdapterV2, tool *config.Tool, method *config.MethodCandidate) bool {
	intent, err := exec.CandidatePlanIntent(tool, method)
	if err != nil {
		return false
	}
	resolved, err := adapter.ResolvePlan(ctx, run.OSExecRunner{}, tool, method, intent)
	if err != nil || resolved == nil {
		return false
	}
	observation, err := adapter.Observe(ctx, run.OSExecRunner{}, tool, method)
	if err != nil {
		return false
	}
	return observation.Presence == plan.PresencePresent
}
