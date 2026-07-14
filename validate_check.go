package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"depengine/pkg/engine"
	"depengine/pkg/exec"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/validate"
)

func runValidate(args []string) {
	validateCmd := flag.NewFlagSet("validate", flag.ExitOnError)
	validateSchema := validateCmd.String("schema", "schema.toml", "path to schema.toml")
	validateCheckEnv := validateCmd.Bool("check-env", false, "check system environment for required tools")
	validateFormat := validateCmd.String("format", "text", "output format: text or json")
	validateStrict := validateCmd.Bool("strict", false, "treat warnings as errors")
	validateCmd.Parse(args)

	ctx := context.Background()

	s, err := schema.ParseSchema(*validateSchema, map[string]string{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	knownKinds := exec.RegisteredKinds()
	result := validate.ValidateSchema(s)

	verr, warnings := schema.Validate(s, knownKinds)
	if verr != nil {
		result.Add(validate.ValidationError{
			Code:    "E_UNKNOWN_METHOD",
			Field:   "tools",
			Message: verr.Error(),
		})
	}
	for _, w := range warnings {
		result.Add(validate.ValidationError{
			Code:    "W_UNKNOWN_METHOD",
			Field:   "tools",
			Message: w,
		})
	}

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

func runCheck(args []string) {
	checkCmd := flag.NewFlagSet("check", flag.ExitOnError)
	checkSchema := checkCmd.String("schema", "schema.toml", "path to schema.toml")
	checkCmd.Parse(args)
	remain := checkCmd.Args()
	if len(remain) < 1 {
		log.Default.Error("usage: depengine check <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

	s, clan, _, err := loadSchema(*checkSchema)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}

	tool, ok := s.Tools[toolName]
	if !ok {
		log.Default.Error("tool not found", "tool", toolName)
		os.Exit(1)
	}

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
		if !adapter.Available(context.Background(), run.OSExecRunner{}) {
			continue
		}
		if adapter.Check(context.Background(), run.OSExecRunner{}, tool, method) {
			fmt.Printf("✓ %s is installed (via %s)\n", toolName, method.Kind)
			os.Exit(0)
		}
	}
	fmt.Printf("✗ %s is not installed\n", toolName)
	os.Exit(1)
}
