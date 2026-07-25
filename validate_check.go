package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/validate"
)

func runValidate(args []string) {
	// flags maintained in help.go:printCommandHelp
	validateCmd := flag.NewFlagSet("validate", flag.ExitOnError)
	validateSchema := validateCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	validateManifest := validateCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	validateNoManifest := validateCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	validateCheckEnv := validateCmd.Bool("check-env", false, "check system environment for required tools")
	validateFormat := validateCmd.String("format", "text", "output format: text or json")
	validateStrict := validateCmd.Bool("strict", false, "treat warnings as errors")
	validateCmd.Parse(args)

	ctx := context.Background()

	s, err := config.ParseSchema(*validateSchema, map[string]string{})
	if err != nil {
		var sce *config.SchemaCodeError
		if errors.As(err, &sce) && *validateFormat == "json" {
			type jsonErr struct {
				Code    string `json:"code"`
				Field   string `json:"field"`
				Message string `json:"message"`
			}
			out := struct {
				Errors   []jsonErr `json:"errors"`
				Warnings []any     `json:"warnings"`
			}{
				Errors: []jsonErr{{
					Code:    string(sce.Code),
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
					Code:    "W_ENV_MISSING",
					Field:   "environment",
					Message: ch.Message,
				})
			}
		}
	}

	if *validateFormat == "json" {
		type jsonOutput struct {
			Errors   []validate.ValidationError `json:"errors"`
			Warnings []validate.ValidationError `json:"warnings"`
		}
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
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "error: %v\n", e)
		}
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %v\n", w)
		}
		if len(result.Errors) == 0 {
			fmt.Fprintf(os.Stderr, "✓ schema is valid\n")
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

	// flags maintained in help.go:printCommandHelp
func runCheck(args []string) {
	checkCmd := flag.NewFlagSet("check", flag.ExitOnError)
	checkSchema := checkCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	checkManifest := checkCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	checkNoManifest := checkCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	checkJSON := checkCmd.Bool("json", false, "JSON output")
	checkFormat := checkCmd.String("format", "", "output format (json)")
	checkLive := checkCmd.Bool("live", false, "check via adapter (may run subprocesses)")
	checkCmd.Parse(args)
	remain := checkCmd.Args()
	if len(remain) < 1 {
		log.Default.Error("usage: depengine check <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

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
				fmt.Fprintf(os.Stderr, "✓ %s is installed (via %s)\n", toolName, method.Kind)
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
		fmt.Fprintf(os.Stderr, "✗ %s is not installed\n", toolName)
	}
	os.Exit(1)
}
