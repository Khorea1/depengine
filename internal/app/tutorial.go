package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
	"github.com/spf13/cobra"
)

const tutorialSchema = `schema_version = 1

[tools.hello.http]
url = "https://example.com/hello-1.0.0.tar.gz"
checksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
`

// tutorialRunCommand executes one tutorial step. Production wires it to a
// subprocess of the current binary; tests substitute a fake.
type tutorialRunCommand func(ctx context.Context, env map[string]string, args []string) run.Result

type tutorialStep struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output"`
	Status  string `json:"status"`
}

type tutorialReport struct {
	Schema    string         `json:"schema"`
	Directory string         `json:"directory"`
	Steps     []tutorialStep `json:"steps"`
}

// newTutorialCmd builds `depengine tutorial`: a guided, read-only tour in a
// disposable directory.
func newTutorialCmd() *cobra.Command {
	keep := new(bool)
	format := new(string)
	jsonFlag := new(bool)

	cmd := &cobra.Command{
		Use:   "tutorial",
		Short: ifPT("Tour guiado em diretório descartável", "Guided tour in a disposable directory"),
		Long:  ifPT("Cria um projeto temporário, mostra um schema, roda validate/why/update/install --dry-run explicando cada saída e remove tudo no final. Nenhum passo muta o host.", "Creates a temporary project, shows a schema, runs validate/why/update/install --dry-run with an explanation of each output, and removes everything at the end. No step mutates the host."),
		Example: `  depengine tutorial             # guided tour, cleans up afterwards
  depengine tutorial --keep      # keep the temporary project for inspection
  depengine tutorial --json      # machine-readable steps`,
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(format, jsonFlag); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			exe, err := os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: locate the depengine binary: %v\n", err)
				return exitWithCode(3)
			}
			runner := run.OSExecRunner{}
			runCommand := func(ctx context.Context, env map[string]string, argv []string) run.Result {
				return runner.RunWithEnv(ctx, env, nil, exe, argv...)
			}
			return runTutorial(cmd.Context(), tutorialOptions{
				Keep: *keep,
				JSON: *format == "json",
				Run:  runCommand,
			})
		},
	}
	cmd.Flags().BoolVar(keep, "keep", false, "keep the temporary project directory")
	cmd.Flags().StringVar(format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(jsonFlag, "json", false, "shorthand for --format=json")
	return cmd
}

type tutorialOptions struct {
	Keep bool
	JSON bool
	Run  tutorialRunCommand
}

// runTutorial builds the disposable project and walks the read-only commands.
func runTutorial(ctx context.Context, opts tutorialOptions) error {
	dir, err := os.MkdirTemp("", "depengine-tutorial-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create tutorial directory: %v\n", err)
		return exitWithCode(3)
	}
	schemaPath := dir + string(os.PathSeparator) + "schema.toml"
	if err := os.WriteFile(schemaPath, []byte(tutorialSchema), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "error: write tutorial schema: %v\n", err)
		return exitWithCode(3)
	}

	env := map[string]string{
		"XDG_STATE_HOME": dir + string(os.PathSeparator) + "state",
		// Keep the tour deterministic regardless of the caller's language.
		"DEPENGINE_LANG": "en",
	}
	common := []string{"--schema", schemaPath, "--no-manifest", "--no-catalog"}

	report := tutorialReport{Schema: schemaPath, Directory: dir}
	steps := []struct {
		name    string
		title   string
		command []string
	}{
		{name: "schema", title: "Write a schema that declares one HTTP tool with a fixed checksum"},
		{name: "validate", title: "Validate the declaration", command: append([]string{"validate"}, common...)},
		{name: "why", title: "See which method would win", command: append([]string{"why", "hello"}, common...)},
		{name: "update", title: "Preview the lock without writing it", command: append([]string{"update", "--dry-run"}, common...)},
		{name: "install", title: "Preview the install without touching the host", command: append([]string{"install", "--dry-run"}, common...)},
	}

	failed := false
	for _, step := range steps {
		entry := tutorialStep{Name: step.name, Title: step.title, Status: "ok"}
		if step.name == "schema" {
			entry.Output = tutorialSchema
		} else {
			entry.Command = strings.ReplaceAll("depengine "+strings.Join(step.command, " "), dir, "<tutorial>")
			res := opts.Run(ctx, env, step.command)
			entry.Output = tutorialDisplayOutput(res, dir)
			if res.Err != nil || res.ExitCode != 0 {
				entry.Status = fmt.Sprintf("failed (exit %d)", res.ExitCode)
				failed = true
			}
		}
		report.Steps = append(report.Steps, entry)
		if failed {
			break
		}
	}

	if opts.Keep {
		report.Schema = schemaPath
		report.Directory = dir
	} else {
		report.Schema = "<tutorial>/schema.toml"
		report.Directory = "<tutorial>"
	}
	if !opts.Keep {
		_ = os.RemoveAll(dir)
	}

	if opts.JSON {
		if err := writeMachineOutput(os.Stdout, "tutorial", report); err != nil {
			return exitWithCode(3)
		}
	} else {
		renderTutorialText(report, opts.Keep)
	}
	if failed {
		return exitWithCode(1)
	}
	return nil
}

// tutorialDisplayOutput merges the step output and hides the random temp path
// so repeated runs read the same.
func tutorialDisplayOutput(res run.Result, dir string) string {
	combined := string(res.Stdout) + string(res.Stderr)
	return strings.ReplaceAll(combined, dir, "<tutorial>")
}

func renderTutorialText(report tutorialReport, kept bool) {
	c := newCLIStyle(os.Stdout)
	_, _ = fmt.Fprintln(c.w, c.bold("depengine tutorial — a safe, disposable project"))
	for i, step := range report.Steps {
		_, _ = fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Step %d/%d · %s", i+1, len(report.Steps), step.Title)))
		if step.Command != "" {
			_, _ = fmt.Fprintf(c.w, "%s\n", c.dim("$ "+step.Command))
		}
		_, _ = fmt.Fprintln(c.w, strings.TrimRight(step.Output, "\n"))
		if step.Status != "ok" {
			c.fail("%s", step.Status)
		}
	}
	_, _ = fmt.Fprintln(c.w)
	if kept {
		_, _ = fmt.Fprintf(c.w, "Kept the project at %s (remove it when done).\n", report.Directory)
	} else {
		_, _ = fmt.Fprintln(c.w, "The temporary project was removed; nothing on the host changed.")
	}
	_, _ = fmt.Fprintln(c.w, c.dim("Next: 'depengine init' for your own project, or 'depengine audit --json' in CI."))
}
