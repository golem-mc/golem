package commands

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPackEntityComponentsReadSavedFields checks that a pack predicate's
// exact and partial components tests read the custom name, custom data,
// a cloud's potion off the kinds that hold them, and that only projectiles hold can_break.
// vanilla: EntityExactDataComponentsPredicate, EntityPartialComponentsPredicate, Entity.get
func TestPackEntityComponentsReadSavedFields(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "predicate")
	entity := func(predicate string) string {
		return `{"condition":"minecraft:entity_properties","entity":"this","predicate":{` + predicate + `}}`
	}
	writeFile(t, filepath.Join(data, "named.json"), entity(`"minecraft:components":{"minecraft:custom_name":"Bob"}`))
	writeFile(t, filepath.Join(data, "poison.json"), entity(`"minecraft:components":{"minecraft:potion_contents":{"potion":"minecraft:poison"}}`))
	writeFile(t, filepath.Join(data, "poison_set.json"), entity(`"minecraft:predicates":{"minecraft:potion_contents":{"potions":"minecraft:poison"}}`))
	writeFile(t, filepath.Join(data, "scale.json"), entity(`"minecraft:components":{"minecraft:potion_duration_scale":0.5}`))
	writeFile(t, filepath.Join(data, "marked.json"), entity(`"minecraft:predicates":{"minecraft:custom_data":"{mark:1b}"}`))
	writeFile(t, filepath.Join(data, "breaks.json"), entity(`"minecraft:components":{"minecraft:can_break":{"blocks":"minecraft:stone"}}`))
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	says := func(name, selector string, want bool) {
		t.Helper()
		command := "execute if entity " + selector[:len(selector)-1] + ",predicate=pk:" + name + "]"
		if want {
			c.ChatCommand(command + " run say hi")
			if got := c.ExpectPlay(protocol.IDPlayDisguisedChat); !bytes.Equal(got, sayPayloadForTest("Testificate", "hi")) {
				t.Errorf("%s said % X, want the say", name, got)
			}
			return
		}
		c.ChatCommand(command)
		if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%s answered % X, want the conditional failure", name, got)
		}
	}
	run := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		c.Settle()
	}

	run(`summon minecraft:zombie 4 64 4 {NoAI:1b,Tags:["bob"],CustomName:"Bob",data:{mark:1b}}`)
	run(`summon minecraft:zombie 6 64 4 {NoAI:1b,Tags:["plain"]}`)
	run(`summon minecraft:area_effect_cloud 8 64 4 {Tags:["cloud"],potion_contents:{potion:"minecraft:poison"},potion_duration_scale:0.5f}`)
	run(`summon minecraft:area_effect_cloud 10 64 4 {Tags:["empty"]}`)
	run(`summon minecraft:area_effect_cloud 12 64 4 {Tags:["broken"],can_break:{blocks:"minecraft:stone"}}`)
	says("breaks", "@e[tag=broken]", false)
	says("named", "@e[tag=bob]", true)
	says("named", "@e[tag=plain]", false)
	says("marked", "@e[tag=bob]", true)
	says("marked", "@e[tag=plain]", false)
	says("poison", "@e[tag=cloud]", true)
	says("poison", "@e[tag=empty]", false)
	says("poison_set", "@e[tag=cloud]", true)
	says("poison_set", "@e[tag=empty]", false)
	says("poison_set", "@e[tag=bob]", false)
	says("scale", "@e[tag=cloud]", true)
	says("scale", "@e[tag=empty]", false)
}
