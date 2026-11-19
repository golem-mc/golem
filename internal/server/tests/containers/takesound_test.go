package containers

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// tableSound watches one exchange for a table's sound. The sound is queued
// after the click's direct writes, so it must follow every slot, cursor,
// property and experience packet of the exchange; vanilla sends it ahead of
// them, which needs the click's answers on the mailbox too.
// vanilla: StonecutterMenu$2.onTake, EnchantmentMenu.clickMenuButton
type tableSound struct {
	w    *servertest.WindowedClient
	name string
	got  [][]byte
}

func newTableSound(w *servertest.WindowedClient, name string) *tableSound {
	return &tableSound{w: w, name: name}
}

func (s *tableSound) tap(id int32, data []byte) {
	switch id {
	case protocol.IDPlaySoundEffect:
		if soundNameOf(data) == s.name {
			s.got = append(s.got, data)
		}
	case protocol.IDPlayContainerSlot, protocol.IDPlaySetCursorItem,
		protocol.IDPlayContainerProperty, protocol.IDPlaySetExperience:
		if len(s.got) > 0 {
			s.w.T.Errorf("packet 0x%02X arrived after the sound %q", id, s.name)
		}
	}
}

// expect checks that exactly one such sound came, at the block's centre,
// and returns its volume and pitch.
func (s *tableSound) expect(x, y, z int32) (volume, pitch float32) {
	s.w.T.Helper()
	if len(s.got) != 1 {
		s.w.T.Errorf("%d sounds named %q came, want one", len(s.got), s.name)
		return 0, 0
	}
	r := protocol.NewReader(s.got[0])
	if sound := r.ReadVarInt(); sound == 0 {
		r.ReadString(32767)
	}
	r.ReadVarInt() // the category
	gotX, gotY, gotZ := int32(r.ReadI32()), int32(r.ReadI32()), int32(r.ReadI32())
	if gotX != x*8+4 || gotY != y*8+4 || gotZ != z*8+4 {
		s.w.T.Errorf("the sound played at (%d,%d,%d), want (%d,%d,%d)",
			gotX/8, gotY/8, gotZ/8, x, y, z)
	}
	return r.ReadF32(), r.ReadF32()
}

func soundNameOf(data []byte) string {
	r := protocol.NewReader(data)
	if sound := r.ReadVarInt(); sound != 0 {
		name, _ := servertest.SoundEventName(sound - 1)
		return name
	}
	return r.ReadString(32767)
}

func expectTableTake(w *servertest.WindowedClient, name string, slots map[int16]protocol.ItemStack, carried protocol.ItemStack) {
	w.T.Helper()
	snd := newTableSound(w, name)
	w.ExpectClickResultTap(slots, carried, snd.tap)
	if volume, pitch := snd.expect(4, 64, 0); len(snd.got) == 1 && (volume != 1.0 || pitch != 1.0) {
		w.T.Errorf("the take sounded at volume %v pitch %v, want both 1", volume, pitch)
	}
}

// expectStonecutterTake reads a stonecutter take's answer: the corrections,
// then the cutting's sound once however many takes the click made.
// vanilla: StonecutterMenu$2.onTake, one per game tick
func expectStonecutterTake(w *servertest.WindowedClient, slots map[int16]protocol.ItemStack, carried protocol.ItemStack) {
	w.T.Helper()
	expectTableTake(w, "minecraft:ui.stonecutter.take_result", slots, carried)
}

// expectCartographyTake reads a cartography take's answer: the corrections,
// then the table's sound once however many takes the click made.
// vanilla: CartographyTableMenu's result slot's onTake, one per game tick
func expectCartographyTake(w *servertest.WindowedClient, slots map[int16]protocol.ItemStack, carried protocol.ItemStack) {
	w.T.Helper()
	expectTableTake(w, "minecraft:ui.cartography_table.take_result", slots, carried)
}

// expectCorrectedSlotsThenChime is expectCorrectedSlots for a press that
// enchants: the exchange's one chime at the block's centre comes after the
// slots, the offers and the experience.
func expectCorrectedSlotsThenChime(w *servertest.WindowedClient, want map[int16]protocol.ItemStack) {
	w.T.Helper()
	snd := newTableSound(w, "minecraft:block.enchantment_table.use")
	expectCorrectedSlotsTap(w, want, snd.tap)
	snd.expect(4, 64, 0)
}
