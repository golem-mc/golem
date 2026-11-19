package commands

import (
	"compress/gzip"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// Recorded Java HashMap collision groups at the player save's 64-bucket size.
// vanilla: Entity.saveWithoutId, LivingEntity/Player/ServerPlayer.addAdditionalSaveData
func expectPlayerHistory(t *testing.T, c *servertest.Client) {
	t.Helper()
	for _, path := range []string{"{}", "abilities", "equipment", "recipeBook"} {
		for _, cmd := range []string{
			"data get entity @s " + path,
			`tellraw @s {nbt:"` + path + `",entity:"@s"}`,
			`tellraw @s {nbt:"` + path + `",entity:"@s",plain:1b}`,
		} {
			if path == "{}" && strings.HasPrefix(cmd, "data ") {
				cmd = "data get entity @s"
			}
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
			if path == "{}" {
				for _, group := range []string{"Invulnerable FallFlying", "PortalCooldown AbsorptionAmount", "fall_distance Air OnGround"} {
					last := -1
					for key := range strings.FieldsSeq(group) {
						at := slices.Index(keys, key)
						if at <= last {
							t.Errorf("%s: collision group %s out of order in %v", cmd, group, keys)
						}
						last = at
					}
				}
			} else {
				want := map[string]string{
					"abilities":  "invulnerable mayfly instabuild walkSpeed mayBuild flying flySpeed",
					"equipment":  "head feet chest legs offhand",
					"recipeBook": "recipes isBlastingFurnaceFilteringCraftable isSmokerGuiOpen isFilteringCraftable toBeDisplayed isFurnaceGuiOpen isGuiOpen isFurnaceFilteringCraftable isBlastingFurnaceGuiOpen isSmokerFilteringCraftable",
				}[path]
				if !slices.Equal(keys, strings.Fields(want)) {
					t.Errorf("%s: keys=%v, want %s", cmd, keys, want)
				}
			}
		}
	}
}

func TestPlayerCompoundInsertionHistory(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	for _, pair := range []string{"weapon.mainhand stone", "weapon.offhand shield", "armor.feet leather_boots", "armor.legs leather_leggings", "armor.chest leather_chestplate", "armor.head leather_helmet"} {
		fields := strings.Fields(pair)
		c.ChatCommand("item replace entity @s " + fields[0] + " with " + fields[1])
		servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.replace.success.single.known_item")
	}
	for book := range int32(4) {
		c.Send(protocol.IDPlayRecipeBookChangeSettings, func(b *protocol.Buffer) { b.PutVarInt(book); b.PutBool(true); b.PutBool(true) })
	}
	c.ChatCommand("tag @s add order")
	servertest.AwaitSystemChatSaying(t, c, "commands.tag.add.success.single")
	c.Settle()
	expectPlayerHistory(t, c)
	c.ChatCommand("tag @s add written")
	servertest.AwaitSystemChatSaying(t, c, "commands.tag.add.success.single")
	c.Settle()
	expectPlayerHistory(t, c)
	c.ChatCommand(`data modify storage order text set string entity @s`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.Extra) == 1 {
		line = line.Extra[0]
	}
	if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 {
		t.Fatalf("compact SNBT: %+v", line)
	}
	if keys := rootCompoundKeys(line.With[0].Plain()); !slices.IsSorted(keys) {
		t.Errorf("compact SNBT keys=%v", keys)
	}
	c.ChatCommand(`summon oak_boat 1 64 1 {NoGravity:1b,Tags:["saved_order"]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`ride @s mount @e[type=oak_boat,tag=saved_order,limit=1]`)
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.Settle()
	c.Disconnect()
	srv.Shutdown()
	// Inspect the actual file without converting its ordered compounds to maps.
	f, err := os.Open(servertest.PlayerFile(cfg.LevelName, "Alice"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zipped, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	data, err := io.ReadAll(zipped)
	if err != nil {
		t.Fatal(err)
	}
	_, saved, err := nbt.DecodeOrdered(data)
	if err != nil {
		t.Fatal(err)
	}
	root := saved.(nbt.CompoundTag)
	vehicle, ok := nbt.Get[nbt.Compound](root.Entries(), "RootVehicle")
	if !ok {
		t.Fatal("RootVehicle missing from player file")
	}
	body, ok := vehicle["Entity"].(nbt.CompoundTag)
	if !ok {
		t.Fatal("RootVehicle.Entity missing from player file")
	}
	// Recorded Entity.saveAsPassenger collision group, including id at save time.
	keys := nbt.HashOrderedKeys(body)
	if !(slices.Index(keys, "fall_distance") >= 0 && slices.Index(keys, "fall_distance") < slices.Index(keys, "Air") && slices.Index(keys, "Air") < slices.Index(keys, "OnGround")) {
		t.Errorf("saved boat keys=%v", keys)
	}
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	back := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	back.Settle()
	expectPlayerHistory(t, back)
	expectSavedScoreboardTag(t, back, "@s", "written")
}

func expectSavedScoreboardTag(t *testing.T, c *servertest.Client, target, want string) {
	t.Helper()
	c.ChatCommand("data get entity " + target + " Tags")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 || !strings.Contains(line.With[1].Plain(), `"`+want+`"`) {
		t.Errorf("saved tag %s missing: %+v", want, line)
	}
}
