package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

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

// TestRandomSequenceResumesFromSavedState checks the pair a saved sequence
// carries rebuilds the stream at the point the last draw left it: the
// all-zero pair the generator's own escape, the saved state of the console's
// nonexistent_seq, and the state the same console's one draw of
// minecraft:extra — the draw printed the 1 the fresh stream rolls here.
// The extra state is the pair the session's random_sequences.dat kept.
func TestRandomSequenceResumesFromSavedState(t *testing.T) {
	t.Parallel()
	s := RandomSequenceFromState(0, 0)
	if lo, hi := s.State(); lo != xoroshiroGoldenRatio || hi != xoroshiroSilverRatio {
		t.Errorf("FromState(0, 0) = %d, %d, want the ratios %d, %d",
			lo, hi, xoroshiroGoldenRatio, xoroshiroSilverRatio)
	}
	if got := s.nextLong(); got != 6807859099481836695 {
		t.Errorf("FromState(0, 0) long[0] = %d, want 6807859099481836695", got)
	}
	if got := s.NextIntBound(3); got != 0 {
		t.Errorf("FromState(0, 0) IntBound(3) = %d, want 0", got)
	}
	extra := NewRandomSequence(7, "minecraft:extra")
	if got := extra.NextIntBetween(1, 2); got != 1 {
		t.Errorf("the fresh minecraft:extra drew %d, want 1", got)
	}
	if lo, hi := extra.State(); lo != -1048954605574995705 || hi != -6620296267031638231 {
		t.Errorf("after the draw the state = %d, %d, want -1048954605574995705, -6620296267031638231", lo, hi)
	}
}

// TestRandomSequenceResetsToTheSameStream checks a reset makes the name's
// stream over: the same seed and id draw what the fresh one drew.
func TestRandomSequenceResetsToTheSameStream(t *testing.T) {
	t.Parallel()
	first := NewRandomSequence(7, "minecraft:kdice")
	var draws [8]int32
	for i := range draws {
		draws[i] = first.NextIntBetween(0, 2147483646)
	}
	reset := NewRandomSequence(7, "minecraft:kdice")
	for i, want := range draws {
		if got := reset.NextIntBetween(0, 2147483646); got != want {
			t.Fatalf("the reset drew %d at %d, want %d", got, i, want)
		}
	}
}

// TestRandomSequencesDrawDoesNotRaceSave draws one name from two goroutines
// while a third saves the sequences, for the race detector: Draw steps a
// stream under the lock the save reads every stream under, so neither sees
// the other's half-finished state.
func TestRandomSequencesDrawDoesNotRaceSave(t *testing.T) {
	t.Parallel()
	s := NewRandomSequences(t.TempDir())
	const seed = int64(123456789)
	const turns = 200

	var wg sync.WaitGroup
	wg.Add(3)
	for range 2 {
		go func() {
			defer wg.Done()
			for range turns {
				s.Draw("minecraft:dice", seed, 1, 6)
			}
		}()
	}
	go func() {
		defer wg.Done()
		for range turns {
			s.Save()
		}
	}()
	wg.Wait()
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
	for range 3 {
		kdice.NextIntBetween(0, 1<<30)
	}
	wantLo, wantHi := kdice.State()
	s.MarkDirty()
	s.Save()

	reopened := NewRandomSequences(dir)
	if lo, hi := reopened.Get("minecraft:kdice", seed).State(); lo != wantLo || hi != wantHi {
		t.Fatalf("the reopened kdice stands at %d, %d, want %d, %d", lo, hi, wantLo, wantHi)
	}

	// A reset-all's salt and flags move the defaults the streams created
	// afterwards derive from: this one folds the world seed out and the
	// name in, so it is the seed-42 stream of its own name.
	reopened.ResetAll(42, true, false, true)
	afterwards := reopened.Get("minecraft:afterwards", seed)
	want2Lo, want2Hi := NewRandomSequence(42, "minecraft:afterwards").State()
	if lo, hi := afterwards.State(); lo != want2Lo || hi != want2Hi {
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

// TestRandomSequenceResetRestartsTheStream checks the reset forms: the one
// that names a sequence restarts it under the defaults where the words gave
// no salt and under the words' salt and flags where they did, and the star
// reset answers by the count it cleared.
func TestRandomSequenceResetRestartsTheStream(t *testing.T) {
	s := NewRandomSequences(t.TempDir())
	const seed = int64(-987654321)

	freshLo, freshHi := s.Get("minecraft:x", seed).State()
	// A reset without a salt rebuilds the stream the fresh one was.
	s.Reset("minecraft:x", seed, 0, false, true, true)
	if lo, hi := s.Get("minecraft:x", seed).State(); lo != freshLo || hi != freshHi {
		t.Errorf("the default reset moved the stream to %d, %d, want %d, %d", lo, hi, freshLo, freshHi)
	}
	// A salted reset seeds the stream the words named: no world seed, and
	// the name folded out by its own flag.
	saltedLo, saltedHi := NewRandomSequence(37, "").State()
	s.Reset("minecraft:x", seed, 37, true, false, false)
	if lo, hi := s.Get("minecraft:x", seed).State(); lo != saltedLo || hi != saltedHi {
		t.Errorf("the salted reset moved the stream to %d, %d, want %d, %d", lo, hi, saltedLo, saltedHi)
	}
	// A second name makes the count the star reset answers with.
	s.Get("minecraft:y", seed)
	if count := s.ResetAll(0, false, true, true); count != 2 {
		t.Errorf("the star reset counted %d, want the two sequences it cleared", count)
	}
	if ids := s.IDs(); len(ids) != 0 {
		t.Errorf("after the star reset the ids = %v, want none", ids)
	}
}
