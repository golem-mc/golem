package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EntityDataAccessor.setData, AreaEffectCloud.readAdditionalSaveData,
// FireworkRocketEntity.readAdditionalSaveData, Painting.readAdditionalSaveData
func TestCommandDataWritesCloudRocketAndPainting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind                         string
		typ                          int32
		initial, change, path, value string
		index                        uint8
	}{
		{kind: "area_effect_cloud", typ: protocol.EntityTypeAreaEffectCloud, initial: `Duration:-1,WaitTime:0,Radius:2f`, change: `Radius:4f,potion_contents:{custom_color:16711680}`, path: "Radius", value: "4.0f", index: protocol.MetadataIndexCloudRadius},
		{kind: "firework_rocket", typ: protocol.EntityTypeFireworkRocket, initial: `LifeTime:100000,ShotAtAngle:0b`, change: `ShotAtAngle:1b,FireworksItem:{id:"minecraft:firework_rocket",count:1,components:{"minecraft:fireworks":{flight_duration:3,explosions:[]}}}`, path: "ShotAtAngle", value: "1b", index: protocol.MetadataIndexFireworkShotAtAngle},
		{kind: "painting", typ: protocol.EntityTypePainting, initial: `variant:"minecraft:kebab",facing:2b`, change: `variant:"minecraft:pool",facing:0b,block_pos:[I;5,64,5]`, path: "variant", value: `"minecraft:pool"`, index: protocol.MetadataIndexPaintingVariant},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			srv, c := servertest.Join(t, cfg, "Op", servertest.OpsListing(t, "Op"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 5.5 64 5.5 {Tags:["initial"],%s}`, tc.kind, tc.initial))
			spawn := c.AwaitSpawn(tc.typ)
			c.AwaitMetadataFor(spawn.EntityID)
			selector := "@e[type=minecraft:" + tc.kind + ",limit=1]"
			display := servertest.EntityDisplayComponent("minecraft:"+tc.kind, "entity.minecraft."+tc.kind, spawn.Uuid)
			c.ExpectCommandFeedbackText("commands.summon.success", display)
			c.ChatCommand("data modify entity " + selector + " Tags append value written")
			c.ChatCommand("data get entity " + selector + " Tags")
			c.ExpectCommandFeedbackText("commands.data.entity.modified", display)
			if t.Failed() {
				t.FailNow()
			}
			c.ExpectCommandFeedbackText("commands.data.entity.query", display, servertest.NBTArg(`["initial", "written"]`))
			c.ChatCommand("data merge entity " + selector + " {" + tc.change + "}")
			meta := c.AwaitMetadataFor(spawn.EntityID)
			if field := servertest.MetaOf(meta, tc.index); field == nil {
				t.Fatalf("write sent no metadata field %d: %+v", tc.index, meta)
			} else {
				switch tc.kind {
				case "area_effect_cloud":
					if field.FloatVal != 4 {
						t.Errorf("radius = %g, want 4", field.FloatVal)
					}
				case "firework_rocket":
					if !field.FlagVal {
						t.Error("rocket is not shot at angle")
					}
					stack := servertest.MetaOf(meta, protocol.MetadataIndexItemSlot)
					if stack == nil {
						t.Fatal("rocket write sent no stack")
					}
					if fw, ok := stack.SlotVal.FireworksOf(); !ok || fw.FlightDuration != 3 {
						t.Errorf("rocket fireworks = %+v, %t", fw, ok)
					}
				case "painting":
					variants, _ := registry.Get("minecraft:painting_variant")
					if field.VarintVal != variants.ID("minecraft:pool")+1 {
						t.Errorf("variant = %d", field.VarintVal)
					}
					if dir := servertest.MetaOf(meta, protocol.MetadataIndexHangingDirection); dir == nil || dir.VarintVal != protocol.FaceSouth {
						t.Errorf("direction = %+v, want south", dir)
					}
				}
			}
			c.ExpectCommandFeedbackText("commands.data.entity.modified", display)
			query := func(c *servertest.Client) {
				t.Helper()
				c.ChatCommand("data get entity " + selector + " Tags")
				c.ExpectCommandFeedbackText("commands.data.entity.query", display, servertest.NBTArg(`["initial", "written"]`))
				c.ChatCommand("data get entity " + selector + " " + tc.path)
				c.ExpectCommandFeedbackText("commands.data.entity.query", display, servertest.NBTArg(tc.value))
			}
			query(c)
			srv.FlushMobs()
			srv.FlushSaves()
			servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:"+tc.kind, func(tag nbt.Compound) {
				if got, ok := nbt.Get[nbt.List](tag, "Tags"); !ok || len(got.Items) != 2 || got.Items[1] != nbt.String("written") {
					t.Errorf("saved tags = %#v", tag["Tags"])
				}
			})
			c.Disconnect()
			srv.Shutdown()
			restarted := *cfg
			again, c2 := servertest.Join(t, &restarted, "Op", servertest.OpsListing(t, "Op"))
			servertest.HoldTicks(t, again)
			query(c2)
			live := again.SelectedEntities("minecraft:" + tc.kind)
			if len(live) != 1 {
				t.Fatalf("restarted entities = %d", len(live))
			}
			id := live[0].EntityID()
			c2.ChatCommand("data merge entity " + selector + ` {CustomName:"Written",CustomNameVisible:1b}`)
			named := c2.AwaitMetadataFor(id)
			if field := servertest.MetaOf(named, protocol.MetadataIndexCustomName); field == nil || field.TextVal != nbt.String("Written") {
				t.Errorf("custom name = %+v", field)
			}
			if field := servertest.MetaOf(named, protocol.MetadataIndexCustomNameVisible); field == nil || !field.FlagVal {
				t.Errorf("name visibility = %+v", field)
			}
			servertest.AwaitSystemChatSaying(t, c2, "commands.data.entity.modified")
			c2.ChatCommand("data remove entity " + selector + " CustomName")
			cleared := c2.AwaitMetadataFor(id)
			if field := servertest.MetaOf(cleared, protocol.MetadataIndexCustomName); field == nil || field.TextVal != nil {
				t.Errorf("cleared name = %+v", field)
			}
			c2.ExpectCommandFeedbackText("commands.data.entity.modified", display)
			if tc.kind == "firework_rocket" {
				c2.ChatCommand("data merge entity " + selector + " {ShotAtAngle:0b}")
				reset := c2.AwaitMetadataFor(id)
				if field := servertest.MetaOf(reset, protocol.MetadataIndexFireworkShotAtAngle); field == nil || field.FlagVal {
					t.Errorf("reset angle = %+v", field)
				}
				c2.ExpectCommandFeedbackText("commands.data.entity.modified", display)
			}

			c2.Disconnect()
		})
	}
}
