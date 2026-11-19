package server

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand"
	"sync"

	"github.com/golem-mc/golem/internal/server/rules"
)

// lockedSource makes a rand.Rand safe across goroutines: the Rand methods
// the server calls touch only their source.
type lockedSource struct {
	mu  sync.Mutex
	src rand.Source64
}

func (l *lockedSource) Int63() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.src.Int63()
}

func (l *lockedSource) Uint64() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.src.Uint64()
}

func (l *lockedSource) Seed(seed int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.src.Seed(seed)
}

// newDice returns a goroutine-safe random source seeded with seed.
func newDice(seed int64) *rand.Rand {
	return rand.New(&lockedSource{src: rand.NewSource(seed).(rand.Source64)})
}

// subSeed derives a named consumer's seed from a root, so each source's
// sequence is independent of others' roll order.
func subSeed(root int64, name string, n int64) int64 {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:8], uint64(root))
	binary.LittleEndian.PutUint64(b[8:], uint64(n))
	h := fnv.New64a()
	h.Write(b[:])
	h.Write([]byte(name))
	return int64(h.Sum64())
}

// seedFor derives a seed from the root for the named consumer, n telling
// several of one kind apart (an entity ID, say).
func (s *Server) seedFor(name string, n int64) int64 {
	return subSeed(s.seed, name, n)
}

// seedDice seeds every server-owned random source from New's root seed.
func (s *Server) seedDice(root int64) {
	s.seed = root
	s.lootRng = newDice(s.seedFor("loot", 0))
	s.popRng = newDice(s.seedFor("pop", 0))
	s.infestRng = newDice(s.seedFor("infested", 0))
	s.levelRng = newDice(s.seedFor("level", 0))
	s.soundRng = newDice(s.seedFor("sound", 0))
	s.spawnRng = rand.New(rand.NewSource(s.seedFor("spawn", 0)))
	s.envRng = newDice(s.seedFor("environment", 0))
	s.dropRng = rules.NewJavaRandom(s.seedFor("spill", 0))
	s.dispenseRng = rules.NewJavaRandom(s.seedFor("dispense", 0))
	for i, d := range s.allDims() {
		d.w.SetLootFill(s.lootFillFor(lootOpener{}))
		d.w.SetLootReads(lootReads{s})
		d.w.SetSittingCats(sittingCatSource{s: s, dim: d})
		d.w.SeedDice(s.seedFor("world", int64(i)))
	}
}

// entitySeed is the seed of the entity's own random source.
func (s *Server) entitySeed(id int32) int64 {
	return s.seedFor("entity", int64(id))
}
