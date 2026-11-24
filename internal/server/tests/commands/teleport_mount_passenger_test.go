package commands

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// angleByte is the protocol angle of degrees.
func angleByte(degrees float64) uint8 {
	return uint8(int32(math.Floor(math.Mod(degrees, 360) * 256 / 360)))
}

// vanilla: Entity.calculatePassengerTransition, TeleportCommand.performTeleport
func TestTeleportTurnsMobPassengerWithMount(t *testing.T) {
	t.Parallel()
	const chickenSummon = `summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Rotation:[170f,-80f],Passengers:[{id:"minecraft:pig",NoAI:1b,NoGravity:1b,Rotation:[170f,-80f]}]}`
	cases := []struct {
		name, tp, want string
		yaw, pitch     float64
		frozen, level  bool
	}{
		{"absolute turn clamps each rider", "tp @e[type=chicken] 6.5 64 6.5 180 120", "[-180.0f, 90.0f]", -180, 90, false, false},
		{"relative turn wraps its offset", "execute rotated 0 0 run tp @e[type=chicken,limit=1] 6.5 64 6.5 ~-170 ~120", "[190.0f, -90.0f]", 190, -90, false, false},
		{"relative turn while frozen", "execute rotated 0 0 run tp @e[type=chicken,limit=1] 6.5 64 6.5 ~-170 ~120", "[190.0f, -90.0f]", 190, -90, true, false},
		{"facing turns only the mount", "tp @e[type=chicken] 6.5 64 6.5 facing 100 64 6.5", "[170.0f, -80.0f]", 170, -80, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			cfg.SimulationDistance = config.MinDistance
			srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)
			c.Settle()
			c.ChatCommand(chickenSummon)
			spawn := c.AwaitSpawn(protocol.EntityTypePig)
			pig := spawn.EntityID
			seated := false
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlaySetPassengers {
					if _, riders := servertest.ReadPassengers(c, data); len(riders) == 1 && riders[0] == pig {
						seated = true
					}
				}
			})
			if !seated {
				t.Fatal("the pig was not seated on the chicken")
			}
			if tc.frozen {
				srv.RunLine("tick freeze")
			}
			c.ChatCommand(tc.tp)
			// Settle first so the command has run before the ticks that send the turn.
			c.Settle()
			var turned, dropped bool
			c.ReadTicks(srv, entity.MobUpdateInterval(entity.TypePig), func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlayEntityRotation:
					r := protocol.NewReader(data)
					if r.ReadVarInt() != pig {
						break
					}
					r.ReadBool() // on ground
					yaw, pitch := r.ReadU8(), r.ReadU8()
					// A level case sends nothing: any turn of the pig is wrong.
					if tc.level || yaw == angleByte(tc.yaw) && pitch == angleByte(tc.pitch) {
						turned = true
					}
				case protocol.IDPlaySetPassengers:
					if _, riders := servertest.ReadPassengers(c, data); len(riders) == 0 {
						dropped = true
					}
				}
				return false
			})
			if dropped {
				t.Error("the pig left its seat")
			}
			if tc.level && turned {
				t.Error("a facing clause turned the rider")
			}
			if !tc.level && !turned {
				t.Errorf("the pig's turn to %v, %v was not sent while seated", tc.yaw, tc.pitch)
			}
			c.ChatCommand("data get entity @e[type=pig,limit=1] Rotation")
			c.ExpectCommandFeedbackText("commands.data.entity.query", servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", spawn.Uuid), servertest.NBTArg(tc.want))
		})
	}
}
