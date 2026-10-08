package chat_completions

import "strings"

// isAntigravityModel reports whether the model name identifies an Antigravity
// agent. Kept as a named predicate so the model-name sniff stays in one place.
func isAntigravityModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "antigravity")
}
