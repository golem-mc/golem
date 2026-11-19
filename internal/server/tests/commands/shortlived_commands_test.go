package commands

import (
	"bytes"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.teleport, EntityDataAccessor.setData
func TestShortLivedEntityTeleportAndData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Writer", servertest.OpsListing(t, "Writer"))
	observer := servertest.DialServer(t, srv).Join("Observer")
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	for _, tc := range []struct{ kind, fields, predicate string }{
		{"evoker_fangs", ",Warmup:40,Owner:[I;1,2,3,4]", "Warmup:40,Owner:[I;1,2,3,4]"},
		{"eye_of_ender", `,Item:{id:"minecraft:diamond",count:8}`, `Item:{id:"minecraft:diamond",count:1}`},
		{"lightning_bolt", "", ""},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			selector := "@e[type=" + tc.kind + ",limit=1]"
			c.ChatCommand("summon " + tc.kind + " 2.5 64 4.5")
			typ, _ := registry.EntityTypeID("minecraft:" + tc.kind)
			spawn := observer.AwaitSpawn(typ)
			c.AwaitSpawn(typ)
			if tc.kind == "eye_of_ender" {
				observer.AwaitMetadataFor(spawn.EntityID)
				c.AwaitMetadataFor(spawn.EntityID)
			}
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("tp " + selector + " 6.5 64 4.5 90 30")
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("execute if entity @e[type=" + tc.kind + ",nbt={Pos:[6.5d,64d,4.5d],Rotation:[90f,30f]}]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("teleport did not move/turn %s: % X", tc.kind, got)
			}
			c.ChatCommand("data merge entity " + selector + ` {Pos:[8.5d,65d,4.5d],Motion:[0.25d,0.5d,0.75d],Rotation:[45f,15f],Tags:["written"],CustomName:"Written",CustomNameVisible:1b` + tc.fields + "}")
			c.AwaitMetadataFor(spawn.EntityID)
			if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.data.entity.modified")) || !bytes.Contains(got, []byte("Written")) {
				t.Fatalf("write refused for %s: % X", tc.kind, got)
			}
			predicate := `Pos:[8.5d,65d,4.5d],Motion:[0.25d,0.5d,0.75d],Rotation:[45f,15f],CustomName:"Written",CustomNameVisible:1b`
			if tc.predicate != "" {
				predicate += "," + tc.predicate
			}
			c.ChatCommand("execute if entity @e[type=" + tc.kind + ",name=Written,tag=written,nbt={" + predicate + "}]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("write did not reach %s: % X", tc.kind, got)
			}
			meta := observer.AwaitMetadataFor(spawn.EntityID)
			if named := servertest.MetaOf(meta, protocol.MetadataIndexCustomName); named == nil || named.TextVal == nil {
				t.Error("write omitted the custom name metadata")
			}
			if tc.kind == "eye_of_ender" {
				if item := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot); item == nil || item.SlotVal.ItemID != int32(registry.ItemDiamond) || item.SlotVal.Count != 1 {
					t.Error("write did not show a single diamond")
				}
				r := protocol.NewReader(observer.ExpectWithinTicks(srv, 0, protocol.IDPlaySetEntityMotion))
				if id := r.ReadVarInt(); id != spawn.EntityID {
					t.Fatalf("motion packet for %d, want %d", id, spawn.EntityID)
				}
				x, y, z := r.ReadLpVec3()
				if math.Abs(x-0.25) > 0.0001 || math.Abs(y-0.5) > 0.0001 || math.Abs(z-0.75) > 0.0001 || r.Err() != nil {
					t.Errorf("client motion = (%v, %v, %v), decode error %v", x, y, z, r.Err())
				}
			}
			r := protocol.NewReader(observer.ExpectWithinTicks(srv, 2, protocol.IDPlayTeleportEntity))
			if id := r.ReadVarInt(); id != spawn.EntityID {
				t.Fatalf("position packet for %d, want %d", id, spawn.EntityID)
			}
			r.ReadVarInt() // position path
			if x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64(); x != 8.5 || y != 65 || z != 4.5 {
				t.Errorf("client position = (%v, %v, %v)", x, y, z)
			}
			if yaw, pitch := r.ReadF32(), r.ReadF32(); yaw != 45 || pitch != 15 {
				t.Errorf("client rotation = (%v, %v)", yaw, pitch)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
			c.ChatCommand("data remove entity " + selector + " CustomName")
			c.AwaitMetadataFor(spawn.EntityID)
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("execute if entity @e[type=" + tc.kind + ",name=Written]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
			if tc.kind == "eye_of_ender" {
				c.ChatCommand("data remove entity " + selector + " Item")
				c.AwaitMetadataFor(spawn.EntityID)
				c.ExpectSystemChatPastMobs()
				c.ChatCommand(`execute if entity @e[type=eye_of_ender,nbt={Item:{id:"minecraft:ender_eye",count:1}}]`)
				if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
					t.Errorf("removing Item did not restore the default eye: % X", got)
				}
			}
			c.ChatCommand("kill " + selector)
			c.ExpectSystemChatPastMobs()
		})
	}
}

// vanilla: Entity.teleportCrossDimension, Entity.restoreFrom
func TestShortLivedEntityTeleportAcrossDimensions(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Traveler", servertest.OpsListing(t, "Traveler"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	c.ChatCommand("execute in minecraft:the_nether run forceload add 0 0")
	c.ExpectSystemChatPastMobs()
	c.ChatCommand("execute in minecraft:the_nether run setblock 0 69 0 stone")
	c.ExpectSystemChatPastMobs()
	for _, tc := range []struct{ mode, destination, motion string }{
		{"relative", "positioned 6.5 70 4.5 run tp ", "0.25d,0.5d,0.75d"},
		{"absolute", "run tp ", "0d,0d,0d"},
		{"mixed", "positioned 6.5 70 4.5 run tp ", "0.25d,0d,0.75d"},
	} {
		for _, kind := range []string{"evoker_fangs", "eye_of_ender", "lightning_bolt"} {
			t.Run(kind+"/"+tc.mode, func(t *testing.T) {
				c.ChatCommand("summon " + kind + ` 2.5 64 4.5 {Motion:[0.25d,0.5d,0.75d],OnGround:0b,Tags:["travel"],Warmup:40,Item:{id:"minecraft:diamond",count:1}}`)
				typ, _ := registry.EntityTypeID("minecraft:" + kind)
				spawn := c.AwaitSpawn(typ)
				if kind == "eye_of_ender" {
					c.AwaitMetadataFor(spawn.EntityID)
				}
				c.ExpectSystemChatPastMobs()
				coords := " ~ ~ ~ 90 30"
				if tc.mode == "absolute" {
					coords = " 6.5 70 4.5 90 30"
				}
				if tc.mode == "mixed" {
					coords = " ~ 70 ~ 90 30"
				}
				c.ChatCommand("execute in minecraft:the_nether " + tc.destination + spawn.Uuid.String() + coords)
				c.ExpectSystemChatPastMobs()
				c.ChatCommand("execute in minecraft:the_nether if entity @e[type=" + kind + ",tag=travel,nbt={Pos:[6.5d,70d,4.5d],Rotation:[90f,30f],OnGround:0b,Motion:[" + tc.motion + "]}]")
				if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
					t.Errorf("%s did not arrive in the nether: % X", kind, got)
				}
				c.ChatCommand("execute if entity @e[type=" + kind + ",distance=..1000]")
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
				c.ChatCommand("execute in minecraft:the_nether run tp " + spawn.Uuid.String() + " 7.5 70 4.5")
				c.ExpectSystemChatPastMobs()
				c.ChatCommand("execute in minecraft:the_nether run kill " + spawn.Uuid.String())
				if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.kill.success.single")) {
					t.Errorf("%s lost its identity on arrival: % X", kind, got)
				}
			})
		}
	}
}
