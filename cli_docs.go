package main

//go:generate env LANGUAGE=C LC_ALL=C UPDATE_CLI_DOCS=1 go test . -run ^TestCLIDocsGolden$ -count=1

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	markdownDocsPath = "docs/cli-reference.md"
	manDocsPath      = "docs/depengine.1"
	markdownStart    = "<!-- BEGIN GENERATED CLI REFERENCE -->"
	markdownEnd      = "<!-- END GENERATED CLI REFERENCE -->"
	manStart         = `.\" BEGIN GENERATED CLI REFERENCE`
	manEnd           = `.\" END GENERATED CLI REFERENCE`
)

type cliCommandDoc struct {
	Path        string
	UseLine     string
	Description string
	Aliases     []string
	Hidden      bool
	Deprecated  string
	Flags       []cliFlagDoc
}

type cliFlagDoc struct {
	Name       string
	Shorthand  string
	Usage      string
	Default    string
	Type       string
	Scope      string
	Hidden     bool
	Deprecated string
}

// generateCLIDocs renders both formats from one snapshot of the Cobra tree.
func generateCLIDocs(root *cobra.Command) ([]byte, []byte, error) {
	docs := normalizeCLI(root)
	markdown, err := renderCLIReferenceDocs(docs)
	if err != nil {
		return nil, nil, err
	}
	man, err := renderManPageDocs(docs)
	if err != nil {
		return nil, nil, err
	}
	return markdown, man, nil
}

// documentedCommands returns Cobra's stable command order, including the root,
// generated completion tree, help command, and the compatibility version command.
func documentedCommands(root *cobra.Command) []*cobra.Command {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	commands := []*cobra.Command{root}
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Hidden && cmd.Name() != "version" {
				continue
			}
			commands = append(commands, cmd)
			walk(cmd)
		}
	}
	walk(root)
	return commands
}

func normalizeCLI(root *cobra.Command) []cliCommandDoc {
	commands := documentedCommands(root)
	docs := make([]cliCommandDoc, 0, len(commands))
	for _, cmd := range commands {
		cmd.InitDefaultHelpFlag()
		docs = append(docs, cliCommandDoc{
			Path:        cmd.CommandPath(),
			UseLine:     cmd.UseLine(),
			Description: commandDescription(cmd),
			Aliases:     append([]string(nil), cmd.Aliases...),
			Hidden:      cmd.Hidden,
			Deprecated:  cmd.Deprecated,
			Flags:       commandFlags(cmd),
		})
	}
	return docs
}

func commandDescription(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return strings.TrimSpace(cmd.Long)
	}
	return strings.TrimSpace(cmd.Short)
}

func commandFlags(cmd *cobra.Command) []cliFlagDoc {
	seen := make(map[string]bool)
	var flags []cliFlagDoc
	collect := func(scope string, set *pflag.FlagSet) {
		set.VisitAll(func(flag *pflag.Flag) {
			if seen[flag.Name] {
				return
			}
			seen[flag.Name] = true
			flags = append(flags, cliFlagDoc{
				Name: flag.Name, Shorthand: flag.Shorthand, Usage: flag.Usage,
				Default: flag.DefValue, Type: flag.Value.Type(), Scope: scope,
				Hidden: flag.Hidden, Deprecated: flag.Deprecated,
			})
		})
	}
	collect("local", cmd.LocalNonPersistentFlags())
	collect("persistent", cmd.PersistentFlags())
	collect("inherited", cmd.InheritedFlags())
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

func renderCLIReferenceDocs(docs []cliCommandDoc) ([]byte, error) {
	var generated strings.Builder
	for _, cmd := range docs {
		fmt.Fprintf(&generated, "## `%s`\n\n", markdownEscape(cmd.UseLine))
		if cmd.Description != "" {
			generated.WriteString(markdownEscape(cmd.Description) + "\n\n")
		}
		if len(cmd.Aliases) > 0 {
			fmt.Fprintf(&generated, "Aliases: `%s`.\n\n", strings.Join(cmd.Aliases, "`, `"))
		}
		if cmd.Hidden {
			generated.WriteString("Visibility: hidden (documented compatibility command).\n\n")
		}
		if cmd.Deprecated != "" {
			fmt.Fprintf(&generated, "Deprecated: %s\n\n", markdownEscape(cmd.Deprecated))
		}
		flags := visibleFlags(cmd.Flags)
		if len(flags) == 0 {
			continue
		}
		generated.WriteString("| Flag | Scope | Default | Description |\n|---|---|---|---|\n")
		for _, flag := range flags {
			fmt.Fprintf(&generated, "| `%s` | %s | `%s` | %s |\n",
				markdownFlag(flag), flag.Scope, markdownEscape(flag.Default), markdownFlagDescription(flag))
		}
		generated.WriteByte('\n')
	}
	return replaceGenerated(markdownDocsPath, markdownStart, markdownEnd, generated.String())
}

func renderManPageDocs(docs []cliCommandDoc) ([]byte, error) {
	var generated strings.Builder
	generated.WriteString(".SH COMMANDS\n")
	for _, cmd := range docs {
		generated.WriteString(".TP\n.B \"")
		generated.WriteString(roffEscape(cmd.UseLine))
		generated.WriteString("\"\n")
		generated.WriteString(roffText(cmd.Description) + "\n")
		if len(cmd.Aliases) > 0 {
			generated.WriteString("Aliases: " + roffText(strings.Join(cmd.Aliases, ", ")) + ".\n")
		}
		if cmd.Hidden {
			generated.WriteString("Hidden compatibility command.\n")
		}
		if cmd.Deprecated != "" {
			generated.WriteString("Deprecated: " + roffText(cmd.Deprecated) + "\n")
		}
	}
	generated.WriteString(".SH OPTIONS\n")
	for _, cmd := range docs {
		flags := visibleFlags(cmd.Flags)
		if len(flags) == 0 {
			continue
		}
		fmt.Fprintf(&generated, ".SS \"%s flags\"\n", roffEscape(cmd.Path))
		for _, flag := range flags {
			generated.WriteString(".TP\n.B \"")
			generated.WriteString(roffEscape(plainFlag(flag)))
			generated.WriteString("\"\n")
			fmt.Fprintf(&generated, "%s Scope: %s. Default: %s.%s\n",
				roffText(flag.Usage), flag.Scope, roffText(flag.Default), roffFlagState(flag))
		}
	}
	return replaceGenerated(manDocsPath, manStart, manEnd, generated.String())
}

func visibleFlags(flags []cliFlagDoc) []cliFlagDoc {
	visible := make([]cliFlagDoc, 0, len(flags))
	for _, flag := range flags {
		if !flag.Hidden {
			visible = append(visible, flag)
		}
	}
	return visible
}

func markdownFlag(flag cliFlagDoc) string {
	value := "--" + flag.Name
	if flag.Shorthand != "" {
		value = "-" + flag.Shorthand + ", " + value
	}
	if flag.Type != "bool" {
		value += " <" + flag.Type + ">"
	}
	return value
}

func plainFlag(flag cliFlagDoc) string { return markdownFlag(flag) }

func markdownFlagDescription(flag cliFlagDoc) string {
	description := markdownEscape(flag.Usage)
	if flag.Hidden {
		description += " **Hidden.**"
	}
	if flag.Deprecated != "" {
		description += " **Deprecated:** " + markdownEscape(flag.Deprecated)
	}
	return description
}

func roffFlagState(flag cliFlagDoc) string {
	var state string
	if flag.Hidden {
		state += " Hidden."
	}
	if flag.Deprecated != "" {
		state += " Deprecated: " + roffText(flag.Deprecated) + "."
	}
	return state
}

func markdownEscape(value string) string {
	value = strings.ReplaceAll(value, "|", `\|`)
	return strings.ReplaceAll(value, "\n", " ")
}

func roffEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\e`)
	value = strings.ReplaceAll(value, "-", `\-`)
	if strings.HasPrefix(value, ".") || strings.HasPrefix(value, "'") {
		value = `\&` + value
	}
	return value
}

func roffText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return roffEscape(value)
}

func replaceGenerated(path, start, end, generated string) ([]byte, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	text := string(contents)
	startAt := strings.Index(text, start)
	endAt := strings.Index(text, end)
	if startAt < 0 || endAt < 0 || endAt < startAt {
		return nil, fmt.Errorf("%s: missing or invalid generated-section markers", path)
	}
	endAt += len(end)
	var output bytes.Buffer
	output.WriteString(text[:startAt])
	output.WriteString(start)
	output.WriteByte('\n')
	output.WriteString(generated)
	output.WriteString(end)
	output.WriteString(text[endAt:])
	return output.Bytes(), nil
}
