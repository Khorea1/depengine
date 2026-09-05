package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/validate"
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
		RunE: func(_ *cobra.Command, args []string) error {
			runValidate(validateSchema, validateManifest, validateNoManifest, validateCheckEnv, validateFormat, validateStrict)
			return nil
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

func runValidate(validateSchema, validateManifest *string, validateNoManifest, validateCheckEnv *bool, validateFormat *string, validateStrict *bool) {
	ctx := context.Background()

	s, err := config.ParseSchema(*validateSchema, map[string]string{})
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
				os.Exit(3)
			}
			os.Exit(2)
		}
		// Fallback: plain text on stderr (existing behavior)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
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
		manifestSchema, merr := config.ParseSchema(manifestPath, nil, "packages")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", merr)
			os.Exit(2)
		}
		config.FilterManifestTools(s, manifestSchema)
		if gerr := config.ValidateManifestLayer(manifestSchema); gerr != nil {
			fmt.Fprintf(os.Stderr, "error validating manifest: %v\n", gerr)
			os.Exit(2)
		}
		count := len(manifestSchema.Tools)
		if count > 0 {
			s = config.MergeLayers(manifestSchema, s)
			if manifestAuto {
				fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
			}
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
			os.Exit(3)
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
	os.Exit(exitCode)
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
		RunE: func(_ *cobra.Command, args []string) error {
			runCheck(args[0], checkSchema, checkManifest, checkNoManifest, checkJSON, checkFormat, checkLive)
			return nil
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
func runCheck(toolName string, checkSchema, checkManifest *string, checkNoManifest, checkJSON *bool, checkFormat *string, checkLive *bool) {
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
		os.Exit(exitCodeForError(err))
	}
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}

	tool, ok := s.Tools[toolName]
	if !ok {
		log.Default.Error("tool not found", "tool", toolName)
		os.Exit(1)
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
		if !*checkLive && !adapter.Available(context.Background(), run.OSExecRunner{}) {
			continue
		}
		if adapter.Check(context.Background(), run.OSExecRunner{}, tool, method) {
			if useJSON {
				json.NewEncoder(os.Stdout).Encode(map[string]string{
					"tool":   toolName,
					"status": "installed",
					"method": method.Kind,
				})
			} else {
				newCLIStyle(os.Stderr).ok("%s is installed (via %s)", toolName, method.Kind)
			}
			os.Exit(0)
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
	os.Exit(1)
}
