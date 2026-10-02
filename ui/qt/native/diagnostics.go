//go:build qt

package native

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	diagnosticLogEntryLimit = 500
	diagnosticLogByteLimit  = 512 << 10
	diagnosticLineByteLimit = 16 << 10
)

type diagnosticLogEntry struct {
	Time    string `json:"time"`
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

var diagnosticLog = struct {
	sync.Mutex
	entries []diagnosticLogEntry
	bytes   int
}{entries: make([]diagnosticLogEntry, 0, diagnosticLogEntryLimit)}

// RecordDiagnostic adds a process or application message to the bounded,
// in-memory desktop log. Messages are never written to disk by this collector.
func RecordDiagnostic(stream, message string) {
	message = strings.TrimRight(message, "\r\n")
	message = strings.ToValidUTF8(message, "�")
	if len(message) > diagnosticLineByteLimit {
		end := diagnosticLineByteLimit
		for end > 0 && !utf8.RuneStart(message[end]) {
			end--
		}
		message = message[:end] + " … [truncated]"
	}
	if message == "" {
		return
	}
	if stream == "" {
		stream = "application"
	}
	entry := diagnosticLogEntry{Time: time.Now().Format("15:04:05.000"), Stream: stream, Message: message}
	entryBytes := len(entry.Time) + len(entry.Stream) + len(entry.Message) + 32

	diagnosticLog.Lock()
	defer diagnosticLog.Unlock()
	diagnosticLog.entries = append(diagnosticLog.entries, entry)
	diagnosticLog.bytes += entryBytes
	for len(diagnosticLog.entries) > diagnosticLogEntryLimit || diagnosticLog.bytes > diagnosticLogByteLimit {
		oldest := diagnosticLog.entries[0]
		diagnosticLog.bytes -= len(oldest.Time) + len(oldest.Stream) + len(oldest.Message) + 32
		copy(diagnosticLog.entries, diagnosticLog.entries[1:])
		diagnosticLog.entries = diagnosticLog.entries[:len(diagnosticLog.entries)-1]
	}
}

// DiagnosticLogJSON returns a snapshot suitable for the QML log viewer.
func DiagnosticLogJSON() string {
	diagnosticLog.Lock()
	entries := append([]diagnosticLogEntry(nil), diagnosticLog.entries...)
	diagnosticLog.Unlock()
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func isDiagnosticStatus(status string) bool {
	lower := strings.ToLower(status)
	for _, marker := range []string{"failed", "error", "incomplete", "stopped", "skipped", "warning", "cancelled", "canceled", "safety limit", "no visible layers"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
