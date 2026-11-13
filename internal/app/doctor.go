package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/validate"
	"github.com/spf13/cobra"
)

// doctorLayer describes one configuration layer discovered by `doctor`.
type doctorLayer struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Origin  string `json:"origin"`
}

type doctorHost struct {
	Target          string `json:"target"`
	OS              string `json:"os"`
	Distro          string `json:"distro"`
	DistroVersion   string `json:"distro_version,omitempty"`
	Family          string `json:"family"`
	Arch            string `json:"arch"`
	DetectionMethod string `json:"detection_method"`
	Confidence      string `json:"confidence"`
	Kernel          string `json:"kernel,omitempty"`
	Libc            string `json:"libc,omitempty"`
	InitSystem      string `json:"init_system,omitempty"`
	WSL             string `json:"wsl"`
	Container       string `json:"container"`
	Android         string `json:"android"`
}

type doctorLayers struct {
	Schema   doctorLayer   `json:"schema"`
	Manifest doctorLayer   `json:"manifest"`
	Catalog  []doctorLayer `json:"catalog"`
}

type doctorManagers struct {
	Default          string   `json:"default,omitempty"`
	DefaultAvailable bool     `json:"default_available"`
	ClanCandidates   []string `json:"clan_candidates"`
	Available        []string `json:"available"`
}

type doctorElevation struct {
	Method   string `json:"method"`
	Root     bool   `json:"root"`
	Required bool   `json:"required"`
}

type doctorLock struct {
	Path     string `json:"path"`
	Present  bool   `json:"present"`
	Version  int    `json:"version,omitempty"`
	Readable bool   `json:"readable"`
	Error    string `json:"error,omitempty"`
}

type doctorReport struct {
	Host      doctorHost      `json:"host"`
	Layers    doctorLayers    `json:"layers"`
	Managers  doctorManagers  `json:"managers"`
	Elevation doctorElevation `json:"elevation"`
	Lock      doctorLock      `json:"lock"`
	Problems  []string        `json:"problems"`
}

// newDoctorCmd builds `depengine doctor`. It is read-only: it probes the host
// and configuration layers but never writes state, locks, or downloads.
func newDoctorCmd() *cobra.Command {
	doctorSchema := new(string)
	*doctorSchema = defaultSchemaPath()
	doctorManifest := new(string)
	doctorNoManifest := new(bool)
	doctorCatalog := new([]string)
	doctorNoCatalog := new(bool)
	doctorJSON := new(bool)
	doctorFormat := new(string)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: ifPT("Diagnosticar o host e as camadas sem mutar nada", "Diagnose the host and layers without mutating anything"),
		Long:  ifPT("Imprime fatos do host, camadas descobertas (schema, manifesto, catálogo), gerenciadores e elevação disponíveis, e o estado do lock. Não instala, não grava estado e não altera arquivos.", "Print host facts, discovered layers (schema, manifest, catalog), available managers and elevation, and lock state. It installs nothing, writes no state, and changes no files."),
		Example: `  depengine doctor
  depengine doctor --json
  depengine doctor --schema ./schema.toml --no-manifest`,
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(doctorFormat, doctorJSON); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitWithCode(2)
			}
			return runDoctor(cmd, doctorSchema, doctorManifest, doctorNoManifest, doctorCatalog, doctorNoCatalog, doctorJSON)
		},
	}
	f := cmd.Flags()
	f.StringVar(doctorSchema, "schema", *doctorSchema, "schema path to inspect")
	f.StringVar(doctorManifest, "manifest", "", "personal manifest path to inspect (default: auto-detect)")
	f.BoolVar(doctorNoManifest, "no-manifest", false, "ignore the personal manifest")
	addCatalogFlags(cmd, doctorCatalog, doctorNoCatalog)
	f.BoolVar(doctorJSON, "json", false, "shorthand for --format=json")
	f.StringVar(doctorFormat, "format", "text", "output format: text or json")
	return cmd
}

// doctorOptions is the resolved flag view for report construction. Keeping it
// separate from Cobra makes the report testable without executing a command.
type doctorOptions struct {
	schemaPath       string
	schemaExplicit   bool
	manifestPath     string
	manifestExplicit bool
	noManifest       bool
	catalog          *config.Catalog
	catalogErr       error
	catalogExplicit  bool
	noCatalog        bool
}

func runDoctor(cmd *cobra.Command, schema, manifest *string, noManifest *bool, catalogRoots *[]string, noCatalog, jsonOut *bool) error {
	catalog, catalogErr := catalogFromCommand(cmd)
	opts := doctorOptions{
		schemaPath:       *schema,
		schemaExplicit:   cmd.Flags().Changed("schema"),
		manifestPath:     *manifest,
		manifestExplicit: cmd.Flags().Changed("manifest"),
		noManifest:       *noManifest,
		catalog:          catalog,
		catalogErr:       catalogErr,
		catalogExplicit:  len(*catalogRoots) > 0,
		noCatalog:        *noCatalog,
	}
	report := buildDoctorReport(cmd.Context(), run.OSExecRunner{}, opts)
	w := cmd.OutOrStdout()
	if *jsonOut {
		if err := writeMachineOutput(w, "doctor", report); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error encoding JSON: %v\n", err)
			return exitWithCode(3)
		}
	} else {
		writeDoctorText(w, report)
	}
	if len(report.Problems) > 0 {
		return exitWithCode(1)
	}
	return nil
}

func buildDoctorReport(ctx context.Context, rn run.Runner, opts doctorOptions) doctorReport {
	report := doctorReport{Problems: []string{}}

	facts, factsErr := platform.GatherFacts(rn)
	if factsErr != nil {
		report.Problems = append(report.Problems, "host detection failed: "+factsErr.Error())
	}
	var factMap map[string]string
	clan := ""
	if facts != nil {
		clan = platform.ResolveFamily(facts)
		factMap = config.BuildMap(facts, clan)
		report.Host = doctorHostFromFacts(facts, clan)
	}
	if factMap == nil {
		factMap = map[string]string{}
	}

	report.Layers, report.Lock = doctorLayersAndLock(opts, factMap, &report)
	report.Managers = doctorManagersFor(ctx, rn, clan, &report)
	report.Elevation = doctorElevationFor(clan, &report)
	return report
}

func doctorHostFromFacts(facts *platform.Facts, clan string) doctorHost {
	target := facts.DistroID
	if target == "" {
		target = facts.OS
	}
	if clan != "" {
		target = fmt.Sprintf("%s (%s) · %s", target, clan, facts.TargetArch)
	}
	return doctorHost{
		Target:          target,
		OS:              facts.OS,
		Distro:          facts.DistroID,
		DistroVersion:   facts.DistroVersion,
		Family:          clan,
		Arch:            facts.TargetArch,
		DetectionMethod: facts.DetectionMethod,
		Confidence:      facts.Confidence,
		Kernel:          facts.Kernel,
		Libc:            facts.Libc,
		InitSystem:      facts.InitSystem,
		WSL:             triState(facts.IsWSL, facts.IsWSLUnknown),
		Container:       triState(facts.IsContainer, facts.IsContainerUnknown),
		Android:         triState(facts.IsAndroid, false),
	}
}

func triState(value, unknown bool) string {
	switch {
	case value:
		return "yes"
	case unknown:
		return "unknown"
	default:
		return "no"
	}
}

func doctorLayersAndLock(opts doctorOptions, factMap map[string]string, report *doctorReport) (doctorLayers, doctorLock) {
	var layers doctorLayers

	// Schema layer: explicit --schema wins; otherwise use the same candidate
	// discovery as every schema-consuming command.
	schemaPath := opts.schemaPath
	origin := "auto"
	if opts.schemaExplicit {
		origin = "flag"
	}
	discovery := discoverDefaultSchema()
	if !opts.schemaExplicit && len(discovery.Found) > 1 {
		report.Problems = append(report.Problems,
			fmt.Sprintf("multiple schema files found (%s); pass --schema to choose", strings.Join(discovery.Found, ", ")))
	}
	absSchema, schemaPresent := resolveDoctorSchemaPath(schemaPath)
	layers.Schema = doctorLayer{Path: absSchema, Present: schemaPresent, Origin: origin}
	if !schemaPresent {
		report.Problems = append(report.Problems,
			fmt.Sprintf("no schema found at %s; run 'depengine init' or pass --schema", absSchema))
	}

	// Manifest layer: disabled, explicit, or auto-detected.
	var manifestPath string
	switch {
	case opts.noManifest:
		layers.Manifest = doctorLayer{Origin: "disabled"}
	case opts.manifestExplicit:
		manifestPath = opts.manifestPath
		abs, present := absoluteFile(manifestPath)
		layers.Manifest = doctorLayer{Path: abs, Present: present, Origin: "flag"}
		if !present {
			report.Problems = append(report.Problems, fmt.Sprintf("manifest not found: %s", abs))
		}
	default:
		manifestPath = config.DefaultManifestPath()
		if manifestPath == "" {
			layers.Manifest = doctorLayer{Path: config.DefaultManifestCandidatePath(), Origin: "default"}
		} else {
			abs, present := absoluteFile(manifestPath)
			layers.Manifest = doctorLayer{Path: abs, Present: present, Origin: "default"}
		}
	}

	// Catalog layer: resolve with the same precedence as execution.
	catalog := opts.catalog
	if opts.catalogErr != nil {
		report.Problems = append(report.Problems, "catalog: "+opts.catalogErr.Error())
		layers.Catalog = []doctorLayer{{Origin: "invalid"}}
	} else if opts.noCatalog {
		layers.Catalog = []doctorLayer{{Origin: "disabled"}}
	} else if catalog != nil {
		origin := "default"
		if opts.catalogExplicit {
			origin = "flag"
		}
		for _, root := range catalog.Roots() {
			abs, present := absoluteDir(root)
			layers.Catalog = append(layers.Catalog, doctorLayer{Path: abs, Present: present, Origin: origin})
		}
	}

	// Schema diagnosis runs only when the file exists. It mirrors the validate
	// pipeline without logging, so `doctor` reports one concise problem.
	if schemaPresent {
		if err := diagnoseSchema(absSchema, factMap, manifestPath, catalog); err != nil {
			report.Problems = append(report.Problems, "schema: "+err.Error())
		}
	}

	return layers, doctorLockFor(absSchema)
}

func diagnoseSchema(schemaPath string, factMap map[string]string, manifestPath string, catalog *config.Catalog) error {
	schema, err := config.ParseProjectSchema(schemaPath, factMap)
	if err != nil {
		return err
	}
	var manifest *config.Schema
	if manifestPath != "" {
		manifest, err = config.ParseManifest(manifestPath, factMap)
		if err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
	}
	resolution, err := config.ResolveLayers(schema, manifest, catalog, factMap, false)
	if err != nil {
		return err
	}
	if err := config.ValidateEffectiveIntegrity(resolution.Schema); err != nil {
		return err
	}
	vr := validate.ValidateSchema(resolution.Schema, exec.RegisteredKinds())
	if vr.HasErrors() {
		return errors.New(vr.Errors[0].Error())
	}
	return nil
}

func doctorLockFor(schemaPath string) doctorLock {
	path := lock.DefaultPath(schemaPath)
	lk, err := lock.Load(path)
	if err != nil {
		return doctorLock{Path: path, Present: true, Readable: false, Error: err.Error()}
	}
	if lk == nil {
		return doctorLock{Path: path, Present: false, Readable: true}
	}
	return doctorLock{Path: path, Present: true, Version: lk.Version, Readable: true}
}

func doctorManagersFor(ctx context.Context, rn run.Runner, clan string, report *doctorReport) doctorManagers {
	var managers doctorManagers
	for _, name := range native.ManagerExecutableNames() {
		if run.LookPath(ctx, rn, name) {
			managers.Available = append(managers.Available, name)
		}
	}
	if clan == "" {
		return managers
	}
	managers.ClanCandidates = native.ManagerNamesForClan(clan)
	mgr, ok := native.Lookup(clan)
	if !ok {
		return managers
	}
	managers.Default = mgr.Name
	managers.DefaultAvailable = run.LookPath(ctx, rn, mgr.Name)
	if !managers.DefaultAvailable {
		report.Problems = append(report.Problems,
			fmt.Sprintf("default native manager %q is not on PATH", mgr.Name))
	}
	return managers
}

func doctorElevationFor(clan string, report *doctorReport) doctorElevation {
	elevation := doctorElevation{
		Method: run.ElevationMethod(),
		Root:   os.Geteuid() == 0,
	}
	if mgr, ok := native.Lookup(clan); ok && mgr.SudoRequired {
		elevation.Required = !elevation.Root
	}
	if elevation.Required && elevation.Method == "" {
		report.Problems = append(report.Problems,
			"native installs need elevation but no sudo/doas/pkexec/run0 is available")
	}
	return elevation
}

func resolveDoctorSchemaPath(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(abs)
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		abs = filepath.Join(abs, "schema.toml")
		info, err = os.Stat(abs)
	}
	return abs, err == nil && !info.IsDir()
}

func absoluteFile(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(abs)
	}
	info, err := os.Stat(abs)
	return abs, err == nil && !info.IsDir()
}

func absoluteDir(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(abs)
	}
	info, err := os.Stat(abs)
	return abs, err == nil && info.IsDir()
}

func writeDoctorText(w io.Writer, report doctorReport) {
	row := func(label, value string) {
		_, _ = fmt.Fprintf(w, "  %-13s %s\n", label, value)
	}
	section := func(name string) {
		_, _ = fmt.Fprintf(w, "\n%s\n", name)
	}

	section("Host")
	row("target", report.Host.Target)
	row("detection", strings.TrimSpace(report.Host.DetectionMethod+" ("+report.Host.Confidence+")"))
	row("kernel", report.Host.Kernel)
	row("libc", report.Host.Libc)
	row("init", report.Host.InitSystem)
	row("wsl", report.Host.WSL)
	row("container", report.Host.Container)
	row("android", report.Host.Android)

	section("Layers")
	row("schema", doctorLayerText(report.Layers.Schema))
	row("manifest", doctorLayerText(report.Layers.Manifest))
	if len(report.Layers.Catalog) == 0 {
		row("catalog", "(none)")
	}
	for _, layer := range report.Layers.Catalog {
		row("catalog", doctorLayerText(layer))
	}

	section("Managers")
	if report.Managers.Default == "" {
		row("default", "(none for this host)")
	} else {
		state := "missing"
		if report.Managers.DefaultAvailable {
			state = "available"
		}
		row("default", fmt.Sprintf("%s (%s)", report.Managers.Default, state))
	}
	row("candidates", joinOrNone(report.Managers.ClanCandidates))
	row("available", joinOrNone(report.Managers.Available))

	section("Elevation")
	method := report.Elevation.Method
	if method == "" {
		method = "(none)"
	}
	row("method", method)
	row("root", yesNo(report.Elevation.Root))
	row("required", yesNo(report.Elevation.Required))

	section("Lock")
	row("path", report.Lock.Path)
	switch {
	case !report.Lock.Present:
		row("state", "absent (run 'depengine update' to create it)")
	case !report.Lock.Readable:
		row("state", "unreadable: "+report.Lock.Error)
	default:
		row("version", fmt.Sprintf("v%d", report.Lock.Version))
	}

	section("Problems")
	if len(report.Problems) == 0 {
		row("", "none")
		return
	}
	for _, problem := range report.Problems {
		_, _ = fmt.Fprintf(w, "  - %s\n", problem)
	}
}

func doctorLayerText(layer doctorLayer) string {
	switch layer.Origin {
	case "disabled":
		return "(disabled)"
	case "":
		return "(none)"
	}
	state := "absent"
	if layer.Present {
		state = "present"
	}
	return fmt.Sprintf("%s (%s, %s)", layer.Path, layer.Origin, state)
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
