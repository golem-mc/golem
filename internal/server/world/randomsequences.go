package world

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"log/slog"
	"math/bits"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// randomSequencesFile is vanilla's file for /random's named sequences, beside the
// game rules and clocks.
// vanilla: RandomSequences.TYPE
const randomSequencesFile = "data/minecraft/random_sequences.dat"

// RandomSequence is the Xoroshiro128++ stream vanilla's /random draws a named
// sequence from, seeded as RandomSequence.createSequence does: a number widened
// to two longs, the resource id hashed over them when present, both halves
// mixed with mixStafford13. It uses the JVM's arithmetic because /random values
// and a reopened world's random_sequences.dat state must match the real stream
// exactly.
//
// Go's int64 wraps and shifts like the JVM's. The stream takes no lock:
// RandomSequences.Draw steps it under the sequences' lock, which the save also
// reads under.
type RandomSequence struct{ lo, hi int64 }

// The two ratios RandomSupport widens and escapes with. vanilla:
// RandomSupport.GOLDEN_RATIO_64, RandomSupport.SILVER_RATIO_64,
// Xoroshiro128PlusPlus(long, long)
const (
	xoroshiroGoldenRatio int64 = -7046029254386353131
	xoroshiroSilverRatio int64 = 7640891576956012809
)

// mixStafford13 mixes each half of a 128-bit seed. Java's >>> must run on the
// unsigned form: Go's >> would shift in the sign where the JVM shifts in zeros.
// vanilla: RandomSupport.mixStafford13
func mixStafford13(z int64) int64 {
	v := uint64(z)
	v ^= v >> 30
	v *= 0xBF58476D1CE4E5B9
	v ^= v >> 27
	v *= 0x94D049BB133111EB
	v ^= v >> 31
	return int64(v)
}

// NewRandomSequence derives the stream a named sequence makes from seed:
// widened as upgradeSeedTo128bitUnmixed does, the id's MD5 xored over both
// longs when the id is non-empty, then mixed. The digest is read big-endian, as
// Guava's Longs.fromBytes gives seedFromHashOf.
// vanilla: RandomSequence.createSequence, RandomSupport.seedFromHashOf
func NewRandomSequence(seed int64, id string) *RandomSequence {
	lo := seed ^ xoroshiroSilverRatio
	hi := lo + xoroshiroGoldenRatio
	if id != "" {
		sum := md5.Sum([]byte(id))
		lo ^= int64(binary.BigEndian.Uint64(sum[0:8]))
		hi ^= int64(binary.BigEndian.Uint64(sum[8:]))
	}
	return &RandomSequence{lo: mixStafford13(lo), hi: mixStafford13(hi)}
}

// RandomSequenceFromState rebuilds a stream from a saved sequence's two longs;
// the all-zero pair, which the generator cannot step from, becomes the two
// ratios.
// vanilla: Xoroshiro128PlusPlus(long, long)
func RandomSequenceFromState(lo, hi int64) *RandomSequence {
	if lo|hi == 0 {
		return &RandomSequence{lo: xoroshiroGoldenRatio, hi: xoroshiroSilverRatio}
	}
	return &RandomSequence{lo: lo, hi: hi}
}

// State is the sequence's two longs, the saved form a reopened world resumes
// from.
func (s *RandomSequence) State() (lo, hi int64) { return s.lo, s.hi }

// nextLong steps the state once and returns the Xoroshiro128++ draw.
// vanilla: Xoroshiro128PlusPlus.nextLong
func (s *RandomSequence) nextLong() int64 {
	s0, s1 := s.lo, s.hi
	result := int64(bits.RotateLeft64(uint64(s0+s1), 17)) + s0
	s1 ^= s0
	s.lo = int64(bits.RotateLeft64(uint64(s0), 49)) ^ s1 ^ s1<<21
	s.hi = int64(bits.RotateLeft64(uint64(s1), 28))
	return result
}

// nextInt is a long's low half, the source's nextInt().
// vanilla: XoroshiroRandomSource.nextInt()
func (s *RandomSequence) nextInt() int32 { return int32(s.nextLong()) }

// NextIntBound draws in [0, bound): one 32-bit draw scaled by bound, cut at the
// low half, redrawn while the cut falls in the share of the 2^32 wrap the bound
// cannot divide evenly. It is 64-bit arithmetic, wrapping where the JVM's
// widening makes its rejection loop trip.
// vanilla: XoroshiroRandomSource.nextInt(int)
func (s *RandomSequence) NextIntBound(bound int32) int32 {
	if bound <= 0 {
		// Java throws for a non-positive bound, and the commands' range checks
		// keep every caller above zero.
		panic("world: RandomSequence.NextIntBound with a non-positive bound")
	}
	product := uint64(uint32(s.nextInt())) * uint64(bound)
	low := product & 0xFFFFFFFF
	if low < uint64(bound) {
		// remainderUnsigned(~bound + 1, bound): the low halves a fair share
		// spills below the bound.
		threshold := (uint64(1)<<32 - uint64(bound)) % uint64(bound)
		for low < threshold {
			product = uint64(uint32(s.nextInt())) * uint64(bound)
			low = product & 0xFFFFFFFF
		}
	}
	return int32(product >> 32)
}

// NextIntBetween draws in [lo, hi] inclusive, as Mth.randomBetweenInclusive
// does for /random.
// vanilla: Mth.randomBetweenInclusive
func (s *RandomSequence) NextIntBetween(lo, hi int32) int32 {
	return lo + s.NextIntBound(hi-lo+1)
}

// RandomSequences is vanilla's RandomSequences saved data: resource ids to
// seeded streams, plus the salt and two flags a reset defaults to. The file is
// one compound, read when the world opens and written whole by later saves.
// vanilla: RandomSequences
type RandomSequences struct {
	root string
	mu   sync.Mutex

	salt              int32
	includeWorldSeed  bool
	includeSequenceID bool
	sequences         map[string]*RandomSequence
	// dirty is set by a draw and, unlike vanilla, by a create or reset too, so
	// a sequence made but never drawn from still reaches the file.
	dirty bool
}

// NewRandomSequences loads root's sequences from a file this server or vanilla
// left, or starts from vanilla's default salt and flags.
func NewRandomSequences(root string) *RandomSequences {
	s := &RandomSequences{
		root:              root,
		includeWorldSeed:  true,
		includeSequenceID: true,
		sequences:         map[string]*RandomSequence{},
	}
	data, err := readSavedData(root, randomSequencesFile)
	switch {
	case errors.Is(err, errNoSavedData):
		return s
	case err != nil:
		slog.Warn("could not read the random sequences", "error", err)
		return s
	}
	if v, ok := nbt.Get[nbt.Int](data, "salt"); ok {
		s.salt = int32(v)
	}
	// The flags are RecordCodecBuilder optional fields: a writer keeping the
	// default wrote none, and a missing flag reads the default.
	// vanilla: RandomSequences.CODEC
	if v, ok := nbt.Get[nbt.Byte](data, "include_world_seed"); ok {
		s.includeWorldSeed = v != 0
	}
	if v, ok := nbt.Get[nbt.Byte](data, "include_sequence_id"); ok {
		s.includeSequenceID = v != 0
	}
	if all, ok := nbt.Get[nbt.Compound](data, "sequences"); ok {
		for id, tag := range all {
			one, ok := tag.(nbt.Compound)
			if !ok {
				slog.Warn("random sequence with no compound", "id", id)
				continue
			}
			source, ok := nbt.Get[nbt.LongArray](one, "source")
			if !ok || len(source) != 2 {
				slog.Warn("random sequence with no seed pair", "id", id)
				continue
			}
			s.sequences[id] = RandomSequenceFromState(int64(source[0]), int64(source[1]))
		}
	}
	return s
}

// Get returns the named sequence, creating it from the world seed under the
// current defaults if absent (vanilla's computeIfAbsent on a name's first
// draw).
// vanilla: RandomSequences.get
func (s *RandomSequences) Get(id string, worldSeed int64) *RandomSequence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id, worldSeed)
}

// getLocked is Get with s.mu held.
func (s *RandomSequences) getLocked(id string, worldSeed int64) *RandomSequence {
	if seq, ok := s.sequences[id]; ok {
		return seq
	}
	seq := s.create(id, worldSeed, s.salt, s.includeWorldSeed, s.includeSequenceID)
	s.sequences[id] = seq
	s.dirty = true
	return seq
}

// Draw draws in [lo, hi] from the named sequence, creating it if absent, and
// marks the sequences changed. It runs under the lock because commands run on
// each client's read goroutine, so two players and a save could otherwise step
// and read one state at once.
// vanilla: RandomSequences.get, RandomSequences$DirtyMarkingRandomSource
func (s *RandomSequences) Draw(id string, worldSeed int64, lo, hi int32) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := s.getLocked(id, worldSeed)
	s.dirty = true
	return seq.NextIntBetween(lo, hi)
}

// Reset restarts the named sequence with the salt and flags the command gave,
// or the current defaults; an unseen name starts fresh either way.
// vanilla: RandomSequences.reset
func (s *RandomSequences) Reset(id string, worldSeed int64, salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !hasSalt {
		salt, includeWorldSeed, includeSequenceID = s.salt, s.includeWorldSeed, s.includeSequenceID
	}
	s.sequences[id] = s.create(id, worldSeed, salt, includeWorldSeed, includeSequenceID)
	s.dirty = true
}

// ResetAll clears every sequence and returns the count; the command's salt and
// flags become the defaults for later sequences.
// vanilla: RandomSequences.setSeedDefaults, RandomSequences.clear
func (s *RandomSequences) ResetAll(salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasSalt {
		s.salt, s.includeWorldSeed, s.includeSequenceID = salt, includeWorldSeed, includeSequenceID
	}
	count := len(s.sequences)
	s.sequences = map[string]*RandomSequence{}
	s.dirty = true
	return count
}

// MarkDirty records a draw as a change the next save owes, as
// DirtyMarkingRandomSource does.
// vanilla: RandomSequences$DirtyMarkingRandomSource
func (s *RandomSequences) MarkDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

// IDs lists the existing sequences, for the sequence argument's completion.
func (s *RandomSequences) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sequences))
	for id := range s.sequences {
		ids = append(ids, id)
	}
	return ids
}

// create derives a sequence from its flags: the world seed folded in if
// flagged, the salt xored over the seed, the name hashed in if flagged.
// vanilla: RandomSequences.createSequence
func (s *RandomSequences) create(id string, worldSeed int64, salt int32, includeWorldSeed, includeSequenceID bool) *RandomSequence {
	seed := int64(0)
	if includeWorldSeed {
		seed = worldSeed
	}
	if !includeSequenceID {
		id = ""
	}
	return NewRandomSequence(seed^int64(salt), id)
}

// Save writes the sequences if any changed since the last save.
func (s *RandomSequences) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	data := nbt.Compound{"salt": nbt.Int(s.salt)}
	// A flag at its default is not written, as RecordCodecBuilder's optional
	// fields encode.
	// vanilla: RandomSequences.CODEC
	if !s.includeWorldSeed {
		data["include_world_seed"] = nbt.Byte(0)
	}
	if !s.includeSequenceID {
		data["include_sequence_id"] = nbt.Byte(0)
	}
	sequences := nbt.Compound{}
	for id, seq := range s.sequences {
		lo, hi := seq.State()
		sequences[id] = nbt.Compound{"source": nbt.LongArray{lo, hi}}
	}
	data["sequences"] = sequences
	if err := writeSavedData(s.root, randomSequencesFile, data); err != nil {
		slog.Error("could not save the random sequences", "error", err)
		return
	}
	s.dirty = false
}
