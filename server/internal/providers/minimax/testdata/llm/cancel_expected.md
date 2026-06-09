# MiniMax LLM Cancel Fixture

On context cancel, stop reading the SSE stream and close the HTTP response. The adapter must preserve `base_resp` in error mapping without logging raw payload text by default.
