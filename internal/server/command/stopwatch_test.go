package command

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestExecuteStopwatchRangeParse checks the range an /execute stopwatch test
// compares against is read as vanilla's double range: a word that is no double
// is refused as one, and a word with anything after the range is the
// dispatcher's missing separator.
// vanilla: RangeArgument.floatRange, MinMaxBounds.Doubles
func TestExecuteStopwatchRangeParse(t *testing.T) {
	tree := ForLevel(PermLevelOwners)
	for _, tc := range []struct {
		input string
		key   string
		with  []string
	}{
		{"execute if stopwatch foo:bar 1-2", keyDoubleInvalid, []string{"1-2"}},
		{"execute if stopwatch foo:bar 1..2x", keyExpectedSeparator, nil},
	} {
		node, fail := Match(tree, tc.input, Tokenize(tc.input), true)
		if node != nil {
			t.Errorf("%q: accepted, want refused", tc.input)
			continue
		}
		if fail.Err == nil {
			t.Errorf("%q: no typed error, want %s", tc.input, tc.key)
			continue
		}
		if fail.Err.Key != tc.key || !slices.Equal(fail.Err.With, tc.with) {
			t.Errorf("%q: got %s %q, want %s %q", tc.input, fail.Err.Key, fail.Err.With, tc.key, tc.with)
		}
	}
	if node, _ := Match(tree, "execute if stopwatch foo:bar 1..2", Tokenize("execute if stopwatch foo:bar 1..2"), true); node == nil {
		t.Error("execute if stopwatch foo:bar 1..2: refused, want accepted")
	}
}

// stopwatchSource is the Source the /stopwatch command tests read: the elapsed
// time a query answers with and the feedback and admin echo it sends, the rest
// of the interface left to the embedded nil.
type stopwatchSource struct {
	Source
	seconds     float64
	has         bool
	feedbackKey string
	feedback    []protocol.Text
	adminKey    string
	adminWith   []protocol.Text
}

func (s *stopwatchSource) CreateStopwatch(string) bool  { return true }
func (s *stopwatchSource) RestartStopwatch(string) bool { return true }
func (s *stopwatchSource) RemoveStopwatch(string) bool  { return true }
func (s *stopwatchSource) StopwatchIDs() []string       { return nil }

func (s *stopwatchSource) StopwatchSeconds(string) (float64, bool) { return s.seconds, s.has }

func (s *stopwatchSource) Feedback(key string, with ...protocol.Text) error {
	s.feedbackKey = key
	s.feedback = with
	return nil
}

func (s *stopwatchSource) InformAdmins(message protocol.Text) {
	s.adminKey = message.Translate
	s.adminWith = message.With
}

func (s *stopwatchSource) Debug(string, ...any) {}

// TestStopwatchQueryFeedback checks the query prints the seconds the way the
// client prints a Double argument, and echoes the line to the other operators
// as vanilla's sendSuccess with broadcast true does.
func TestStopwatchQueryFeedback(t *testing.T) {
	for _, tc := range []struct {
		seconds float64
		want    string
	}{
		{2.6, "2.6"},
		{0, "0.0"},
	} {
		src := &stopwatchSource{seconds: tc.seconds, has: true}
		if _, err := stopwatchQuery(src, []string{"foo:bar"}); err != nil {
			t.Fatalf("stopwatchQuery: %v", err)
		}
		if src.feedbackKey != stopwatchQueryKey {
			t.Errorf("feedback key = %q, want %q", src.feedbackKey, stopwatchQueryKey)
		}
		if len(src.feedback) != 2 || src.feedback[1].Text != tc.want {
			t.Errorf("seconds for %v = %+v, want %q", tc.seconds, src.feedback, tc.want)
		}
		if src.adminKey != stopwatchQueryKey || len(src.adminWith) != 2 || src.adminWith[1].Text != tc.want {
			t.Errorf("admin echo for %v = %q %+v, want the query line", tc.seconds, src.adminKey, src.adminWith)
		}
	}
}
