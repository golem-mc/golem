package admin

import (
	"bytes"
	"sync"
)

// Copies of internal/server/servertest helpers for the tests still in internal/server/; they
// go with the last of those tests.

// logBuffer is a writer the log can share between goroutines.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
