package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// skeletonHorseInPool joins a creative listener and summons a tamed, saddled
// skeleton horse standing in a 3-deep water pool, returning its entity ID.
func skeletonHorseInPool(t *testing.T) (*server.Server, *servertest.Client, int32) {
	srv, c := servertest.BreedingPlayer(t, "creative", "Listener", "", 0,
		func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	water := servertest.MustState(t, "minecraft:water", nil)
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(6, y, 0, water)
	}
	srv.RunLine(`summon minecraft:skeleton_horse 6.5 64 0.5 {Tame:1b,NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	var id int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:skeleton_horse" {
			id = m.ID
		}
	}
	if id == 0 {
		t.Fatal("skeleton horse was not summoned")
	}
	return srv, c, id
}

// vanilla: SkeletonHorse.getAmbientSound
func TestSkeletonHorseAmbientWaterSoundWhenSubmerged(t *testing.T) {
	t.Parallel()
	srv, c, _ := skeletonHorseInPool(t)
	srv.TickMobs(5)
	c.Settle()
	heard := ""
	for tick := 0; tick < 400 && heard == ""; tick += 10 {
		srv.TickMobs(10)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if name == "minecraft:entity.skeleton_horse.ambient_water" || name == "minecraft:entity.skeleton_horse.ambient" {
				heard = name
			}
		})
	}
	if heard != "minecraft:entity.skeleton_horse.ambient_water" {
		t.Fatalf("ambient voice = %q, want entity.skeleton_horse.ambient_water", heard)
	}
}

// vanilla: SkeletonHorse.playJumpSound
func TestSkeletonHorseJumpWaterSound(t *testing.T) {
	t.Parallel()
	srv, c, id := skeletonHorseInPool(t)
	srv.TickMobs(2)
	c.UseOn(id)
	c.Settle()
	c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(c.PlayerEntityID)
		b.PutVarInt(protocol.PlayerCommandStartRidingJump)
		b.PutVarInt(80)
	})
	var volume float32
	heard := ""
	record := func(pid int32, data []byte) {
		if pid != protocol.IDPlaySoundEffect {
			return
		}
		name, _, _, _, _, v, _ := servertest.ReadAmbient(t, data)
		if name == "minecraft:entity.skeleton_horse.jump_water" || name == "minecraft:entity.horse.jump" {
			heard, volume = name, v
		}
	}
	c.SettleAndCollect(record)
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if heard != "minecraft:entity.skeleton_horse.jump_water" || volume != 0.4 {
		t.Fatalf("jump sound = %q at %v, want entity.skeleton_horse.jump_water at 0.4", heard, volume)
	}
}
