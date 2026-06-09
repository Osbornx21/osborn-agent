# DashScope ASR Cancel Fixture

On context cancel, send no more binary audio frames, close the WebSocket, and drain no further events. The stream must close before emitting a final transcript after cancel.
