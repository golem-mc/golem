package mobs

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
