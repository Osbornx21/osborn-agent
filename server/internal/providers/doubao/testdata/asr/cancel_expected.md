# Doubao ASR Cancel Fixture

On context cancel, stop sending `input_audio_buffer.append`, close the Realtime WebSocket, and do not emit a completed transcript after cancel.
