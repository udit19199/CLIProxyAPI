// Package constant defines provider name constants used throughout the CLI Proxy API.
// These constants identify different AI service providers and their variants,
// ensuring consistent naming across the application.
package constant

const (
	// Gemini represents the Google Gemini provider identifier.
	Gemini = "gemini"

	// GeminiInteractions represents the native Google Interactions API provider identifier.
	GeminiInteractions = "gemini-interactions"

	// Codex represents the OpenAI Codex provider identifier.
	Codex = "codex"

	// Claude represents the Anthropic Claude provider identifier.
	Claude = "claude"

	// OpenAI represents the OpenAI provider identifier.
	OpenAI = "openai"

	// OpenaiResponse represents the OpenAI response format identifier.
	OpenaiResponse = "openai-response"

	// Interactions represents the Google Interactions API format identifier.
	// This is a wire-format identifier used by the retained inbound/outbound
	// translators, not an account provider identifier.
	Interactions = "interactions"
)
