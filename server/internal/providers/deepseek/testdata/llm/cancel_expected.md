# DeepSeek LLM Cancel Fixture

On context cancel, stop reading the data-only SSE stream and close the HTTP response. The parser must treat `data: [DONE]` as normal finish only when not canceled.
