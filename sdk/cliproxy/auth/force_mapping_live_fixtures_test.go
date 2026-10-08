package auth

// Live CPA-derived upstream response fixtures (2026-06-24, local 8343).
// Executors emit these upstream model names; tests assert client-visible aliases after force-mapping.

const liveCodexResponsesCreatedUpstream = `{"type":"response.created","response":{"id":"resp_live","object":"response","created_at":1782272843,"status":"in_progress","model":"gpt-5.4","output":[],"parallel_tool_calls":true}}`

const liveCodexResponsesCompletedUpstream = `{"type":"response.completed","response":{"id":"resp_live","object":"response","created_at":1782272843,"status":"completed","model":"gpt-5.4","output":[{"type":"message","content":[{"type":"output_text","text":"Hi!"}]}]}}`

// liveAnthropicMessagesStartUpstream is the Anthropic Messages streaming shape
// (`type: message_start`). It is a wire format, not a provider-specific payload.
const liveAnthropicMessagesStartUpstream = `{"type": "message_start", "message": {"id": "UVM7aqirB-npz7IP8rfZuQQ", "type": "message", "role": "assistant", "content": [], "model": "gemini-3-flash", "stop_reason": null, "stop_sequence": null, "usage": {"input_tokens": 2, "output_tokens": 1}}}`

const liveXAIMessagesStartUpstream = `{"type":"message_start","message":{"id":"4aeb964a-1190-98f6-9978-a8a7548848d8","type":"message","role":"assistant","model":"grok-4.3","stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0},"content":[],"stop_reason":null}}`

const liveCodexResponsesNonStreamUpstream = `{"model":"gpt-5.4","output":[{"type":"message","content":[{"type":"output_text","text":"Hi!"}]}]}`
