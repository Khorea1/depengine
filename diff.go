package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newDiffCmd builds `depengine diff`.
func newDiffCmd() *cobra.Command {
	diffOther := new(string)
	diffJSON := new(bool)

	cmd := &cobra.Command{
		Use:     "diff [file1] [file2]",
		Short:   ifPT("Comparar dois arquivos de estado", "Compare two state files"),
		GroupID: groupInspect,
		Args:    cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			runDiff(args, diffOther, diffJSON)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(diffOther, "other", "", "path to other state file (used when no args)")
	f.BoolVar(diffJSON, "json", false, "output as JSON")
	return cmd
}

// runDiff compares two state files and outputs the differences. Body
// unchanged from the pre-Cobra version — diffArgs is now the positional
// args Cobra already separated out, and cobra.MaximumNArgs(2) replaces the
// old `default:` branch of the length switch below.
func runDiff(diffArgs []string, diffOther *string, diffJSON *bool) {
	var aPath, bPath string
	var aState, bState *state.State
	var ls *state.LockedState
	var err error

	switch len(diffArgs) {
	case 0:
		if *diffOther == "" {
			fmt.Fprintf(os.Stderr, "error: --other is required when no arguments are given\n")
			os.Exit(2)
		}
		bPath = *diffOther
	case 1:
		bPath = diffArgs[0]
	case 2:
		aPath = diffArgs[0]
		bPath = diffArgs[1]
		aState, err = state.LoadFrom(aPath)
		if err != nil {
			log.Default.Error("load first state", "path", aPath, "error", err)
			os.Exit(3)
		}
		bState, err = state.LoadFrom(bPath)
		if err != nil {
			log.Default.Error("load second state", "path", bPath, "error", err)
			os.Exit(3)
		}
	}

	if len(diffArgs) != 2 {
		ls, err = state.LoadShared()
		if err != nil {
			log.Default.Error("load current state", "error", err)
			os.Exit(3)
		}
		defer ls.Close()
		aState = ls.State()
		bState, err = state.LoadFrom(bPath)
		if err != nil {
			log.Default.Error("load other state", "path", bPath, "error", err)
			closeStateAndExit(ls, 3)
		}
	}

	items := state.Diff(aState, bState)
	if len(items) == 0 {
		if *diffJSON {
			fmt.Println("[]")
		} else {
			fmt.Fprintln(os.Stderr, "No differences found.")
		}
		return
	}

	if *diffJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			log.Default.Error("encode JSON", "error", err)
			closeStateAndExit(ls, 3)
		}
	} else {
		c := newCLIStyle(os.Stderr)
		var onlyA, onlyB, diffCount int
		for _, item := range items {
			switch item.Side {
			case "only_a":
				onlyA++
			case "only_b":
				onlyB++
			case "different":
				diffCount++
			}
		}

		// Section headers are bold, counts live in the header line, and each
		// entry is one aligned line — a diff is scanned section-first, so the
		// header must carry the "is there anything here" answer by itself.
		if onlyA > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Only in current (%d)", onlyA)))
			for _, item := range items {
				if item.Side == "only_a" {
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("+"), item.Name, c.dim(fmt.Sprintf("%s, %s", item.MethodA, item.InstalledAtA)))
				}
			}
		}

		if onlyB > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Only in other (%d)", onlyB)))
			for _, item := range items {
				if item.Side == "only_b" {
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.red("-"), item.Name, c.dim(fmt.Sprintf("%s, %s", item.MethodB, item.InstalledAtB)))
				}
			}
		}

		if diffCount > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Definition changed (%d)", diffCount)))
			for _, item := range items {
				if item.Side == "different" {
					fmt.Fprintf(c.w, "  %s %s\n", c.yellow("~"), item.Name)
					fmt.Fprintf(c.w, "    %s %s %s\n", c.dim("current:"), item.MethodA, c.dim(fmt.Sprintf("(hash: %s)", item.HashA)))
					fmt.Fprintf(c.w, "    %s %s %s\n", c.dim("other:  "), item.MethodB, c.dim(fmt.Sprintf("(hash: %s)", item.HashB)))
				}
			}
		}

		fmt.Fprintf(c.w, "\n%s\n", c.dim(fmt.Sprintf("%s differ.", plural(len(items), "tool"))))
	}
}
