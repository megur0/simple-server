package server

import (
	"context"
	"log"
)

var (
	l Logger = &defaultLogger{}
)

func SetLogger(lg Logger) {
	l = lg
}

type Logger interface {
	Info(c context.Context, args ...any)
	Debug(c context.Context, args ...any)
	Warn(c context.Context, args ...any)
	Error(c context.Context, args ...any)
}

type defaultLogger struct{}

func (l *defaultLogger) Info(c context.Context, args ...any) {
	log.Print(append([]any{"[INFO]"}, args...)...)
}

func (l *defaultLogger) Debug(c context.Context, args ...any) {
	log.Print(append([]any{"[DEBUG]"}, args...)...)
}

func (l *defaultLogger) Warn(c context.Context, args ...any) {
	log.Print(append([]any{"[WARN]"}, args...)...)
}

func (l *defaultLogger) Error(c context.Context, args ...any) {
	log.Print(append([]any{"[ERROR]"}, args...)...)
}
