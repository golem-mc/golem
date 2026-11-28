package items

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// alive reports whether an entity carries the tag.
func alive(srv *server.Server, tag string) bool {
	for _, line := range srv.RunLine("execute if entity @e[tag=" + tag + "]") {
		if strings.HasPrefix(line.Translate, "commands.execute.conditional.pass") {
			return true
		}
	}
	return false
}

// TestALavaCauldronBurnsItemsOrbsAndVehicles: a lava cauldron is lava to every
// body, not only a living one. vanilla: LavaCauldronBlock.entityInside
func TestALavaCauldronBurnsItemsOrbsAndVehicles(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Burner")
	servertest.HoldTicks(t, srv)

	for _, c := range []struct {
		x   int32
		lit string
	}{{20, "minecraft:lava_cauldron"}, {10, "minecraft:cauldron"}} {
		if !srv.SetBlock(c.x, 63, 4, registry.MustState(c.lit, nil)) {
			t.Fatal("the world did not take the cauldron")
		}
	}
	srv.RunLine(`summon item 20.5 65 4.5 {Item:{id:stone,count:5},Tags:["lava_item"]}`)
	srv.RunLine(`summon experience_orb 20.5 65 4.5 {Value:3s,Motion:[0d,0d,0d],Tags:["lava_orb"]}`)
	srv.RunLine(`summon item 10.5 65 4.5 {Item:{id:stone,count:5},Tags:["dry_item"]}`)
	srv.RunLine(`summon minecart 20.5 63 4.5 {Tags:["lava_cart"]}`)
	srv.RunLine(`summon oak_boat 20.5 63 4.5 {Tags:["lava_boat"]}`)

	servertest.RunTicks(t, srv, 40)
	for _, tag := range []string{"lava_item", "lava_orb", "lava_cart", "lava_boat"} {
		if alive(srv, tag) {
			t.Errorf("%s survived the lava cauldron", tag)
		}
	}
	if !alive(srv, "dry_item") {
		t.Error("the item over an empty cauldron is gone")
	}
}
