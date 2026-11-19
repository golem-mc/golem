package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// querySource is the Source the time-query unit tests read: the clock the
// commands run against and the feedback lines they send, with the rest of
// the interface left to the embedded nil, so a command reaching for a method
// the test did not set panics rather than answering from a half-built world.
type querySource struct {
	Source
	dayTime int64
	keys    []string
	with    [][]protocol.Text
	errs    []string
}

func (s *querySource) ClockTime(string) (int64, int64, bool) { return 0, s.dayTime, true }

func (s *querySource) DefaultClock() (string, bool) { return overworldClock, true }

func (s *querySource) Feedback(key string, with ...protocol.Text) error {
	s.keys = append(s.keys, key)
	s.with = append(s.with, with)
	return nil
}

func (s *querySource) ErrorWith(key string, with ...protocol.Text) error {
	s.errs = append(s.errs, key)
	s.with = append(s.with, with)
	return nil
}

func (s *querySource) Debug(string, ...any) {}

// TestTimeQueryTimelineNamesResolvedID checks that the feedback names the
// timeline by the id the walk's resource argument resolves, and that a word
// that reader refuses names nothing rather than echoing the raw token.
func TestTimeQueryTimelineNamesResolvedID(t *testing.T) {
	src := &querySource{dayTime: 986}
	_, _ = runTime(src, "time query day", []string{"query", "day"})
	if len(src.keys) != 1 {
		t.Fatalf("feedback lines = %d, want 1", len(src.keys))
	}
	if src.keys[0] != "commands.time.query.timeline" {
		t.Errorf("key = %q, want commands.time.query.timeline", src.keys[0])
	}
	want := []protocol.Text{{Text: "minecraft:day"}, longFeedback(986)}
	if len(src.with[0]) != len(want) {
		t.Fatalf("feedback args = %v, want %v", src.with[0], want)
	}
	if src.with[0][0].Text != "minecraft:day" {
		t.Errorf("feedback name = %q, want minecraft:day", src.with[0][0].Text)
	}
	if *src.with[0][1].Long != 986 {
		t.Errorf("feedback value = %d, want 986 as a long", *src.with[0][1].Long)
	}

	// An empty namespace stands for the minecraft one, so the handler
	// resolves ":day" to the same timeline the walk resolves it to.
	sent := len(src.keys)
	_, _ = runTime(src, "time query :day", []string{"query", ":day"})
	if len(src.keys) != sent+1 {
		t.Fatalf("feedback lines = %d, want %d", len(src.keys), sent+1)
	}
	if got := src.with[len(src.with)-1][0].Text; got != "minecraft:day" {
		t.Errorf(":day answered with %q, want minecraft:day", got)
	}
}

// TestTimeQueryTimelinePeriods checks the registry's timelines against the
// clock they ride: a period's modulo and repetition division, and the
// period-less timeline that answers the raw total and counts none.
func TestTimeQueryTimelinePeriods(t *testing.T) {
	for _, tc := range []struct {
		name       string
		timeline   string
		dayTime    int64
		repetition bool
		want       int64
	}{
		{"day is the day's remainder", "minecraft:day", 30007, false, 6007},
		{"day repeats every 24000", "minecraft:day", 30007, true, 1},
		{"moon repeats every 192000", "minecraft:moon", 30007, true, 0},
		{"villager schedule is a day", "minecraft:villager_schedule", 30007, false, 6007},
		{"early game answers the total", "minecraft:early_game", 30007, false, 30007},
		{"early game counts no repetitions", "minecraft:early_game", 30007, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &querySource{dayTime: tc.dayTime}
			args := []string{"query", tc.timeline}
			if tc.repetition {
				args = append(args, "repetition")
			}
			_, _ = runTime(src, "time "+args[0]+" "+args[1], args)
			if len(src.keys) != 1 {
				t.Fatalf("feedback lines = %d, want 1", len(src.keys))
			}
			got := *src.with[0][1].Long
			if got != tc.want {
				t.Errorf("%s of %d = %d, want %d", tc.timeline, tc.dayTime, got, tc.want)
			}
		})
	}
}

// TestTimeQueryWrongTimelineClock checks that a timeline asked of the clock
// it does not ride fails with wrong_timeline_for_clock, naming the timeline
// and then the clock.
func TestTimeQueryWrongTimelineClock(t *testing.T) {
	src := &querySource{dayTime: 986}
	_, _ = runTime(src, "time of the_end query day", []string{"of", "the_end", "query", "day"})
	if len(src.errs) != 1 || src.errs[0] != "commands.time.wrong_timeline_for_clock" {
		t.Fatalf("errors = %v, want commands.time.wrong_timeline_for_clock", src.errs)
	}
	if src.with[0][0].Text != "minecraft:day" || src.with[0][1].Text != "minecraft:the_end" {
		t.Errorf("error args = %v, want the timeline then the clock", src.with[0])
	}
}
