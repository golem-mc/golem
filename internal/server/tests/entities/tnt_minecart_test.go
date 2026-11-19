package entities

import (
	"math"
	"math/rand"
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

// TestAnUnlitActivatorLeavesATNTCartBe: an activator without power does not
// prime.
// vanilla: MinecartTNT.activateMinecart
func TestAnUnlitActivatorLeavesATNTCartBe(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	id, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "", 1)
	litActivator(t, srv, 4, "false")
	srv.TickMinecarts(3)
	if fuse, _ := srv.MinecartFuse(id); fuse != -1 {
		t.Errorf("fuse = %d, want -1", fuse)
	}
}

// TestATNTCartDoesNotPrimeWhereTNTDoesNotExplode: tnt_explodes off keeps the
// activator from lighting the fuse.
// vanilla: MinecartTNT.primeFuse
func TestATNTCartDoesNotPrimeWhereTNTDoesNotExplode(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	srv.RunLine("gamerule tnt_explodes false")
	id, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "", 1)
	litActivator(t, srv, 4, "true")
	srv.TickMinecarts(3)
	if fuse, _ := srv.MinecartFuse(id); fuse != -1 {
		t.Errorf("fuse = %d, want -1 with tnt_explodes off", fuse)
	}
}

// TestATNTCartThatFallsThreeBlocksExplodesWithTheFallsSpeed: a landing from
// at least three blocks sets it off, the power growing with a speed of the
// fall over ten; a shorter fall does not.
// vanilla: MinecartTNT.causeFallDamage, MinecartTNT.explode
func TestATNTCartThatFallsThreeBlocksExplodesWithTheFallsSpeed(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	_, radii := tntCartAt(t, srv, 12.5, 70, 8.5, "", 11)
	srv.TickMinecarts(60)
	want := 4 + rand.New(rand.NewSource(11)).Float64()*1.5*0.6
	if len(radii.list()) != 1 || math.Abs((radii.list())[0]-float64(float32(want))) > 1e-6 {
		t.Fatalf("blast radii = %v, want one of %v for a six block fall", radii.list(), want)
	}
	if cartCount(srv) != 0 {
		t.Error("the cart survived its fall")
	}

	short, radii2 := tntCartAt(t, srv, 40.5, 66, 8.5, "", 11)
	srv.TickMinecarts(60)
	if len(radii2.list()) != 0 || cartCount(srv) != 1 {
		t.Errorf("a two block fall: radii %v, carts %d, want none and the cart standing", radii2.list(), cartCount(srv))
	}
	if fuse, _ := srv.MinecartFuse(short); fuse != -1 {
		t.Errorf("fuse = %d, want -1", fuse)
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

func TestAPlainArrowBreaksATNTCartIntoItsItem(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	_, radii := tntCartAt(t, srv, 4.5, 64.0625, 5.5, "", 5)
	srv.ShootArrow(4.5, 64.4, 2.5, 0, 0, 3)
	servertest.WaitForTicks(t, srv, 20, func() bool {
		return cartCount(srv) == 0 && servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), tntCart)
	}, "the arrow never broke the cart into its item")
	if len(radii.list()) != 0 {
		t.Errorf("blast radii = %v, want none", radii.list())
	}
}

// TestFireDamageLightsATNTCartsFuseWithTwoRolls: a fire source lights the
// fuse at two rolls of twenty and leaves the cart standing.
// vanilla: MinecartTNT.destroy
func TestFireDamageLightsATNTCartsFuseWithTwoRolls(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	id, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "", 3)
	r := rand.New(rand.NewSource(3))
	want := int32(r.Intn(20) + r.Intn(20))
	if !srv.HurtMinecart(id, 1, true) {
		t.Fatal("the blow did not land")
	}
	if fuse, ok := srv.MinecartFuse(id); !ok || fuse != want {
		t.Errorf("fuse = %d (standing %v), want %d", fuse, ok, want)
	}
}

// TestLavaLightsATNTCartsFuse: a cart in lava is hurt as a fire source is.
// vanilla: Entity.lavaHurt, MinecartTNT.destroy
func TestLavaLightsATNTCartsFuse(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	id, _ := tntCartAt(t, srv, 12.5, 64, 8.5, "", 9)
	if !srv.SetBlock(12, 64, 8, servertest.MustState(t, "minecraft:lava", map[string]string{"level": "0"})) {
		t.Fatal("no lava")
	}
	srv.TickMinecarts(1)
	if fuse, _ := srv.MinecartFuse(id); fuse < 0 {
		t.Errorf("fuse = %d, want a lit fuse", fuse)
	}
}

// TestABlowBreaksAStillTNTCartButLightsAMovingOne: damage past forty drops
// the item from a cart at rest and lights the fuse of one moving.
// vanilla: MinecartTNT.destroy
func TestABlowBreaksAStillTNTCartButLightsAMovingOne(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	still, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "", 1)
	srv.HurtMinecart(still, 5, false)
	srv.TickMinecarts(1)
	if cartCount(srv) != 0 || !servertest.BlastLeftItem(srv.ItemsInDimension(world.DimensionName), tntCart) {
		t.Fatal("a blow past forty did not break the still cart into its item")
	}

	moving, _ := tntCartAt(t, srv, 5.5, 64.0625, 1.5, "", 1)
	srv.SetMinecartMotion(moving, 0.3, 0, 0)
	srv.HurtMinecart(moving, 5, false)
	if fuse, ok := srv.MinecartFuse(moving); !ok || fuse < 0 {
		t.Errorf("the moving cart's fuse = %d (standing %v), want a lit fuse", fuse, ok)
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

// TestASummonedTNTCartWithAFuseExplodesWhenItRunsOut: a cart summoned lit
// goes off after the fuse it was given.
// vanilla: MinecartTNT.tick
func TestASummonedTNTCartWithAFuseExplodesWhenItRunsOut(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	_, radii := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "{fuse:5,explosion_power:7.0f}", 1)
	srv.TickMinecarts(5)
	if len(radii.list()) != 0 || cartCount(srv) != 1 {
		t.Fatalf("after five ticks: radii %v, carts %d, want the cart still standing", radii.list(), cartCount(srv))
	}
	srv.TickMinecarts(1)
	if len(radii.list()) != 1 || (radii.list())[0] != 7 || cartCount(srv) != 0 {
		t.Errorf("after the sixth tick: radii %v, carts %d, want one blast of 7", radii.list(), cartCount(srv))
	}
}

// TestAPrimedTNTCartsBlastSparesRailsAndTheBlocksUnderThem: a lit cart's
// blast leaves the rails and the cell under each standing, though the blast
// breaks the ground beside them.
// vanilla: MinecartTNT.getBlockExplosionResistance, shouldBlockExplode
func TestAPrimedTNTCartsBlastSparesRailsAndTheBlocksUnderThem(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	cart, _ := tntCartAt(t, srv, 4.5, 64.0625, 1.5, "{fuse:1,explosion_power:7.0f}", 1)
	rail := srv.BlockAt(4, 64, 1)
	under := srv.BlockAt(4, 63, 1)
	beside := srv.BlockAt(4, 63, 3)
	if under == 0 || beside == 0 {
		t.Fatalf("the test ground is air: under %d, beside %d", under, beside)
	}
	srv.TickMinecarts(2)
	if fuse, ok := srv.MinecartFuse(cart); ok {
		t.Fatalf("the cart is still there with fuse %d", fuse)
	}
	for x := int32(3); x <= 6; x++ {
		if srv.BlockAt(x, 64, 1) != rail {
			t.Errorf("the rail at x=%d was destroyed", x)
		}
		if srv.BlockAt(x, 63, 1) == 0 {
			t.Errorf("the block under the rail at x=%d was destroyed", x)
		}
	}
	if srv.BlockAt(4, 63, 3) != 0 {
		t.Errorf("the ground beside the track survived the blast")
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

func TestALitTNTCartsBlastBreaksAPlainCartBesideIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()
	_, radii := tntCartAt(t, srv, 3.5, 64.0625, 1.5, "{fuse:1}", 1)
	srv.RunLine("summon minecraft:minecart 5.5 64.0625 1.5")
	if cartCount(srv) != 2 {
		t.Fatalf("carts %d, want 2", cartCount(srv))
	}
	srv.TickMinecarts(5)
	if n := len(radii.list()); n != 1 || cartCount(srv) != 0 {
		t.Errorf("blasts %v, carts %d, want one blast and no cart", radii.list(), cartCount(srv))
	}
}
