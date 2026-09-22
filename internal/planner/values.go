package planner

func stringValue(cfg map[string]any, key string) string {
	value, _ := cfg[key].(string)
	return value
}

func firstValue(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(cfg, key); value != "" {
			return value
		}
	}
	return ""
}
