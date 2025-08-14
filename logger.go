package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var logLevelNames = map[LogLevel]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
}

// Logger provides structured logging functionality
type Logger struct {
	level  LogLevel
	output io.Writer
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// NewLogger creates a new structured logger
func NewLogger(level string, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}

	logLevel := parseLogLevel(level)
	
	return &Logger{
		level:  logLevel,
		output: output,
	}
}

func parseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	default:
		return LevelInfo
	}
}

// WithFields returns a new log entry with the specified fields
func (l *Logger) WithFields(fields map[string]interface{}) *LogEntryBuilder {
	return &LogEntryBuilder{
		logger: l,
		fields: fields,
	}
}

// WithField returns a new log entry with a single field
func (l *Logger) WithField(key string, value interface{}) *LogEntryBuilder {
	return l.WithFields(map[string]interface{}{key: value})
}

// WithError returns a new log entry with an error field
func (l *Logger) WithError(err error) *LogEntryBuilder {
	return &LogEntryBuilder{
		logger: l,
		err:    err,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(message string) {
	l.log(LevelDebug, message, nil, nil)
}

// Info logs an info message
func (l *Logger) Info(message string) {
	l.log(LevelInfo, message, nil, nil)
}

// Warn logs a warning message
func (l *Logger) Warn(message string) {
	l.log(LevelWarn, message, nil, nil)
}

// Error logs an error message
func (l *Logger) Error(message string) {
	l.log(LevelError, message, nil, nil)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(message string) {
	l.log(LevelFatal, message, nil, nil)
	os.Exit(1)
}

func (l *Logger) log(level LogLevel, message string, fields map[string]interface{}, err error) {
	if level < l.level {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     logLevelNames[level],
		Message:   message,
		Fields:    fields,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// Add caller information for errors and above
	if level >= LevelError {
		if pc, file, line, ok := runtime.Caller(3); ok {
			if fn := runtime.FuncForPC(pc); fn != nil {
				entry.Caller = fmt.Sprintf("%s:%d (%s)", file, line, fn.Name())
			} else {
				entry.Caller = fmt.Sprintf("%s:%d", file, line)
			}
		}
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		// Fallback to standard log if JSON marshaling fails
		log.Printf("Failed to marshal log entry: %v", err)
		log.Printf("[%s] %s", entry.Level, entry.Message)
		return
	}

	fmt.Fprintln(l.output, string(jsonBytes))
}

// LogEntryBuilder helps build log entries with fields
type LogEntryBuilder struct {
	logger *Logger
	fields map[string]interface{}
	err    error
}

// WithField adds a field to the log entry
func (b *LogEntryBuilder) WithField(key string, value interface{}) *LogEntryBuilder {
	if b.fields == nil {
		b.fields = make(map[string]interface{})
	}
	b.fields[key] = value
	return b
}

// WithFields adds multiple fields to the log entry
func (b *LogEntryBuilder) WithFields(fields map[string]interface{}) *LogEntryBuilder {
	if b.fields == nil {
		b.fields = make(map[string]interface{})
	}
	for k, v := range fields {
		b.fields[k] = v
	}
	return b
}

// WithError adds an error to the log entry
func (b *LogEntryBuilder) WithError(err error) *LogEntryBuilder {
	b.err = err
	return b
}

// Debug logs a debug message with fields
func (b *LogEntryBuilder) Debug(message string) {
	b.logger.log(LevelDebug, message, b.fields, b.err)
}

// Info logs an info message with fields
func (b *LogEntryBuilder) Info(message string) {
	b.logger.log(LevelInfo, message, b.fields, b.err)
}

// Warn logs a warning message with fields
func (b *LogEntryBuilder) Warn(message string) {
	b.logger.log(LevelWarn, message, b.fields, b.err)
}

// Error logs an error message with fields
func (b *LogEntryBuilder) Error(message string) {
	b.logger.log(LevelError, message, b.fields, b.err)
}

// Fatal logs a fatal message with fields and exits
func (b *LogEntryBuilder) Fatal(message string) {
	b.logger.log(LevelFatal, message, b.fields, b.err)
	os.Exit(1)
}

// Global logger instance
var AppLogger *Logger

// InitializeLogger initializes the global logger
func InitializeLogger(config *Config) {
	var output io.Writer = os.Stdout
	
	// In production, you might want to write to a file
	if config.Environment == "production" {
		// Create logs directory if it doesn't exist
		if err := os.MkdirAll("logs", 0755); err == nil {
			if file, err := os.OpenFile("logs/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
				output = file
			}
		}
	}

	AppLogger = NewLogger(config.LogLevel, output)
}