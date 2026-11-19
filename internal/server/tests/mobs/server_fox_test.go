package mobs

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The fox, black box: its variant by biome, its sleep under a roof by day,
// the berries it picks and the item it carries in its mouth.

const (
	foxType = "minecraft:fox"
	// foxX is far enough from the player's spot that the fox never counts
	// the player as someone near enough to wake for.
	foxX = 40.5
	// foxBlock is the block column foxX stands in.
	foxBlock = 40
)

// foxScene joins a player at the origin with the loop held, in the daytime
// or the night, and hands back the server.
func foxScene(t *testing.T, day bool) *server.Server {
	t.Helper()
	srv, _ := foxSceneClient(t, day)
	return srv
}

// foxSceneClient is foxScene with the player's connection kept, to hear what
// the server sends.
func foxSceneClient(t *testing.T, day bool) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	if day {
		srv.SetDayTime(6000)
	} else {
		srv.SetDayTime(18000)
	}
	c := connect().Join("Fox")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	return srv, c
}

// foxState reads the fox, failing if it is gone.
func foxState(t *testing.T, srv *server.Server, id int32) server.TestFox {
	t.Helper()
	st, ok := srv.MobFox(id)
	if !ok {
		t.Fatal("the fox is not in the herd")
	}
	return st
}

// TestAFoxTakesItsVariantFromTheBiome: a fox is born snow in the biomes of
// spawns_snow_foxes and red anywhere else.
// vanilla: Fox.Variant.byBiome
func TestAFoxTakesItsVariantFromTheBiome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		biome string
		want  int32
	}{
		{"minecraft:snowy_taiga", 1},
		{"minecraft:taiga", 0},
		{"minecraft:plains", 0},
	} {
		t.Run(tc.biome, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, true)
			srv.SetOverworldBiome(foxBlock, 0, tc.biome)
			// The zoomed read at z=0.5 can land in the chunk to the north.
			srv.SetOverworldBiome(foxBlock, -1, tc.biome)
			id := srv.SpawnMob(foxType, foxX, 64, 0.5)
			if got := foxState(t, srv, id).Variant; got != tc.want {
				t.Errorf("variant in %s = %d, want %d", tc.biome, got, tc.want)
			}
		})
	}
}

// stoneRoom walls a three by three room of glass around the column (x, z),
// two high, with a stone roof over it, so a fox standing in it can neither
// wander out nor see the sky, while the glass lets the daylight in.
func stoneRoom(t *testing.T, srv *server.Server, x, z int32) {
	t.Helper()
	stone := servertest.BlockState(t, "minecraft:stone")
	glass := servertest.BlockState(t, "minecraft:glass")
	glow := servertest.BlockState(t, "minecraft:glowstone")
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			srv.SetBlock(x+dx, 66, z+dz, stone)
			if dx == -2 || dx == 2 || dz == -2 || dz == 2 {
				srv.SetBlock(x+dx, 64, z+dz, glass)
				srv.SetBlock(x+dx, 65, z+dz, glass)
			}
		}
	}
	// The lamp keeps the room bright enough for a fox at night too, so the
	// hour alone decides.
	srv.SetBlock(x, 66, z, glow)
	srv.RelightAt(x, z)
}

// TestAFoxSleepsByDayUnderARoof: in daylight and under a roof a fox lies
// down within a few seconds; at night, or under open sky, it does not.
// vanilla: Fox.SleepGoal
func TestAFoxSleepsByDayUnderARoof(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		day   bool
		roof  bool
		sleep bool
	}{
		{"day under a roof", true, true, true},
		{"night under a lit roof", false, true, false},
		{"day under the sky", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, tc.day)
			if tc.roof {
				stoneRoom(t, srv, foxBlock, 0)
			}
			id := srv.SpawnMob(foxType, foxX, 64, 0.5)
			servertest.RunTicks(t, srv, 400)
			if got := foxState(t, srv, id).Sleeping; got != tc.sleep {
				t.Errorf("sleeping = %v, want %v", got, tc.sleep)
			}
		})
	}
}

// TestAFoxWakesWhenSomethingComesNear: a sleeping fox gets up when a player
// walks within the alert range of it.
// vanilla: Fox.SleepGoal.canContinueToUse, FoxBehaviorGoal.alertable
func TestAFoxWakesWhenSomethingComesNear(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, true)
	stoneRoom(t, srv, foxBlock, 0)
	id := srv.SpawnMob(foxType, foxX, 64, 0.5)
	servertest.RunTicks(t, srv, 400)
	if !foxState(t, srv, id).Sleeping {
		t.Fatal("the fox did not fall asleep")
	}
	pig := srv.SpawnMob("minecraft:pig", foxX+5, 64, 0.5)
	_ = pig
	servertest.RunTicks(t, srv, 20)
	if foxState(t, srv, id).Sleeping {
		t.Error("the fox slept on with a pig beside it")
	}
}

// TestAFoxSeeksShelterByDay: a fox in the open by day walks under a nearby
// roof and lies down there.
// vanilla: Fox.SeekShelterGoal
func TestAFoxSeeksShelterByDay(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, true)
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(foxBlock + 1); x <= foxBlock+10; x++ {
		for z := int32(-10); z <= 10; z++ {
			srv.SetBlock(x, 67, z, stone)
		}
	}
	srv.RelightAt(foxBlock+1, 0)
	srv.RelightAt(foxBlock+9, 0)
	id := srv.SpawnMob(foxType, foxX, 64, 0.5)
	servertest.UntilTicks(t, srv, 3000, "the fox asleep under the roof", func() bool {
		st := foxState(t, srv, id)
		return st.Sleeping && st.X > foxBlock+1
	})
}

// TestAFoxPicksARipeBerryBush: a fox with an empty mouth walks to a ripe
// sweet berry bush, takes a berry in its mouth, and the bush goes back to
// age 1 with the rest of the berries dropped.
// vanilla: Fox.FoxEatBerriesGoal
func TestAFoxPicksARipeBerryBush(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	bush := servertest.MustState(t, "minecraft:sweet_berry_bush", map[string]string{"age": "3"})
	srv.SetBlock(foxBlock+5, 64, 0, bush)
	id := srv.SpawnMob(foxType, foxX, 64, 0.5)
	srv.SetMobHand(id, protocol.ItemStack{})
	servertest.UntilTicks(t, srv, 1200, "the fox pocketing a berry", func() bool {
		return !srv.MobHand(id).Empty()
	})
	if got, want := srv.MobHand(id), servertest.MustItem("minecraft:sweet_berries"); got.ItemID != want || got.Count != 1 {
		t.Errorf("mouth = %+v, want one sweet berry", got)
	}
	young := servertest.MustState(t, "minecraft:sweet_berry_bush", map[string]string{"age": "1"})
	if got := srv.BlockAt(foxBlock+5, 64, 0); got != young {
		t.Errorf("bush state = %d, want age 1 (%d)", got, young)
	}
}

// TestAFoxCarriesOneItemOfAStack: a fox with an empty mouth walks to an item
// on the ground and takes one of it, leaving the rest where it lay; a full
// mouth takes nothing.
// vanilla: Fox.FoxSearchForItemsGoal, Fox.pickUpItem
func TestAFoxCarriesOneItemOfAStack(t *testing.T) {
	t.Parallel()
	for _, held := range []bool{false, true} {
		name := "empty mouth"
		if held {
			name = "full mouth"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, false)
			id := srv.SpawnMob(foxType, foxX, 64, 0.5)
			mouth := protocol.ItemStack{}
			if held {
				mouth = protocol.ItemStack{ItemID: servertest.MustItem("minecraft:feather"), Count: 1}
			}
			srv.SetMobHand(id, mouth)
			srv.SpawnItemEntityDelay(foxX+4, 64, 0.5, "minecraft:stick", 3, 0)
			servertest.RunTicks(t, srv, 600)
			got := srv.MobHand(id)
			if held {
				if got.ItemID != mouth.ItemID {
					t.Errorf("a full mouth swapped its item: %+v", got)
				}
				return
			}
			if got.ItemID != servertest.MustItem("minecraft:stick") || got.Count != 1 {
				t.Fatalf("mouth = %+v, want one stick", got)
			}
			var left int32
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.Item == "minecraft:stick" {
					left += it.Count
				}
			}
			if left != 2 {
				t.Errorf("%d sticks left on the ground, want 2", left)
			}
		})
	}
}

// TestAFoxShowsItsVariantAndSleep checks Fox.DATA_TYPE_ID and the sleeping
// bit of Fox.DATA_FLAGS_ID on the wire: a summoned fox carries the Type and
// Sleeping of its tag, and the red, awake default is left unsaid.
func TestAFoxShowsItsVariantAndSleep(t *testing.T) {
	t.Parallel()
	const name = "Warden"
	for _, tc := range []struct {
		tag      string
		variant  uint8
		hasType  bool
		flags    uint8
		hasFlags bool
	}{
		{`{Type:"snow",Sleeping:1b}`, 1, true, protocol.FoxSleeping, true},
		{`{Type:"snow"}`, 1, true, 0, false},
		{`{Type:"red",Sleeping:1b}`, 0, false, protocol.FoxSleeping, true},
		{`{Type:"purple"}`, 0, false, 0, false},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			_, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
			c.Settle()
			c.ChatCommand("summon minecraft:fox 5.5 64 5.5 " + tc.tag)
			fox := c.ExpectMobSpawn(protocol.EntityTypeFox)
			if fox.HasFoxType != tc.hasType || fox.FoxType != tc.variant {
				t.Errorf("%s: type %d (sent %v), want %d (sent %v)", tc.tag, fox.FoxType, fox.HasFoxType, tc.variant, tc.hasType)
			}
			if fox.HasFoxFlags != tc.hasFlags || fox.FoxFlags != tc.flags {
				t.Errorf("%s: flags %#x (sent %v), want %#x (sent %v)", tc.tag, fox.FoxFlags, fox.HasFoxFlags, tc.flags, tc.hasFlags)
			}
		})
	}
}

// TestAFoxLeavesABushUnderRipeAge: a bush of age 1 is not worth the walk, one
// of age 2 is picked.
// vanilla: Fox.FoxEatBerriesGoal.isValidTarget
func TestAFoxLeavesABushUnderRipeAge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		age    string
		picked bool
	}{{"1", false}, {"2", true}} {
		t.Run("age "+tc.age, func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, false)
			srv.SetBlock(foxBlock+5, 64, 0, servertest.MustState(t, "minecraft:sweet_berry_bush", map[string]string{"age": tc.age}))
			id := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 11)
			srv.SetMobHand(id, protocol.ItemStack{})
			if tc.picked {
				servertest.UntilTicks(t, srv, 1200, "the fox pocketing a berry", func() bool {
					return !srv.MobHand(id).Empty()
				})
				return
			}
			// An unripe bush is never walked to: the fox stays out of
			// its reach while it strolls.
			for range 300 {
				servertest.RunTicks(t, srv, 1)
				if st := foxState(t, srv, id); math.Hypot(st.X-(foxBlock+5.5), st.Z-0.5) < 2 {
					t.Fatalf("the fox walked up to an unripe bush at (%.1f, %.1f)", st.X, st.Z)
				}
			}
		})
	}
}

// TestAFoxPicksBerriesInItsOwnVoice: the pick sound is played in the fox's
// own category, not the block one.
// vanilla: Fox.FoxEatBerriesGoal.pickSweetBerries
func TestAFoxPicksBerriesInItsOwnVoice(t *testing.T) {
	t.Parallel()
	srv, c := foxSceneClient(t, false)
	srv.SetBlock(10, 64, 0, servertest.MustState(t, "minecraft:sweet_berry_bush", map[string]string{"age": "3"}))
	id := srv.SpawnMob(foxType, 6.5, 64, 0.5)
	srv.TrustFox(id, "Fox")
	srv.SetMobHand(id, protocol.ItemStack{})
	servertest.UntilTicks(t, srv, 1200, "the fox pocketing a berry", func() bool {
		return !srv.MobHand(id).Empty()
	})
	pick, ok := registry.SoundEventID("minecraft:block.sweet_berry_bush.pick_berries")
	if !ok {
		t.Fatal("no pick sound in the registry")
	}
	var heard bool
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt()-1 != pick {
			return
		}
		heard = true
		if cat := r.ReadVarInt(); cat != protocol.SoundCategoryNeutral {
			t.Errorf("pick sound category = %d, want neutral", cat)
		}
	})
	if !heard {
		t.Error("the pick sound never reached the player")
	}
}

// TestAFoxSleepsOnThroughATamedMob: a tame cat within a sleeping fox's alert
// range, near enough to its owner not to be called over, does
// not wake it.
// vanilla: Fox.FoxAlertableEntitiesSelector
func TestAFoxSleepsOnThroughATamedMob(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, true)
	stoneRoom(t, srv, 20, 0)
	id := srv.SpawnMob(foxType, 20.5, 64, 0.5)
	servertest.RunTicks(t, srv, 400)
	if !foxState(t, srv, id).Sleeping {
		t.Fatal("the fox did not fall asleep")
	}
	cat := srv.SpawnMob("minecraft:cat", 9, 64, 0.5)
	if !srv.TameMobFor(cat, "Fox") {
		t.Fatal("could not tame the cat")
	}
	servertest.RunTicks(t, srv, 40)
	if !foxState(t, srv, id).Sleeping {
		t.Error("a tame cat woke the fox")
	}
}

// foxVillage places a workstation and optionally reserves its ticket with a
// stationary villager, far enough away not to alert the fox.
func foxVillage(t *testing.T, srv *server.Server, x int32, claimed bool) {
	t.Helper()
	srv.SetBlock(x, 64, 0, servertest.BlockState(t, "minecraft:lectern"))
	if claimed {
		id := srv.SpawnVillagerAtJobSite("minecraft:librarian", nil, float64(x)+0.5, 64, 4.5, x, 64, 0)
		srv.SetMobNoAI(id)
	}
}

func TestAFoxWalksTowardAnOccupiedVillageAtNight(t *testing.T) {
	for _, tc := range []struct {
		name         string
		day, claimed bool
	}{
		{"night claimed", false, true},
		{"night unclaimed", false, false},
		{"day claimed", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := foxScene(t, tc.day)
			foxVillage(t, srv, 0, tc.claimed)
			id := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 8)
			if !tc.day && tc.claimed {
				servertest.UntilTicks(t, srv, 3000, "the fox approaching the occupied village", func() bool { return foxState(t, srv, id).X < foxX-16 })
			} else {
				servertest.RunTicks(t, srv, 3000)
			}
			st := foxState(t, srv, id)
			if want := !tc.day && tc.claimed; (st.X < foxX-16) != want {
				t.Errorf("fox at x=%.2f, want movement toward village=%v", st.X, want)
			}
		})
	}
}

func TestAFoxSuppressesDaytimeShelterInAnOccupiedVillage(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		name := "unclaimed workstation"
		if claimed {
			name = "occupied village"
		}
		t.Run(name, func(t *testing.T) {
			srv := foxScene(t, true)
			foxVillage(t, srv, 24, claimed)
			stone := servertest.BlockState(t, "minecraft:stone")
			for x := int32(foxBlock + 1); x <= foxBlock+10; x++ {
				for z := int32(-10); z <= 10; z++ {
					srv.SetBlock(x, 67, z, stone)
				}
			}
			srv.RelightAt(foxBlock+1, 0)
			srv.RelightAt(foxBlock+9, 0)
			id := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 1)
			enteredShelter := false
			for range 200 {
				servertest.RunTicks(t, srv, 1)
				st := foxState(t, srv, id)
				if st.X >= foxBlock+1 && st.X < foxBlock+11 && st.Z >= -10 && st.Z < 11 {
					enteredShelter = true
				}
			}
			if enteredShelter == claimed {
				t.Errorf("entered shelter=%v, want %v", enteredShelter, !claimed)
			}
		})
	}
}
