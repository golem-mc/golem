package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: VibrationInfo, VibrationSelector, VibrationSystem.Data,
// AngerManagement, GossipEntry, VillagerData, SuspiciousStewEffects.Entry codecs.
func TestMobNestedRecordInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	const vibration = `{game_event:"minecraft:step",distance:2f,pos:[1d,64d,2d],source:[I;1,2,3,4],projectile_owner:[I;5,6,7,8]}`
	kinds := []string{"warden", "villager", "mooshroom", "allay", "axolotl", "tadpole", "goat", "frog", "sniffer", "nautilus", "piglin", "piglin_brute"}
	for _, command := range []string{
		`summon warden 4.5 70 4.5 {NoAI:1b,NoGravity:1b,listener:{event:` + vibration + `,selector:{event:` + vibration + `,tick:123L},event_delay:1000},anger:{suspects:[{uuid:[I;1,2,3,4],anger:50}]}}`,
		`summon villager 4.5 70 4.5 {NoAI:1b,NoGravity:1b,VillagerData:{type:"minecraft:taiga",profession:"minecraft:farmer",level:3},Gossips:[{Target:[I;1,2,3,4],Type:"major_positive",Value:10}],Brain:{memories:{"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}},"minecraft:last_slept":{value:123L}}}}`,
		`summon mooshroom 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Type:"brown",stew_effects:[{id:"minecraft:speed",duration:200}]}`,
		`summon allay 4.5 70 4.5 {NoAI:1b,NoGravity:1b,listener:{event:` + vibration + `,selector:{event:` + vibration + `,tick:123L},event_delay:1000},Brain:{memories:{"minecraft:liked_player":{value:[I;1,2,3,4]}}}}`,
		`summon axolotl 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:has_hunting_cooldown":{value:1b,ttl:1000L},"minecraft:play_dead_ticks":{value:1000},"minecraft:temptation_cooldown_ticks":{value:1000}}}}`,
		`summon tadpole 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:temptation_cooldown_ticks":{value:1000}}}}`,
		`summon goat 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:ram_cooldown_ticks":{value:1000},"minecraft:long_jump_cooling_down":{value:1000}}}}`,
		`summon frog 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:is_pregnant":{value:{}},"minecraft:long_jump_cooling_down":{value:1000}}}}`,
		`summon sniffer 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:sniff_cooldown":{value:{},ttl:1000L},"minecraft:sniffer_explored_positions":{value:[{dimension:"minecraft:overworld",pos:[I;1,64,2]}]}}}}`,
		`summon nautilus 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Brain:{memories:{"minecraft:attack_target_cooldown":{value:1000},"minecraft:angry_at":{value:[I;1,2,3,4],ttl:1000L}}}}`,
		`summon piglin 4.5 70 4.5 {NoAI:1b,NoGravity:1b,IsImmuneToZombification:1b,Brain:{memories:{"minecraft:hunted_recently":{value:1b,ttl:1000L}}}}`,
		`summon piglin_brute 4.5 70 4.5 {NoAI:1b,NoGravity:1b,IsImmuneToZombification:1b,Brain:{memories:{"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}}}}}`,
	} {
		c.ChatCommand(command)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	check := func(c *servertest.Client) {
		t.Helper()
		c.Settle()
		for _, record := range []struct{ kind, path, keys string }{
			{"warden", "listener.event", "distance pos source projectile_owner game_event"},
			{"warden", "listener.selector.event", "distance pos source projectile_owner game_event"},
			{"warden", "anger.suspects[0]", "uuid anger"},
			{"villager", "Gossips[0]", "Target Type Value"},
			{"villager", "VillagerData", "profession level type"},
			{"mooshroom", "stew_effects[0]", "duration id"},
			{"allay", "listener.event", "distance pos source projectile_owner game_event"},
			{"allay", "listener.selector.event", "distance pos source projectile_owner game_event"},
			{"allay", `Brain.memories."minecraft:liked_player"`, "value"},
			{"axolotl", `Brain.memories."minecraft:has_hunting_cooldown"`, "value ttl"},
			{"axolotl", `Brain.memories."minecraft:play_dead_ticks"`, "value"},
			{"tadpole", `Brain.memories."minecraft:temptation_cooldown_ticks"`, "value"},
			{"goat", `Brain.memories."minecraft:ram_cooldown_ticks"`, "value"},
			{"goat", `Brain.memories."minecraft:long_jump_cooling_down"`, "value"},
			{"frog", `Brain.memories."minecraft:is_pregnant"`, "value"},
			{"sniffer", `Brain.memories."minecraft:sniff_cooldown"`, "value ttl"},
			{"sniffer", `Brain.memories."minecraft:sniffer_explored_positions".value[0]`, "pos dimension"},
			{"nautilus", `Brain.memories."minecraft:angry_at"`, "value ttl"},
			{"piglin", `Brain.memories."minecraft:hunted_recently"`, "value ttl"},
			{"piglin_brute", `Brain.memories."minecraft:home".value`, "pos dimension"},
			{"villager", `Brain.memories."minecraft:home".value`, "pos dimension"},
		} {
			selector := "@e[type=" + record.kind + ",limit=1]"
			for _, command := range []string{
				"data get entity " + selector + " " + record.path,
				`tellraw @s {nbt:'` + record.path + `',entity:'` + selector + `'}`,
				`tellraw @s {nbt:'` + record.path + `',entity:'` + selector + `',plain:1b}`,
			} {
				c.ChatCommand(command)
				line := servertest.FeedbackComponent(t, c)
				text := line.Plain()
				if strings.HasPrefix(command, "data ") {
					if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
						t.Fatalf("%s: %+v", command, line)
					}
					text = line.With[1].Plain()
				}
				if keys := rootCompoundKeys(text); !slices.Equal(keys, strings.Fields(record.keys)) {
					t.Errorf("%s: keys=%v, want %s", command, keys, record.keys)
				}
				value, err := nbt.ParseSNBT(text)
				if err != nil {
					t.Fatalf("%s: %v", command, err)
				}
				switch {
				case strings.Contains(record.path, "listener"):
					if !nbt.Equal(value["source"], nbt.IntArray{1, 2, 3, 4}) || !nbt.Equal(value["projectile_owner"], nbt.IntArray{5, 6, 7, 8}) || value["distance"] != nbt.Float(2) {
						t.Errorf("vibration values changed: %s", text)
					}
				case record.path == "anger.suspects[0]":
					anger, _ := nbt.IntField(value, "anger")
					if !nbt.Equal(value["uuid"], nbt.IntArray{1, 2, 3, 4}) || anger <= 0 {
						t.Errorf("suspect values changed: %s", text)
					}
				case record.path == "Gossips[0]":
					if value["Value"] != nbt.Int(10) || value["Type"] != nbt.String("major_positive") {
						t.Errorf("gossip values changed: %s", text)
					}
				case record.path == "VillagerData":
					if value["profession"] != nbt.String("minecraft:farmer") || value["level"] != nbt.Int(3) {
						t.Errorf("villager values changed: %s", text)
					}
				case record.path == "stew_effects[0]":
					if value["id"] != nbt.String("minecraft:speed") || value["duration"] != nbt.Int(200) {
						t.Errorf("stew values changed: %s", text)
					}
				case strings.HasSuffix(record.path, ".value"), strings.HasSuffix(record.path, ".value[0]"):
					if value["dimension"] != nbt.String("minecraft:overworld") || !nbt.Equal(value["pos"], nbt.IntArray{1, 64, 2}) {
						t.Errorf("GlobalPos values changed: %s", text)
					}
				}
			}
		}
	}
	check(c)
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
	servertest.UntilTicks(t, again, 20, "saved record owners wake", func() bool {
		for _, kind := range kinds {
			if len(again.SelectedEntities("minecraft:"+kind)) != 1 {
				return false
			}
		}
		return true
	})
	check(back)
}
