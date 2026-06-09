# Doubao LLM Cancel Fixture

On context cancel, stop reading the OpenAI-compatible stream and close the HTTP response. The adapter must not retry on cancel and must not emit stale chunks.
