package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func nonlivingDamageServer(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

func damageNonliving(t *testing.T, c *servertest.Client, command, key string, observe func(int32, []byte)) {
	t.Helper()
	c.ChatCommand(command)
	var chat []byte
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			chat = bytes.Clone(data)
		}
		if observe != nil {
			observe(id, data)
		}
	})
	if !bytes.Contains(chat, []byte(key)) {
		t.Errorf("/%s feedback = %q, want %s", command, chat, key)
	}
}

func TestDamageCommandDetonatesCrystal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tag, source  string
		exploded, survives bool
	}{
		{name: "generic", exploded: true},
		{name: "zero", source: "0", exploded: true},
		{name: "explosion", source: "1 minecraft:explosion"},
		{name: "invulnerable", tag: "{Invulnerable:1b}", survives: true},
		{name: "bypass", tag: "{Invulnerable:1b}", source: "1 minecraft:out_of_world", exploded: true},
		{name: "creative source", tag: "{Invulnerable:1b}", source: "1 minecraft:generic by Tester", exploded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:end_crystal 8.5 70 0.5 " + damageTag(tc.tag))
			c.Settle()
			if srv.EndCrystalCount() != 1 {
				t.Fatal("crystal was not summoned")
			}
			source := tc.source
			if source == "" {
				source = "1"
			}
			key := "commands.damage.success"
			if tc.survives {
				key = "commands.damage.invulnerable"
			}
			exploded, removed := false, false
			damageNonliving(t, c, "damage @e[type=minecraft:end_crystal,limit=1] "+source, key, func(id int32, _ []byte) {
				exploded = exploded || id == protocol.IDPlayExplosion
				removed = removed || id == protocol.IDPlayRemoveEntities
			})
			if exploded != tc.exploded || removed == tc.survives || (srv.EndCrystalCount() == 1) != tc.survives {
				t.Errorf("exploded=%v removed=%v crystals=%d, want explosion=%v survives=%v", exploded, removed, srv.EndCrystalCount(), tc.exploded, tc.survives)
			}
		})
	}
}

func TestDamageCommandHurtsVehicles(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart", "chest_minecart", "tnt_minecart"} {
		t.Run(kind, func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:" + kind + " 3.5 64 0.5")
			c.Settle()
			var shakeTime int32
			var shakeDamage float32
			target := "@e[type=minecraft:" + kind + ",limit=1]"
			damageNonliving(t, c, "damage "+target+" 1", "commands.damage.success", func(id int32, data []byte) {
				if id != protocol.IDPlayEntityMetadata {
					return
				}
				r := protocol.NewReader(data)
				r.ReadVarInt()
				fields, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("invalid vehicle metadata")
				}
				for _, f := range fields {
					switch f.Index {
					case protocol.MetadataIndexVehicleShake:
						shakeTime = f.VarintVal
					case protocol.MetadataIndexVehicleShakeDamage:
						shakeDamage = f.FloatVal
					}
				}
			})
			if shakeTime != 10 || shakeDamage != 10 {
				t.Errorf("rocking time=%d damage=%v, want 10 and 10", shakeTime, shakeDamage)
			}
			damageNonliving(t, c, "damage "+target+" 4", "commands.damage.success", nil)
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			if srv.BoatCount() != 0 || len(srv.MinecartsInDimension(world.DimensionName)) != 0 {
				t.Error("vehicle survived accumulated damage > 40")
			}
			if !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:"+kind) {
				t.Error("broken vehicle did not drop its item")
			}
		})
	}
}

func TestDamageCommandHurtsItem(t *testing.T) {
	t.Parallel()
	srv, c := nonlivingDamageServer(t)
	c.ChatCommand(`summon minecraft:item 3.5 64 0.5 {Item:{id:"minecraft:diamond",count:1},PickupDelay:32767s}`)
	c.Settle()
	target := "@e[type=minecraft:item,limit=1]"
	damageNonliving(t, c, "damage "+target+" 1.5", "commands.damage.success", nil)
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Health != 3 {
		t.Fatalf("items=%+v, want health 3", items)
	}
	damageNonliving(t, c, "damage "+target+" 3", "commands.damage.success", nil)
	if srv.ItemCount() != 0 {
		t.Error("destroyed item survived")
	}
}

func TestDamageCommandHurtsArmorStand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source, tag   string
		dead, success bool
		health        float64
	}{
		{source: "1", health: 20},
		{source: "1 minecraft:arrow", dead: true, success: true},
		{source: "1 minecraft:explosion", dead: true},
		{source: "1 minecraft:out_of_world", tag: "{Invulnerable:1b}", dead: true},
		{source: "1 minecraft:arrow", tag: "{Marker:1b}", health: 20},
		{source: "1 minecraft:on_fire", health: 16},
		{source: "1 minecraft:player_attack by Tester", dead: true, success: true},
	} {
		t.Run(fmt.Sprint(tc.source, tc.tag), func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:armor_stand 3.5 64 0.5 " + damageTag(tc.tag))
			c.Settle()
			key := "commands.damage.invulnerable"
			if tc.success {
				key = "commands.damage.success"
			}
			damageNonliving(t, c, "damage @e[type=minecraft:armor_stand,limit=1] "+tc.source, key, nil)
			stands := srv.ArmorStandsInDimension(world.DimensionName)
			if tc.dead {
				if len(stands) != 0 {
					t.Error("stand survived")
				}
				return
			}
			if len(stands) != 1 {
				t.Fatalf("stands=%+v, want one", stands)
			}
			if tc.health == 16 {
				for range 4 {
					damageNonliving(t, c, "damage @e[type=minecraft:armor_stand,limit=1] "+tc.source, key, nil)
				}
				if srv.ArmorStandCount() != 0 {
					t.Error("stand survived five on_fire hurts")
				}
			}
		})
	}
}

func TestDamageCommandBreaksHangingEntities(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"painting", "item_frame", "glow_item_frame"} {
		t.Run(kind, func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
			c.ChatCommand("summon minecraft:" + kind + " 4 64 4 {facing:2b}")
			c.Settle()
			if srv.PaintingCount()+srv.FrameCount() != 1 {
				t.Fatal("hanging entity was not summoned")
			}
			damageNonliving(t, c, "damage @e[type=minecraft:"+kind+",limit=1] 0", "commands.damage.success", nil)
			if srv.PaintingCount()+srv.FrameCount() != 0 {
				t.Error("hanging entity survived")
			}
			if !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:"+kind) {
				t.Error("no hanging entity drop")
			}
		})
	}
}

func damageTag(tag string) string {
	if tag == "" {
		return "{}"
	}
	return tag
}

func TestDamageCommandCreativeVehicleSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:" + kind + " 3.5 64 0.5")
			c.Settle()
			// The executor has infinite materials in both tests. Only the source
			// of the hurt controls the vehicle's creative discard.
			damageNonliving(t, c, "damage @e[type=minecraft:"+kind+",limit=1] 0 minecraft:generic by Tester", "commands.damage.success", nil)
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			if srv.BoatCount() != 0 || len(srv.MinecartsInDimension(world.DimensionName)) != 0 || srv.ItemCount() != 0 {
				t.Error("creative source did not discard vehicle without drops")
			}
		})
	}
}

func TestDamageCommandItemResistance(t *testing.T) {
	t.Parallel()
	srv, c := nonlivingDamageServer(t)
	c.ChatCommand(`summon minecraft:item 3.5 64 0.5 {Item:{id:"minecraft:netherite_ingot",count:1},PickupDelay:32767s,invulnerable_time:20}`)
	c.Settle()
	target := "@e[type=minecraft:item,limit=1]"
	damageNonliving(t, c, "damage "+target+" 10 minecraft:in_fire by Tester", "commands.damage.invulnerable", nil)
	damageNonliving(t, c, "damage "+target+" 1", "commands.damage.invulnerable", nil)
	damageNonliving(t, c, "damage "+target+" 5 minecraft:out_of_world", "commands.damage.success", nil)
	if srv.ItemCount() != 0 {
		t.Error("bypassing damage left item alive")
	}
}

func TestDamageCommandFrameContents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tag, source string
		survives, success bool
	}{
		{name: "pop", tag: `{Item:{id:"minecraft:diamond",count:1}}`, source: "0", survives: true, success: true},
		{name: "explosion", tag: `{Item:{id:"minecraft:diamond",count:1}}`, source: "0 minecraft:explosion", success: true},
		{name: "fixed", tag: "{Fixed:1b}", source: "1", survives: true},
		{name: "fixed bypass", tag: "{Fixed:1b}", source: "1 minecraft:out_of_world", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:item_frame 4 64 4 " + tc.tag)
			c.Settle()
			if srv.FrameCount() != 1 {
				t.Fatal("frame was not summoned")
			}
			key := "commands.damage.success"
			if !tc.success {
				key = "commands.damage.invulnerable"
			}
			damageNonliving(t, c, "damage @e[type=minecraft:item_frame,limit=1] "+tc.source, key, nil)
			if (srv.FrameCount() == 1) != tc.survives {
				t.Error("wrong frame survival")
			}
			if tc.name == "pop" {
				if !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:diamond") {
					t.Error("frame did not pop its diamond")
				}
				damageNonliving(t, c, "damage @e[type=minecraft:item_frame,limit=1] 0", key, nil)
				if srv.FrameCount() != 0 {
					t.Error("empty frame survived second hit")
				}
			}
		})
	}
}

func TestDamageCommandOtherHurtTargets(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"experience_orb", "shulker_bullet", "leash_knot", "cushion"} {
		t.Run(kind, func(t *testing.T) {
			_, c := nonlivingDamageServer(t)
			if kind == "leash_knot" {
				c.ChatCommand("setblock 3 64 0 minecraft:oak_fence")
				c.Settle()
			}
			c.ChatCommand("summon minecraft:" + kind + " 3.5 64 0.5")
			var spawned bool
			c.SettleAndCollect(func(id int32, _ []byte) { spawned = spawned || id == protocol.IDPlaySpawnEntity })
			if !spawned {
				t.Fatal("target was not summoned")
			}
			removed := false
			damageNonliving(t, c, "damage @e[type=minecraft:"+kind+",limit=1] 5", "commands.damage.success", func(id int32, _ []byte) { removed = removed || id == protocol.IDPlayRemoveEntities })
			if !removed {
				t.Error("hurt target was not removed")
			}
		})
	}
}
