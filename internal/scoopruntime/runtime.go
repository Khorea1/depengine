// Package scoopruntime owns the Scoop-specific boundary between ecosystem operations and a concrete runtime.
package scoopruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

// Capabilities describes the operations supported by a concrete Scoop runtime.
type Capabilities struct {
	ExactVersion           bool
	BucketSelection        bool
	Scope                  bool
	Architecture           bool
	Removal                bool
	BucketRevisionLocation bool
}

// InstallTarget is the resolved Scoop package identity to install or remove.
type InstallTarget struct {
	Package      string
	Version      string
	Bucket       string
	Scope        string
	Architecture string
}

// InstalledPackage is Scoop's semantic report for one installed package.
type InstalledPackage struct {
	Present   bool
	Malformed bool
	Version   string
	Bucket    string
	Scope     string
}

// Bucket is one configured Scoop bucket and its source location, when known.
type Bucket struct {
	Name     string
	Location string
}

// Runtime translates semantic Scoop operations to one concrete runtime protocol.
type Runtime interface {
	Capabilities() Capabilities
	Available(context.Context, run.Runner) bool
	ObserveInstalled(context.Context, run.Runner, string, string) (InstalledPackage, error)
	InstallResolved(context.Context, run.Runner, InstallTarget) error
	RemoveResolved(context.Context, run.Runner, InstallTarget) error
	BucketList(context.Context, run.Runner) ([]Bucket, error)
	BucketAdd(context.Context, run.Runner, string, string, map[string]string, []string) error
	BucketRemove(context.Context, run.Runner, string) error
	BucketRepository(context.Context, run.Runner, string) (string, error)
}

// CheckInstallCapabilities rejects a resolved identity the selected runtime cannot honor.
func CheckInstallCapabilities(caps Capabilities, target InstallTarget) error {
	if target.Version != "" && !caps.ExactVersion {
		return fmt.Errorf("scoop runtime does not support exact versions")
	}
	if target.Bucket != "" && !caps.BucketSelection {
		return fmt.Errorf("scoop runtime does not support bucket selection")
	}
	if target.Scope != "" && !caps.Scope {
		return fmt.Errorf("scoop runtime does not support package scope")
	}
	if target.Architecture != "" && !caps.Architecture {
		return fmt.Errorf("scoop runtime does not support architecture selection")
	}
	return nil
}

// CheckRemoveCapabilities validates removal and every scoped identity selector before mutation.
func CheckRemoveCapabilities(caps Capabilities, target InstallTarget) error {
	if !caps.Removal {
		return fmt.Errorf("scoop runtime does not support removal")
	}
	return CheckInstallCapabilities(caps, target)
}

// CheckBucketAddCapabilities gates custom-location and revision-aware bucket mutation.
func CheckBucketAddCapabilities(caps Capabilities, location string, revisionRequired bool) error {
	if location != "" && !caps.BucketSelection {
		return fmt.Errorf("scoop runtime does not support bucket location selection")
	}
	if (location != "" || revisionRequired) && !caps.BucketRevisionLocation {
		return fmt.Errorf("scoop runtime does not support bucket location or revision verification")
	}
	return nil
}

// CheckBucketRemoveCapabilities validates the runtime before bucket removal.
func CheckBucketRemoveCapabilities(caps Capabilities) error {
	if !caps.Removal {
		return fmt.Errorf("scoop runtime does not support removal")
	}
	return nil
}

// Official implements the supported scoop.exe command protocol.
type Official struct{}

func NewOfficial() Official { return Official{} }

func (Official) Capabilities() Capabilities {
	return Capabilities{ExactVersion: true, BucketSelection: true, Scope: true, Architecture: true, Removal: true, BucketRevisionLocation: true}
}

func (Official) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "scoop")
}

func (Official) ObserveInstalled(ctx context.Context, rn run.Runner, pkg, scope string) (InstalledPackage, error) {
	args := []string{"list", pkg}
	if scope == "global" {
		args = append(args, "--global")
	}
	result := rn.Run(ctx, "scoop", args...)
	if err := run.CheckResultCompleteOutput(result, "scoop: installed package check"); err != nil {
		return InstalledPackage{}, err
	}
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], pkg) {
			continue
		}
		if len(fields) < 3 || fields[1] == "" {
			return InstalledPackage{Present: true, Malformed: true}, nil
		}
		return InstalledPackage{Present: true, Version: fields[1], Bucket: fields[2], Scope: scope}, nil
	}
	return InstalledPackage{Scope: scope}, nil
}

func (o Official) InstallResolved(ctx context.Context, rn run.Runner, target InstallTarget) error {
	if err := CheckInstallCapabilities(o.Capabilities(), target); err != nil {
		return err
	}
	identity := target.Package
	if target.Bucket != "" {
		identity = target.Bucket + "/" + identity
	}
	if target.Version != "" {
		identity += "@" + target.Version
	}
	args := []string{"install", identity}
	if target.Scope == "global" {
		args = append(args, "--global")
	}
	if target.Architecture != "" {
		args = append(args, "--arch", target.Architecture)
	}
	return run.CheckResult(rn.Run(ctx, "scoop", args...), "scoop: install")
}

func (o Official) RemoveResolved(ctx context.Context, rn run.Runner, target InstallTarget) error {
	if err := CheckRemoveCapabilities(o.Capabilities(), target); err != nil {
		return err
	}
	identity := target.Package
	if target.Bucket != "" {
		identity = target.Bucket + "/" + identity
	}
	args := []string{"uninstall", identity}
	if target.Scope == "global" {
		args = append(args, "--global")
	}
	return run.CheckResult(rn.Run(ctx, "scoop", args...), "scoop: uninstall")
}

func (Official) BucketList(ctx context.Context, rn run.Runner) ([]Bucket, error) {
	result := rn.Run(ctx, "scoop", "bucket", "list")
	if err := run.CheckResultCompleteOutput(result, "source check"); err != nil {
		return nil, err
	}
	buckets := make([]Bucket, 0)
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.EqualFold(fields[0], "name") {
			continue
		}
		bucket := Bucket{Name: fields[0]}
		if len(fields) > 1 {
			bucket.Location = fields[1]
		}
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

func (o Official) BucketAdd(ctx context.Context, rn run.Runner, name, location string, env map[string]string, sensitive []string) error {
	if err := CheckBucketAddCapabilities(o.Capabilities(), location, false); err != nil {
		return err
	}
	args := []string{"bucket", "add", name}
	if location != "" {
		args = append(args, location)
	}
	var result run.Result
	if len(env) != 0 {
		result = run.RunWithEnv(ctx, rn, env, sensitive, "scoop", args...)
	} else {
		result = rn.Run(ctx, "scoop", args...)
	}
	return run.CheckResult(result, "source add")
}

func (o Official) BucketRemove(ctx context.Context, rn run.Runner, name string) error {
	if err := CheckBucketRemoveCapabilities(o.Capabilities()); err != nil {
		return err
	}
	return run.CheckResult(rn.Run(ctx, "scoop", "bucket", "rm", name), "source remove")
}

func (Official) BucketRepository(ctx context.Context, rn run.Runner, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("invalid bucket name")
	}
	result := rn.Run(ctx, "scoop", "prefix", "scoop")
	if err := run.CheckResultCompleteOutput(result, "source repository path"); err != nil {
		return "", fmt.Errorf("locate Scoop root: %w", err)
	}
	prefix := strings.Trim(strings.TrimSpace(string(result.Stdout)), "\"")
	if prefix == "" || strings.ContainsAny(prefix, "\r\n") || !filepath.IsAbs(prefix) {
		return "", fmt.Errorf("scoop returned a malformed core prefix")
	}
	prefix = filepath.Clean(prefix)
	scoopAppDir := filepath.Dir(prefix)
	appsDir := filepath.Dir(scoopAppDir)
	root := filepath.Dir(appsDir)
	if !strings.EqualFold(filepath.Base(scoopAppDir), "scoop") || !strings.EqualFold(filepath.Base(appsDir), "apps") || root == appsDir {
		return "", fmt.Errorf("scoop returned an unexpected core prefix")
	}
	return filepath.Join(root, "buckets", name), nil
}
