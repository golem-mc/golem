package protocol_test

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// BenchmarkReadString measures the three shapes a string read can take: a
// short message within the limit, a valid one over the limit whose code units
// still have to be counted, and malformed bytes that get replaced.
func BenchmarkReadString(b *testing.B) {
	b.Run("short ASCII", func(b *testing.B) {
		benchReadString(b, "hello there", "hello there", 256)
	})
	b.Run("valid over the limit", func(b *testing.B) {
		text := strings.Repeat("ä", 256)
		benchReadString(b, text, text, 256)
	})
	b.Run("invalid", func(b *testing.B) {
		benchReadString(b, strings.Repeat("\x80", 256), strings.Repeat("\uFFFD", 256), 32767)
	})
}

func benchReadString(b *testing.B, text, want string, max int32) {
	b.Helper()
	b.ReportAllocs()
	var buf protocol.Buffer
	buf.PutString(text)
	payload := buf.Bytes()
	for b.Loop() {
		if got := protocol.NewReader(payload).ReadString(max); got != want {
			b.Fatalf("ReadString = %q, want %q", got, want)
		}
	}
}
