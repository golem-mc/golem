package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// A goat horn's use plays the instrument's sound to the players in its
// range, not to the player, and starts a cooldown of the instrument's use
// duration, during which a second use does nothing.

// hornCooldown is one Set Cooldown packet: its group and the ticks it runs for.
type hornCooldown struct {
	group string
	ticks int32
}

type hornNews struct {
	sounds    int
	volume    float32
	soundID   int32
	cooldowns []hornCooldown
}

func (n *hornNews) record(id int32, data []byte) {
	r := protocol.NewReader(data)
	switch id {
	case protocol.IDPlayEntitySoundEffect:
		n.sounds++
		n.soundID = r.ReadVarInt() - 1
		r.ReadVarInt() // category
		r.ReadVarInt() // entity
		n.volume = r.ReadF32()
	case protocol.IDPlayCooldown:
		group := r.ReadString(256)
		n.cooldowns = append(n.cooldowns, hornCooldown{group: group, ticks: r.ReadVarInt()})
	}
}

func TestGoatHornPlaysItsInstrumentAndStartsItsCooldown(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	blower := connect().Join("Blower")
	near := connect().Join("Near")
	far := connect().Join("Far")
	far.MoveTo(400.5, 64, 0.5, 0, 0)
	far.Settle()
	blower.Settle()
	near.Settle()
	srv.GiveSlot("Blower", 36, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:goat_horn"), Count: 1})

	servertest.UseHeld(blower, 0, 0)
	servertest.UseHeld(blower, 0, 0)
	var own, heard, distant hornNews
	blower.SettleAndCollect(own.record)
	near.SettleAndCollect(heard.record)
	far.SettleAndCollect(distant.record)

	if heard.sounds != 1 {
		t.Fatalf("a listener in range heard %d horn sounds, want 1", heard.sounds)
	}
	want, _ := registry.SoundEventID("minecraft:item.goat_horn.sound.0")
	if heard.soundID != int32(want) || heard.volume != 16 {
		t.Errorf("the horn played sound %d at volume %v, want %d at 16", heard.soundID, heard.volume, want)
	}
	if own.sounds != 0 {
		t.Errorf("the player was sent the horn's sound %d times, its own servertest.Client plays it", own.sounds)
	}
	if distant.sounds != 0 {
		t.Errorf("a listener 400 blocks away heard the horn %d times, want 0", distant.sounds)
	}
	if len(own.cooldowns) != 1 || own.cooldowns[0].group != "minecraft:goat_horn" || own.cooldowns[0].ticks != 140 {
		t.Errorf("the cooldowns sent = %+v, want one of 140 ticks for minecraft:goat_horn", own.cooldowns)
	}
}
