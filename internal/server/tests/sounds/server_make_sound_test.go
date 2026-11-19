package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

func TestAdultNautilusEatingRollsPitchButEquippingDoesNot(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"nautilus", "zombie_nautilus"} {
		t.Run(kind, func(t *testing.T) {
			srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", "minecraft:cod", 8)
			servertest.HoldTicks(t, srv)
			id := srv.SpawnMob("minecraft:"+kind, 1.5, 64, 0.5)
			srv.SetMobBaby(id, false)
			srv.SetMobNoAI(id)
			if !srv.TameMobFor(id, "Keeper") {
				t.Fatal("could not tame nautilus")
			}
			c.Settle()
			pitches := map[float32]bool{}
			for range 8 {
				srv.SetMobHealth(id, 5)
				c.UseOn(id)
				name := "minecraft:entity." + kind + ".eat"
				_, volume, pitch := awaitVoice(t, c, name)
				if volume != 1 || pitch < 0.79 || pitch > 1.21 {
					t.Errorf("%s: volume=%v pitch=%v; want volume=1 pitch=[0.79,1.21]", name, volume, pitch)
				}
				pitches[pitch] = true
			}
			if len(pitches) < 2 {
				t.Errorf("eight meals all had the same pitch: %v", pitches)
			}
			srv.GiveSlot("Keeper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:saddle", 1))
			c.UseOn(id)
			_, volume, pitch := awaitVoice(t, c, "minecraft:item.nautilus_saddle_equip")
			if volume != 1 || pitch != 1 {
				t.Errorf("equip: volume=%v pitch=%v; want 1/1", volume, pitch)
			}
		})
	}
}

func TestBabyPiglinConversionUsesItsOldVoice(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Listener", "", 0, servertest.OpsListing(t, "Listener"))
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:piglin", 1.5, 64, 0.5)
	srv.SetMobBaby(id, true)
	c.ChatCommand("data merge entity @e[type=minecraft:piglin,limit=1] {TimeInOverworld:300}")
	c.Settle()
	srv.TickMobs(1)
	category, volume, pitch := awaitVoice(t, c, "minecraft:entity.piglin.converted_to_zombified")
	if category != protocol.SoundCategoryHostile || volume != 1 || pitch < 1.29 || pitch > 1.71 {
		t.Errorf("conversion: category=%d volume=%v pitch=%v; want hostile/1/[1.29,1.71]", category, volume, pitch)
	}
}

func awaitVoice(t *testing.T, c *servertest.Client, want string) (category int32, volume, pitch float32) {
	t.Helper()
	found := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlaySoundEffect {
			return
		}
		name, cat, _, _, _, vol, p := servertest.ReadAmbient(t, data)
		if name == want {
			found, category, volume, pitch = true, cat, vol, p
		}
	})
	if !found {
		t.Fatalf("never heard %s", want)
	}
	return
}
