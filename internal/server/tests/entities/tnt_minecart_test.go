package entities

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const tntCart = "minecraft:tnt_minecart"

// tntCartAt summons a TNT minecart on the test rails with the tick loop held
// and its dice seeded, and records the radius of every blast it runs.
func tntCartAt(t *testing.T, srv *server.Server, x, y, z float64, tag string, seed int64) (int32, *blastLog) {
	t.Helper()
	for x := int32(3); x <= 6; x++ {
		srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "east_west"}))
	}
	radii := &blastLog{}
	srv.ObserveCartBlasts(radii.add)
	// The dispatcher refuses input left behind the last argument, so
	// trim the space an empty tag leaves.
	srv.RunLine(strings.TrimSpace("summon " + tntCart + " " + ftoa(x) + " " + ftoa(y) + " " + ftoa(z) + " " + tag))
	var newest int32
	for _, cart := range srv.MinecartsInDimension(world.DimensionName) {
		newest = max(newest, cart.ID)
	}
	if newest == 0 {
		t.Fatal("the summon stood no cart")
	}
	srv.SeedMinecart(newest, seed)
	return newest, radii
}

// blastLog collects the radii of the blasts a test's carts run.
type blastLog struct {
	mu sync.Mutex
	r  []float64
}

func (b *blastLog) add(r float64) {
	b.mu.Lock()
	b.r = append(b.r, r)
	b.mu.Unlock()
}

func (b *blastLog) list() []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]float64(nil), b.r...)
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func cartCount(srv *server.Server) int { return len(srv.MinecartsInDimension(world.DimensionName)) }

func litActivator(t *testing.T, srv *server.Server, x int32, powered string) {
	t.Helper()
	if !srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:activator_rail",
		map[string]string{"shape": "east_west", "powered": powered})) {
		t.Fatal("the activator did not stand")
	}
}

// TestATNTMinecartItemStandsATNTCartOnARail: the item used on a rail stands
// a cart of the TNT cart entity type.
// vanilla: MinecartItem.useOn
func TestATNTMinecartItemStandsATNTCartOnARail(t *testing.T) {
	t.Parallel()
	srv, c := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Testificate", player.HotbarSlot, itemOf(t, tntCart, 1))
	servertest.CartRails(t, srv, 2, 5)
	c.MoveTo(2.5, 64, 2.9, 0, 60)
	c.Settle()

	c.PlaceBlock(2, 64, 1, protocol.FaceTop)
	typeID, _ := registry.EntityTypeID(tntCart)
	spawned := servertest.AwaitBoatSpawn(c.Client, typeID)
	if got := srv.MinecartKind(spawned.EntityID); got != tntCart {
		t.Fatalf("the laid cart is a %q, want %s", got, tntCart)
	}
}

// TestALitActivatorPrimesATNTCartForEightyTicks: the powered activator lights
// the fuse, which runs eighty ticks and explodes on the next, the blast of a
// cart at rest being the base power 4.
// vanilla: MinecartTNT.activateMinecart, MinecartTNT.tick
func TestALitActivatorPrimesATNTCartForEightyTicks(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	srv.StopTick()
	id, radii := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "", 1)
	litActivator(t, srv, 4, "true")

	if fuse, _ := srv.MinecartFuse(id); fuse != -1 {
		t.Fatalf("an unlit cart's fuse is %d, want -1", fuse)
	}
	srv.TickMinecarts(1)
	if fuse, _ := srv.MinecartFuse(id); fuse != 79 {
		t.Fatalf("after the priming tick the fuse is %d, want 79 (80 set, one spent)", fuse)
	}
	srv.TickMinecarts(79)
	if fuse, ok := srv.MinecartFuse(id); !ok || fuse != 0 {
		t.Fatalf("after 80 ticks the cart is (fuse %d, standing %v), want fuse 0 still standing", fuse, ok)
	}
	if len(radii.list()) != 0 {
		t.Fatal("the cart blew up before its fuse ran out")
	}
	srv.TickMinecarts(1)
	if cartCount(srv) != 0 {
		t.Error("the cart still stands after its fuse ran out")
	}
	if len(radii.list()) != 1 || math.Abs((radii.list())[0]-4) > 1e-9 {
		t.Errorf("blast radii = %v, want one of 4 for a cart at rest", radii.list())
	}
	if servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), tntCart) {
		t.Error("an exploded cart left its item")
	}
}

// TestATNTCartHittingAWallFastExplodes: a hard horizontal hit explodes it, a
// soft one does not.
// vanilla: MinecartTNT.tick
func TestATNTCartHittingAWallFastExplodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		vz    float64
		blast bool
	}{{"hard", 0.4, true}, {"soft", 0.05, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			srv.StopTick()
			id, radii := tntCartAt(t, srv, 12.5, 64, 8.5, "", 3)
			stone := servertest.MustState(t, "minecraft:stone", nil)
			for z := int32(6); z <= 12; z++ {
				srv.SetBlock(13, 64, z, stone)
			}
			srv.SetMinecartMotion(id, 0.4, 0, tc.vz)
			srv.TickMinecarts(2)
			if got := len(radii.list()) == 1; got != tc.blast {
				t.Fatalf("blast radii = %v, want a blast: %v", radii.list(), tc.blast)
			}
			if tc.blast && ((radii.list())[0] <= 4 || (radii.list())[0] > 4+1.5*0.4) {
				t.Errorf("radius = %v, want between 4 and the speed's share above it", (radii.list())[0])
			}
		})
	}
}

// TestAFireArrowSetsATNTCartOffAtOnce: a burning arrow explodes the cart where
// it stands; a plain one breaks it into its item.
// vanilla: MinecartTNT.hurtServer
func TestAFireArrowSetsATNTCartOffAtOnce(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	_, radii := tntCartAt(t, srv, 4.5, 64.0625, 5.5, "", 5)
	arrow := srv.ShootArrow(4.5, 64.4, 2.5, 0, 0, 3)
	srv.IgniteArrow(arrow, 100)
	srv.TickMinecarts(1)
	servertest.WaitForTicks(t, srv, 20, func() bool { return cartCount(srv) == 0 && len(radii.list()) == 1 },
		"the burning arrow never set the cart off")
	if r := (radii.list())[0]; r < 4 || r > 4+1.5*5 {
		t.Errorf("radius = %v, want within 4..11.5", r)
	}
	if servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), tntCart) {
		t.Error("an exploded cart left its item")
	}
}

// TestATNTCartSavesItsFuseAndNonDefaultExplosion: the fuse is always saved,
// the explosion settings only when they differ from the defaults, and a load
// clamps them to 0..128.
// vanilla: MinecartTNT.addAdditionalSaveData, readAdditionalSaveData
func TestATNTCartSavesItsFuseAndNonDefaultExplosion(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	id, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "{fuse:30,explosion_power:500.0f,explosion_speed_factor:2.5f}", 1)
	tag, ok := srv.MinecartSaveTag(id)
	if !ok {
		t.Fatal("no cart")
	}
	if v, _ := nbt.Get[nbt.Int](tag, "fuse"); v != 30 {
		t.Errorf("fuse = %d, want 30", v)
	}
	if v, _ := nbt.Get[nbt.Float](tag, "explosion_power"); v != 128 {
		t.Errorf("explosion_power = %v, want the 128 cap", v)
	}
	if v, _ := nbt.Get[nbt.Float](tag, "explosion_speed_factor"); v != 2.5 {
		t.Errorf("explosion_speed_factor = %v, want 2.5", v)
	}

	plain, _ := tntCartAt(t, srv, 5.5, 64.0625, 1.5, "", 1)
	tag, _ = srv.MinecartSaveTag(plain)
	if v, _ := nbt.Get[nbt.Int](tag, "fuse"); v != -1 {
		t.Errorf("unlit fuse = %d, want -1", v)
	}
	if _, has := tag["explosion_power"]; has {
		t.Error("the default power was saved")
	}
	if _, has := tag["explosion_speed_factor"]; has {
		t.Error("the default speed factor was saved")
	}
}

func TestALitTNTCartsBlastPrimesTheTNTCartBesideIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	tntCartAt(t, srv, 3.5, 64.0625, 1.5, "{fuse:1}", 1)
	// The second call's observer replaces the first's and sees both blasts.
	_, radii := tntCartAt(t, srv, 5.5, 64.0625, 1.5, "", 2)
	srv.TickMinecarts(45)
	if n := len(radii.list()); n != 2 || cartCount(srv) != 0 {
		t.Errorf("blasts %v, carts %d, want two blasts and no cart", radii.list(), cartCount(srv))
	}
}
