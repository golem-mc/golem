package commands

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// Fixed identity hashes select legal Map.copyOf histories, not a universal JVM order.
func TestBrainMemoryMapHistory(t *testing.T) {
	t.Parallel()
	for _, salt := range []uint32{0, 0xffffffff} {
		t.Run(fmt.Sprint(salt), func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			ops := servertest.OpsListing(t, "Alice")
			srv, c := servertest.Join(t, cfg, "Alice", ops)
			servertest.HoldTicks(t, srv)
			const home = `"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;1,64,2]}}`
			const meeting = `"minecraft:meeting_point":{value:{dimension:"minecraft:overworld",pos:[I;3,64,4]}}`
			const sniff = `"minecraft:sniff_cooldown":{value:{},ttl:1000L}`
			const sniffing = `"minecraft:is_sniffing":{value:{}}`
			const axolotl = `"minecraft:has_hunting_cooldown":{value:1b,ttl:1000L},"minecraft:play_dead_ticks":{value:1000},"minecraft:temptation_cooldown_ticks":{value:1000}`
			fixtures := []struct{ kind, tag, memories, keys string }{
				{"villager", "v1", home + "," + meeting, "meeting_point home"},
				{"villager", "v2", meeting + "," + home, "meeting_point home"},
				{"warden", "w1", sniff + "," + sniffing, "sniff_cooldown is_sniffing"},
				{"warden", "w2", sniffing + "," + sniff, "sniff_cooldown is_sniffing"},
				{"axolotl", "a1", axolotl, "play_dead_ticks has_hunting_cooldown temptation_cooldown_ticks"},
			}
			hashes := map[string]uint32{"home": 2, "meeting_point": 1, "sniff_cooldown": 1, "is_sniffing": 2,
				"play_dead_ticks": 1, "has_hunting_cooldown": 1, "temptation_cooldown_ticks": 1}
			selector := func(tag string) string { return "@e[tag=" + tag + ",limit=1]" }
			for i, f := range fixtures {
				c.ChatCommand("summon " + f.kind + ` 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Tags:["` + f.tag + `"],Brain:{memories:{` + f.memories + `}}}`)
				servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
				if salt != 0 && f.kind != "axolotl" {
					keys := strings.Fields(f.keys)
					slices.Reverse(keys)
					fixtures[i].keys = strings.Join(keys, " ")
				}
			}
			srv.SetBrainMemoryMapOrder(hashes, salt)
			modified := false
			check := func(c *servertest.Client) {
				t.Helper()
				c.Settle()
				for _, f := range fixtures {
					want, err := nbt.ParseSNBT("{" + f.memories + "}")
					if err != nil {
						t.Fatal(err)
					}
					if modified {
						if f.kind == "villager" {
							memory, _ := nbt.CompoundView(want["minecraft:home"])
							value, _ := nbt.CompoundView(memory["value"])
							value["pos"] = nbt.IntArray{5, 65, 6}
						}
						if f.kind == "warden" {
							memory, _ := nbt.CompoundView(want["minecraft:sniff_cooldown"])
							memory["ttl"] = nbt.Long(2000)
						}
					}
					var wantKeys []string
					for k := range strings.FieldsSeq(f.keys) {
						wantKeys = append(wantKeys, "minecraft:"+k)
					}
					for _, cmd := range []string{
						"data get entity " + selector(f.tag) + " Brain.memories",
						`tellraw @s {nbt:'Brain.memories',entity:'` + selector(f.tag) + `'}`,
						`tellraw @s {nbt:'Brain.memories',entity:'` + selector(f.tag) + `',plain:1b}`,
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
						keys := rootCompoundKeys(text)
						for i, key := range keys {
							if unquoted, err := strconv.Unquote(key); err == nil {
								keys[i] = unquoted
							}
						}
						if !slices.Equal(keys, wantKeys) {
							t.Errorf("%s: keys=%v, want %v", cmd, keys, wantKeys)
						}
						value, err := nbt.ParseSNBT(text)
						if err != nil || !nbt.Equal(value, want) {
							t.Fatalf("%s: values=%s, want %s (parse: %v)", cmd, text, nbt.Stringify(want), err)
						}
					}
				}
			}
			check(c)
			for _, f := range fixtures {
				c.ChatCommand("data merge entity " + selector(f.tag) + " {Health:2f}")
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			}
			check(c)
			for _, f := range fixtures {
				var path, value string
				switch f.kind {
				case "villager":
					path, value = `"minecraft:home".value.pos`, `[I;5,65,6]`
				case "warden":
					path, value = `"minecraft:sniff_cooldown".ttl`, `2000L`
				default:
					continue
				}
				c.ChatCommand("data modify entity " + selector(f.tag) + " Brain.memories." + path + " set value " + value)
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			}
			modified = true
			check(c)
			c.ChatCommand(`data modify storage history compact set string entity ` + selector("v1") + ` Brain.memories`)
			line := servertest.FeedbackComponent(t, c)
			if len(line.Extra) == 1 {
				line = line.Extra[0]
			}
			if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 || !slices.IsSorted(rootCompoundKeys(line.With[0].Plain())) {
				t.Fatalf("compact SNBT must stay sorted: %+v", line)
			}
			srv.FlushMobs()
			c.Disconnect()
			srv.Shutdown()
			again, connect := servertest.Start(t, cfg, ops)
			servertest.FreezeUntilHeld(t, again)
			back := connect().Join("Alice")
			servertest.HoldTicks(t, again)
			servertest.UntilTicks(t, again, 20, "saved memory owners wake", func() bool {
				return len(again.SelectedEntities("minecraft:villager")) == 2 &&
					len(again.SelectedEntities("minecraft:warden")) == 2 &&
					len(again.SelectedEntities("minecraft:axolotl")) == 1
			})
			again.SetBrainMemoryMapOrder(hashes, salt)
			check(back)
		})
	}
}
