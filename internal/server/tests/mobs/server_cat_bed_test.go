package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// Dye IDs of the collar metadata.
// vanilla: DyeColor.getId
const (
	dyeBlue = 11
	dyeRed  = 14
)

// stateOf is the state of the block with the properties given.
func stateOf(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state of %s with %v", name, props)
	}
	return id
}

// catState reads the cat's server-side bed and seat state.
func catState(t *testing.T, srv *server.Server, id int32) server.CatBeds {
	t.Helper()
	st, ok := srv.CatState(id)
	if !ok {
		t.Fatalf("cat %d is not in the herd", id)
	}
	return st
}

// TestAnOwnersDyeRecoloursACatsCollar: the owner's dye sets the collar and is
// spent; the colour it already wears is no change and sits the cat; a
// stranger's dye does nothing.
// vanilla: Cat.mobInteract
func TestAnOwnersDyeRecoloursACatsCollar(t *testing.T) {
	t.Parallel()
	const name = "Dyer"
	srv, c := catYard(t, name, "minecraft:blue_dye")
	mine := spawnCat(t, srv, c, 1.5, 2.5)
	other := spawnCat(t, srv, c, -1.5, 2.5)
	srv.SetMobOwner(mine.EntityID, protocol.OfflineUUID(name))
	srv.SetMobOwner(other.EntityID, protocol.OfflineUUID("Stranger"))
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:blue_dye"), Count: 5})

	c.UseOn(other.EntityID)
	c.UseOn(mine.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 2, &mine, &other)
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 4 {
		t.Errorf("%d dyes left after one click on each cat, want 4", got)
	}
	if !mine.HasCatCollar || mine.CatCollar != dyeBlue {
		t.Errorf("the owner's cat tells collar %d (told %v), want blue", mine.CatCollar, mine.HasCatCollar)
	}
	if other.HasCatCollar || catState(t, srv, other.EntityID).Collar != "red" {
		t.Errorf("a stranger's dye recoloured the collar")
	}

	c.UseOn(mine.EntityID)
	c.Settle()
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 4 {
		t.Errorf("the colour already worn spent a dye: %d left, want 4", got)
	}
	if st, _ := srv.MobTamable(mine.EntityID); !st.OrderedToSit {
		t.Error("a dye the collar already wears did not sit the cat")
	}
}

// TestATameCatLiesOnABedButNotWhenOrderedToSit: a tame cat walks to a bed and
// lies on it, told by the bed-pose metadata; one ordered to sit stays put.
// The goal's search starts three layers below the cat's feet, so the cats
// stand on a stone platform over the bed.
// vanilla: CatLieOnBedGoal
func TestATameCatLiesOnABedButNotWhenOrderedToSit(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	srv, c := catYard(t, name, "")
	stone := stateOf(t, "minecraft:stone", nil)
	for _, at := range [][2]int32{{1, 5}, {1, 6}, {2, 5}, {2, 6}} {
		for y := int32(64); y <= 66; y++ {
			if at[0] == 2 && y == 66 {
				continue
			}
			srv.SetBlock(at[0], y, at[1], stone)
		}
	}
	srv.SetBlock(5, 64, 6, servertest.BedStateName("red", "head", "south"))
	srv.SetBlock(5, 64, 5, servertest.BedStateName("red", "foot", "south"))
	srv.SpawnMob("minecraft:cat", 1.5, 67, 5.5)
	lier := c.ExpectMobSpawn(protocol.EntityTypeCat)
	srv.SpawnMob("minecraft:cat", 1.5, 67, 6.5)
	sitter := c.ExpectMobSpawn(protocol.EntityTypeCat)
	for _, m := range []servertest.SpawnedMob{lier, sitter} {
		srv.SetMobOwner(m.EntityID, protocol.OfflineUUID(name))
	}
	c.UseOn(sitter.EntityID)
	c.Settle()
	advanceCat(t, srv, c, 300, &lier, &sitter)
	if st := catState(t, srv, lier.EntityID); !st.Lying || st.SittingPose {
		t.Errorf("the tame cat lies=%v sitting=%v after 300 ticks above a bed, want lying", st.Lying, st.SittingPose)
	}
	if !lier.CatLying {
		t.Error("the lying cat's metadata does not say so")
	}
	if st := catState(t, srv, sitter.EntityID); st.Lying {
		t.Error("a cat ordered to sit lay down on the bed")
	}
}

// huntYard joins a player and spawns the hunter and its prey, a wild
// hunter outside the sixteen blocks it flees a player from.
func huntYard(t *testing.T, hunter, prey string, tame bool) (*server.Server, *servertest.Client, servertest.SpawnedMob, servertest.SpawnedMob) {
	t.Helper()
	const name = "Watcher"
	srv, c := catYard(t, name, "")
	types := map[string]int32{
		"minecraft:cat": protocol.EntityTypeCat, "minecraft:ocelot": protocol.EntityTypeOcelot,
		"minecraft:rabbit": protocol.EntityTypeRabbit, "minecraft:chicken": protocol.EntityTypeChicken,
	}
	// A tame cat stays near its owner, so its prey is brought there.
	x := 40.5
	if tame {
		x = 4.5
	}
	srv.SpawnMob(hunter, x, 64, 0.5)
	h := c.ExpectMobSpawn(types[hunter])
	srv.SpawnMob(prey, x+3, 64, 0.5)
	p := c.ExpectMobSpawn(types[prey])
	if tame {
		srv.SetMobOwner(h.EntityID, protocol.OfflineUUID(name))
	}
	return srv, c, h, p
}

// TestWildFelinesHuntTheirPrey: a wild cat kills a rabbit and an ocelot a
// chicken; a tame cat leaves the rabbit be.
// vanilla: Cat.registerGoals, Ocelot.registerGoals, OcelotAttackGoal
func TestWildFelinesHuntTheirPrey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		hunter, prey string
		tame, kills  bool
	}{
		{"minecraft:cat", "minecraft:rabbit", false, true},
		{"minecraft:ocelot", "minecraft:chicken", false, true},
		{"minecraft:cat", "minecraft:rabbit", true, false},
	} {
		t.Run(tc.hunter+" "+tc.prey, func(t *testing.T) {
			t.Parallel()
			srv, c, _, prey := huntYard(t, tc.hunter, tc.prey, tc.tame)
			advanceCat(t, srv, c, 400)
			health, alive := srv.MobHealth(prey.EntityID)
			if killed := !alive || health <= 0; killed != tc.kills {
				t.Errorf("%s hunting a %s (tame %v): prey killed = %v (health %v), want %v",
					tc.hunter, tc.prey, tc.tame, killed, health, tc.kills)
			}
		})
	}
}
