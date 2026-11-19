package commands

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestPackLootCopyCustomDataInServerRolls checks that copy_custom_data runs in
// the server's own rolls: a broken smoker's table copies from block_entity,
// and a killed goat's table copies from this, its scoreboard tags included.
// It rolls tables (smoker, goat) no other test of the package rolls, since
// the pack layer is process-wide.
// vanilla: CopyCustomDataFunction.run, ContextNbtProvider
func TestPackLootCopyCustomDataInServerRolls(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	writeFile(t, filepath.Join(tables, "blocks", "smoker.json"), oneItemTable("minecraft:stick",
		`[{"function":"minecraft:copy_custom_data","source":"block_entity",`+
			`"ops":[{"source":"id","target":"be.id","op":"replace"},{"source":"x","target":"be.x","op":"replace"}]}]`))
	writeFile(t, filepath.Join(tables, "entities", "goat.json"), oneItemTable("minecraft:stick",
		`[{"function":"minecraft:copy_custom_data","source":{"type":"minecraft:context","target":"this"},`+
			`"ops":[{"source":"Tags","target":"tags","op":"replace"}]}]`))
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	stick := func(what string) nbt.Compound {
		t.Helper()
		var data nbt.Compound
		servertest.UntilTicks(t, srv, 100, what, func() bool {
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				if it.Item == "minecraft:stick" {
					data = it.Stack.CustomData()
					srv.DiscardItem(it.ID)
					return true
				}
			}
			return false
		})
		return data
	}

	c.ChatCommand("setblock 3 64 3 minecraft:smoker")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("setblock 3 64 3 minecraft:air destroy")
	servertest.FeedbackComponent(t, c)
	be, _ := stick("the smoker's stick")["be"].(nbt.Compound)
	if be["id"] != nbt.String("minecraft:smoker") || be["x"] != nbt.Int(3) {
		t.Errorf("broken smoker's stick carries be = %v, want the block entity's id and x", be)
	}

	c.ChatCommand(`summon minecraft:goat 4.5 64 4.5 {NoAI:1b,Tags:["marked"]}`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("kill @e[type=minecraft:goat]")
	servertest.FeedbackComponent(t, c)
	tags, _ := stick("the goat's stick")["tags"].(nbt.List)
	if len(tags.Items) != 1 || tags.Items[0] != nbt.String("marked") {
		t.Errorf("killed goat's stick carries tags %v, want [marked] from this", tags)
	}
}

// TestPackLootCopyCustomDataBeforeRemoval checks that a destroyed block's
// block_entity is the compound from before its removal: an unopened chest
// still names its loot table and a furnace still holds its recipe counts,
// though the break then unpacks the one and pays out the other.
// vanilla: Level.destroyBlock (dropResources before setBlock)
func TestPackLootCopyCustomDataBeforeRemoval(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data")
	copyBE := func(key string) string {
		return oneItemTable("minecraft:stick", `[{"function":"minecraft:copy_custom_data","source":"block_entity",`+
			`"ops":[{"source":"`+key+`","target":"be.`+key+`","op":"replace"}]}]`)
	}
	writeFile(t, filepath.Join(data, "minecraft", "loot_table", "blocks", "chest.json"), copyBE("LootTable"))
	writeFile(t, filepath.Join(data, "minecraft", "loot_table", "blocks", "furnace.json"), copyBE("RecipesUsed"))
	writeFile(t, filepath.Join(data, "a", "loot_table", "fill.json"), oneItemTable("minecraft:apple", `[]`))
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	be := func(what string) nbt.Compound {
		t.Helper()
		var be nbt.Compound
		servertest.UntilTicks(t, srv, 100, what, func() bool {
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				if it.Item == "minecraft:stick" {
					be, _ = it.Stack.CustomData()["be"].(nbt.Compound)
					srv.DiscardItem(it.ID)
					return true
				}
			}
			return false
		})
		return be
	}

	c.ChatCommand(`setblock 3 64 3 minecraft:chest{LootTable:"a:fill"}`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("setblock 3 64 3 minecraft:air destroy")
	servertest.FeedbackComponent(t, c)
	if got := be("the chest's stick")["LootTable"]; got != nbt.String("a:fill") {
		t.Errorf("broken chest's stick carries LootTable %v, want a:fill", got)
	}
	// The break still unpacks the table into the spill.
	servertest.UntilTicks(t, srv, 100, "the chest's apple", func() bool {
		return slices.ContainsFunc(srv.ItemsInDimension(world.DimensionName), func(it server.TestItem) bool {
			return it.Item == "minecraft:apple"
		})
	})

	c.ChatCommand(`setblock 3 64 3 minecraft:furnace{RecipesUsed:{"minecraft:iron_ingot":2}}`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("setblock 3 64 3 minecraft:air destroy")
	servertest.FeedbackComponent(t, c)
	used, _ := be("the furnace's stick")["RecipesUsed"].(nbt.Compound)
	if used["minecraft:iron_ingot"] != nbt.Int(2) {
		t.Errorf("broken furnace's stick carries RecipesUsed %v, want minecraft:iron_ingot: 2", used)
	}
}

// TestPackLootCopyCustomDataFromKillingPlayer checks that a mob a player kills
// rolls its table with the player's compound as attacker and
// attacking_player, its held stack as SelectedItem. It rolls the chicken's
// table, which no other test of the package rolls.
// vanilla: LivingEntity.dropFromLootTable, NbtPredicate.getEntityTagToCompare
func TestPackLootCopyCustomDataFromKillingPlayer(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	writeFile(t, filepath.Join(tables, "entities", "chicken.json"), oneItemTable("minecraft:stick",
		`[{"function":"minecraft:copy_custom_data","source":"attacking_player",`+
			`"ops":[{"source":"SelectedItem.id","target":"held","op":"replace"}]},`+
			`{"function":"minecraft:copy_custom_data","source":"attacker",`+
			`"ops":[{"source":"SelectedItem.id","target":"attacker_held","op":"replace"}]}]`))
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("item replace entity @s weapon.mainhand with minecraft:netherite_sword")
	c.Settle()

	chicken := srv.SpawnMob("minecraft:chicken", 0.5, 64, 2)
	servertest.AwaitFullSwing(t, srv, "Testificate")
	c.Attack(chicken)
	var data nbt.Compound
	servertest.UntilTicks(t, srv, 100, "the chicken's stick", func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:stick" {
				data = it.Stack.CustomData()
				return true
			}
		}
		return false
	})
	for _, key := range []string{"held", "attacker_held"} {
		if got := data[key]; got != nbt.String("minecraft:netherite_sword") {
			t.Errorf("killed chicken's stick carries %s = %v, want the killer's minecraft:netherite_sword", key, got)
		}
	}
}
