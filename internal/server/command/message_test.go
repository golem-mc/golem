package command

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/google/uuid"
)

type argumentTestSource struct {
	Source
	results int32
	silent  bool
}

func (s *argumentTestSource) ChatArgument(string) *chatsecurity.Message { return nil }
func (s *argumentTestSource) InContext(Context) (Source, bool)          { return s, true }
func (s *argumentTestSource) WantsFeedback() bool                       { return false }
func (s *argumentTestSource) Silent() bool                              { return s.silent }
func (s *argumentTestSource) Result(_ bool, result int32)               { s.results += result }

func TestChatArgumentsInvocationScope(t *testing.T) {
	base := &argumentTestSource{}
	now := time.Now()
	decoder := chatsecurity.NewDecoder(uuid.New(), nil, false)
	arguments, err := decoder.CollectArguments([]chatsecurity.Argument{{Name: "message", Value: "héllo"}}, nil, 0, 0, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	src := WithChatArguments(base, arguments)
	for range 3 {
		for range 2 {
			fork, ok := src.InContext(Context{})
			if !ok || fork.ChatArgument("message") == nil || fork.ChatArgument("message").Body().Content() != "héllo" {
				t.Fatal("fork lost argument")
			}
		}
		src, _ = src.InContext(Context{})
	}
	if src.ChatArgument("action") != nil || base.ChatArgument("message") != nil {
		t.Fatal("argument context leaked")
	}
	function, ok := src.InContext(Context{Silent: true})
	if !ok || function.ChatArgument("message") != nil {
		t.Fatal("function inherited calling command's arguments")
	}
	if wantsFeedback(src) {
		t.Fatal("wrapper lost feedback preference")
	}
	if src.Silent() {
		t.Fatal("wrapper made a loud source silent")
	}
	base.silent = true
	if !src.Silent() {
		t.Fatal("wrapper lost silence")
	}
	reportResult(src, true, 7)
	if base.results != 7 {
		t.Fatal("wrapper lost result sink")
	}
}
