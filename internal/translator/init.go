package translator

import (
	// Inbound protocol formats -> Codex (the only account provider target).
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/openai/responses"

	// Inbound protocol formats -> Claude.
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/responses"

	// Inbound protocol formats -> OpenAI.
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/interactions/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/interactions/responses"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/openai/responses"
)
