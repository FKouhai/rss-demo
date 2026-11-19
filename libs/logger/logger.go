// Package logger is the central logging library for the rss-microservices
package logger

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var zapLog *zap.Logger

func init() {
	var err error
	config := zap.NewProductionConfig()
	encoderConfig := zap.NewProductionEncoderConfig()
	zapcore.TimeEncoderOfLayout("Jan _2 15:04:05.000000000")
	encoderConfig.StacktraceKey = "" // to hide stacktrace info
	config.EncoderConfig = encoderConfig

	zapLog, err = config.Build(zap.AddCallerSkip(1))
	if err != nil {
		panic(err)
	}
}

// Info logger
func Info(message string, fields ...zap.Field) {
	zapLog.Info(message, fields...)
}

// Debug logger
func Debug(message string, fields ...zap.Field) {
	zapLog.Debug(message, fields...)
}

// Error logger
func Error(message string, fields ...zap.Field) {
	zapLog.Error(message, fields...)
}

// InfoFmt -> Info formatted logger
func InfoFmt(message string, args ...any) {
	s := zapLog.Sugar()
	s.Infof(message, args...)
}

// ErrorFmt -> Error formatted logger
func ErrorFmt(message string, args ...any) {
	s := zapLog.Sugar()
	s.Errorf(message, args...)
}

// DebugFmt -> Debug formatted logger
func DebugFmt(message string, args ...any) {
	s := zapLog.Sugar()
	s.Debugf(message, args...)
}

// TraceFields returns trace_id/span_id fields from the span in ctx, or nil
// when ctx carries no valid span. Append it to hand-built field lists;
// prefer the Ctx loggers below for plain calls.
func TraceFields(ctx context.Context) []zap.Field {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}

// SpanFields returns trace_id/span_id fields from a span, or nil when the
// span carries no valid context. For log lines that have a span but no ctx.
func SpanFields(span trace.Span) []zap.Field {
	sc := span.SpanContext()
	if !sc.IsValid() {
		return nil
	}
	return []zap.Field{
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	}
}

// InfoCtx -> Info logger with the ctx's trace fields attached, so the line
// joins to the trace in Loki/Tempo without hand-placed trace_id fields.
func InfoCtx(ctx context.Context, message string, fields ...zap.Field) {
	Info(message, append(TraceFields(ctx), fields...)...)
}

// DebugCtx -> Debug logger with the ctx's trace fields attached.
func DebugCtx(ctx context.Context, message string, fields ...zap.Field) {
	Debug(message, append(TraceFields(ctx), fields...)...)
}

// ErrorCtx -> Error logger with the ctx's trace fields attached.
func ErrorCtx(ctx context.Context, message string, fields ...zap.Field) {
	Error(message, append(TraceFields(ctx), fields...)...)
}

// InfoFmtCtx -> Info formatted logger with the ctx's trace fields attached.
func InfoFmtCtx(ctx context.Context, message string, args ...any) {
	s := zapLog.Sugar()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		s = s.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	s.Infof(message, args...)
}

// ErrorFmtCtx -> Error formatted logger with the ctx's trace fields attached.
func ErrorFmtCtx(ctx context.Context, message string, args ...any) {
	s := zapLog.Sugar()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		s = s.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	s.Errorf(message, args...)
}

// DebugFmtCtx -> Debug formatted logger with the ctx's trace fields attached.
func DebugFmtCtx(ctx context.Context, message string, args ...any) {
	s := zapLog.Sugar()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		s = s.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	s.Debugf(message, args...)
}
