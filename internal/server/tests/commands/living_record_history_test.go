package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: AttributeInstance.Packed.CODEC, AttributeModifier.MAP_CODEC,
// MobEffectInstance.CODEC and Details.MAP_CODEC (including hidden details).
func TestLivingRecordCompoundHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	kinds := []string{"pig", "zombie", "horse", "wolf", "mannequin"}
	for _, kind := range kinds {
		c.ChatCommand("summon " + kind + ` 4.5 70 4.5 {NoGravity:1b,NoAI:1b,attributes:[{id:"minecraft:scale",base:1.25d,modifiers:[{id:"minecraft:kept",amount:0.25d,operation:"add_value"}]}],active_effects:[{id:"minecraft:speed",duration:100,amplifier:2b,ambient:1b,show_particles:0b,show_icon:0b,hidden_effect:{duration:200,amplifier:1b,ambient:1b,show_particles:0b,show_icon:0b}}]}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	c.ChatCommand("attribute @s minecraft:scale base set 1.25")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	c.ChatCommand("attribute @s minecraft:scale modifier add minecraft:kept 0.25 add_value")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.modifier.add.success")
	for _, args := range []string{"100 1", "50 2"} {
		c.ChatCommand("effect give @s speed " + args + " true")
		servertest.AwaitSystemChatSaying(t, c, "commands.effect.give.success.single")
	}
	check := func(c *servertest.Client) {
		t.Helper()
		c.Settle()
		for _, kind := range append(slices.Clone(kinds), "player") {
			selector := "@e[type=" + kind + ",limit=1]"
			if kind == "player" {
				selector = "@s"
			}
			for _, record := range []struct{ path, keys string }{
				{`attributes[{id:"minecraft:scale"}]`, "id modifiers base"},
				{`attributes[{id:"minecraft:scale"}].modifiers[0]`, "amount id operation"},
				{"active_effects[0]", "duration show_icon hidden_effect amplifier ambient id show_particles"},
				{"active_effects[0].hidden_effect", "duration show_icon amplifier ambient show_particles"},
			} {
				want := record.keys
				if kind == "player" {
					want = strings.ReplaceAll(want, " ambient", "")
				}
				for _, cmd := range []string{
					"data get entity " + selector + " " + record.path,
					`tellraw @s {nbt:'` + record.path + `',entity:'` + selector + `'}`,
					`tellraw @s {nbt:'` + record.path + `',entity:'` + selector + `',plain:1b}`,
				} {
					c.ChatCommand(cmd)
					line := servertest.FeedbackComponent(t, c)
					text := line.Plain()
					if strings.HasPrefix(cmd, "data ") {
						if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
							t.Fatalf("%s: %+v", cmd, line)
						}
						text = line.With[1].Plain()
					}
					if got := rootCompoundKeys(text); !slices.Equal(got, strings.Fields(want)) {
						t.Errorf("%s: keys=%v, want %s", cmd, got, want)
					}
					value, err := nbt.ParseSNBT(text)
					if err != nil {
						t.Fatalf("%s: %v", cmd, err)
					}
					switch record.path {
					case `attributes[{id:"minecraft:scale"}]`:
						if value["base"] != nbt.Double(1.25) {
							t.Errorf("%s: base=%v", cmd, value["base"])
						}
					case `attributes[{id:"minecraft:scale"}].modifiers[0]`:
						if value["amount"] != nbt.Double(0.25) || value["id"] != nbt.String("minecraft:kept") || value["operation"] != nbt.String("add_value") {
							t.Errorf("%s: modifier=%s", cmd, text)
						}
					default:
						amplifier := nbt.Byte(2)
						if strings.HasSuffix(record.path, ".hidden_effect") {
							amplifier = 1
							if _, hasID := value["id"]; hasID {
								t.Errorf("hidden details contain an effect id: %s", text)
							}
						}
						if value["amplifier"] != amplifier || value["show_icon"] != nbt.Byte(0) || value["show_particles"] != nbt.Byte(0) {
							t.Errorf("%s: effect=%s", cmd, text)
						}
					}
				}
			}
		}
	}
	check(c)
	c.ChatCommand(`data modify storage history compact set string entity @e[type=pig,limit=1] active_effects[0]`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.Extra) == 1 {
		line = line.Extra[0]
	}
	if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 || !slices.IsSorted(rootCompoundKeys(line.With[0].Plain())) {
		t.Fatalf("compact SNBT must keep sorted keys: %+v", line)
	}
	for _, kind := range kinds {
		c.ChatCommand("data merge entity @e[type=" + kind + ",limit=1] {Health:2f}")
		servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	}
	check(c)
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	back := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved living entities wake", func() bool {
		for _, kind := range kinds {
			if len(again.SelectedEntities("minecraft:"+kind)) != 1 {
				return false
			}
		}
		return true
	})
	check(back)
}
