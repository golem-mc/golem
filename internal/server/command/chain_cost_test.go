package command

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// longExecuteChain is an execute of about 32k characters, the most a chat
// command carries, that chains as @s until it runs a say.
func longExecuteChain() string {
	return "execute " + strings.Repeat("as @s ", 32700/6) + "run say hi"
}

// forkSource is a Source holding bodies for an @e to name, which counts the
// contexts a step reads through it.
type forkSource struct {
	Source
	bodies []Entity
	limit  int32
	views  int
}

func (s *forkSource) InContext(Context) (Source, bool)  { s.views++; return s, true }
func (s *forkSource) IntRule(world.GameRuleIndex) int32 { return s.limit }
func (s *forkSource) PermLevel() int                    { return PermLevelOwners }
func (s *forkSource) Entities(q EntityQuery) []Entity {
	if q.Limit > 0 {
		return filterUpTo(s.bodies, q.Accept, q.Limit)
	}
	return s.bodies
}
func (s *forkSource) OnlineTargets() []Target   { return nil }
func (s *forkSource) Position() player.Position { return player.Position{} }
func (s *forkSource) DimensionName() string     { return world.DimensionName }

// forkBody is a body an @e names.
type forkBody struct{ Entity }

func (forkBody) Position() player.Position { return player.Position{} }
func (forkBody) DimensionName() string     { return world.DimensionName }
func (forkBody) Alive() bool               { return true }

// BenchmarkMatchLongExecuteChain is the walk of the longest chained execute.
func BenchmarkMatchLongExecuteChain(b *testing.B) {
	tree := ForLevel(PermLevelGamemasters)
	input := longExecuteChain()
	tokens := Tokenize(input)
	b.ReportAllocs()
	for b.Loop() {
		if node, _ := Match(tree, input, tokens, true); node == nil {
			b.Fatal("no match")
		}
	}
}

// BenchmarkSuggestLongExecuteChain completes the targets of the last as in
// the longest chained execute.
func BenchmarkSuggestLongExecuteChain(b *testing.B) {
	tree := ForLevel(PermLevelGamemasters)
	input := longExecuteChain()
	partial := "/" + input[:strings.LastIndex(input, " as ")+len(" as ")]
	b.ReportAllocs()
	for b.Loop() {
		Suggest(tree, partial, nil)
	}
}
