package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/config"
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
		Short:   ifPT("Verificar se uma ferramenta atende ao estado desejado", "Check whether a tool satisfies desired state"),
		Long:    ifPT("Reconcilia a identidade observada no host com o plano resolvido. Só 'satisfied' retorna sucesso; absent, drifted, unknown e broken retornam código de falha.", "Reconciles observed host identity with the resolved plan. Only 'satisfied' exits successfully; absent, drifted, unknown, and broken return a failure code."),
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

type checkOutput struct {
	Tool         string                  `json:"tool"`
	Method       string                  `json:"method,omitempty"`
	Status       plan.VerificationState  `json:"status"`
	Verification plan.VerificationResult `json:"verification"`
}

// runCheck reports whether a single tool satisfies its resolved desired state.
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

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(*checkSchema, manifestPath)
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

	ex := exec.New()
	exec.WithRunner(run.OSExecRunner{})(ex)
	exec.WithFacts(facts)(ex)
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	checked, checkErr := ex.CheckDesiredState(ctx, tool, clan, *checkLive)
	if checkErr != nil {
		checked.Verification = plan.VerificationResult{State: plan.StateBroken, Detail: checkErr.Error()}
	}
	if useJSON {
		_ = json.NewEncoder(os.Stdout).Encode(checkOutput{Tool: toolName, Method: checked.Method, Status: checked.Verification.State, Verification: checked.Verification})
	} else if checked.Verification.State == plan.StateSatisfied {
		newCLIStyle(os.Stderr).ok("%s satisfies desired state (via %s)", toolName, checked.Method)
	} else {
		newCLIStyle(os.Stderr).fail("%s desired state is %s: %s", toolName, checked.Verification.State, checked.Verification.Detail)
	}
	if checkErr == nil && checked.Verification.State == plan.StateSatisfied {
		return nil
	}
	return exitWithCode(1)
}
