package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestMakeSoundVoicesRollTheVoicePitch(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"armadillo", "nautilus", "zoglin"} {
		t.Run(kind, func(t *testing.T) {
			srv, c := servertest.BreedingPlayer(t, "survival", "Listener", "minecraft:cod", 1,
				func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000)
			x := 3.5
			if kind == "nautilus" {
				x = 1.5
			} else if kind == "zoglin" {
				x = 6.5
			}
			id := srv.SpawnMob("minecraft:"+kind, x, 64, 0.5)
			c.Settle()
			want := "minecraft:entity." + kind + ".angry"
			category := int32(protocol.SoundCategoryHostile)
			minPitch, maxPitch := float32(0.79), float32(1.21)
			switch kind {
			case "zoglin":
				srv.SetMobBaby(id, false)
			case "armadillo":
				srv.SetMobBaby(id, true)
				srv.SpawnMob("minecraft:zombie", 8.5, 64, 0.5)
				want = "minecraft:entity.armadillo.roll"
				category = protocol.SoundCategoryNeutral
				minPitch, maxPitch = 1.29, 1.71
			case "nautilus":
				srv.SetMobBaby(id, true)
				srv.SetMobNoAI(id)
				want = "minecraft:entity.baby_nautilus.eat"
				category = protocol.SoundCategoryNeutral
				minPitch, maxPitch = 1.29, 1.71
				c.UseOn(id)
			}
			heard := false
			for tick := 0; tick < 100 && !heard; tick++ {
				srv.TickMobs(1)
				c.SettleAndCollect(func(pid int32, data []byte) {
					if pid != protocol.IDPlaySoundEffect {
						return
					}
					name, gotCategory, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
					if name != want {
						return
					}
					heard = true
					if gotCategory != category || volume != 1 || pitch < minPitch || pitch > maxPitch {
						t.Errorf("%s: category=%d volume=%v pitch=%v; want category=%d volume=1 pitch=[%v,%v]",
							name, gotCategory, volume, pitch, category, minPitch, maxPitch)
					}
				})
			}
			if !heard {
				t.Errorf("never heard %s within 100 ticks", want)
			}
		})
	}
}
