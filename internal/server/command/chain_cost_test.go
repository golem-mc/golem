package command

import (
	"errors"
	"runtime"
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

// allocatedBy is how many bytes run allocates.
func allocatedBy(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestLongExecuteChainCostsLinear checks that matching and completing a
// chained execute of the longest line a client sends costs memory in
// proportion to its words: a walk that copied its path or the rest of the
// line at each word took a gigabyte for it. The bound is generous, so only a
// walk that has gone back to costing the square of the line's words fails.
func TestLongExecuteChainCostsLinear(t *testing.T) {
	const bound = 64 << 20
	tree := ForLevel(PermLevelGamemasters)
	input := longExecuteChain()
	tokens := Tokenize(input)
	var node *Node
	if n := allocatedBy(func() { node, _ = Match(tree, input, tokens, true) }); n > bound {
		t.Errorf("Match allocated %d bytes, want at most %d", n, bound)
	}
	if node == nil || !node.Executable {
		t.Fatalf("Match(%d words) = %v, want the say it runs", len(tokens), node)
	}
	partial := "/" + input[:strings.LastIndex(input, " as ")+len(" as ")]
	var matches []string
	if n := allocatedBy(func() { _, matches = Suggest(tree, partial, nil) }); n > bound {
		t.Errorf("Suggest allocated %d bytes, want at most %d", n, bound)
	}
	if len(matches) != 0 {
		t.Errorf("Suggest offered %q with no source to complete targets from, want nothing", matches)
	}
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

// TestForkStopsReadingAtTheLimit checks that a fork step stops at the first
// context whose bodies bring its list to max_command_forks, without reading
// the contexts behind it.
// vanilla: BuildContexts.execute
func TestForkStopsReadingAtTheLimit(t *testing.T) {
	src := &forkSource{limit: 55}
	for range 10 {
		src.bodies = append(src.bodies, forkBody{})
	}
	ctxs := make([]execContext, 100)
	settle := func(c *execContext, e Entity) { c.executor = e }

	next, _, err := stepFork(src, ctxs, "@e", settle)
	if !errors.Is(err, errForkLimit) || next != nil {
		t.Fatalf("stepFork over 100 contexts of 10 bodies = %d contexts, %v; want errForkLimit", len(next), err)
	}
	// Five contexts make 50; the sixth would make 60.
	if src.views != 6 {
		t.Errorf("stepFork read %d contexts, want the 6 up to the one that reached the limit", src.views)
	}

	src.views, src.limit = 0, 1001
	next, _, err = stepFork(src, ctxs, "@e", settle)
	if err != nil || len(next) != 1000 {
		t.Errorf("stepFork under the limit = %d contexts, %v; want 1000", len(next), err)
	}
}

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
