package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEntityQueryTypeAcrossKinds checks that a typed selector finds exactly
// the one body of its type among bodies of every other tracked kind, and that
// an untyped one still finds all of them in summon order.
// vanilla: EntitySelector.findEntities
func TestEntityQueryTypeAcrossKinds(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := servertest.StartExecuteWorldForTwo(t, "Alpha", "Bravo")
	servertest.HoldTicks(t, srv)
	srv.RunLine("scoreboard objectives add ord dummy")
	srv.RunLine("scoreboard objectives add n dummy")
	types := []string{"marker", "item", "arrow", "armor_stand", "oak_boat", "minecart", "zombie", "item_frame"}
	for _, typ := range types {
		extra := ""
		switch typ {
		case "item":
			extra = ` {Item:{id:"stone",count:1}}`
		case "zombie":
			extra = ` {NoAI:1b}`
		}
		srv.RunLine("summon minecraft:" + typ + " 0.5 64 0.5" + extra)
	}
	pass := func(cmd string) bool { return strings.Contains(fmt.Sprint(srv.RunLine(cmd)), "conditional.pass") }
	for _, typ := range types {
		if !pass("execute if entity @e[type=" + typ + ",limit=2]") {
			t.Errorf("@e[type=%s] found nothing", typ)
		}
		srv.RunLine("scoreboard players set #n n 0")
		srv.RunLine("execute as @e[type=" + typ + "] run scoreboard players add #n n 1")
		if !pass("execute if score #n n matches 1") {
			t.Errorf("@e[type=%s] did not find exactly one", typ)
		}
	}
	srv.RunLine("scoreboard players set #n n 0")
	srv.RunLine("execute as @e[type=!player] store result score @s ord run scoreboard players add #n n 1")
	for i, typ := range types {
		if !pass(fmt.Sprintf("execute if entity @e[type=%s,scores={ord=%d}]", typ, i+1)) {
			t.Errorf("@e did not order %s at %d", typ, i+1)
		}
	}
}
