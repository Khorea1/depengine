package exec

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/run"
)

type batchCandidate struct {
	toolName string
	tool     *config.Tool
	method   *config.MethodCandidate
	pkg      string
}

func (ex *Executor) identifyBatchCandidates(ctx context.Context, level []string, s *config.Schema, report *ExecReport) (candidates []batchCandidate, remaining []string) {
	remaining = make([]string, 0, len(level))
	for _, toolName := range level {
		tool, ok := s.Tools[toolName]
		if !ok {
			continue
		}
		if len(tool.Methods) == 0 {
			remaining = append(remaining, toolName)
			continue
		}

		foundNative := false
		for _, method := range config.SelectMethods(tool, ex.defaultMethodOrder, ex.nativeManagerName) {
			if method.When != nil && !method.When.Match(ex.facts) {
				continue
			}
			adapter := ex.LookupAdapter(method.Kind)
			if adapter == nil || !adapter.Available(ctx, ex.probeRunner(toolName, method.Kind)) {
				continue
			}
			if method.Kind != "native" && !native.IsNativeManagerName(method.Kind) {
				break
			}
			if len(method.Requires) > 0 || len(method.Sources) > 0 {
				break
			}
			if adapter.Check(ctx, ex.probeRunner(toolName, method.Kind), tool, method) {
				ex.recordToolResult(ctx, &ToolResult{Tool: toolName, Status: StatusAlready, Method: method.Kind}, report)
				foundNative = true
				break
			}
			if !checkAvailable(ctx, ex.probeRunner(toolName, method.Kind), adapter, tool, method) {
				break
			}
			pkg := pkgFromConfig(method, ex.clan)
			if pkg == "" || !validBatchPkgName(pkg) || !native.IsBatchCapable(ex.clan) {
				break
			}
			candidates = append(candidates, batchCandidate{toolName: toolName, tool: tool, method: method, pkg: pkg})
			foundNative = true
			break
		}
		if !foundNative {
			remaining = append(remaining, toolName)
		}
	}
	return candidates, remaining
}

var pkgNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+-_]*$`)

func validBatchPkgName(name string) bool {
	return pkgNameRegexp.MatchString(name)
}

func (ex *Executor) batchNativeInstall(ctx context.Context, candidates []batchCandidate) bool {
	pkgs := make([]string, len(candidates))
	for i, candidate := range candidates {
		pkgs[i] = candidate.pkg
	}
	cmd := native.BuildBatchInstallCmd(ex.clan, pkgs)
	if cmd == nil {
		return false
	}

	timeout := ex.methodTimeout * time.Duration(max(1, len(pkgs)))
	if ex.batchTimeout > 0 {
		timeout = ex.batchTimeout
	}
	if ex.toolTimeout > 0 && timeout > ex.toolTimeout {
		timeout = ex.toolTimeout
	}
	batchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runner := ex.mutationRunner("batch", "native")
	ex.outputf("  ⚡  batch installing %d packages via %s (1 elevation)\n", len(pkgs), ex.nativeManagerName)
	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "started")
	result := runner.Run(batchCtx, cmd[0], cmd[1:]...)
	if result.Err != nil || result.ExitCode != 0 {
		ex.outputf("  ⚡  batch install failed (%s), falling back to per-tool install\n", formatBatchError(result))
		ex.logWarn(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "failed", "error", result.Err, "exit_code", result.ExitCode)
		return false
	}
	ex.logDebug(ctx, "batch", "clan", ex.clan, "packages", strings.Join(pkgs, " "), "status", "succeeded")
	return true
}

func formatBatchError(result run.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	return fmt.Sprintf("exit code %d", result.ExitCode)
}
