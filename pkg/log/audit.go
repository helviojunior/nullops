package log

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// Audit events (access, authentication, file operations) go to the console
// and, when configured, to a JSON-lines file with RFC 3339 timestamps.
var audit struct {
	mu   sync.Mutex
	file *os.File
}

// EnableTimestamps prefixes console lines with the local date and time.
func EnableTimestamps() {
	Logger.SetReportTimestamp(true)
	Logger.SetTimeFormat("2006-01-02 15:04:05")
}

// OpenAuditFile appends audit events to path as JSON lines (0600).
func OpenAuditFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	audit.mu.Lock()
	audit.file = f
	audit.mu.Unlock()
	return nil
}

// CloseAuditFile closes the audit file.
func CloseAuditFile() {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.file != nil {
		audit.file.Close()
		audit.file = nil
	}
}

func writeAudit(level, msg string, keyvals []interface{}) {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.file == nil {
		return
	}
	rec := map[string]interface{}{
		"time":  time.Now().Format(time.RFC3339Nano),
		"level": level,
		"event": msg,
	}
	for i := 0; i+1 < len(keyvals); i += 2 {
		k := fmt.Sprint(keyvals[i])
		if k == "time" || k == "level" || k == "event" {
			k = "_" + k
		}
		rec[k] = keyvals[i+1]
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	audit.file.Write(append(b, '\n'))
}

// AuditInfo logs an audit event at info level.
func AuditInfo(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Log(log.InfoLevel, msg, keyvals...)
	writeAudit("info", msg, keyvals)
}

// AuditWarn logs an audit event at warn level.
func AuditWarn(msg string, keyvals ...interface{}) {
	Logger.Helper()
	Logger.Log(log.WarnLevel, msg, keyvals...)
	writeAudit("warn", msg, keyvals)
}
