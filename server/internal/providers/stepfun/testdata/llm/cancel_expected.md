# StepFun LLM Cancel Fixture

On context cancel, stop reading the SSE stream and close the HTTP response. The parser must handle `delta.reasoning` fields when they appear, then stop cleanly on cancel.
