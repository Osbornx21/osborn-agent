package observability

import (
	"context"
	"io"
	"log/slog"
)

type RedactingHandler struct {
	handler       slog.Handler
	redactSecrets bool
}

func NewJSONLogger(writer io.Writer, redactSecrets bool, options *slog.HandlerOptions) *slog.Logger {
	if writer == nil {
		writer = io.Discard
	}
	return slog.New(NewRedactingHandler(slog.NewJSONHandler(writer, options), redactSecrets))
}

func NewRedactingHandler(handler slog.Handler, redactSecrets bool) slog.Handler {
	return RedactingHandler{handler: handler, redactSecrets: redactSecrets}
}

func (h RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	if !h.redactSecrets {
		return h.handler.Handle(ctx, record)
	}

	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(redactAttr(attr))
		return true
	})
	return h.handler.Handle(ctx, redacted)
}

func (h RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h.redactSecrets {
		for index := range attrs {
			attrs[index] = redactAttr(attrs[index])
		}
	}
	return RedactingHandler{handler: h.handler.WithAttrs(attrs), redactSecrets: h.redactSecrets}
}

func (h RedactingHandler) WithGroup(name string) slog.Handler {
	return RedactingHandler{handler: h.handler.WithGroup(name), redactSecrets: h.redactSecrets}
}

func redactAttr(attr slog.Attr) slog.Attr {
	if IsSecretKey(attr.Key) {
		return slog.String(attr.Key, redactedValue)
	}
	if attr.Value.Kind() != slog.KindGroup {
		return attr
	}

	group := attr.Value.Group()
	for index := range group {
		group[index] = redactAttr(group[index])
	}
	return slog.Group(attr.Key, attrsToAny(group)...)
}

func attrsToAny(attrs []slog.Attr) []any {
	values := make([]any, len(attrs))
	for index, attr := range attrs {
		values[index] = attr
	}
	return values
}
