// Package logger is the central logging library for the rss-microservices
package logger

import (
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
	s.Infof(message, args)
}

// ErrorFmt -> Error formatted logger
func ErrorFmt(message string, args ...any) {
	s := zapLog.Sugar()
	s.Errorf(message, args)
}

// DebugFmt -> Debug formatted logger
func DebugFmt(message string, args ...any) {
	s := zapLog.Sugar()
	s.Debugf(message, args)
}
