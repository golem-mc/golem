package hello_test

import (
	"strings"
	"testing"

	_ "example.com/hello"
	"github.com/golem-mc/golem/plugintest"
)

// TestVisitsCommand joins a player and reads the reply to /hello visits.
func TestVisitsCommand(t *testing.T) {
	srv := plugintest.Start(t)
	alice := srv.Join("Alice")

	alice.Run("/hello visits")
	msgs := alice.Messages(5)
	if !strings.Contains(strings.Join(msgs, "\n"), "You joined 1 times.") {
		t.Fatalf("messages = %q, want the visit count", msgs)
	}
}
