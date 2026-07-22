package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"depengine/pkg/lang"
	"depengine/pkg/lock"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/sbom"
	"depengine/pkg/schema"
	"depengine/pkg/state"
)

func runUpdate(args []string) {
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	updateSchema := updateCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	updateManifest := updateCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	updateLock := updateCmd.String("lock", "", "path to schema.lock (default: alongside schema.toml)")
	updateProfile := updateCmd.String("profile", "", "only resolve & pin tools with matching tag")
	updateFrozen := updateCmd.Bool("frozen-lockfile", false, "abort if schema.lock does not exist")
	updateDryRun := updateCmd.Bool("dry-run", false, "show what would be updated without writing lock")
	updateVerbose := updateCmd.Bool("v", false, "detailed output")
	updateCmd.Parse(args)

	ctx := context.Background()
	lg := log.Default

	manifestPath := *updateManifest
	manifestAuto := false
	if manifestPath == "" {
		manifestPath = schema.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(*updateSchema, manifestPath)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		lang.ReconfigureAUR(helper)
	}

	fmt.Fprintf(os.Stderr, "depengine update: distro=%s clan=%s arch=%s tools=%d\n",
		facts.DistroID, clan, facts.TargetArch, len(s.Tools))

	s.Tools = filterTools(s.Tools, "", "", *updateProfile)

	fmt.Fprint(os.Stderr, "Resolving latest versions... ")
	newLock, err := lock.ResolveAll(ctx, s, run.OSExecRunner{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL")
		lg.Error("resolve lock", "error", err)
		os.Exit(1)
	}

	lockPath := *updateLock
	if lockPath == "" {
		lockPath = lock.DefaultPath(*updateSchema)
	}
	if *updateFrozen {
		if _, err := os.Stat(lockPath); err != nil {
			lg.Error("frozen-lockfile: lockfile not found", "path", lockPath)
			os.Exit(1)
		}
	}
	if *updateDryRun {
		fmt.Fprintf(os.Stderr, "dry-run: %d pins would be written to %s\n", len(newLock.Tools), lockPath)
	} else {
		if err := lock.Save(lockPath, newLock); err != nil {
			fmt.Fprintln(os.Stderr, "FAIL")
			lg.Error("save lock", "error", err)
			os.Exit(1)
		}
	}

	pinned := len(newLock.Tools)
	fmt.Fprintf(os.Stderr, "done (%d pinned)\n", pinned)

	if *updateVerbose {
		for key, pin := range newLock.Tools {
			fmt.Fprintf(os.Stderr, "  %s → %s\n", key, pin.Latest)
			if pin.Checksum != "" {
				fmt.Fprintf(os.Stderr, "    checksum: %s\n", pin.Checksum)
			}
		}
	}

	fmt.Fprintln(os.Stderr, "Run 'depengine install' to use the updated lock.")
}

func runSBOM(args []string) {
	sbomCmd := flag.NewFlagSet("sbom", flag.ExitOnError)
	sbomFormat := sbomCmd.String("format", "cyclonedx", "output format: cyclonedx or spdx")
	sbomCmd.Parse(args)

	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("load state", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	var data []byte
	switch *sbomFormat {
	case "cyclonedx", "cyclonedx-json":
		data, err = sbom.ExportCycloneDX(st)
	case "spdx", "spdx-json":
		data, err = sbom.ExportSPDX(st)
	default:
		log.Default.Error("unsupported format", "format", *sbomFormat)
		fmt.Fprintf(os.Stderr, "Formatos suportados: cyclonedx, spdx\n")
		os.Exit(2)
	}

	if err != nil {
		log.Default.Error("generate sbom", "error", err)
		os.Exit(3)
	}

	fmt.Println(string(data))
}

// runDiff compares two state files and outputs the differences.
func runDiff(args []string) {
	diffCmd := flag.NewFlagSet("diff", flag.ExitOnError)
	diffOther := diffCmd.String("other", "", "path to other state file (used when no args)")
	diffJSON := diffCmd.Bool("json", false, "output as JSON")
	diffCmd.Parse(args)

	var aPath, bPath string
	var aState, bState *state.State
	var err error

	switch diffCmd.NArg() {
	case 0:
		if *diffOther == "" {
			fmt.Fprintf(os.Stderr, "error: --other is required when no arguments are given\n")
			os.Exit(2)
		}
		bPath = *diffOther
	case 1:
		bPath = diffCmd.Arg(0)
	case 2:
		aPath = diffCmd.Arg(0)
		bPath = diffCmd.Arg(1)
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
	default:
		fmt.Fprintf(os.Stderr, "usage: depengine diff [--json] [--other <path>] [<file1> [<file2>]]\n")
		os.Exit(2)
	}

	if diffCmd.NArg() != 2 {
		aPath = state.DefaultPath()
		ls, err := state.LoadShared()
		if err != nil {
			log.Default.Error("load current state", "error", err)
			os.Exit(3)
		}
		defer ls.Close()
		aState = ls.State()
		bState, err = state.LoadFrom(bPath)
		if err != nil {
			log.Default.Error("load other state", "path", bPath, "error", err)
			os.Exit(3)
		}
	}

	items := state.Diff(aState, bState)
	if len(items) == 0 {
		if *diffJSON {
			fmt.Println("[]")
		} else {
			fmt.Println("No differences found.")
		}
		return
	}

	if *diffJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			log.Default.Error("encode JSON", "error", err)
			os.Exit(3)
		}
	} else {
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

		if onlyA > 0 {
			fmt.Println("=== Only in current ===")
			for _, item := range items {
				if item.Side == "only_a" {
					fmt.Printf("  %s (%s, installed %s)\n", item.Name, item.MethodA, item.InstalledAtA)
				}
			}
		}

		if onlyB > 0 {
			fmt.Println("=== Only in other ===")
			for _, item := range items {
				if item.Side == "only_b" {
					fmt.Printf("  %s (%s, installed %s)\n", item.Name, item.MethodB, item.InstalledAtB)
				}
			}
		}

		if diffCount > 0 {
			fmt.Println("=== Definition changed ===")
			for _, item := range items {
				if item.Side == "different" {
					fmt.Printf("  %s\n", item.Name)
					fmt.Printf("    current: %s (hash: %s)\n", item.MethodA, item.HashA)
					fmt.Printf("    other: %s (hash: %s)\n", item.MethodB, item.HashB)
				}
			}
		}

		fmt.Printf("\n%d tools differ.\n", len(items))
	}
}
