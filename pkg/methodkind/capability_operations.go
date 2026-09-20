package methodkind

import "github.com/Khorea1/depengine/pkg/plan"

func planOperations(p plan.ResolvedInstallPlan) []plan.Operation {
	operations := append([]plan.Operation(nil), p.SourceMutations...)
	operations = append(operations, p.Operations...)
	if p.Preparation != nil {
		operations = append(operations, preparationOperations(*p.Preparation)...)
	}
	for _, hook := range p.Hooks {
		operations = append(operations, hook.Operation)
	}
	for _, ensure := range p.Ensures {
		operations = append(operations, ensure.Check, ensure.Apply)
	}
	return operations
}

func operationsHaveArbitraryCode(operations []plan.Operation) bool {
	for _, operation := range operations {
		if operation.ArbitraryCode {
			return true
		}
	}
	return false
}
