package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/state"
	"github.com/spf13/cobra"
)

// newStatusCmd builds `depengine status`.
func newStatusCmd() *cobra.Command {
	statusSchema := new(string)
	statusManifest := new(string)
	statusNoManifest := new(bool)
	statusFormat := new(string)
	statusJSON := new(bool)
	statusOrphans := new(bool)

	cmd := &cobra.Command{
		Use:     "status",
		Short:   ifPT("Mostrar estado das ferramentas em relação ao schema", "Show tool installation state vs schema"),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			runStatus(statusSchema, statusManifest, statusNoManifest, statusFormat, statusJSON, statusOrphans)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(statusSchema, "schema", "", "override schema path")
	f.StringVar(statusManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(statusNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(statusFormat, "format", "text", "output format: text or json")
	f.BoolVar(statusJSON, "json", false, "JSON output (shorthand for --format=json)")
	f.BoolVar(statusOrphans, "orphans", false, "show only orphaned tools")
	return cmd
}

// runStatus shows the installation status of tools by comparing the state
// file against the schema. It reports installed, missing, and outdated tools.
// Body unchanged from the pre-Cobra version — only the flag declarations
// above it moved.
func runStatus(statusSchema, statusManifest *string, statusNoManifest *bool, statusFormat *string, statusJSON, statusOrphans *bool) {
	if *statusJSON {
		if *statusFormat == "text" {
			*statusFormat = "json"
		}
		fmt.Fprintln(os.Stderr, "depengine: --json is deprecated; use --format=json instead")
	}

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

	// Load the lockfile alongside the schema for installed-vs-pinned
	// version comparisons (outdated detection). A missing lock is fine.
	var lk *lock.Lock
	if schemaPath != "" {
		lk, _ = lock.Load(lock.DefaultPath(schemaPath))
	}

	var s *config.Schema
	if schemaPath != "" {
		var err error
		s, err = config.ParseProjectSchema(schemaPath, nil)
		if err != nil {
			log.Default.Warn("load schema for comparison", "error", err)
			s = nil
		}

		if s != nil {
			noManifest := *statusNoManifest
			manifestPath := *statusManifest
			manifestAuto := false
			if !noManifest && manifestPath == "" {
				manifestPath = config.DefaultManifestPath()
				if manifestPath != "" {
					manifestAuto = true
				}
			}
			if manifestPath != "" {
				merged, count, merr := mergeManifest(s, manifestPath, false)
				if merr != nil {
					log.Default.Warn("load manifest", "error", merr)
				} else if count > 0 {
					s = merged
					if manifestAuto {
						fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
					}
				}
			}
		}
	}

	type toolStatus struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Method  string `json:"method,omitempty"`
		Version string `json:"version,omitempty"`
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
		if status == "installed" && s != nil && !*statusOrphans {
			outdated := false
			if stTool, inSchema := s.Tools[name]; inSchema {
				// Definition drift: the schema definition changed since install.
				if ts.DefinitionHash != "" && state.DefinitionHash(stTool) != ts.DefinitionHash {
					outdated = true
				}
				// Version drift: the installed version differs from the pinned one.
				if !outdated && ts.Version != "" {
					if pin, ok := lockPinForToolState(lk, name, stTool, ts); ok && state.VersionOutdated(ts.Version, pin.Latest) {
						outdated = true
					}
				}
			}
			if outdated {
				status = "outdated"
			}
		}
		tools = append(tools, toolStatus{
			Name:    name,
			Status:  status,
			Method:  ts.Method,
			Version: ts.Version,
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

	if *statusFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tools); err != nil {
			log.Default.Error("json output", "error", err)
			closeStateAndExit(ls, 3)
		}
		return
	}

	if len(tools) == 0 {
		if *statusOrphans {
			fmt.Fprintln(os.Stderr, "No orphan tools.")
		} else {
			fmt.Fprintln(os.Stderr, "No tools in state. Run 'depengine install' first.")
		}
		return
	}

	// Actionable states first: outdated tools need attention, missing ones
	// block the schema, orphaned ones are cleanup candidates. Installed and
	// healthy come last — they're the background noise.
	sort.SliceStable(tools, func(i, j int) bool {
		pi, pj := statusRank(tools[i].Status), statusRank(tools[j].Status)
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})

	c := newCLIStyle(os.Stderr)
	nameW, stW, methW, verW := len("Tool"), len("Status"), len("Method"), len("Version")
	for _, t := range tools {
		if len(t.Name) > nameW {
			nameW = len(t.Name)
		}
		if len(t.Status) > stW {
			stW = len(t.Status)
		}
		if len(t.Method) > methW {
			methW = len(t.Method)
		}
		if len(t.Version) > verW {
			verW = len(t.Version)
		}
	}

	fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
		c.dim(padRight("Tool", nameW)), c.dim(padRight("Status", stW)),
		c.dim(padRight("Method", methW)), c.dim(padRight("Version", verW)), c.dim("Installed"))
	counts := map[string]int{}
	for _, t := range tools {
		counts[t.Status]++
		method := t.Method
		if method == "" {
			method = "—"
		}
		version := t.Version
		if version == "" {
			version = "—"
		}
		// Relative time scans faster than an RFC3339 timestamp for a "how old
		// is this install" question the status table answers constantly.
		installed := "—"
		if ts, err := time.Parse(time.RFC3339, t.Updated); err == nil {
			installed = relativeTime(ts)
		} else if t.Updated != "" {
			installed = t.Updated
		}
		fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
			padRight(t.Name, nameW), statusStyled(c, padRight(t.Status, stW), t.Status),
			padRight(method, methW), c.dim(padRight(version, verW)), c.dim(installed))
	}

	fmt.Fprintln(c.w)
	var parts []string
	for _, st := range []string{"outdated", "missing", "orphaned", "installed"} {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	fmt.Fprintf(c.w, "  %s\n", c.dim(strings.Join(parts, "  ·  ")))
}

func statusRank(s string) int {
	switch s {
	case "outdated":
		return 0
	case "missing":
		return 1
	case "orphaned":
		return 2
	default: // installed
		return 3
	}
}

// statusStyled colors a status word by severity. The string must already be
// padded — the caller pads before colorizing so ANSI escapes don't break
// column alignment.
func statusStyled(c *cliStyle, padded, status string) string {
	switch status {
	case "outdated":
		return c.yellow(padded)
	case "missing":
		return c.red(padded)
	case "orphaned":
		return c.yellow(padded)
	default: // installed
		return c.green(padded)
	}
}
