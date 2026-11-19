package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// bulletsIn lists the bullets in flight.
func bulletsIn(srv *server.Server) map[int32]bool {
	out := map[int32]bool{}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == "minecraft:shulker_bullet" {
			out[it.ID] = true
		}
	}
	return out
}

// TestAShulkerBulletHurtsAndLevitatesItsTarget: a shulker beside a player
// opens up, fires, and the bullet that lands takes four points and gives
// Levitation for ten seconds.
// vanilla: ShulkerBullet.onHitEntity
func TestAShulkerBulletHurtsAndLevitatesItsTarget(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	srv.SpawnMob("minecraft:shulker", at.X+3, y, at.Z)
	before := srv.PlayerHealth("Watcher")
	for range 900 {
		srv.TickMobs(1)
		srv.TickEntities(1)
		if _, _, ok := srv.PlayerEffect("Watcher", "minecraft:levitation"); ok {
			break
		}
	}
	dur, amp, ok := srv.PlayerEffect("Watcher", "minecraft:levitation")
	if !ok || amp != 0 || dur < 190 || dur > 200 {
		t.Fatalf("levitation = %d ticks, amplifier %d, present %v; want 200 ticks, amplifier 0", dur, amp, ok)
	}
	if got := before - srv.PlayerHealth("Watcher"); got != 4 {
		t.Errorf("the bullet took %v health, want 4", got)
	}
}

// TestAShulkerFiresOnItsCadence: the first bullet leaves after the 20 ticks
// the goal starts on and the next ones 20, 30, ... or 110 ticks apart.
// vanilla: Shulker.ShulkerAttackGoal.tick
func TestAShulkerFiresOnItsCadence(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	srv.SpawnMob("minecraft:shulker", at.X+12, y, at.Z)
	seen := map[int32]bool{}
	var shots []int
	for tick := 1; tick <= 1500; tick++ {
		srv.TickMobs(1)
		srv.TickEntities(1)
		for id := range bulletsIn(srv) {
			if !seen[id] {
				seen[id] = true
				shots = append(shots, tick)
			}
		}
	}
	if len(shots) < 3 {
		t.Fatalf("only %d bullets in 1500 ticks", len(shots))
	}
	if shots[0] < 18 || shots[0] > 30 {
		t.Errorf("the first bullet left at tick %d, want about 20 ticks after the shell opened", shots[0])
	}
	for i := 1; i < len(shots); i++ {
		if gap := shots[i] - shots[i-1]; gap < 20 || gap > 110 || gap%10 != 0 {
			t.Errorf("bullets %d and %d were %d ticks apart, want one of 20, 30, ..., 110", i-1, i, gap)
		}
	}
}

// TestAShulkerBulletVanishesInPeaceful: the bullet is discarded as soon as
// the world is peaceful.
// vanilla: ShulkerBullet.checkDespawn
func TestAShulkerBulletVanishesInPeaceful(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", at.X+30, y, at.Z)
	srv.LaunchShulkerBullet(at.X+25, y+1, at.Z, id, 1)
	if len(bulletsIn(srv)) != 1 {
		t.Fatal("the bullet was not launched")
	}
	srv.SetDifficulty(0)
	srv.TickEntities(1)
	if n := len(bulletsIn(srv)); n != 0 {
		t.Errorf("%d bullets survived the switch to peaceful", n)
	}
}

// TestAShulkerHitByABulletSlipsAwayAndLeavesACopy: an open shell teleports
// and a new shulker stands where it was.
// vanilla: Shulker.hitByShulkerBullet
func TestAShulkerHitByABulletSlipsAwayAndLeavesACopy(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+3.5, y, math.Floor(at.Z)+0.5)
	srv.TickMobs(30)
	moved := false
	// A teleport finds no cell in five tries about three times in four.
	for range 80 {
		x0, y0, z0, _ := srv.MobPos(id)
		srv.LaunchShulkerBullet(x0+2, y0+0.5, z0, id, 3)
		srv.TickEntities(40)
		srv.TickMobs(12)
		x1, y1, z1, _ := srv.MobPos(id)
		if moved = x1 != x0 || y1 != y0 || z1 != z0; moved {
			break
		}
	}
	if !moved {
		t.Fatal("the open shulker never slipped away from the bullets")
	}
	if n := len(srv.MobIDsOfKind("minecraft:shulker")); n != 2 {
		t.Errorf("%d shulkers after the teleport, want the original and one copy", n)
	}
}

// TestAShulkerWithAShutShellStaysPutWhenHit: bullets that land on a shut
// shell neither move it nor split it.
// vanilla: Shulker.hitByShulkerBullet
func TestAShulkerWithAShutShellStaysPutWhenHit(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	// Past the follow range no shulker has a target, and no mob tick passes
	// before the hits, so no shell has cracked open.
	const shulkers = 12
	var ids []int32
	for i := range shulkers {
		ids = append(ids, srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+22.5, y, math.Floor(at.Z)+float64(3*i)+0.5))
	}
	for _, id := range ids {
		x, y, z, _ := srv.MobPos(id)
		srv.LaunchShulkerBullet(x-2, y+0.5, z, id, 3)
	}
	srv.TickEntities(60)
	srv.TickMobs(1)
	for i, id := range ids {
		if x, py, z, _ := srv.MobPos(id); x != math.Floor(at.X)+22.5 || py != y || z != math.Floor(at.Z)+float64(3*i)+0.5 {
			t.Errorf("shulker %d moved to %v,%v,%v", i, x, py, z)
		}
	}
	if n := len(srv.MobIDsOfKind("minecraft:shulker")); n != shulkers {
		t.Errorf("%d shulkers, want %d", n, shulkers)
	}
}

// TestAShulkerBulletCreepsAroundABlockInItsWay: a stone cell between the
// bullet and its target is never picked as the next leg, so the bullet
// survives where one that creeps straight into it would burst.
// vanilla: ShulkerBullet.selectNextMoveDirection
func TestAShulkerBulletCreepsAroundABlockInItsWay(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	bx, bz := int32(math.Floor(at.X))+2, int32(math.Floor(at.Z))+8
	cy := int32(math.Floor(y))
	// Row of the free cells the bullet may creep through, with the stone
	// directly toward the target.
	id := srv.SpawnMob("minecraft:shulker", float64(bx)+6.5, y, float64(bz)+0.5)
	srv.SetBlock(bx+1, cy, bz, 1)
	for seed := int64(1); seed <= 8; seed++ {
		srv.LaunchShulkerBullet(float64(bx)+0.5, y+0.5, float64(bz)+0.5, id, seed)
	}
	// One tick is the first creep: a bullet that chose the stone's cell
	// would already have moved east.
	srv.TickEntities(1)
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == "minecraft:shulker_bullet" && it.X > float64(bx)+0.5 {
			t.Errorf("bullet %d crept east to x=%v, toward the stone", it.ID, it.X)
		}
	}
	srv.TickEntities(19)
	if n := len(bulletsIn(srv)); n != 8 {
		t.Errorf("%d of 8 bullets still flying, want all: the stone was not steered around", n)
	}
}

// TestAShulkerBulletBurstsWithASoundOnABlock: a bullet that runs into a block
// plays shulker_bullet.hit in the hostile category and bursts into two
// explosion particles.
// vanilla: ShulkerBullet.onHitBlock
func TestAShulkerBulletBurstsWithASoundOnABlock(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	bx, bz := int32(math.Floor(at.X))+2, int32(math.Floor(at.Z))+2
	cy := int32(math.Floor(y))
	id := srv.SpawnMob("minecraft:shulker", float64(bx)-6.5, y, float64(bz)+0.5)
	// Stone all around the bullet's cell, so it cannot creep anywhere.
	for _, d := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}} {
		srv.SetBlock(bx+d[0], cy+d[1], bz+d[2], 1)
	}
	c.Settle()
	srv.LaunchShulkerBullet(float64(bx)+0.5, y+0.5, float64(bz)+0.5, id, 1)
	srv.TickEntities(60)
	var sound, burst bool
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlaySoundEffect:
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name == "minecraft:entity.shulker_bullet.hit" {
				sound = true
				if category != protocol.SoundCategoryHostile || volume != 1 || pitch != 1 {
					t.Errorf("hit sound: category %d volume %v pitch %v, want hostile 1 1", category, volume, pitch)
				}
			}
		case protocol.IDPlayLevelParticles:
			r := protocol.NewReader(data)
			pkind := r.ReadVarInt()
			ptail := servertest.ReadParticleTail(t, r)
			count := ptail.Count
			if pkind == protocol.ParticleExplosion {
				burst = true
				if count != 2 {
					t.Errorf("explosion particle count %d, want 2", count)
				}
			}
		}
		if sound && burst {
			break
		}
	}
	if !sound || !burst {
		t.Errorf("hit sound heard %v, explosion burst seen %v, want both", sound, burst)
	}
}
