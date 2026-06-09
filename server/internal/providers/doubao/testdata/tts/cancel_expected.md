# Doubao TTS Cancel Fixture

On context cancel, stop sending `input_text.append`, close the Realtime WebSocket, and drop any `response.audio.delta` received after cancel.
