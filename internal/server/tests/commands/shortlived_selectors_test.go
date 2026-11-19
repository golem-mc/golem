package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EntitySelector.findEntities, Entity.kill
func TestSelectorsFindShortLivedEntities(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)

	for _, tc := range []struct {
		kind, name string
		wide, tall bool
	}{
		{"evoker_fangs", "Evoker Fangs", true, true},
		{"eye_of_ender", "Eye of Ender", false, false},
		{"lightning_bolt", "Lightning Bolt", false, false},
	} {
		kind := tc.kind
		t.Run(kind, func(t *testing.T) {
			c.ChatCommand("summon " + kind + " 5.5 64 5.5")
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("execute if entity @e[type=" + kind + "]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("selector missed live %s: % X", kind, got)
			}
			for _, filter := range []string{
				`name="` + tc.name + `"`,
				"x=5,y=63.5,z=5,dx=0,dy=0,dz=0",
				"nbt={Pos:[5.5d,64d,5.5d]}",
			} {
				c.ChatCommand("execute if entity @e[type=" + kind + "," + filter + "]")
				if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
					t.Errorf("%s did not match %s: % X", kind, filter, got)
				}
			}
			for _, box := range []struct {
				filter string
				match  bool
			}{
				{"x=5.7,y=64,z=5,dx=0,dy=0,dz=0", tc.wide},
				{"x=5,y=64.5,z=5,dx=0,dy=0,dz=0", tc.tall},
			} {
				c.ChatCommand("execute if entity @e[type=" + kind + "," + box.filter + "]")
				got := c.ExpectSystemChatPastMobs()
				if pass := bytes.Equal(got, conditionalPassCountForTest(1)); pass != box.match {
					t.Errorf("%s box %s matched = %v, want %v", kind, box.filter, pass, box.match)
				}
			}
			c.ChatCommand("execute as @e[type=" + kind + "] run tag @s add executor")
			c.Settle()
			c.ChatCommand("execute if entity @e[type=" + kind + ",tag=executor]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("execute as did not tag %s: % X", kind, got)
			}
			c.ChatCommand("kill @e[type=" + kind + "]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Contains(got, []byte("commands.kill.success.single")) {
				t.Errorf("kill did not find exactly one %s: % X", kind, got)
			}
			c.ChatCommand("execute if entity @e[type=" + kind + "]")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
		})
	}
}

// vanilla: EvokerFangs.addAdditionalSaveData, EyeOfEnder.addAdditionalSaveData,
// LightningBolt.addAdditionalSaveData, Entity.saveWithoutId
func TestShortLivedEntitySelectorNamesTagsAndData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Writer", servertest.OpsListing(t, "Writer"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	for _, tc := range []struct {
		kind, fields, path, value string
	}{
		{"evoker_fangs", ",Warmup:7,Owner:[I;1,2,3,4]", "Warmup", "7"},
		{"eye_of_ender", `,Item:{id:"minecraft:diamond",count:1}`, "Item.id", `"minecraft:diamond"`},
		{"lightning_bolt", "", "Fire", "0s"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			cmd := "summon " + tc.kind + ` 5.5 64 5.5 {UUID:[I;4,3,2,1],Tags:["given"],CustomName:"Named"` + tc.fields + "}"
			c.ChatCommand(cmd)
			typ, _ := registry.EntityTypeID("minecraft:" + tc.kind)
			spawn := c.AwaitSpawn(typ)
			meta := c.AwaitMetadataFor(spawn.EntityID)
			if named := servertest.MetaOf(meta, protocol.MetadataIndexCustomName); named == nil || named.TextVal != nbt.String("Named") {
				t.Errorf("%s spawn omitted its custom name: %+v", tc.kind, named)
			}
			if tc.kind == "eye_of_ender" {
				if item := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot); item == nil || item.SlotVal.ItemID != int32(registry.ItemDiamond) {
					t.Errorf("eye spawn omitted its diamond item: %+v", item)
				}
			}
			c.ExpectSystemChatPastMobs()
			c.ChatCommand(cmd)
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.summon.failed.uuid"))
			name := servertest.EntityDisplayComponent("minecraft:"+tc.kind, "", spawn.Uuid)
			name.Text = "Named"
			name.Translate = ""
			name.HoverEntity.Name = protocol.Text{Text: "Named"}
			for _, cmd := range []string{
				"execute if entity @e[type=" + tc.kind + ",name=Named,tag=given]",
				"execute if entity @e[type=" + tc.kind + `,nbt={CustomName:"Named",Tags:["given"]}]`,
			} {
				c.ChatCommand(cmd)
				if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
					t.Errorf("/%s failed: % X", cmd, got)
				}
			}
			c.ChatCommand("data get entity @e[type=" + tc.kind + ",limit=1] " + tc.path)
			c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg(tc.value))
			if tc.kind == "evoker_fangs" {
				c.ChatCommand("data get entity @e[type=evoker_fangs,limit=1] Owner")
				c.ExpectCommandFeedbackText("commands.data.entity.query", name, servertest.NBTArg("[I; 1, 2, 3, 4]"))
			}
			c.ChatCommand("tag @e[type=" + tc.kind + "] add selected")
			c.ExpectSystemChatPastMobs()
			c.ChatCommand("execute if entity @e[type=" + tc.kind + ",tag=selected]")
			if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
				t.Errorf("added tag did not reach live %s: % X", tc.kind, got)
			}
			c.ChatCommand("kill @e[type=" + tc.kind + "]")
			c.ExpectCommandFeedbackText("commands.kill.success.single", name)
		})
	}
}
