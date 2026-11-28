package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// ticksFrozenOf reads TicksFrozen of the one entity the selector finds, "" when absent.
func ticksFrozenOf(t *testing.T, c *servertest.Client, selector string) string {
	t.Helper()
	c.ChatCommand("data get entity " + selector + " TicksFrozen")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
		return ""
	}
	return line.With[1].Plain()
}

// TestNonLivingBodiesFreezeInPowderSnow: an item, a minecart and a boat in powder
// snow count frozen ticks up to 140 (the vehicles twice a tick, as its block
// checks run twice), and a body taken out of the snow keeps its count.
// vanilla: PowderSnowBlock.entityInside, InsideBlockEffectType.FREEZE, Entity.applyEffectsFromBlocks
func TestNonLivingBodiesFreezeInPowderSnow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	const x, z = 4, 4
	srv.SetBlock(x, 64, z, registry.BlockPowderSnow.DefaultState())
	srv.SetBlock(x+8, 64, z, registry.BlockPowderSnow.DefaultState())
	srv.SetBlock(x+16, 64, z, registry.BlockPowderSnow.DefaultState())
	c.ChatCommand(fmt.Sprintf("summon item %d.5 66 %d.5 {Item:{id:stone,count:5},Motion:[0d,0d,0d],PickupDelay:0s,Tags:[\"a\"]}", x, z))
	c.ChatCommand(fmt.Sprintf("summon minecart %d.5 64 %d.5 {Tags:[\"b\"]}", x+8, z))
	c.ChatCommand(fmt.Sprintf("summon oak_boat %d.5 64 %d.5 {Tags:[\"c\"]}", x+16, z))
	c.Settle()
	const item, cart, boat = "@e[type=item,tag=a,limit=1]", "@e[type=minecart,tag=b,limit=1]", "@e[type=oak_boat,tag=c,limit=1]"

	servertest.RunTicks(t, srv, 30)
	if got := ticksFrozenOf(t, c, item); got != "24" {
		t.Errorf("item TicksFrozen after 30 ticks = %q, want 24", got)
	}
	if got := ticksFrozenOf(t, c, cart); got != "60" {
		t.Errorf("minecart TicksFrozen after 30 ticks = %q, want 60", got)
	}

	if got := ticksFrozenOf(t, c, boat); got != "60" {
		t.Errorf("boat TicksFrozen after 30 ticks = %q, want 60", got)
	}

	servertest.RunTicks(t, srv, 200)
	for _, sel := range []string{item, cart, boat} {
		if got := ticksFrozenOf(t, c, sel); got != "140" {
			t.Errorf("%s TicksFrozen after 230 ticks = %q, want 140", sel, got)
		}
	}

	// Out of the snow nothing thaws a non-living body.
	srv.SetBlock(x, 64, z, registry.BlockAir.DefaultState())
	c.ChatCommand(fmt.Sprintf("data merge entity %s {TicksFrozen:50}", item))
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if got := ticksFrozenOf(t, c, item); got != "50" {
		t.Errorf("item TicksFrozen out of the snow = %q, want 50 kept", got)
	}
}
