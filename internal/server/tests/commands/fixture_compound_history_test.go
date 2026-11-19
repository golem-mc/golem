package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// Recorded Java HashMap iteration for supported fields, inserted by vanilla 26.3
// Entity.saveWithoutId, LivingEntity/Mannequin and the fixture addAdditionalSaveData
// methods. Marker has exactly twelve query keys: inserting id first would resize it.
var fixtureCompoundHistory = []struct{ kind, tag, keys string }{
	{"block_display", `{brightness:{block:1,sky:2}}`, "NoGravity Motion glow_color_override shadow_radius Invulnerable fall_distance Air OnGround PortalCooldown transformation billboard shadow_strength Rotation interpolation_duration teleport_duration brightness Pos block_state Fire width view_range UUID Tags height"},
	{"item_display", `{brightness:{block:1,sky:2},item:{id:"minecraft:stone",count:1}}`, "NoGravity glow_color_override Invulnerable PortalCooldown interpolation_duration teleport_duration item_display UUID Tags height Motion item shadow_radius fall_distance Air OnGround transformation billboard shadow_strength Rotation brightness Pos Fire width view_range"},
	{"text_display", `{brightness:{block:1,sky:2},text:"order"}`, "NoGravity glow_color_override shadow line_width text_opacity Invulnerable PortalCooldown interpolation_duration teleport_duration see_through text UUID Tags height Motion shadow_radius fall_distance Air OnGround transformation billboard shadow_strength Rotation default_background brightness Pos background Fire width view_range alignment"},
	{"interaction", `{attack:{player:[I;1,2,3,4],timestamp:10L},interaction:{player:[I;1,2,3,4],timestamp:20L}}`, "NoGravity Motion Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos attack response Fire width interaction UUID Tags height"},
	{"marker", `{}`, "NoGravity Motion Pos Fire Invulnerable fall_distance Air OnGround PortalCooldown UUID Rotation Tags"},
	{"cushion", `{color:"red"}`, "NoGravity Motion color block_pos Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire UUID Tags"},
	{"ominous_item_spawner", `{item:{id:"minecraft:stone",count:1},spawn_item_after_ticks:1000L}`, "NoGravity Motion item spawn_item_after_ticks Invulnerable fall_distance Air OnGround PortalCooldown Rotation Pos Fire UUID Tags"},
	{"mannequin", `{TicksFrozen:1000,attributes:[{id:"minecraft:max_health",base:22d}],immovable:1b,hide_description:1b,profile:{id:[I;1,2,3,4],name:"Notch",properties:[{name:"textures",value:"abc",signature:"sig"}],texture:"minecraft:entity/player/wide/steve",cape:"minecraft:entity/player/wide/steve",elytra:"minecraft:entity/player/wide/steve",model:"slim"},equipment:{mainhand:{id:"minecraft:stone"},offhand:{id:"minecraft:stone"},feet:{id:"minecraft:leather_boots"},legs:{id:"minecraft:leather_leggings"},chest:{id:"minecraft:leather_chestplate"},head:{id:"minecraft:leather_helmet"}}}`, "NoGravity Brain immovable hide_description Invulnerable FallFlying PortalCooldown AbsorptionAmount TicksFrozen DeathTime UUID Tags hidden_layers Motion Health pose profile equipment fall_distance Air OnGround Rotation current_impulse_context_reset_grace_time main_hand Pos Fire attributes HurtTime"},
}

func expectFixtureNestedHistory(t *testing.T, c *servertest.Client, kind string) {
	t.Helper()
	paths := map[string]string{}
	switch kind {
	case "block_display", "item_display", "text_display":
		paths["transformation"] = "left_rotation translation right_rotation scale"
	case "mannequin":
		paths["equipment"] = "mainhand head feet chest legs offhand"
		paths["profile"] = "elytra texture name model id cape properties"
		paths["profile.properties[0]"] = "signature name value"
	default:
		return
	}
	selector := "@e[type=" + kind + ",limit=1]"
	for path, want := range paths {
		for _, cmd := range []string{
			"data get entity " + selector + " " + path,
			`tellraw @s {nbt:"` + path + `",entity:"` + selector + `"}`,
			`tellraw @s {nbt:"` + path + `",entity:"` + selector + `",plain:1b}`,
		} {
			c.ChatCommand(cmd)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if strings.HasPrefix(cmd, "data ") {
				text = line.With[1].Plain()
			}
			if got := rootCompoundKeys(text); !slices.Equal(got, strings.Fields(want)) {
				t.Errorf("%s: keys = %v, want %s", cmd, got, want)
			}
		}
	}
}

func TestFixtureCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 69, 4, registry.BlockStone.DefaultState())
	for _, fixture := range fixtureCompoundHistory {
		t.Run(fixture.kind, func(t *testing.T) {
			tag := strings.TrimSuffix(fixture.tag, "}") + `,NoGravity:1b,Tags:["order"]}`
			if fixture.tag == "{}" {
				tag = `{NoGravity:1b,Tags:["order"]}`
			}
			c.ChatCommand("summon " + fixture.kind + " 4.5 70 4.5 " + tag)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			expectNonlivingHistory(t, c, fixture.kind, fixture.keys)
			expectFixtureNestedHistory(t, c, fixture.kind)
			c.ChatCommand("data modify entity @e[type=" + fixture.kind + ",limit=1] Tags append value persisted")
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			c.Settle()
			expectNonlivingHistory(t, c, fixture.kind, fixture.keys)
			expectFixtureNestedHistory(t, c, fixture.kind)
			expectSavedScoreboardTag(t, c, "@e[type="+fixture.kind+",limit=1]", "persisted")
		})
	}
	c.ChatCommand(`data modify storage order text set string entity @e[type=mannequin,limit=1]`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.Extra) == 1 {
		line = line.Extra[0]
	}
	if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 {
		t.Fatalf("compact SNBT feedback = %+v", line)
	}
	if keys := rootCompoundKeys(line.With[0].Plain()); !slices.IsSorted(keys) {
		t.Errorf("compact SNBT keys = %v", keys)
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved fixtures wake", func() bool {
		for _, fixture := range fixtureCompoundHistory {
			if len(again.SelectedEntities("minecraft:"+fixture.kind)) != 1 {
				return false
			}
		}
		return true
	})
	client.Settle()
	for _, fixture := range fixtureCompoundHistory {
		t.Run("reload/"+fixture.kind, func(t *testing.T) {
			expectNonlivingHistory(t, client, fixture.kind, fixture.keys)
			expectFixtureNestedHistory(t, client, fixture.kind)
			expectSavedScoreboardTag(t, client, "@e[type="+fixture.kind+",limit=1]", "persisted")
		})
	}
}
