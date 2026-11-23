package world

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestRandomSequenceVanillaFloat(t *testing.T) {
	t.Parallel()
	// Float.floatToRawIntBits on the 26.3 XoroshiroRandomSource.nextFloat.
	const id = "minecraft:entities/zombie"
	seq := NewRandomSequence(12345, id)
	for i, want := range []uint32{0x3f5fb36c, 0x3f6c3e90, 0x3ed48cd4, 0x3f0d3769, 0x3f788bf2} {
		if got := math.Float32bits(seq.Float32()); got != want {
			t.Errorf("float draw %d bits = %#08x, want %#08x", i, got, want)
		}
	}
}

// oneVanilla walks a stream through the draws the vanilla classes made
// of it — new RandomSequence(seed, id) drawn through nextInt and
// Mth.randomBetweenInclusive — state, four longs, five bounds, the state
// then, and two rolls, in the order they came out of the JVM.
func oneVanilla(t *testing.T, seed int64, id string, state [2]int64,
	longs [4]int64, bounds [5]int32, after [2]int64, rolls [2]int32) {
	t.Helper()
	s := NewRandomSequence(seed, id)
	if lo, hi := s.State(); lo != state[0] || hi != state[1] {
		t.Errorf("NewRandomSequence(%d, %q) state = %d, %d, want %d, %d",
			seed, id, lo, hi, state[0], state[1])
	}
	for i, want := range longs {
		if got := s.nextLong(); got != want {
			t.Errorf("NewRandomSequence(%d, %q) long[%d] = %d, want %d", seed, id, i, got, want)
		}
	}
	for i, bound := range []int32{2, 3, 100, 2147483646, 2147483647} {
		if got := s.NextIntBound(bound); got != bounds[i] {
			t.Errorf("NewRandomSequence(%d, %q) IntBound(%d) = %d, want %d",
				seed, id, bound, got, bounds[i])
		}
	}
	if lo, hi := s.State(); lo != after[0] || hi != after[1] {
		t.Errorf("NewRandomSequence(%d, %q) state after = %d, %d, want %d, %d",
			seed, id, lo, hi, after[0], after[1])
	}
	if got := s.NextIntBetween(1, 6); got != rolls[0] {
		t.Errorf("NewRandomSequence(%d, %q) Between(1, 6) = %d, want %d", seed, id, got, rolls[0])
	}
	if got := s.NextIntBetween(-2147483646, -1); got != rolls[1] {
		t.Errorf("NewRandomSequence(%d, %q) Between(-2147483646, -1) = %d, want %d",
			seed, id, got, rolls[1])
	}
}

// TestRandomSequenceMatchesTheJVM pins the port against the draws the
// vanilla classes themselves printed. The minecraft:nonexistent_seq stream
// is the one a vanilla console left in its random_sequences.dat under
// minecraft:extra's salt defaults — the same numbers the live class draws,
// so a saved file this server reopens agrees with a vanilla one that did.
func TestRandomSequenceMatchesTheJVM(t *testing.T) {
	t.Parallel()
	oneVanilla(t, 7, "minecraft:nonexistent_seq",
		[2]int64{-5875842738941693184, 1486724888991878591},
		[4]int64{2276755418746321197, 7278819106130130492, 2473823755598918441, 1351172360755595720},
		[5]int32{1, 2, 0, 786042429, 1849769306},
		[2]int64{-7549536476841735281, 5612987330906271448},
		[2]int32{2, -876711319})
	// The nameless stream the include_sequence_id flag leaves.
	oneVanilla(t, 7, "",
		[2]int64{3289223769585499172, -2442997608809567226},
		[4]int64{-427546222996335712, -6591573495626427012, 4190106153678085083, -7419072112150520130},
		[5]int32{1, 1, 90, 158245290, 234431401},
		[2]int64{-8103968927931825137, -1548840710332807017},
		[2]int32{3, -2088142397})
	oneVanilla(t, 123456789, "minecraft:custom",
		[2]int64{1311538640859519306, 8859669797844694442},
		[4]int64{-2696934583440870504, 8576288486200807077, 2790798329320545434, -1922463493798627170},
		[5]int32{0, 0, 50, 938194522, 1421852613},
		[2]int64{1855912701844020199, 2815376828685892257},
		[2]int32{1, -262032345})
	oneVanilla(t, 0, "minecraft:x",
		[2]int64{9128829177386127285, 6781291133258701389},
		[4]int64{-5552602774730438323, -3803243969687128671, -3298123642694141237, 5117646890827600695},
		[5]int32{1, 0, 41, 2120858061, 1463736909},
		[2]int64{-2854759423976847859, 9077449731726134742},
		[2]int32{6, -1711063534})
	oneVanilla(t, -987654321, "minecraft:weird_id",
		[2]int64{-1966002369664902861, 1357496498179186867},
		[4]int64{3673784974669645903, 1308709101530410658, 4577027966483935832, 2970696014482289346},
		[5]int32{1, 0, 65, 1708788067, 197251923},
		[2]int64{8100379612707234243, 9040649013649855487},
		[2]int32{6, -1506938923})
	// A seed at the golden ratio itself, which the widening makes twofold.
	oneVanilla(t, -7046029254386353131, "",
		[2]int64{580542743492490023, 1130685008087477333},
		[4]int64{663205598535688870, -217595390635831721, -3365592150225030665, 7473037712926346349},
		[5]int32{1, 2, 54, 378134985, 1604730760},
		[2]int64{2675302194994965500, -3334628491961200956},
		[2]int32{6, -1355040038})
}

// TestRandomSequencesRoundTrip walks three sessions of one world folder: a
// sequence created and drawn in the first, the file the save writes
// carrying its advanced state and the reset defaults, and the reopened
// worlds resuming the stream from where the draws left it. An empty world
// reads the defaults vanilla starts a world from.
func TestRandomSequencesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const seed = int64(123456789)

	s := NewRandomSequences(dir)
	if s.salt != 0 || !s.includeWorldSeed || !s.includeSequenceID || len(s.sequences) != 0 {
		t.Fatalf("an empty world reads salt %d, flags %v/%v and %d sequences, want 0, true, true, none",
			s.salt, s.includeWorldSeed, s.includeSequenceID, len(s.sequences))
	}

	// The first draw of a name creates the stream and the draws advance it;
	// the save owes the file the state they left.
	kdice := s.Get("minecraft:kdice", seed)
	s.Save()
	for range 3 {
		kdice.IntBetween(0, 1<<30)
	}
	kdice.Float32()
	kdice.NextBoolean()
	wantLo, wantHi := kdice.sequence.State()
	s.Save()

	reopened := NewRandomSequences(dir)
	if lo, hi := reopened.Get("minecraft:kdice", seed).sequence.State(); lo != wantLo || hi != wantHi {
		t.Fatalf("the reopened kdice stands at %d, %d, want %d, %d", lo, hi, wantLo, wantHi)
	}

	// A reset-all's salt and flags move the defaults the streams created
	// afterwards derive from: this one folds the world seed out and the
	// name in, so it is the seed-42 stream of its own name.
	reopened.ResetAll(42, true, false, true)
	afterwards := reopened.Get("minecraft:afterwards", seed)
	want2Lo, want2Hi := NewRandomSequence(42, "minecraft:afterwards").State()
	if lo, hi := afterwards.sequence.State(); lo != want2Lo || hi != want2Hi {
		t.Errorf("after the reset a new sequence stands at %d, %d, want %d, %d",
			lo, hi, want2Lo, want2Hi)
	}
	reopened.Save()

	data, err := readSavedData(dir, randomSequencesFile)
	if err != nil {
		t.Fatalf("read the saved sequences: %v", err)
	}
	if got, ok := nbt.Get[nbt.Int](data, "salt"); !ok || int32(got) != 42 {
		t.Errorf("the saved salt = %d, ok %v, want 42", got, ok)
	}
	if got, ok := nbt.Get[nbt.Byte](data, "include_world_seed"); !ok || got != 0 {
		t.Errorf("the saved include_world_seed = %d, ok %v, want the 0 of false", got, ok)
	}
	// vanilla: RandomSequences.CODEC, whose optional field writes back
	// nothing beside the default it stands at.
	if _, ok := nbt.Get[nbt.Byte](data, "include_sequence_id"); ok {
		t.Errorf("the saved file carries include_sequence_id, want it beside the default to be absent")
	}
	// The reset-all emptied the map the drawn kdice filled; the session
	// after the save reads the one sequence the reset made.
	third := NewRandomSequences(dir)
	if len(third.sequences) != 1 || third.sequences["minecraft:afterwards"] == nil {
		t.Errorf("the third session reads %v, want the minecraft:afterwards the reset made", third.sequences)
	}
	if third.salt != 42 || third.includeWorldSeed || !third.includeSequenceID {
		t.Errorf("the third session reads salt %d, flags %v/%v, want 42, false, true",
			third.salt, third.includeWorldSeed, third.includeSequenceID)
	}
}
