package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestTeleportShortLivedEntitiesSameDimension(t *testing.T) {
	t.Parallel()
	testTeleportShortLivedEntities(t, world.DimensionName)
}

func TestTeleportShortLivedEntitiesAcrossDimensions(t *testing.T) {
	t.Parallel()
	testTeleportShortLivedEntities(t, world.NetherName)
}

// vanilla: Entity.teleportSameDimension, Entity.teleportCrossDimension
func testTeleportShortLivedEntities(t *testing.T, destination string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Traveller", servertest.OpsListing(t, "Traveller"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	if destination != world.DimensionName {
		t.Cleanup(srv.HoldChunkInDimension(destination, 6, 4))
	}

	for _, tc := range []struct {
		kind, fields, path, value string
	}{
		{"evoker_fangs", ",Warmup:40,Owner:[I;1,2,3,4]", "Warmup", "40"},
		{"eye_of_ender", `,Item:{id:"minecraft:diamond",count:1}`, "Item.id", `"minecraft:diamond"`},
		{"lightning_bolt", "", "Tags", `["travelling"]`},
	} {
		c.ChatCommand("summon " + tc.kind + ` 5.5 64 5.5 {Tags:["travelling"],CustomName:"Traveller"` + tc.fields + "}")
		typ, _ := registry.EntityTypeID("minecraft:" + tc.kind)
		spawn := c.AwaitSpawn(typ)
		c.ExpectSystemChatPastMobs()
		identity := spawn.Uuid.String()

		c.ChatCommand("execute in " + destination + " run tp " + identity + " 6.5 70 4.5")
		var feedback protocol.Text
		if destination == world.DimensionName {
			moved := false
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlaySystemChat:
					feedback, _ = protocol.ReadText(r)
				case protocol.IDPlayTeleportEntity:
					if r.ReadVarInt() != spawn.EntityID {
						break
					}
					r.ReadVarInt() // position path
					x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
					if x != 6.5 || y != 70 || z != 4.5 || r.Err() != nil {
						t.Errorf("%s wire position = (%v, %v, %v): %v", tc.kind, x, y, z, r.Err())
					}
					moved = true
				}
				return false
			})
			if !moved {
				t.Errorf("%s teleport was not sent to its viewer", tc.kind)
			}
		} else {
			feedback = servertest.FeedbackComponent(t, c)
		}
		if feedback.Translate != "commands.teleport.success.location.single" {
			t.Fatalf("%s teleport: %+v", tc.kind, feedback)
		}
		// A dimension-bound selector must find the arrival under the original
		// UUID; a global UUID lookup alone could still find the source body.
		selector := "@e[type=" + tc.kind + ",tag=travelling,distance=..16,limit=1]"
		for _, check := range []struct{ path, want string }{
			{"Pos", "[6.5d, 70.0d, 4.5d]"},
			{tc.path, tc.value},
		} {
			c.ChatCommand("execute in " + destination + " positioned 6.5 70 4.5 run data get entity " + selector + " " + check.path)
			got := servertest.FeedbackComponent(t, c)
			if got.Translate != "commands.data.entity.query" || len(got.With) != 2 {
				t.Errorf("%s arrival %s: %+v", tc.kind, check.path, got)
				continue
			}
			if name := got.With[0]; name.Insertion != identity || name.Plain() != "Traveller" {
				t.Errorf("%s arrival lost its identity/name: %+v", tc.kind, name)
			}
			if value := got.With[1].Plain(); value != check.want {
				t.Errorf("%s arrival %s = %s, want %s", tc.kind, check.path, value, check.want)
			}
		}
		if destination != world.DimensionName {
			c.ChatCommand("execute in " + world.DimensionName + " positioned 5.5 64 5.5 if entity " + selector)
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
		}
		c.ChatCommand("kill " + identity)
		c.ExpectSystemChatPastMobs()
	}
	c.Disconnect()
}

func TestFangsTeleportPairsNewViewer(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Traveller", servertest.OpsListing(t, "Traveller"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	// Keep the source selectable without bringing it into the player's view.
	t.Cleanup(srv.HoldChunk(115, 5))
	c.ChatCommand(`summon evoker_fangs 115.5 64 5.5 {Warmup:40,CustomName:"Arrival"}`)
	var feedback protocol.Text
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeEvokerFangs {
				t.Fatal("fangs were paired before entering tracking range")
			}
		case protocol.IDPlaySystemChat:
			feedback, _ = protocol.ReadText(r)
		}
		return false
	})
	if feedback.Translate != "commands.summon.success" {
		t.Fatalf("summon: %+v", feedback)
	}
	c.ChatCommand("tp @e[type=evoker_fangs,limit=1] 5.5 64 5.5")
	feedback = protocol.Text{}
	spawned, named := false, false
	var entityID int32
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			gotID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() != protocol.EntityTypeEvokerFangs {
				break
			}
			entityID = gotID
			if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 5.5 || y != 64 || z != 5.5 || r.Err() != nil {
				t.Errorf("arrival spawn = (%v, %v, %v): %v", x, y, z, r.Err())
			}
			spawned = true
		case protocol.IDPlayEntityMetadata:
			if spawned && r.ReadVarInt() == entityID {
				meta, ok := servertest.ReadMetadata(r)
				name := servertest.MetaOf(meta, protocol.MetadataIndexCustomName)
				if !ok || name == nil || name.TextVal != nbt.String("Arrival") {
					t.Errorf("arrival custom name = %+v, metadata valid = %v", name, ok)
				} else {
					named = true
				}
			}
		case protocol.IDPlaySystemChat:
			feedback, _ = protocol.ReadText(r)
		}
		return false
	})
	if feedback.Translate != "commands.teleport.success.location.single" {
		t.Fatalf("teleport: %+v", feedback)
	}
	if !spawned || !named {
		t.Fatalf("arrival pairing: spawn=%v, metadata=%v", spawned, named)
	}
	c.Disconnect()
}

// vanilla: TeleportCommand.getRelatives, PositionMoveRotation.calculateAbsolute
func TestEyeTeleportPreservesRelativeMotion(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Traveller", servertest.OpsListing(t, "Traveller"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 5, 5))
	for _, destination := range []string{world.DimensionName, world.NetherName} {
		for _, tc := range []struct{ coords, want, across string }{
			{"~ ~ ~", "[0.5d, 0.0d, 0.25d]", "[0.5d, 0.75d, 0.25d]"},
			{"~ ~ 5.5", "[0.5d, 0.0d, 0.0d]", "[0.5d, 0.75d, 0.0d]"},
			{"5.5 ~ ~", "[0.0d, 0.0d, 0.25d]", "[0.0d, 0.75d, 0.25d]"},
			{"5.5 64 5.5", "[0.0d, 0.0d, 0.0d]", "[0.0d, 0.0d, 0.0d]"},
		} {
			c.ChatCommand(`summon eye_of_ender 5.5 64 5.5 {Motion:[0.5d,0.75d,0.25d]}`)
			spawn := c.AwaitSpawn(protocol.EntityTypeEyeOfEnder)
			c.ExpectSystemChatPastMobs()
			identity := spawn.Uuid.String()
			c.ChatCommand("execute as " + identity + " at @s in " + destination + " run tp @s " + tc.coords)
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("data get entity " + identity + " Motion")
			got := servertest.FeedbackComponent(t, c)
			want := tc.want
			if destination != world.DimensionName {
				// Command finalization stops the removed original; the destination
				// twin retains velocity on every relative axis.
				want = tc.across
			}
			if got.Translate != "commands.data.entity.query" || len(got.With) != 2 || got.With[1].Plain() != want {
				t.Errorf("%s tp %s motion: %s; want %s", destination, tc.coords, got.Plain(), want)
			}
			c.ChatCommand("kill " + identity)
			c.ExpectSystemChatPastMobs()
		}
	}
	c.Disconnect()
}
