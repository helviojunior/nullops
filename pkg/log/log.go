package log

import (
	"fmt"
	stdlog "log"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

// LLogger is a charmbracelet logger type redefinition
type LLogger = log.Logger

// Logger is this package level logger
var Logger *LLogger

func init() {
	styles := log.DefaultStyles()
	styles.Keys["err"] = lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
	styles.Values["err"] = lipgloss.NewStyle().Bold(true)

	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: false,
	})
	Logger.SetStyles(styles)
	Logger.SetLevel(log.InfoLevel)
}

// verbosity: -1 quiet, 0 normal, 1 debug (-v), 2 trace (-vv)
var verbosity = 0

// SetVerbosity configures the level:
//
//	<0  only errors (quiet)
//	 0  info: access, login and file operations (default)
//	 1  debug: plus TLS/HTTP server errors and read-only API calls
//	 2+ trace: plus every HTTP request and the source location of each line
func SetVerbosity(v int) {
	verbosity = v
	switch {
	case v < 0:
		Logger.SetLevel(log.ErrorLevel)
	case v == 0:
		Logger.SetLevel(log.InfoLevel)
	case v == 1:
		Logger.SetLevel(log.DebugLevel)
	default:
		Logger.SetLevel(log.DebugLevel)
		Logger.SetReportCaller(true)
	}
}

// Verbosity returns the configured verbosity level.
func Verbosity() int { return verbosity }

// EnableDebug enables debug logging and caller reporting (same as -vv).
func EnableDebug() {
	SetVerbosity(2)
}

// EnableSilence will silence most logs, except this written with Print
func EnableSilence() {
	verbosity = -1
	Logger.SetLevel(log.FatalLevel + 100)
}

// debugWriter adapts the standard library logger (net/http ErrorLog) to the
// debug level: TLS handshake failures, broken connections, etc.
type debugWriter struct{ msg string }

func (w debugWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\r\n")
	if line != "" {
		Logger.Debug(w.msg, "detail", line)
	}
	return len(p), nil
}

// StdDebugLogger returns a *stdlog.Logger whose output goes to Debug.
func StdDebugLogger(msg string) *stdlog.Logger {
	return stdlog.New(debugWriter{msg: msg}, "", 0)
}

// Debug logs debug messages
func Debug(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Debug(msg, keyvals...)
}
func Debugf(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Debug(fmt.Sprintf(format, a...))
}

// Info logs info messages
func Info(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Info(msg, keyvals...)
}
func Infof(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Info(fmt.Sprintf(format, a...))
}

// Warn logs warning messages
func Warn(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Warn(msg, keyvals...)
}
func Warnf(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Warn(fmt.Sprintf(format, a...))
}

// Error logs error messages
func Error(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Error(msg, keyvals...)
}
func Errorf(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Error(fmt.Sprintf(format, a...))
}

// Fatal logs fatal messages and panics
func Fatal(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Fatal(msg, keyvals...)
}
func Fatalf(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Fatal(fmt.Sprintf(format, a...))
}

// Print logs messages regardless of level
func Print(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Print(msg, keyvals...)
}
func Printf(format string, a ...interface{}) {
	Logger.Helper()
	Logger.Print(fmt.Sprintf(format, a...))
}

// With returns a sublogger with a prefix
func With(keyvals ...interface{}) *LLogger {
	return Logger.With(keyvals...)
}
