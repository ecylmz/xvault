package app

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Envelope struct {
	OK         bool       `json:"ok"`
	Command    string     `json:"command"`
	StartedAt  string     `json:"started_at,omitempty"`
	FinishedAt string     `json:"finished_at,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	Data       any        `json:"data,omitempty"`
	Error      *ErrorInfo `json:"error,omitempty"`
}

type ErrorInfo struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	SafeForAgent bool   `json:"safe_for_agent"`
}

func writeJSON(w io.Writer, command string, started time.Time, data any) {
	finished := time.Now().UTC()
	_ = json.NewEncoder(w).Encode(Envelope{OK: true, Command: command, StartedAt: started.UTC().Format(time.RFC3339), FinishedAt: finished.Format(time.RFC3339), DurationMS: finished.Sub(started).Milliseconds(), Data: data})
}

func writeJSONError(w io.Writer, command, code, message string, retryable bool) {
	_ = json.NewEncoder(w).Encode(Envelope{OK: false, Command: command, Error: &ErrorInfo{Code: code, Message: message, Retryable: retryable, SafeForAgent: true}})
}

func writeJSONErrorWithData(w io.Writer, command string, started time.Time, data any, code, message string, retryable bool) {
	finished := time.Now().UTC()
	_ = json.NewEncoder(w).Encode(Envelope{
		OK:         false,
		Command:    command,
		StartedAt:  started.UTC().Format(time.RFC3339),
		FinishedAt: finished.Format(time.RFC3339),
		DurationMS: finished.Sub(started).Milliseconds(),
		Data:       data,
		Error:      &ErrorInfo{Code: code, Message: message, Retryable: retryable, SafeForAgent: true},
	})
}

func human(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
