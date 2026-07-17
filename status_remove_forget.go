package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"depengine/pkg/exec"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/state"
)

// runStatus shows the installation status of tools by comparing the state
// file against the schema. It reports installed, missing, and outdated tools.
func runStatus(args []string) {
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusSchema := statusCmd.String("schema", "", "override schema path")
	statusFormat := statusCmd.String("format", "text", "output format: text or json")
	statusJSON := statusCmd.Bool("json", false, "JSON output (shorthand for --format=json)")
	statusOrphans := statusCmd.Bool("orphans", false, "show only orphaned tools")
	statusCmd.Parse(args)

	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	schemaPath := st.SchemaPath
	if *statusSchema != "" {
		schemaPath = *statusSchema
	}
	if schemaPath == "" {
		if len(st.Tools) == 0 {
			fmt.Fprintln(os.Stderr, "No tools in state (nothing installed yet). Use --schema to compare against a schema.")
			return
		}
	}

	var s *schema.Schema
	if schemaPath != "" {
		var err error
		s, err = schema.ParseSchemaNoFacts(schemaPath)
		if err != nil {
			log.Default.Warn("load schema for comparison", "error", err)
			s = nil
		}
	}

	type toolStatus struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Method  string `json:"method,omitempty"`
		Updated string `json:"updated,omitempty"`
	}

	var tools []toolStatus

	for name, ts := range st.Tools {
		status := "installed"
		if s != nil {
			if _, inSchema := s.Tools[name]; !inSchema {
				status = "orphaned"
			}
		}
		if *statusOrphans && status != "orphaned" {
			continue
		}
		if status == "installed" && s != nil && !*statusOrphans && ts.DefinitionHash != "" {
			if stTool, inSchema := s.Tools[name]; inSchema {
				if state.DefinitionHash(stTool) != ts.DefinitionHash {
					status = "outdated"
				}
			}
		}
		tools = append(tools, toolStatus{
			Name:    name,
			Status:  status,
			Method:  ts.Method,
			Updated: ts.InstalledAt,
		})
	}

	if s != nil && !*statusOrphans {
		for name := range s.Tools {
			if _, inState := st.Tools[name]; !inState {
				tools = append(tools, toolStatus{
					Name:   name,
					Status: "missing",
				})
			}
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	if *statusFormat == "json" || *statusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tools); err != nil {
			log.Default.Error("json output", "error", err)
			os.Exit(3)
		}
		return
	}

	if len(tools) == 0 {
		if *statusOrphans {
			fmt.Println("No orphan tools.")
		} else {
			fmt.Println("No tools in state. Run 'depengine install' first.")
		}
		return
	}

	fmt.Printf("%-30s %-12s %-10s  %s\n", "Tool", "Status", "Method", "Installed At")
	fmt.Println(strings.Repeat("-", 70))
	for _, t := range tools {
		fmt.Printf("%-30s %-12s %-10s  %s\n", t.Name, t.Status, t.Method, t.Updated)
	}
}

// runRemove removes a tool using the adapter that installed it.
func runRemove(args []string) {
	removeCmd := flag.NewFlagSet("remove", flag.ExitOnError)
	removeAll := removeCmd.Bool("all", false, "remove all tools")
	removeDryRun := removeCmd.Bool("dry-run", false, "show what would be removed")
	removeCmd.Parse(args)

	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	removeTool := func(toolName string) bool {
		toolState, ok := st.Tools[toolName]
		if !ok {
			log.Default.Error("tool not found in state", "tool", toolName)
			return false
		}

		adapter := exec.Lookup(toolState.Method)
		if adapter == nil {
			log.Default.Warn("adapter not found for method", "tool", toolName, "method", toolState.Method)
			log.Default.Warn("manual remove required", "tool", toolName)
			return false
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("manual remove required", "tool", toolName, "method", toolState.Method)
			return false
		}

		remover := adapter.(exec.Remover)
		mc := &schema.MethodCandidate{
			Kind:   toolState.Method,
			Config: toolState.Config,
		}
		tool := &schema.Tool{Name: toolName}

		if *removeDryRun {
			log.Default.Info("would remove", "tool", toolName, "method", toolState.Method)
			return true
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed", "tool", toolName, "error", err)
			return false
		}

		log.Default.Info("removed", "tool", toolName, "method", toolState.Method)
		delete(st.Tools, toolName)
		return true
	}

	hadFailure := false
	if *removeAll {
		for toolName := range st.Tools {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else {
		if len(removeCmd.Args()) != 1 {
			log.Default.Error("usage: depengine remove <tool>")
			ls.Close()
			os.Exit(1)
		}
		if !removeTool(removeCmd.Arg(0)) {
			hadFailure = true
		}
	}

	if err := ls.Save(); err != nil {
		log.Default.Error("failed to update state", "error", err)
		ls.Close()
		os.Exit(3)
	}

	if hadFailure {
		ls.Close()
		os.Exit(1)
	}
}

// runForget removes a tool from state without attempting system removal.
func runForget(args []string) {
	forgetCmd := flag.NewFlagSet("forget", flag.ExitOnError)
	forgetCmd.Parse(args)

	if len(forgetCmd.Args()) != 1 {
		log.Default.Error("usage: depengine forget <tool>")
		os.Exit(1)
	}

	toolName := forgetCmd.Arg(0)
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()
	st := ls.State()
	if _, ok := st.Tools[toolName]; !ok {
		log.Default.Error("tool not found in state", "tool", toolName)
		ls.Close()
		os.Exit(1)
	}

	delete(st.Tools, toolName)
	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		ls.Close()
		os.Exit(3)
	}

	log.Default.Info("forgotten", "tool", toolName)
}
