package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
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
		return `{"rolls":1,"conditions":[{"type":"minecraft:weather_check","raining":` + flag +
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
