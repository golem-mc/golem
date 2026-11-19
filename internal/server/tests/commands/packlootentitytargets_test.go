package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestPackConditionsNameEveryEntityTarget checks that entity_scores reads
// the player runner of /loot kill as attacking_player, that execute if
// predicate carries no attacking_player, and that entity_properties finds the
// brushed armadillo as target_entity and its brusher as interacting_entity.
// It rolls tables (silverfish, armadillo brush) no other test of the package
// rolls, since the pack layer is process-wide.
// vanilla: LootContext.EntityTarget, LootCommand.dropKillLoot
func TestPackConditionsNameEveryEntityTarget(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	pool := func(condition, item string) string {
		return `{"rolls":1,"conditions":[` + condition + `],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	const scored = `{"condition":"minecraft:entity_scores","entity":"attacking_player","scores":{"pts":5}}`
	writeFile(t, filepath.Join(tables, "entities", "silverfish.json"), `{"pools":[`+
		pool(scored, "minecraft:diamond")+`,`+
		pool(`{"condition":"minecraft:inverted","term":`+scored+`}`, "minecraft:stick")+`]}`)
	is := func(target, kind string) string {
		return `{"condition":"minecraft:entity_properties","entity":"` + target + `","predicate":{"minecraft:entity_type":"` + kind + `"}}`
	}
	writeFile(t, filepath.Join(tables, "brush", "armadillo.json"), `{"pools":[`+
		pool(is("target_entity", "minecraft:armadillo"), "minecraft:emerald")+`,`+
		pool(is("interacting_entity", "minecraft:player"), "minecraft:diamond")+`,`+
		pool(is("this", "minecraft:armadillo"), "minecraft:stick")+`]}`)
	srv, c := joinPacks(t, cfg)

	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Testificate", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:brush"), Count: 1})
	servertest.HoldTicks(t, srv)
	armadillo, _ := registry.EntityTypeID("minecraft:armadillo")
	id := srv.SpawnMob("minecraft:armadillo", 1.5, 64, 2.5)
	c.ExpectMobSpawn(int32(armadillo))
	c.UseOn(id)
	c.Settle()
	got := map[string]int32{}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		got[it.Item] += it.Count
	}
	if got["minecraft:emerald"] != 1 || got["minecraft:diamond"] != 1 || len(got) != 2 {
		t.Errorf("brush drops = %v, want an emerald (target_entity) and a diamond (interacting_entity)", got)
	}

	c.ChatCommand("scoreboard objectives add pts dummy")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("scoreboard players set @s pts 5")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("summon minecraft:silverfish 4.5 64 4.5 {NoAI:1b}")
	servertest.FeedbackComponent(t, c)
	expectGave(t, c, "loot give @s kill @e[type=minecraft:silverfish,limit=1]", "minecraft:diamond", 1)
	c.ChatCommand("execute as @e[type=minecraft:silverfish,limit=1] run loot give Testificate kill @s")
	updates, chats := servertest.AwaitSlotUpdates(t, c, 1)
	if len(chats) == 0 {
		c.ExpectPlay(protocol.IDPlaySystemChat)
	}
	if updates[0].Stack.ItemID != servertest.MustItem("minecraft:stick") {
		t.Errorf("a silverfish runner's kill gave item %d, want a stick: no attacking_player", updates[0].Stack.ItemID)
	}

	// The command context carries no LAST_DAMAGE_PLAYER.
	c.ChatCommand(`execute if predicate {condition:"minecraft:entity_scores",entity:"attacking_player",scores:{pts:5}}`)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("attacking_player predicate answered % X, want the conditional failure", got)
	}
	c.ChatCommand(`execute if predicate {condition:"minecraft:entity_scores",entity:"this",scores:{pts:5}} run say hi`)
	if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
		t.Errorf("this predicate said % X, want the tail's say", got)
	}
}

// TestAttackingPlayerIsTheKillCreditOfABurnAfterAHit checks that a mob a
// player hit and fire then finished rolls with that player as
// attacking_player, though no attacker struck the killing blow, and that one
// no player hit rolls without one. It rolls the endermite table, which no
// other test of the package rolls.
// vanilla: LivingEntity.dropFromLootTable, lastHurtByPlayer
func TestAttackingPlayerIsTheKillCreditOfABurnAfterAHit(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	const isPlayer = `{"condition":"minecraft:entity_properties","entity":"attacking_player","predicate":{"minecraft:entity_type":"minecraft:player"}}`
	pool := func(condition, item string) string {
		return `{"rolls":1,"conditions":[` + condition + `],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "entities", "endermite.json"),
		`{"pools":[`+pool(isPlayer, "minecraft:diamond")+`,`+pool(`{"condition":"minecraft:inverted","term":`+isPlayer+`}`, "minecraft:stick")+`]}`)
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	drops := func(want string) {
		t.Helper()
		c.ChatCommand("damage @e[type=minecraft:endermite,limit=1] 100 minecraft:on_fire")
		servertest.FeedbackComponent(t, c)
		got := map[string]int32{}
		// The loot drops when the corpse goes, 20 ticks after the death.
		servertest.UntilTicks(t, srv, 40, "the endermite's drop", func() bool {
			clear(got)
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				got[it.Item] += it.Count
			}
			return len(got) > 0
		})
		if got[want] != 1 || len(got) != 1 {
			t.Errorf("burnt endermite drops = %v, want one %s", got, want)
		}
		c.ChatCommand("kill @e[type=minecraft:item]")
		servertest.FeedbackComponent(t, c)
		c.Settle()
	}

	endermite, _ := registry.EntityTypeID("minecraft:endermite")
	srv.SpawnMob("minecraft:endermite", 2.5, 64, 0.5)
	c.ExpectMobSpawn(int32(endermite))
	drops("minecraft:stick")

	id := srv.SpawnMob("minecraft:endermite", 2.5, 64, 0.5)
	c.ExpectMobSpawn(int32(endermite))
	servertest.AwaitFullSwing(t, srv, "Testificate")
	c.Attack(id)
	c.Settle()
	drops("minecraft:diamond")
}

// TestATameWolfsKillCreditsItsOwner checks that a mob a player's tame wolf
// kills rolls with the owner as attacking_player, and that an ownerless tame
// wolf's killing blow wipes a player's earlier hit. It rolls the cow table,
// which no other test of the package rolls.
// vanilla: LivingEntity.resolvePlayerResponsibleForDamage
func TestATameWolfsKillCreditsItsOwner(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	const isPlayer = `{"condition":"minecraft:entity_properties","entity":"attacking_player","predicate":{"minecraft:entity_type":"minecraft:player"}}`
	pool := func(condition, item string) string {
		return `{"rolls":1,"conditions":[` + condition + `],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "entities", "cow.json"),
		`{"pools":[`+pool(isPlayer, "minecraft:diamond")+`,`+
			pool(`{"condition":"minecraft:inverted","term":{"condition":"minecraft:killed_by_player"}}`, "minecraft:stick")+`]}`)
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	wolfType, _ := registry.EntityTypeID("minecraft:wolf")
	cowType, _ := registry.EntityTypeID("minecraft:cow")
	wolf := srv.SpawnMob("minecraft:wolf", -2.5, 64, 0.5)
	c.ExpectMobSpawn(int32(wolfType))
	if !srv.SetMobOwner(wolf, protocol.OfflineUUID("Testificate")) {
		t.Fatal("no wolf to tame")
	}
	killed := func(cow int32, want string) {
		t.Helper()
		if !srv.StrikeMobWith(wolf, cow, 100) {
			t.Fatal("the wolf's blow found no cow")
		}
		got := map[string]int32{}
		// The loot drops when the corpse goes, 20 ticks after the death.
		servertest.UntilTicks(t, srv, 40, "the cow's drop", func() bool {
			clear(got)
			for _, it := range srv.ItemsInDimension(world.DimensionName) {
				got[it.Item] += it.Count
			}
			return len(got) > 0
		})
		if got[want] != 1 || len(got) != 1 {
			t.Errorf("cow drops = %v, want one %s", got, want)
		}
		c.ChatCommand("kill @e[type=minecraft:item]")
		servertest.FeedbackComponent(t, c)
		c.Settle()
	}

	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	c.ExpectMobSpawn(int32(cowType))
	killed(cow, "minecraft:diamond")

	cow = srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	c.ExpectMobSpawn(int32(cowType))
	servertest.AwaitFullSwing(t, srv, "Testificate")
	c.Attack(cow)
	c.Settle()
	srv.SetMobOwner(wolf, uuid.Nil)
	killed(cow, "minecraft:stick")
}
