# Anthropic LLM Cancel Fixture

On context cancel, stop reading the Anthropic event stream and close the HTTP response. The adapter must not reuse the OpenAI parser for `event:` typed messages.
