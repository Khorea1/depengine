package exec

import "github.com/Khorea1/depengine/internal/state"

func cloneExpectedPreviousState(previous state.ToolState) state.ToolState {
	if previous.Config != nil {
		previous.Config = cloneExpectedStateValue(previous.Config).(map[string]any)
	}
	return previous
}

func cloneExpectedStateValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(v))
		for key, child := range v {
			clone[key] = cloneExpectedStateValue(child)
		}
		return clone
	case []any:
		clone := make([]any, len(v))
		for i, child := range v {
			clone[i] = cloneExpectedStateValue(child)
		}
		return clone
	default:
		return value
	}
}
