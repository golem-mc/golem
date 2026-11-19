package command

import (
	"errors"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/world"
)

// failureSource is a Source that records the failures sent to it.
type failureSource struct {
	Source
	failures []protocol.Text
}

func (s *failureSource) Error(key string) error {
	s.failures = append(s.failures, protocol.Text{Translate: key})
	return nil
}

func (s *failureSource) ErrorWith(key string, with ...protocol.Text) error {
	s.failures = append(s.failures, protocol.Text{Translate: key, With: with})
	return nil
}

func (s *failureSource) Feedback(key string, with ...protocol.Text) error {
	s.failures = append(s.failures, protocol.Text{Translate: key, With: with})
	return nil
}

// panickingSaySource is a failureSource whose every /say panics, counting
// the says that reached it.
type panickingSaySource struct {
	failureSource
	says int
}

func (s *panickingSaySource) CommandTree() *Node                        { return ForLevel(s.PermLevel()) }
func (s *panickingSaySource) InContext(Context) (Source, bool)          { return s, true }
func (s *panickingSaySource) IntRule(world.GameRuleIndex) int32         { return 65536 }
func (s *panickingSaySource) PermLevel() int                            { return PermLevelGamemasters - 1 }
func (s *panickingSaySource) Debug(string, ...any)                      {}
func (s *panickingSaySource) ChatArgument(string) *chatsecurity.Message { return nil }
func (s *panickingSaySource) BroadcastSay(*chatsecurity.Message, protocol.Text) {
	s.says++
	panic("boom")
}

func TestAPanickingCommandAnswersCommandFailed(t *testing.T) {
	cmd := Command{Name: "boom", Run: func(Source, string, []string) error { panic("boom") }}
	src := &failureSource{}
	if err := RunSafe(cmd, src, "boom", nil); !errors.Is(err, ErrRefused) {
		t.Errorf("RunSafe = %v, want ErrRefused", err)
	}
	if len(src.failures) != 1 || src.failures[0].Translate != "command.failed" {
		t.Errorf("failures = %+v, want one command.failed", src.failures)
	}
}

// TestAPanickingFunctionLineEndsItsRunSilently checks that a game-loop
// function whose first line panics returns, tells nobody, and drops the rest
// of its run.
// vanilla: ServerFunctionManager.execute
func TestAPanickingFunctionLineEndsItsRunSilently(t *testing.T) {
	fn, err := compileFunction(PermLevelGamemasters, "t:boom", []string{"say first", "say second"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	src := &panickingSaySource{}
	RunFunction(src, fn)
	if src.says != 1 {
		t.Errorf("says reached = %d, want 1: the first panics, the second never runs", src.says)
	}
	if len(src.failures) != 0 {
		t.Errorf("sent %+v, want nothing", src.failures)
	}
}
