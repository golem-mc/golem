package redstone

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// noteHeard collects the note tones (Sound Effect) and note particles
// (Block Action) a stretch of packets carried for one note block.
type noteHeard struct {
	sounds  []noteTone
	actions int
}

// noteTone is one played note's sound event and pitch.
type noteTone struct {
	sound int32
	pitch float32
}

// recordNote keeps the records-category, volume-3 note sounds at (x, y, z)
// and counts the 0/0 block events there.
// vanilla: NoteBlock.triggerEvent
func (h *noteHeard) recordNote(t *testing.T, x, y, z int32, id int32, data []byte) {
	t.Helper()
	switch id {
	case protocol.IDPlaySoundEffect:
		r := protocol.NewReader(data)
		sound := r.ReadVarInt() - 1
		if category := r.ReadVarInt(); category != protocol.SoundCategoryRecord {
			return
		}
		if fx, fy, fz := r.ReadI32(), r.ReadI32(), r.ReadI32(); fx != x*8+4 || fy != y*8+4 || fz != z*8+4 {
			return
		}
		if volume := r.ReadF32(); volume != 3 {
			t.Errorf("a note tone went out at volume %.2f, want vanilla's 3", volume)
			return
		}
		h.sounds = append(h.sounds, noteTone{sound: sound, pitch: r.ReadF32()})
	case protocol.IDPlayBlockAction:
		r := protocol.NewReader(data)
		if ax, ay, az := r.ReadPosition(); ax != x || ay != y || az != z {
			return
		}
		action, param, block := int32(r.ReadU8()), int32(r.ReadU8()), r.ReadVarInt()
		if action != 0 || param != 0 || block != protocol.BlockNoteBlock {
			t.Errorf("the note's block event: action=%d param=%d block=%d, want 0 0 %d",
				action, param, block, protocol.BlockNoteBlock)
			return
		}
		h.actions++
	}
}

// wantTones fails unless exactly the given notes were heard, one particle
// per tone; mob-head imitations play at pitch 1.
func (h *noteHeard) wantTones(t *testing.T, instrument string, notes ...int) {
	t.Helper()
	if len(h.sounds) != len(notes) || h.actions != len(notes) {
		t.Fatalf("heard %d note sounds and %d note events, want %d of each", len(h.sounds), h.actions, len(notes))
	}
	if len(notes) == 0 {
		return
	}
	sound := noteSound(t, instrument)
	for i, note := range notes {
		pitch := float32(1)
		if !noteImitated(instrument) {
			pitch = float32(math.Pow(2, float64(note-12)/12))
		}
		if h.sounds[i].sound != sound {
			t.Errorf("tone %d = sound event %d, want the %s's %d", i, h.sounds[i].sound, instrument, sound)
		}
		if d := h.sounds[i].pitch - pitch; d < -0.001 || d > 0.001 {
			t.Errorf("tone %d = pitch %.4f, want %.4f", i, h.sounds[i].pitch, pitch)
		}
	}
}

// noteSound is the sound event id an instrument plays.
// vanilla: NoteBlockInstrument.getSoundEvent
func noteSound(t *testing.T, instrument string) int32 {
	t.Helper()
	switch instrument {
	case "zombie", "skeleton", "creeper", "dragon", "wither_skeleton", "piglin":
		instrument = "imitate." + instrument
	}
	id, ok := registry.SoundEventID("minecraft:block.note_block." + instrument)
	if !ok {
		t.Fatalf("no sound event for the %s instrument", instrument)
	}
	return id
}

// noteImitated reports whether an instrument is a mob head's voice, whose
// pitch does not follow the note.
func noteImitated(instrument string) bool {
	switch instrument {
	case "zombie", "skeleton", "creeper", "dragon", "wither_skeleton", "piglin":
		return true
	}
	return false
}

// observerAt is the state id of an observer facing the given way.
func observerAt(t *testing.T, facing string, powered bool) uint16 {
	t.Helper()
	want := "false"
	if powered {
		want = "true"
	}
	return servertest.BlockStateWith(t, "minecraft:observer", "facing="+facing+",powered="+want)
}

// recordObserverFlash counts one packet's powered and unpowered writes of
// the observer at (x, y, z).
func recordObserverFlash(t *testing.T, x, y, z int32, facing string, id int32, data []byte) (trues, falses int) {
	t.Helper()
	if id != protocol.IDPlayBlockUpdate {
		return 0, 0
	}
	r := protocol.NewReader(data)
	if ax, ay, az := r.ReadPosition(); ax != x || ay != y || az != z {
		return 0, 0
	}
	if state := r.ReadVarInt(); state == int32(observerAt(t, facing, true)) {
		return 1, 0
	} else if state == int32(observerAt(t, facing, false)) {
		return 0, 1
	}
	return 0, 0
}

// noteClick right-clicks a block, then ticks so an observer's pulse or a
// comparator's answer lands, collecting both stretches into record.
func noteClick(t *testing.T, srv *server.Server, c *servertest.Client, x, y, z int32, record func(int32, []byte)) {
	t.Helper()
	c.UseBlock(x, y, z)
	c.SettleAndCollect(record)
	srv.TickPhysics(8)
	c.SettleAndCollect(record)
}

// noteTicks ticks out and collects a setblock's scheduled wake.
func noteTicks(t *testing.T, srv *server.Server, c *servertest.Client, record func(int32, []byte)) {
	t.Helper()
	srv.TickPhysics(8)
	c.SettleAndCollect(record)
}

// TestARightClickRetunesAndPlaysTheNextNote: each click steps the note up
// and plays the new note.
// vanilla: NoteBlock.useWithoutItem
func TestARightClickRetunesAndPlaysTheNextNote(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "note_block")
	c.Settle()

	var heard noteHeard
	collect := func(id int32, data []byte) { heard.recordNote(t, 4, 64, 2, id, data) }
	c.UseBlock(4, 64, 2)
	c.SettleAndCollect(collect)
	c.UseBlock(4, 64, 2)
	c.SettleAndCollect(collect)

	heard.wantTones(t, "harp", 1, 2)
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 2)); props["note"] != "2" {
		t.Errorf("the note block's note = %s after two clicks, want 2", props["note"])
	}
}

// TestTheTwentyFifthClickWrapsTheNoteAround: note 24 wraps to 0.
func TestTheTwentyFifthClickWrapsTheNoteAround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "note_block")
	c.Settle()

	for range 24 {
		c.UseBlock(4, 64, 2)
		c.Settle()
	}
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 2)); props["note"] != "24" {
		t.Fatalf("the note block's note = %s after twenty-four clicks from 0, want 24", props["note"])
	}
	c.UseBlock(4, 64, 2)
	c.Settle()
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 2)); props["note"] != "0" {
		t.Errorf("the note block's note = %s after twenty-five clicks from 0, want the wrap back to 0", props["note"])
	}
}

// TestAnObserverWatchingANoteBlockPulsesOnARetune: the retune is a state
// change an observer pulses on.
func TestAnObserverWatchingANoteBlockPulsesOnARetune(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "note_block")
	servertest.Setblock(t, c, 4, 64, 3, "observer[facing=north]")
	c.Settle()

	var heard noteHeard
	trues, falses := 0, 0
	noteClick(t, srv, c, 4, 64, 2, func(id int32, data []byte) {
		heard.recordNote(t, 4, 64, 2, id, data)
		t2, f2 := recordObserverFlash(t, 4, 64, 3, "north", id, data)
		trues, falses = trues+t2, falses+f2
	})

	heard.wantTones(t, "harp", 1)
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 2)); props["note"] != "1" {
		t.Errorf("the note = %s after the click, want 1", props["note"])
	}
	if trues != 1 || falses != 1 {
		t.Errorf("the observer flashed %d powered writes and %d unpowered, want one of each", trues, falses)
	}
}

// TestALeverPowersANoteBlockOncePerRisingEdge: each rising edge plays once
// and sets powered; the fall only clears powered.
// vanilla: NoteBlock.neighborChanged
func TestALeverPowersANoteBlockOncePerRisingEdge(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 0, "stone")
	servertest.Setblock(t, c, 4, 65, 0, "lever[face=floor,facing=north,powered=false]")
	servertest.Setblock(t, c, 5, 63, 0, "dirt")
	servertest.Setblock(t, c, 5, 64, 0, "note_block")
	servertest.Setblock(t, c, 5, 64, 1, "observer[facing=north]")
	c.Settle()

	flip := func() (heard noteHeard, trues, falses int) {
		var h noteHeard
		t2, f2 := 0, 0
		noteClick(t, srv, c, 4, 65, 0, func(id int32, data []byte) {
			h.recordNote(c.T, 5, 64, 0, id, data)
			a, b := recordObserverFlash(c.T, 5, 64, 1, "north", id, data)
			t2, f2 = t2+a, f2+b
		})
		return h, t2, f2
	}

	heard, trues, falses := flip()
	heard.wantTones(t, "harp", 0)
	if _, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); props["powered"] != "true" {
		t.Errorf("the note block is powered=%s after the lever rose, want true", props["powered"])
	}
	if trues != 1 || falses != 1 {
		t.Errorf("the observer flashed %d and %d on the rise, want one of each", trues, falses)
	}

	heard, trues, falses = flip()
	heard.wantTones(t, "harp")
	if _, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); props["powered"] != "false" {
		t.Errorf("the note block is powered=%s after the lever fell, want false", props["powered"])
	}
	if trues != 1 || falses != 1 {
		t.Errorf("the observer flashed %d and %d on the fall, want one of each", trues, falses)
	}

	heard, _, _ = flip()
	heard.wantTones(t, "harp", 0)
}

// TestAComparatorFeedingANoteBlockPlaysItOncePerRisingEdge: a comparator's
// output plays the note once, not again while it holds.
func TestAComparatorFeedingANoteBlockPlaysItOncePerRisingEdge(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 3, 63, 0, "dirt")
	servertest.Setblock(t, c, 3, 64, 0, "note_block")
	servertest.Setblock(t, c, 4, 64, 0, "comparator[facing=east]")
	servertest.Setblock(t, c, 5, 64, 0, "stone")
	servertest.Setblock(t, c, 5, 65, 0, "lever[face=floor,facing=north,powered=false]")
	c.Settle()

	var heard noteHeard
	noteClick(t, srv, c, 5, 65, 0, func(id int32, data []byte) { heard.recordNote(t, 3, 64, 0, id, data) })
	heard.wantTones(t, "harp", 0)
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 0)); props["powered"] != "true" {
		t.Errorf("the note block is powered=%s after the comparator rose, want true", props["powered"])
	}
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 0)); props["powered"] != "true" {
		t.Errorf("the comparator is powered=%s, want the powered one", props["powered"])
	}

	heard = noteHeard{}
	noteClick(t, srv, c, 5, 65, 0, func(id int32, data []byte) { heard.recordNote(t, 3, 64, 0, id, data) })
	heard.wantTones(t, "harp")
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 0)); props["powered"] != "false" {
		t.Errorf("the note block is powered=%s after the comparator fell, want false", props["powered"])
	}
}

// TestARepeaterFeedingANoteBlockPlaysItOncePerRisingEdge is the comparator
// test with a repeater.
func TestARepeaterFeedingANoteBlockPlaysItOncePerRisingEdge(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 3, 63, 0, "dirt")
	servertest.Setblock(t, c, 3, 64, 0, "note_block")
	servertest.Setblock(t, c, 4, 64, 0, "repeater[facing=east,delay=1]")
	servertest.Setblock(t, c, 5, 64, 0, "stone")
	servertest.Setblock(t, c, 5, 65, 0, "lever[face=floor,facing=north,powered=false]")
	c.Settle()

	var heard noteHeard
	noteClick(t, srv, c, 5, 65, 0, func(id int32, data []byte) { heard.recordNote(t, 3, 64, 0, id, data) })
	heard.wantTones(t, "harp", 0)
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 0)); props["powered"] != "true" {
		t.Errorf("the note block is powered=%s after the repeater rose, want true", props["powered"])
	}

	heard = noteHeard{}
	noteClick(t, srv, c, 5, 65, 0, func(id int32, data []byte) { heard.recordNote(t, 3, 64, 0, id, data) })
	heard.wantTones(t, "harp")
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 0)); props["powered"] != "false" {
		t.Errorf("the note block is powered=%s after the repeater fell, want false", props["powered"])
	}
}

// TestALeverStronglyPoweringTheFloorPowersANoteBlockBesideIt: power reaches
// the note block through a strongly powered solid block.
func TestALeverStronglyPoweringTheFloorPowersANoteBlockBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 2, "stone")
	servertest.Setblock(t, c, 4, 65, 2, "lever[face=floor,facing=north,powered=false]")
	servertest.Setblock(t, c, 5, 63, 2, "dirt")
	servertest.Setblock(t, c, 5, 64, 2, "note_block")
	c.Settle()

	var heard noteHeard
	noteClick(t, srv, c, 4, 65, 2, func(id int32, data []byte) { heard.recordNote(t, 5, 64, 2, id, data) })
	heard.wantTones(t, "harp", 0)
	if _, props := servertest.StateProps(srv.BlockAt(5, 64, 2)); props["powered"] != "true" {
		t.Errorf("the note block is powered=%s with the block strongly powered, want true", props["powered"])
	}

	heard = noteHeard{}
	noteClick(t, srv, c, 4, 65, 2, func(id int32, data []byte) { heard.recordNote(t, 5, 64, 2, id, data) })
	heard.wantTones(t, "harp")
}

// TestDustOnTheConductorBesideANoteBlockPowersIt: dust on a conductor
// beside the note block powers it.
func TestDustOnTheConductorBesideANoteBlockPowersIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 3, 63, 2, "dirt")
	servertest.Setblock(t, c, 3, 64, 2, "note_block")
	servertest.Setblock(t, c, 4, 64, 2, "stone")
	servertest.Setblock(t, c, 4, 65, 2, "redstone_wire[east=side]")
	c.Settle()

	var heard noteHeard
	collect := func(id int32, data []byte) { heard.recordNote(t, 3, 64, 2, id, data) }
	servertest.Setblock(t, c, 5, 65, 2, "redstone_block")
	noteTicks(t, srv, c, collect)
	heard.wantTones(t, "harp", 0)
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 2)); props["powered"] != "true" {
		t.Errorf("the note block is powered=%s with the dust lit, want true", props["powered"])
	}

	heard = noteHeard{}
	servertest.Setblock(t, c, 5, 65, 2, "air")
	noteTicks(t, srv, c, collect)
	heard.wantTones(t, "harp")
	if _, props := servertest.StateProps(srv.BlockAt(3, 64, 2)); props["powered"] != "false" {
		t.Errorf("the note block is powered=%s with the dust dark, want false", props["powered"])
	}
}

// TestTheBlockBelowNamesTheNoteBlocksInstrument: the instrument follows the
// block below, and an observer sees the change.
// vanilla: NoteBlock.updateShape
func TestTheBlockBelowNamesTheNoteBlocksInstrument(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 2, "dirt")
	servertest.Setblock(t, c, 4, 65, 2, "note_block")
	servertest.Setblock(t, c, 5, 65, 2, "observer[facing=west]")
	c.Settle()

	var heard noteHeard
	trues := 0
	noteCollect := func(id int32, data []byte) {
		heard.recordNote(t, 4, 65, 2, id, data)
		t2, _ := recordObserverFlash(t, 5, 65, 2, "west", id, data)
		trues += t2
	}
	servertest.Setblock(t, c, 4, 64, 2, "gold_block")
	noteTicks(t, srv, c, noteCollect)
	heard.wantTones(t, "harp")
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "bell" {
		t.Errorf("the note block's instrument = %s over gold, want the bell", props["instrument"])
	}
	if trues != 1 {
		t.Errorf("the observer watched %d powered writes of the instrument change, want one", trues)
	}

	heard = noteHeard{}
	noteClick(t, srv, c, 4, 65, 2, noteCollect)
	heard.wantTones(t, "bell", 1)

	heard = noteHeard{}
	servertest.Setblock(t, c, 4, 64, 2, "dirt")
	noteTicks(t, srv, c, noteCollect)
	heard.wantTones(t, "bell")
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "harp" {
		t.Errorf("the note block's instrument = %s back over dirt, want the harp", props["instrument"])
	}

	heard = noteHeard{}
	noteClick(t, srv, c, 4, 65, 2, noteCollect)
	heard.wantTones(t, "harp", 2)
}

// TestAHeadOnTopOfANoteBlockNamesItsInstrument: a skull above names the
// instrument; a player head without note_block_sound plays nothing.
func TestAHeadOnTopOfANoteBlockNamesItsInstrument(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 2, "dirt")
	servertest.Setblock(t, c, 4, 65, 2, "note_block")
	servertest.Setblock(t, c, 4, 66, 2, "skeleton_skull[rotation=0]")
	c.Settle()
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "skeleton" {
		t.Errorf("the note block's instrument = %s under a skeleton skull, want the skeleton's", props["instrument"])
	}

	var heard noteHeard
	noteClick(t, srv, c, 4, 65, 2, func(id int32, data []byte) { heard.recordNote(t, 4, 65, 2, id, data) })
	heard.wantTones(t, "skeleton", 1)

	servertest.Setblock(t, c, 4, 66, 2, "player_head[rotation=0]")
	c.Settle()
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "custom_head" {
		t.Errorf("the note block's instrument = %s under a player head, want the custom head's", props["instrument"])
	}
	heard = noteHeard{}
	noteClick(t, srv, c, 4, 65, 2, func(id int32, data []byte) { heard.recordNote(t, 4, 65, 2, id, data) })
	heard.wantTones(t, "custom_head")
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["note"] != "2" {
		t.Errorf("the note = %s after the silent retune, want 2", props["note"])
	}
}

// TestACoveredNoteBlockTunesButMayNotSound: a base-block instrument sounds
// only under air, but the click still tunes it.
// vanilla: NoteBlock.playNote
func TestACoveredNoteBlockTunesButMayNotSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	for x := int32(4); x <= 6; x++ {
		servertest.Setblock(t, c, x, 63, 2, "dirt")
		servertest.Setblock(t, c, x, 64, 2, "note_block")
	}
	servertest.Setblock(t, c, 4, 65, 2, "torch")
	servertest.Setblock(t, c, 5, 65, 2, "white_carpet")
	c.Settle()

	for _, setup := range []struct {
		x    int32
		tone bool
	}{{4, false}, {5, false}, {6, true}} {
		var heard noteHeard
		noteClick(t, srv, c, setup.x, 64, 2, func(id int32, data []byte) {
			heard.recordNote(t, setup.x, 64, 2, id, data)
		})
		if setup.tone {
			heard.wantTones(t, "harp", 1)
		} else {
			heard.wantTones(t, "harp")
		}
		if _, props := servertest.StateProps(srv.BlockAt(setup.x, 64, 2)); props["note"] != "1" {
			t.Errorf("note block at x=%d: note = %s after its click, want 1", setup.x, props["note"])
		}
	}
}

// TestAComparatorReadingANoteBlockStaysOff: a note block has no analog
// output, so neither retunes nor power turn the comparator on.
func TestAComparatorReadingANoteBlockStaysOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 3, "dirt")
	servertest.Setblock(t, c, 4, 64, 3, "note_block")
	servertest.Setblock(t, c, 4, 64, 4, "comparator[facing=north]")
	servertest.Setblock(t, c, 4, 64, 2, "lever[face=floor,facing=north,powered=false]")
	c.Settle()

	updated := false
	var heard noteHeard
	collect := func(id int32, data []byte) {
		heard.recordNote(t, 4, 64, 3, id, data)
		if id != protocol.IDPlayBlockUpdate {
			return
		}
		if x, y, z := protocol.NewReader(data).ReadPosition(); x == 4 && y == 64 && z == 4 {
			updated = true
		}
	}
	noteClick(t, srv, c, 4, 64, 3, collect)
	heard.wantTones(t, "harp", 1)

	noteClick(t, srv, c, 4, 64, 2, collect)
	heard.wantTones(t, "harp", 1, 1)

	if updated {
		t.Error("the comparator wrote its own state, want the silence of one reading zero")
	}
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 4)); props["powered"] != "false" {
		t.Errorf("the comparator is powered=%s, want the unpowered one a zero read leaves", props["powered"])
	}
}

// TestASetblockNoteBlockTakesTheBelowBlocksInstrument: a plain /setblock
// takes the instrument from below; an explicit state stays.
// vanilla: BlockInput.place
func TestASetblockNoteBlockTakesTheBelowBlocksInstrument(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 2, "gold_block")
	servertest.Setblock(t, c, 4, 65, 2, "note_block")
	c.Settle()
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "bell" {
		t.Errorf("the servertest.Setblock'd note block's instrument = %s, want the gold's bell", props["instrument"])
	}

	servertest.Setblock(t, c, 5, 64, 2, "gold_block")
	servertest.Setblock(t, c, 5, 65, 2, "note_block[instrument=pling,note=7]")
	c.Settle()
	name, props := servertest.StateProps(srv.BlockAt(5, 65, 2))
	if props["instrument"] != "pling" || props["note"] != "7" {
		t.Errorf("the servertest.Setblock'd note block stands as %s instrument=%s note=%s, want pling 7 spelt out",
			name, props["instrument"], props["note"])
	}
}

// TestAPlacedNoteBlockTakesTheBelowBlocksInstrument: a placed note block
// takes the instrument from below.
func TestAPlacedNoteBlockTakesTheBelowBlocksInstrument(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 64, 2, "gold_block")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:note_block", 6, 3, 180, 4, 64, 2, protocol.FaceTop)
	if _, props := servertest.StateProps(srv.BlockAt(4, 65, 2)); props["instrument"] != "bell" {
		t.Errorf("the placed note block's instrument = %s over gold, want the bell", props["instrument"])
	}
}

// TestASurvivalLeftClickPlaysTheNote: a survival attack plays the note
// without retuning.
// vanilla: NoteBlock.attack
func TestASurvivalLeftClickPlaysTheNote(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "note_block[note=5]")
	c.ChatCommand("gamemode survival")
	c.Settle()

	var heard noteHeard
	c.StartDigging(4, 64, 2)
	c.SettleAndCollect(func(id int32, data []byte) { heard.recordNote(t, 4, 64, 2, id, data) })
	heard.wantTones(t, "harp", 5)
	if _, props := servertest.StateProps(srv.BlockAt(4, 64, 2)); props["note"] != "5" {
		t.Errorf("the note = %s after the left-click, want 5 kept", props["note"])
	}
}

// TestAHeadClickedOntoANoteBlocksTopIsPlaced: a head used on the top face
// is placed instead of tuning the note block.
// vanilla: NoteBlock.useItemOn
func TestAHeadClickedOntoANoteBlocksTopIsPlaced(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 4, 63, 2, "dirt")
	servertest.Setblock(t, c, 4, 64, 2, "note_block")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:skeleton_skull", 6, 2, 90, 4, 64, 2, protocol.FaceTop)

	if name, _ := servertest.StateProps(srv.BlockAt(4, 65, 2)); name != "minecraft:skeleton_skull" {
		t.Errorf("above the note block stands %s, want the placed skeleton skull", name)
	}
	_, props := servertest.StateProps(srv.BlockAt(4, 64, 2))
	if props["note"] != "0" || props["instrument"] != "skeleton" {
		t.Errorf("the note block is note=%s instrument=%s, want 0 and skeleton", props["note"], props["instrument"])
	}
}
