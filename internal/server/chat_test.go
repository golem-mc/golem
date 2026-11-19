package server

import (
	"strings"
	"testing"
	"time"
)

func TestCheckChat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		message string
		ok      bool
	}{
		{"plain", "hello everyone", true},
		{"punctuation", "what?! <o> 100% #1", true},
		{"accented", "grüß dich", true},
		{"outside the basic plane", "hello \U0001F600", true},
		{"at the limit", strings.Repeat("a", maxChatLength), true},
		{"multi-byte at the limit", strings.Repeat("ä", maxChatLength), true},
		// The characters a vanilla server disconnects a client over.
		{"formatting code", "red §c text", false},
		{"newline", "two\nlines", false},
		{"escape", "\x1b[31mred", false},
		{"delete", "gone\x7f", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shown, reason := checkChat(tc.message)
			if ok := reason == nil; ok != tc.ok {
				t.Fatalf("checkChat(%.20q) = %v, want ok=%v", tc.message, reason, tc.ok)
			}
			if !tc.ok && shown == "" {
				t.Error("a rejected message has nothing to tell the player")
			}
		})
	}
}

// TestSpamThrottler checks the counter's accounting: spam threshold's worth
// of messages in one moment is the kick, one short of it is not, and ten
// seconds of quiet wear the counter right down.
func TestSpamThrottler(t *testing.T) {
	t.Parallel()
	var th spamThrottler
	base := time.Unix(0, 0)
	for range spamThreshold(10)/spamStep - 1 {
		if th.hit(base, spamThreshold(10)) {
			t.Fatalf("%d messages at once cross the threshold", spamThreshold(10)/spamStep-1)
		}
	}
	if !th.hit(base, spamThreshold(10)) {
		t.Fatal("the message that reaches the threshold is the kick")
	}

	th = spamThrottler{}
	for range spamThreshold(10) / spamStep {
		th.hit(base, spamThreshold(10))
	}
	// Ten seconds wear the counter down to nothing, so the next message
	// alone is far under the kick.
	if th.hit(base.Add(time.Duration(spamThreshold(10))*tickInterval), spamThreshold(10)) {
		t.Fatal("a burst ten seconds worn off is still the kick")
	}
}
