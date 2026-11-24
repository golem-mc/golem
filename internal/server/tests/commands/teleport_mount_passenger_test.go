package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.calculatePassengerTransition, TeleportCommand.performTeleport
func TestTeleportTurnsMobPassengerWithMount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, tp, want string
	}{
		{"yaw and pitch clamp separately", "tp @e[type=chicken] 6.5 64 6.5 180 120", "[-270.0f, 60.0f]"},
		{"facing turns only the mount", "tp @e[type=chicken] 6.5 64 6.5 facing 100 64 6.5", "[0.0f, 0.0f]"},
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
			c.ChatCommand(`summon chicken 3.5 64 3.5 {NoAI:1b,NoGravity:1b,Rotation:[90f,0f],Passengers:[{id:"minecraft:pig",Tags:["rider"],NoAI:1b,NoGravity:1b,Rotation:[0f,-60f]}]}`)
			spawn := c.AwaitSpawn(protocol.EntityTypePig)
			pig := spawn.EntityID
			if tc.want == "[0.0f, 0.0f]" {
				// The pig starts level in this case.
				c.ChatCommand(`data merge entity @e[type=pig,limit=1] {Rotation:[0f,0f]}`)
				c.Settle()
			}
			c.ChatCommand(tc.tp)
			turned := false
			servertest.RunTicks(t, srv, 40)
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlayEntityRotation && protocol.NewReader(data).ReadVarInt() == pig {
					turned = true
				}
			})
			c.ChatCommand("data get entity @e[type=pig,limit=1] Rotation")
			c.ExpectCommandFeedbackText("commands.data.entity.query", servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", spawn.Uuid), servertest.NBTArg(tc.want))
			if tc.want != "[0.0f, 0.0f]" && !turned {
				t.Error("the seated rider's turn was not sent")
			}
		})
	}
}
