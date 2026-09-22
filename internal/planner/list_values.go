package planner

func stringList(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		return stringValues(values)
	default:
		return nil
	}
}

func stringValues(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}
