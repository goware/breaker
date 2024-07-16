package breaker

type Logger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
