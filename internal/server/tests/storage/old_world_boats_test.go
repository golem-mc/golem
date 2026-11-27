package storage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAVanilla1_16_5WorldsBoatsLoadAsTheirWood loads chunk (0,0) of a
// superflat world vanilla 1.16.5 wrote after
//
//	summon minecraft:boat 2.5 5 2.5 {Type:"birch"}
//	summon minecraft:boat 6.5 5 2.5 {Type:"oak"}
//
// (and a TNT minecart, a villager and an armor stand): each boat loads as the
// entity type of its wood, with its float FallDistance as a double
// fall_distance, and boat is no longer an entity type.
// vanilla: BoatSplitFix, EntityFallDistanceFloatToDoubleFix
func TestAVanilla1_16_5WorldsBoatsLoadAsTheirWood(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName, _ = oldWorld(t, "vanilla-1.16.5-boats-r.0.0.mca")

	srv, _ := servertest.Start(t, cfg)
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(0, 0)
	defer release()
	run := func(line string) string { return fmt.Sprint(srv.RunLine(line)) }
	// The chunk's saved entities wake on a tick.
	servertest.UntilTicks(t, srv, 5, "the oak boat loads", func() bool {
		return strings.Contains(run("execute if entity @e[type=oak_boat]"), "commands.execute.conditional.pass")
	})
	if got := run("execute if entity @e[type=birch_boat]"); !strings.Contains(got, "commands.execute.conditional.pass") {
		t.Errorf("no birch boat: %s", got)
	}
	if got := run("execute if entity @e[type=boat]"); !strings.Contains(got, "argument.entity.options.type.invalid") {
		t.Errorf("type=boat: %s, want an unknown entity type", got)
	}
	got := srv.RunLine("data get entity @e[type=oak_boat,limit=1] fall_distance")
	if len(got) != 1 || got[0].Translate != "commands.data.entity.query" || len(got[0].With) != 2 || !strings.HasSuffix(got[0].With[1].Plain(), "d") {
		t.Errorf("oak boat fall_distance: %v, want a double", got)
	}
}

// TestAVanilla1_16_5WorldsVillagerFollowRangeIsSixteen loads the same
// world's villager, saved with the follow range 48 the game itself gave it,
// and finds the 16 vanilla 26.3 reads after its follow range undo.
// vanilla: EntityAttributeBaseValueFix
func TestAVanilla1_16_5WorldsVillagerFollowRangeIsSixteen(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName, _ = oldWorld(t, "vanilla-1.16.5-boats-r.0.0.mca")

	srv, _ := servertest.Start(t, cfg)
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(0, 0)
	defer release()
	servertest.UntilTicks(t, srv, 5, "the villager loads", func() bool {
		return strings.Contains(fmt.Sprint(srv.RunLine("execute if entity @e[type=villager]")), "commands.execute.conditional.pass")
	})
	got := srv.RunLine("attribute @e[type=villager,limit=1] minecraft:follow_range base get")
	if len(got) != 1 || got[0].With[len(got[0].With)-1].Plain() != "16.0" {
		t.Errorf("villager follow_range base: %+v, want 16.0", got)
	}
}
