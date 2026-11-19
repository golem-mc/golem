package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// vanilla: Raid.updatePlayers, ServerBossEvent.addPlayer, ServerBossEvent.broadcast
func TestRaidReconnectAddsBarBeforeUpdates(t *testing.T) {
	t.Parallel()
	for _, wave := range []bool{false, true} {
		name := "countdown"
		if wave {
			name = "wave"
		}
		t.Run(name, func(t *testing.T) {
			srv, c, _ := village(t, rules.DifficultyNormal)
			srv.SetPlayerGameMode("Hero", protocol.GameModeCreative)
			startRaid(t, c, srv, 0)
			if wave {
				run(c, srv, 330)
			}
			bar := srv.Raids()[0].BarWire
			if srv.Raids()[0].BarPlayers != 1 {
				t.Fatal("the first connection never joined the raid bar")
			}

			servertest.FreezeUntilHeld(t, srv)
			c.Disconnect()
			servertest.ReleaseTicks(t, srv)
			returned := servertest.DialServer(t, srv)
			returned.Login("Hero")
			returned.Configure()
			visible := map[uuid.UUID]bool{}
			adds, updates := 0, 0
			see := func(id int32, data []byte) bool {
				if id != protocol.IDPlayBossEvent {
					return false
				}
				r := protocol.NewReader(data)
				wire, action := r.ReadUUID(), r.ReadVarInt()
				switch action {
				case protocol.BossEventAdd:
					visible[wire] = true
					if wire == bar {
						adds++
					}
				case protocol.BossEventRemove:
					delete(visible, wire)
				default:
					if !visible[wire] {
						t.Fatalf("boss bar %s action %d arrived before an add on the new connection", wire, action)
					}
					if wire == bar {
						updates++
					}
				}
				return false
			}
			returned.ReadJoin(func(id int32, data []byte) { see(id, data) })
			servertest.HoldTicks(t, srv)
			if wave {
				ids := raiderIDs(srv)
				if len(ids) < 3 {
					t.Fatalf("raiders = %d, want at least three", len(ids))
				}
				// Change both the health and the remaining-raiders title before
				// the raid's next membership reconciliation.
				for _, id := range ids[2:] {
					srv.KillMob(id)
				}
				srv.HurtMobByWorld(ids[0], 5, "minecraft:generic")
			}
			returned.ReadTicks(srv, 40, see)
			if adds != 1 || !visible[bar] {
				t.Fatalf("raid bar adds = %d, visible = %v; want one add and a visible bar", adds, visible[bar])
			}
			if wave {
				srv.HurtMobByWorld(raiderIDs(srv)[0], 1, "minecraft:generic")
				returned.ReadTicks(srv, 2, see)
			}
			if updates == 0 {
				t.Fatal("the new connection received no raid bar updates")
			}
		})
	}
}
