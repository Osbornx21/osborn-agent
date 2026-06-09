# DashScope LLM Cancel Fixture

On context cancel, stop reading the SSE response body and close the HTTP response immediately. The adapter must not emit chunks after cancel and must not log the Authorization header.
