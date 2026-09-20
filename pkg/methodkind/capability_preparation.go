package methodkind

import "github.com/Khorea1/depengine/pkg/plan"

func preparationOperations(preparation plan.PreparationPlan) []plan.Operation {
	operations := append([]plan.Operation(nil), preparation.Probe...)
	operations = append(operations, preparation.Commit...)
	for _, mutation := range preparation.Prepare {
		operations = append(operations, mutation.Apply)
		if mutation.Rollback != nil {
			operations = append(operations, *mutation.Rollback)
		}
	}
	return operations
}
