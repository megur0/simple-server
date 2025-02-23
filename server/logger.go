package server

import (
	"log"
)

var (
	l Logger = &defaultLogger{}
)

func SetLogger(lg Logger) {
	l = lg
}

type Logger interface {
	Info(args ...any)
	Debug(args ...any)
	Warn(args ...any)
	Error(args ...any)
}

type defaultLogger struct{}

func (l *defaultLogger) Info(args ...any) {
	log.Print(append([]any{"[INFO]"}, args...)...)
}

func (l *defaultLogger) Debug(args ...any) {
	log.Print(append([]any{"[DEBUG]"}, args...)...)
}

func (l *defaultLogger) Warn(args ...any) {
	log.Print(append([]any{"[WARN]"}, args...)...)
}

func (l *defaultLogger) Error(args ...any) {
	log.Print(append([]any{"[ERROR]"}, args...)...)
}
