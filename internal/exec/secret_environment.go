package exec

import (
	"context"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

// schemaSecretEnvNames covers run-level preparation before a tool is selected.
func schemaSecretEnvNames(schema *config.Schema) []string {
	if schema == nil {
		return nil
	}
	var names []string
	for _, tool := range schema.Tools {
		names = append(names, toolSecretEnvNames(tool)...)
	}
	return names
}

// omitToolSecretEnvironment keeps this tool's typed references available to
// the in-process resolver while excluding their source variables from child
// processes, including hooks, probes, preparation, and adapter commands.
func omitToolSecretEnvironment(ctx context.Context, tool *config.Tool) context.Context {
	return run.WithOmittedEnv(ctx, toolSecretEnvNames(tool)...)
}

func toolSecretEnvNames(tool *config.Tool) []string {
	if tool == nil {
		return nil
	}
	var names []string
	for _, method := range tool.Methods {
		names = append(names, methodSecretEnvNames(method)...)
	}
	return names
}

func omitBatchSecretEnvironment(ctx context.Context, candidates []batchCandidate) context.Context {
	var names []string
	for _, candidate := range candidates {
		names = append(names, toolSecretEnvNames(candidate.tool)...)
	}
	return run.WithOmittedEnv(ctx, names...)
}

func methodSecretEnvNames(method *config.MethodCandidate) []string {
	if method == nil {
		return nil
	}
	var names []string
	appendRef := func(ref *config.SecretReference) {
		if ref != nil && ref.Provider == "env" && ref.Name != "" {
			names = append(names, ref.Name)
		}
	}
	appendRef(method.SecretRef)
	appendRef(method.ChecksumSecretRef)
	appendRef(method.SignatureSecretRef)
	for i := range method.Sources {
		appendRef(method.Sources[i].SecretRef)
	}
	return names
}
