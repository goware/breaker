package breaker

type Logger interface {
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}
