package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkHopperJukeboxTransfer is one successful hopper transfer of a disc
// into (push) or out of (pull) a jukebox with a sculk sensor in range.
func BenchmarkHopperJukeboxTransfer(b *testing.B) {
	disc := protocol.ItemStack{ItemID: int32(registry.ItemMusicDiscCat), Count: 1}
	const y = world.GroundLevel + 3
	run := func(b *testing.B, push bool) {
		s, w := newBlockBenchServer(b, 0)
		hopper := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"})
		jukeY, hopperY := int32(y-1), int32(y)
		if !push {
			jukeY, hopperY = y+1, y
		}
		w.SetBlock(4, jukeY, 4, registry.BlockJukebox.DefaultState())
		w.SetBlock(4, hopperY, 4, hopper)
		hb := w.EnsureBlockEntity(4, hopperY, 4, world.KindHopper)
		w.SetBlock(7, y-1, 4, registry.MustState("minecraft:sculk_sensor", nil))
		w.EnsureBlockEntity(7, y-1, 4, world.KindSculkSensor)

		withRecord, _ := registry.WithBool(registry.BlockJukebox.DefaultState(), registry.PropHasRecord, true)

		tick := int64(100)
		b.ReportAllocs()
		for b.Loop() {
			// Reset to the state before the transfer, outside the hopper's work.
			b.StopTimer()
			jb := w.EnsureBlockEntity(4, jukeY, 4, world.KindJukebox)
			if push {
				hb.Use(func(e *world.BlockEntity) { e.Put(0, disc); e.Wake() })
				jb.Use(func(e *world.BlockEntity) { e.Put(0, protocol.ItemStack{}); e.TicksSinceSongStarted = -1 })
				s.setJukeboxHasRecord(w, 4, jukeY, 4, false)
			} else {
				hb.Use(func(e *world.BlockEntity) { e.Put(0, protocol.ItemStack{}) })
				jb.Use(func(e *world.BlockEntity) { e.Put(0, disc); e.TicksSinceSongStarted = 0 })
				w.SetBlock(4, jukeY, 4, withRecord)
			}
			hb.Cooldown = 0
			tick++
			b.StartTimer()
			s.tickHopper(w, hb, tick)
			jb = w.EnsureBlockEntity(4, jukeY, 4, world.KindJukebox)
			var moved bool
			jb.Use(func(e *world.BlockEntity) { moved = e.Slots[0].Empty() != push })
			if !moved {
				b.Fatal("the hopper moved no disc")
			}
		}
	}
	b.Run("push", func(b *testing.B) { run(b, true) })
	b.Run("pull", func(b *testing.B) { run(b, false) })
}
