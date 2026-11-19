package commands

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestPackConditionsReadWeatherTimeAndScores checks that weather_check,
// time_check and entity_scores answer execute if predicate from the level's
// weather, a clock and the scoreboard, and that a pack loot table gated on
// weather_check loads and rolls the pool the weather allows.
// vanilla: WeatherCheck, TimeCheck, EntityHasScoreCondition
func TestPackConditionsReadWeatherTimeAndScores(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	gated := func(raining bool, item string) string {
		flag := "false"
		if raining {
			flag = "true"
		}
		return `{"rolls":1,"conditions":[{"condition":"minecraft:weather_check","raining":` + flag +
			`}],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	writeFile(t, filepath.Join(data, "loot_table", "rainy.json"),
		`{"pools":[`+gated(true, "minecraft:diamond")+`,`+gated(false, "minecraft:stick")+`]}`)
	srv, c := joinPacks(t, cfg)
	// Held, so the weather fades only while DrainWeather reads its packets.
	servertest.HoldTicks(t, srv)

	says := func(command string, want bool) {
		t.Helper()
		c.ChatCommand(command)
		if want {
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%q said % X, want the tail's say", command, got)
			}
			return
		}
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%q answered % X, want the conditional failure", command, got)
		}
	}
	const raining = `execute if predicate {condition:"minecraft:weather_check",raining:true}`

	// A fresh level starts clear, its rain level at zero.
	says(raining, false)
	says(`execute if predicate {condition:"minecraft:weather_check"} run say hi`, true)
	expectGave(t, c, "loot give @s loot pk:rainy", "minecraft:stick", 1)
	c.ChatCommand("weather rain 100000")
	servertest.FeedbackComponent(t, c)
	// The rain level fades in a hundredth a tick and counts past 0.2.
	servertest.DrainWeather(c, srv, 25)
	if !srv.IsRaining() {
		t.Fatal("not raining 25 ticks after /weather rain")
	}
	says(raining+" run say hi", true)
	says(`execute if predicate {condition:"minecraft:weather_check",raining:true,thundering:true}`, false)
	expectGave(t, c, "loot give @s loot pk:rainy", "minecraft:diamond", 1)

	const daytime = `execute if predicate {condition:"minecraft:time_check",clock:"minecraft:overworld",period:24000,value:{min:0,max:12000}}`
	c.ChatCommand("time set 30000")
	servertest.FeedbackComponent(t, c)
	says(daytime+" run say hi", true)
	c.ChatCommand("time set 18000")
	servertest.FeedbackComponent(t, c)
	says(daytime, false)

	c.ChatCommand("scoreboard objectives add pts dummy")
	servertest.FeedbackComponent(t, c)
	const scored = `execute if predicate {condition:"minecraft:entity_scores",entity:"this",scores:{pts:{min:3,max:7}}}`
	says(scored, false) // no score yet
	c.ChatCommand("scoreboard players set @s pts 5")
	servertest.FeedbackComponent(t, c)
	says(scored+" run say hi", true)
	says(`execute if predicate {condition:"minecraft:entity_scores",entity:"this",scores:{pts:5}} run say hi`, true)
	says(`execute if predicate {condition:"minecraft:entity_scores",entity:"this",scores:{pts:5,nope:{}}}`, false)
	c.ChatCommand("scoreboard players set @s pts 9")
	servertest.FeedbackComponent(t, c)
	says(scored, false)

	says(`execute if predicate {condition:"minecraft:int_value_check",value:4,test:{min:1,max:{type:"minecraft:uniform",min:4,max:6}}} run say hi`, true)
	says(`execute if predicate {condition:"minecraft:float_value_check",value:0.5,test:0.25}`, false)
	says(`execute if predicate {condition:"minecraft:enchantment_active_check",active:true}`, false)
}

// TestGameplayLootRollsReadTheLevelsWeather checks that a broken block's and
// a dead mob's pack loot tables roll in the level they happen in: a pool
// gated on weather_check drops its rain item in rain and its clear item
// otherwise.
// vanilla: LootParams.Builder(ServerLevel), WeatherCheck
func TestGameplayLootRollsReadTheLevelsWeather(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	gated := func(raining, item string) string {
		return `{"rolls":1,"conditions":[{"condition":"minecraft:weather_check","raining":` + raining +
			`}],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	rainy := `{"pools":[` + gated("true", "minecraft:diamond") + `,` + gated("false", "minecraft:stick") + `]}`
	writeFile(t, filepath.Join(tables, "blocks", "slime_block.json"), rainy)
	writeFile(t, filepath.Join(tables, "entities", "pig.json"), rainy)
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	slime, ok := registry.FindState("minecraft:slime_block", nil)
	if !ok {
		t.Fatal("the registry has no slime block")
	}
	diamond, stick := int32(registry.ItemDiamond), int32(registry.ItemStick)
	one := func(w servertest.BreakWire) bool { return len(w.Drops) > 0 }

	drops := func(weather string, want int32) {
		t.Helper()
		x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]
		if !srv.SetBlock(x, y, z, slime) {
			t.Fatal("could not stand the slime block")
		}
		c.Settle()
		// A slime block's hardness is zero: a survival dig breaks it on the
		// start.
		c.BreakBlock(x, y, z)
		if got := c.ReadBreak(x, y, z, one).Drops; len(got) != 1 || got[0].ItemID != want {
			t.Errorf("a slime block broken in %s weather dropped %+v, want item %d", weather, got, want)
		}
		c.ChatCommand("summon minecraft:pig 3.5 64 3.5 {NoAI:1b}")
		servertest.FeedbackComponent(t, c)
		c.ChatCommand("kill @e[type=minecraft:pig]")
		if got := c.ReadBreak(x, y, z, one).Drops; len(got) != 1 || got[0].ItemID != want {
			t.Errorf("a pig killed in %s weather dropped %+v, want item %d", weather, got, want)
		}
	}

	// A fresh level starts clear, its rain level at zero.
	drops("clear", stick)
	c.ChatCommand("weather rain 100000")
	servertest.FeedbackComponent(t, c)
	// The rain level fades in a hundredth a tick and counts past 0.2.
	servertest.UntilTicks(t, srv, 40, "rain", srv.IsRaining)
	drops("rainy", diamond)
}

// TestMobTickLootRollsReadTheLevelsWeather checks that the rolls made inside
// a mob's tick, an armadillo's shed scute and a piglin's barter, carry the
// level: a pool gated on weather_check gives its rain item in rain and its
// clear item otherwise.
// vanilla: LivingEntity.dropFromGiftLootTable, PiglinAi.getBarterResponseItems
func TestMobTickLootRollsReadTheLevelsWeather(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "gameplay")
	gated := func(raining, item string) string {
		return `{"rolls":1,"conditions":[{"condition":"minecraft:weather_check","raining":` + raining +
			`}],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	rainy := `{"pools":[` + gated("true", "minecraft:diamond") + `,` + gated("false", "minecraft:stick") + `]}`
	writeFile(t, filepath.Join(tables, "armadillo_shed.json"), rainy)
	writeFile(t, filepath.Join(tables, "piglin_bartering.json"), rainy)
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	// Gold armour keeps the piglin from hunting the player.
	c.ChatCommand("item replace entity Testificate armor.feet with minecraft:golden_boots")
	c.Settle()
	servertest.HoldTicks(t, srv)

	// loot is the items on the ground but /give's display copy of the ingot.
	loot := func() []server.TestItem {
		return slices.DeleteFunc(srv.ItemsInDimension(world.DimensionName), func(it server.TestItem) bool {
			return it.Item == "minecraft:gold_ingot"
		})
	}
	only := func(what, weather, want string) {
		t.Helper()
		if items := loot(); len(items) != 1 || items[0].Item != want {
			t.Errorf("%s in %s weather gave %v, want one %s", what, weather, items, want)
		}
		c.ChatCommand("kill @e[type=minecraft:item]")
		c.Settle()
	}
	rolls := func(weather, want string) {
		t.Helper()
		a := srv.SpawnMob("minecraft:armadillo", 3.5, 64, 3.5)
		srv.SetScuteTimer(a, 1)
		servertest.RunTicks(t, srv, 1)
		only("a shed scute", weather, want)

		c.ChatCommand("give Testificate minecraft:gold_ingot 1")
		c.Settle()
		p := srv.SpawnMob("minecraft:piglin", 2.5, 64, 0.5)
		c.UseOn(p)
		c.Settle()
		// A piglin admires the ingot for 119 ticks before it barters.
		servertest.UntilTicks(t, srv, 140, "the barter", func() bool { return len(loot()) > 0 })
		only("a barter", weather, want)
	}

	// A fresh level starts clear, its rain level at zero.
	rolls("clear", "minecraft:stick")
	c.ChatCommand("weather rain 100000")
	servertest.FeedbackComponent(t, c)
	// The rain level fades in a hundredth a tick and counts past 0.2.
	servertest.UntilTicks(t, srv, 40, "rain", srv.IsRaining)
	rolls("rainy", "minecraft:diamond")
}

// TestGameplayLootRollsReadScoresAndStorage checks that a broken block's and
// a dead mob's pack loot tables read the level's scoreboard and command
// storage through set_count's score and storage providers.
// vanilla: ScoreboardValue, StoredNumberAccess
func TestGameplayLootRollsReadScoresAndStorage(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table")
	counted := func(count string) string {
		return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stick",` +
			`"functions":[{"function":"minecraft:set_count","count":` + count + `}]}]}]}`
	}
	writeFile(t, filepath.Join(tables, "blocks", "slime_block.json"),
		counted(`{"type":"minecraft:score","target":{"type":"minecraft:fixed","name":"x"},"score":"o"}`))
	writeFile(t, filepath.Join(tables, "entities", "pig.json"),
		counted(`{"type":"minecraft:storage","storage":"pk:s","path":"n"}`))
	srv, c := joinPacks(t, cfg)
	c.MoveToGround(0.5, 64, 0.5, true)
	for _, command := range []string{"scoreboard objectives add o dummy", "scoreboard players set x o 4", "data modify storage pk:s n set value 3"} {
		c.ChatCommand(command)
		servertest.FeedbackComponent(t, c)
	}
	slime, ok := registry.FindState("minecraft:slime_block", nil)
	if !ok {
		t.Fatal("the registry has no slime block")
	}
	stick := int32(registry.ItemStick)
	one := func(w servertest.BreakWire) bool { return len(w.Drops) > 0 }
	x, y, z := servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]
	if !srv.SetBlock(x, y, z, slime) {
		t.Fatal("could not stand the slime block")
	}
	c.Settle()
	// A slime block's hardness is zero: a survival dig breaks it on the start.
	c.BreakBlock(x, y, z)
	if got := c.ReadBreak(x, y, z, one).Drops; len(got) != 1 || got[0].ItemID != stick || got[0].Count != 4 {
		t.Errorf("a slime block counted by x's score o of 4 dropped %+v, want 4 sticks", got)
	}
	c.ChatCommand("summon minecraft:pig 3.5 64 3.5 {NoAI:1b}")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("kill @e[type=minecraft:pig]")
	if got := c.ReadBreak(x, y, z, one).Drops; len(got) != 1 || got[0].ItemID != stick || got[0].Count != 3 {
		t.Errorf("a pig counted by storage pk:s n of 3 dropped %+v, want 3 sticks", got)
	}
}
