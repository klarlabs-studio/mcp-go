package server

// LogLevel represents MCP logging levels.
// These follow syslog severity levels per the MCP specification.
type LogLevel string

const (
	LogLevelDebug     LogLevel = "debug"
	LogLevelInfo      LogLevel = "info"
	LogLevelNotice    LogLevel = "notice"
	LogLevelWarning   LogLevel = "warning"
	LogLevelError     LogLevel = "error"
	LogLevelCritical  LogLevel = "critical"
	LogLevelAlert     LogLevel = "alert"
	LogLevelEmergency LogLevel = "emergency"
)

// LoggingMessage is a log message sent from server to client.
type LoggingMessage struct {
	Level  LogLevel `json:"level"`
	Logger string   `json:"logger,omitempty"`
	Data   any      `json:"data"`
}

// SetLevelRequest is sent by the client to set the logging level.
type SetLevelRequest struct {
	Level LogLevel `json:"level"`
}

// LogSender can send log messages to the client.
type LogSender interface {
	// Log sends a log message at the specified level.
	Log(level LogLevel, logger string, data any)

	// Debug logs a debug message.
	Debug(logger string, data any)

	// Info logs an info message.
	Info(logger string, data any)

	// Notice logs a notice message.
	Notice(logger string, data any)

	// Warning logs a warning message.
	Warning(logger string, data any)

	// Error logs an error message.
	Error(logger string, data any)

	// Critical logs a critical message.
	Critical(logger string, data any)

	// Alert logs an alert message.
	Alert(logger string, data any)

	// Emergency logs an emergency message.
	Emergency(logger string, data any)

	// SetLevel sets the minimum log level to send.
	SetLevel(level LogLevel)

	// Level returns the current minimum log level.
	Level() LogLevel
}

// logLevelPriority returns the priority of a log level (higher = more severe).
func logLevelPriority(level LogLevel) int {
	switch level {
	case LogLevelDebug:
		return 0
	case LogLevelInfo:
		return 1
	case LogLevelNotice:
		return 2
	case LogLevelWarning:
		return 3
	case LogLevelError:
		return 4
	case LogLevelCritical:
		return 5
	case LogLevelAlert:
		return 6
	case LogLevelEmergency:
		return 7
	default:
		return 0
	}
}

// ShouldLog returns true if a message at the given level should be logged
// given the current minimum level.
func ShouldLog(messageLevel, minLevel LogLevel) bool {
	return logLevelPriority(messageLevel) >= logLevelPriority(minLevel)
}
