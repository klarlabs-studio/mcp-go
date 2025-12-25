package server

import (
	"testing"
)

func TestLogLevels(t *testing.T) {
	tests := []struct {
		level LogLevel
		want  string
	}{
		{LogLevelDebug, "debug"},
		{LogLevelInfo, "info"},
		{LogLevelNotice, "notice"},
		{LogLevelWarning, "warning"},
		{LogLevelError, "error"},
		{LogLevelCritical, "critical"},
		{LogLevelAlert, "alert"},
		{LogLevelEmergency, "emergency"},
	}

	for _, tt := range tests {
		if string(tt.level) != tt.want {
			t.Errorf("LogLevel %v: expected %q, got %q", tt.level, tt.want, string(tt.level))
		}
	}
}

func TestLogLevelPriority(t *testing.T) {
	tests := []struct {
		level    LogLevel
		priority int
	}{
		{LogLevelDebug, 0},
		{LogLevelInfo, 1},
		{LogLevelNotice, 2},
		{LogLevelWarning, 3},
		{LogLevelError, 4},
		{LogLevelCritical, 5},
		{LogLevelAlert, 6},
		{LogLevelEmergency, 7},
	}

	for _, tt := range tests {
		got := logLevelPriority(tt.level)
		if got != tt.priority {
			t.Errorf("logLevelPriority(%q): expected %d, got %d", tt.level, tt.priority, got)
		}
	}
}

func TestShouldLog(t *testing.T) {
	tests := []struct {
		name         string
		messageLevel LogLevel
		minLevel     LogLevel
		shouldLog    bool
	}{
		{"debug message at debug level", LogLevelDebug, LogLevelDebug, true},
		{"debug message at info level", LogLevelDebug, LogLevelInfo, false},
		{"info message at debug level", LogLevelInfo, LogLevelDebug, true},
		{"info message at info level", LogLevelInfo, LogLevelInfo, true},
		{"info message at warning level", LogLevelInfo, LogLevelWarning, false},
		{"error message at warning level", LogLevelError, LogLevelWarning, true},
		{"warning message at error level", LogLevelWarning, LogLevelError, false},
		{"emergency message at debug level", LogLevelEmergency, LogLevelDebug, true},
		{"debug message at emergency level", LogLevelDebug, LogLevelEmergency, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldLog(tt.messageLevel, tt.minLevel)
			if got != tt.shouldLog {
				t.Errorf("ShouldLog(%q, %q): expected %v, got %v",
					tt.messageLevel, tt.minLevel, tt.shouldLog, got)
			}
		})
	}
}

func TestLoggingMessage(t *testing.T) {
	msg := LoggingMessage{
		Level:  LogLevelInfo,
		Logger: "myapp.server",
		Data:   "Server started",
	}

	if msg.Level != LogLevelInfo {
		t.Errorf("expected level 'info', got %q", msg.Level)
	}
	if msg.Logger != "myapp.server" {
		t.Errorf("expected logger 'myapp.server', got %q", msg.Logger)
	}
	if msg.Data != "Server started" {
		t.Errorf("expected data 'Server started', got %v", msg.Data)
	}
}

func TestLoggingMessageWithStructuredData(t *testing.T) {
	data := map[string]any{
		"request_id": "123",
		"duration":   45.6,
		"success":    true,
	}

	msg := LoggingMessage{
		Level:  LogLevelDebug,
		Logger: "myapp.metrics",
		Data:   data,
	}

	if msg.Level != LogLevelDebug {
		t.Errorf("expected level 'debug', got %q", msg.Level)
	}

	d, ok := msg.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be map[string]any")
	}
	if d["request_id"] != "123" {
		t.Errorf("expected request_id '123', got %v", d["request_id"])
	}
}

func TestSetLevelRequest(t *testing.T) {
	req := SetLevelRequest{
		Level: LogLevelWarning,
	}

	if req.Level != LogLevelWarning {
		t.Errorf("expected level 'warning', got %q", req.Level)
	}
}

func TestUnknownLogLevelPriority(t *testing.T) {
	// Unknown log levels should return 0 (debug priority)
	priority := logLevelPriority(LogLevel("unknown"))
	if priority != 0 {
		t.Errorf("expected priority 0 for unknown level, got %d", priority)
	}
}
