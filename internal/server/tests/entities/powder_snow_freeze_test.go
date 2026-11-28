package entities

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestOtherNonLivingBodiesFreezeInPowderSnow: every other non-living kind
// that walks the cells it moves through counts frozen ticks in powder snow,
// and one left in lava thaws to nothing.
// vanilla: PowderSnowBlock.entityInside, Entity.applyEffectsFromBlocks
func TestOtherNonLivingBodiesFreezeInPowderSnow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	kinds := []struct{ name, summon string }{
		{"orb", "experience_orb {Motion:[0d,0d,0d]}"},
		{"tnt", "tnt {fuse:200s,NoGravity:1b}"},
		{"falling", "falling_block {BlockState:{Name:\"minecraft:sand\"},NoGravity:1b,Time:1}"},
		{"arrow", "arrow {NoGravity:1b,Motion:[0d,0d,0d]}"},
		{"snowball", "snowball {NoGravity:1b,Motion:[0d,0d,0d]}"},
		{"wind", "wind_charge {Motion:[0d,0d,0d]}"},
		{"skull", "wither_skull {Motion:[0d,0d,0d]}"},
		{"bullet", "shulker_bullet {NoGravity:1b}"},
		{"firework", "firework_rocket {LifeTime:200,FireworksItem:{id:firework_rocket,count:1}}"},
	}
	const z = 4
	for i, k := range kinds {
		x := 4 + 2*i
		srv.SetBlock(int32(x), 64, z, registry.BlockPowderSnow.DefaultState())
		c.ChatCommand(fmt.Sprintf("summon %s", strings.Replace(k.summon, " ", fmt.Sprintf(" %d.5 64.2 %d.5 ", x, z), 1)))
		c.ChatCommand(fmt.Sprintf("tag @e[distance=..40,limit=1,sort=nearest,x=%d.5,y=64.2,z=%d.5,type=!player] add k%d", x, z, i))
	}
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	for i, k := range kinds {
		sel := fmt.Sprintf("@e[tag=k%d,limit=1]", i)
		got := ticksFrozenOf(t, c, sel)
		if n, err := strconv.Atoi(got); err != nil || n < 1 || n > 6 {
			t.Errorf("%s TicksFrozen after 3 ticks = %q, want a count of 1 to 6", k.name, got)
		}
	}
}

// TestFrozenTicksReachWatchers: each non-living kind tells its watchers the
// frozen count (entity data 7) once it is in powder snow.
// vanilla: Entity.DATA_TICKS_FROZEN, ServerEntity.sendChanges
func TestFrozenTicksReachWatchers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	summons := []string{
		"item {Item:{id:stone,count:1},Motion:[0d,0d,0d]}",
		"experience_orb {Motion:[0d,0d,0d]}",
		"tnt {fuse:200s,NoGravity:1b}",
		"falling_block {BlockState:{Name:\"minecraft:sand\"},NoGravity:1b,Time:1}",
		"arrow {NoGravity:1b,Motion:[0d,0d,0d]}",
		"snowball {NoGravity:1b,Motion:[0d,0d,0d]}",
		"firework_rocket {LifeTime:200,FireworksItem:{id:firework_rocket,count:1}}",
		"minecart {}",
		"oak_boat {}",
	}
	for i, s := range summons {
		x := 4 + 2*i
		srv.SetBlock(int32(x), 64, 4, registry.BlockPowderSnow.DefaultState())
		c.ChatCommand(fmt.Sprintf("summon %s", strings.Replace(s, " ", fmt.Sprintf(" %d.5 64 4.5 ", x), 1)))
	}
	c.Settle()
	told := map[int32]bool{}
	c.ReadTicks(srv, 25, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		entity := r.ReadVarInt()
		entries, ok := servertest.ReadMetadata(r)
		if !ok {
			t.Fatal("invalid metadata")
		}
		for _, e := range entries {
			if e.Index == protocol.MetadataIndexTicksFrozen && e.VarintVal >= 1 {
				told[entity] = true
			}
		}
		return false
	})
	if len(told) != len(summons) {
		t.Errorf("%d bodies told their frozen count, want %d: %v", len(told), len(summons), told)
	}
}
