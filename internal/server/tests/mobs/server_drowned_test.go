package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestADrownedThrowsItsTridentAtAPlayer(t *testing.T) {
	t.Parallel()
	for _, armed := range []bool{true, false} {
		name := "empty"
		if armed {
			name = "trident"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BowTestConfig(), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			c := connect().Join("Prey")
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000)
			c.MoveTo(0.5, 64, 0.5, 0, 0)
			c.Settle()
			id := srv.SpawnMob("minecraft:drowned", 8.5, 64, 0.5)
			var held protocol.ItemStack
			if armed {
				held = protocol.ItemStack{ItemID: int32(registry.ItemTrident), Count: 1}
			}
			srv.SetMobHand(id, held)
			var ticks []int
			sound, raised := false, false
			shootID := servertest.ConduitSoundID(t, "minecraft:entity.drowned.shoot")
			for tick := 1; tick <= 130; tick++ {
				c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
					switch pid {
					case protocol.IDPlaySpawnEntity:
						sp := servertest.ReadThrownSpawn(c, data)
						if sp.EntityType == protocol.EntityTypeTrident {
							ticks = append(ticks, tick)
							if sp.OwnerID != id || sp.Vx >= 0 || sp.Vx*(0.5-sp.X)+sp.Vz*(0.5-sp.Z) <= 0 {
								t.Errorf("trident owner/aim = %+v", sp)
							}
						}
					case protocol.IDPlayEntityMetadata:
						r := protocol.NewReader(data)
						if r.ReadVarInt() == id {
							if entries, ok := servertest.ReadMetadata(r); ok {
								if e := servertest.MetaOf(entries, protocol.MetadataIndexHandState); e != nil && e.ByteVal&protocol.EntityFlagHandActive != 0 {
									raised = true
								}
							}
						}
					case protocol.IDPlaySoundEffect:
						var s servertest.SoundSee
						s.ReadSoundSee(t, data)
						if s.Sound == shootID {
							sound = true
							if s.Volume != 1 || s.Pitch < 1/1.2 || s.Pitch > 1/0.8 {
								t.Errorf("shoot sound = %+v", s)
							}
						}
					}
					return false
				})
				if armed && len(ticks) >= 2 {
					break
				}
			}
			if !armed {
				if len(ticks) != 0 || sound {
					t.Fatalf("empty-handed drowned shot at %v, sound %v", ticks, sound)
				}
				return
			}
			if len(ticks) < 2 {
				t.Fatalf("shot ticks = %v, want two throws", ticks)
			}
			if ticks[0] > 80 || ticks[1]-ticks[0] < 40 {
				t.Errorf("shot ticks = %v, want first by 80 and interval >= 40", ticks)
			}
			if !raised {
				t.Error("missing using-item metadata while throwing")
			}
			if !sound {
				t.Error("missing drowned shoot sound")
			}
			for _, arrow := range srv.ArrowsInDimension("overworld") {
				if arrow.Pickup != "disallowed" {
					t.Errorf("mob trident pickup = %s", arrow.Pickup)
				}
			}
			if got := srv.MobHand(id); got.ItemID != held.ItemID || got.Count != 1 {
				t.Errorf("held trident = %+v", got)
			}
		})
	}
}
