package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// bulletBeside launches a bullet 1.5 blocks in front of the player at chest
// height, aimed at a shulker far off, and returns its ID.
func bulletBeside(t *testing.T, srv *server.Server, y float64) server.TestThrown {
	t.Helper()
	at := srv.PlayerPosition("Watcher")
	far := srv.SpawnMob("minecraft:shulker", at.X+15, y, at.Z)
	srv.LaunchShulkerBullet(at.X+1.5, y+1, at.Z, far, 3)
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == "minecraft:shulker_bullet" {
			return it
		}
	}
	t.Fatal("no bullet was launched")
	return server.TestThrown{}
}

// sawBulletBreak reads what the client was sent: the removal of id, the
// bullet's own hurt sound and the crit particles of the burst.
func sawBulletBreak(c *servertest.Client, t *testing.T, id int32) (removed, hurt bool, crits int32) {
	t.Helper()
	crit, _, ok := registry.ParticleID("minecraft:crit")
	if !ok {
		t.Fatal("no crit particle")
	}
	c.SettleAndCollect(func(pid int32, data []byte) {
		switch pid {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == id {
					removed = true
				}
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.shulker_bullet.hurt" {
				hurt = true
			}
		case protocol.IDPlayLevelParticles:
			r := protocol.NewReader(data)
			pkind := r.ReadVarInt()
			ptail := servertest.ReadParticleTail(t, r)
			n := ptail.Count
			if pkind == crit {
				crits += n
			}
		}
	})
	return removed, hurt, crits
}

// TestAPlayerBlowBreaksAShulkerBullet: a hit on a bullet in reach destroys it
// with its own hurt sound.
// vanilla: ShulkerBullet.hurtServer
func TestAPlayerBlowBreaksAShulkerBullet(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	bullet := bulletBeside(t, srv, y)
	c.Settle()
	c.Attack(bullet.ID)
	removed, hurt, crits := sawBulletBreak(c, t, bullet.ID)
	if !removed || !hurt || crits != 15 {
		t.Errorf("bullet removal told %v, hurt sound heard %v, %d crit particles, want both and 15", removed, hurt, crits)
	}
	if n := len(bulletsIn(srv)); n != 0 {
		t.Errorf("%d bullets still fly after the blow, want none", n)
	}
}

// TestABlowOutOfReachLeavesTheBulletFlying: the reach check holds.
// vanilla: Player.attack
func TestABlowOutOfReachLeavesTheBulletFlying(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	far := srv.SpawnMob("minecraft:shulker", at.X+30, y, at.Z)
	srv.LaunchShulkerBullet(at.X+10, y+1, at.Z, far, 3)
	var id int32
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		id = it.ID
	}
	c.Settle()
	c.Attack(id)
	c.Settle()
	if n := len(bulletsIn(srv)); n != 1 {
		t.Errorf("%d bullets after a blow from ten blocks, want the one that flew", n)
	}
}

// TestAnArrowBreaksAShulkerBullet: a shaft that meets a bullet destroys it.
// vanilla: ShulkerBullet.hurtServer, Projectile.onHit
func TestAnArrowBreaksAShulkerBullet(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	bullet := bulletBeside(t, srv, y)
	c.Settle()
	srv.ShootArrow(bullet.X-0.5, bullet.Y, bullet.Z, 1, 0, 0)
	srv.TickEntities(2)
	removed, hurt, crits := sawBulletBreak(c, t, bullet.ID)
	if !removed || !hurt || crits != 15 {
		t.Errorf("bullet removal told %v, hurt sound heard %v, %d crit particles, want both and 15", removed, hurt, crits)
	}
	if n := len(bulletsIn(srv)); n != 0 {
		t.Errorf("%d bullets still fly after the arrow, want none", n)
	}
}

// TestADyedShulkerTellsItsColourAndItsCopyKeepsIt: a dyed shulker's Color
// reaches the client's metadata, and the shulker a bullet splits off is dyed
// the same.
// vanilla: Shulker.DATA_COLOR_ID, hitByShulkerBullet
func TestADyedShulkerTellsItsColourAndItsCopyKeepsIt(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", at.X+3.5, y, at.Z+0.5)
	srv.DyeShulker(id, 3)
	srv.TickMobs(1)
	first := c.ExpectMobSpawn(shulkerType(t))
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			c.ReadMobMetadataInto(&first, data)
		}
	})
	if !first.ShulkerColorTold || first.ShulkerColor != 3 {
		t.Fatalf("the dyed shulker told colour %d (told %v), want 3", first.ShulkerColor, first.ShulkerColorTold)
	}
	srv.TickMobs(30)
	for range 80 {
		x0, y0, z0, _ := srv.MobPos(first.EntityID)
		srv.LaunchShulkerBullet(x0+2, y0+0.5, z0, first.EntityID, 3)
		srv.TickEntities(40)
		srv.TickMobs(12)
		if len(srv.MobIDsOfKind("minecraft:shulker")) == 2 {
			break
		}
	}
	if n := len(srv.MobIDsOfKind("minecraft:shulker")); n != 2 {
		t.Fatalf("%d shulkers after the bullets, want the original and one copy", n)
	}
	var copied servertest.SpawnedMob
	var copyID int32
	c.SettleAndCollect(func(pid int32, data []byte) {
		r := protocol.NewReader(data)
		switch id := r.ReadVarInt(); {
		case pid == protocol.IDPlaySpawnEntity && id != first.EntityID:
			r.ReadUUID()
			if r.ReadVarInt() == shulkerType(t) {
				copyID = id
				copied = servertest.SpawnedMob{EntityID: id, TypeID: shulkerType(t), Variant: -1, SoundVariant: -1}
			}
		case pid == protocol.IDPlayEntityMetadata && id == copyID && copyID != 0:
			c.ReadMobMetadataInto(&copied, data)
		}
	})
	if copyID == 0 {
		t.Fatal("the copy's spawn never reached the player")
	}
	if !copied.ShulkerColorTold || copied.ShulkerColor != 3 {
		t.Errorf("the copy told colour %d (told %v), want 3", copied.ShulkerColor, copied.ShulkerColorTold)
	}
}

// TestAShulkerTeleportSoundPlaysAtTheCellItLeft: the sound is not moved with
// the shulker, and level event 2016 at the old cell packs the offset to the
// new one with radii 8.
// vanilla: Shulker.teleportSomewhere
func TestAShulkerTeleportSoundPlaysAtTheCellItLeft(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	x, z := int32(math.Floor(at.X))+6, int32(math.Floor(at.Z))
	stone, _ := registry.FindState("minecraft:stone", nil)
	top := int32(y) + 2
	for yy := int32(y) - 1; yy < top; yy++ {
		srv.SetBlock(x, yy, z, stone)
	}
	id := srv.SpawnMob("minecraft:shulker", float64(x)+0.5, float64(top), float64(z)+0.5)
	srv.TickMobs(5)
	c.Settle()
	srv.SetBlock(x, top-1, z, 0)
	for range 80 {
		srv.TickMobs(1)
		if mx, my, mz, ok := srv.MobPos(id); ok && (mx != float64(x)+0.5 || my != float64(top) || mz != float64(z)+0.5) {
			break
		}
	}
	mx, my, mz, _ := srv.MobPos(id)
	var sx, sy, sz int32
	heard := false
	var trails [][4]int32
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayWorldEvent {
			if event, ex, ey, ez, d := servertest.ReadWorldEvent(t, data); event == protocol.WorldEventShulkerTeleport {
				trails = append(trails, [4]int32{ex, ey, ez, d})
			}
		}
		if pid != protocol.IDPlaySoundEffect {
			return
		}
		if name, _, px, py, pz, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.shulker.teleport" {
			sx, sy, sz, heard = px, py, pz, true
		}
	})
	if !heard {
		t.Fatal("no teleport sound reached the player")
	}
	// Sound coordinates are eighths of a block.
	if sx != (x*8)+4 || sy != top*8 || sz != (z*8)+4 {
		t.Errorf("the teleport sounded at %v,%v,%v eighths, want the old cell %v,%v,%v", sx, sy, sz, x*8+4, top*8, z*8+4)
	}
	if len(trails) != 1 {
		t.Fatalf("got %d shulker teleport events, want 1", len(trails))
	}
	tr := trails[0]
	if [3]int32{tr[0], tr[1], tr[2]} != [3]int32{x, top, z} {
		t.Errorf("the teleport event is at %v, want the old cell %v", tr[:3], [3]int32{x, top, z})
	}
	want := [3]int32{int32(math.Floor(mx)), int32(math.Floor(my)), int32(math.Floor(mz))}
	if to := servertest.UnpackPosDiff(x, top, z, tr[3], 8, 8, 8); to != want {
		t.Errorf("the teleport event points at %v, want the shulker's new cell %v", to, want)
	}
}

// TestAnExplosionBreaksAShulkerBullet: a blast hurts every body in reach, and
// any hurt destroys a bullet.
// vanilla: ServerExplosion.hurtEntities, ShulkerBullet.hurtServer
func TestAnExplosionBreaksAShulkerBullet(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	bullet := bulletBeside(t, srv, y)
	c.Settle()
	srv.BlastAt(bullet.X, bullet.Y+1, bullet.Z, 3)
	removed, hurt, crits := sawBulletBreak(c, t, bullet.ID)
	if !removed || !hurt || crits != 15 {
		t.Errorf("bullet removal told %v, hurt sound heard %v, %d crit particles, want both and 15", removed, hurt, crits)
	}
	if n := len(bulletsIn(srv)); n != 0 {
		t.Errorf("%d bullets still fly after the blast, want none", n)
	}
}

// TestAShulkerWithAColourPastFifteenTellsItRaw: DATA_COLOR_ID keeps the byte
// it was given.
// vanilla: Shulker.DATA_COLOR_ID
func TestAShulkerWithAColourPastFifteenTellsItRaw(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", at.X+3.5, y, at.Z+0.5)
	srv.DyeShulker(id, 20)
	srv.TickMobs(1)
	got := c.ExpectMobSpawn(shulkerType(t))
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			c.ReadMobMetadataInto(&got, data)
		}
	})
	if !got.ShulkerColorTold || got.ShulkerColor != 20 {
		t.Errorf("the shulker told colour %d (told %v), want the raw 20", got.ShulkerColor, got.ShulkerColorTold)
	}
}

// TestABrokenShulkerBulletPostsAnEntityDamageEvent: the destroyed bullet's
// damage is a vibration, whoever broke it.
// vanilla: ShulkerBullet.hurtServer, destroy
func TestABrokenShulkerBulletPostsAnEntityDamageEvent(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	bullet := bulletBeside(t, srv, y)
	c.Settle()
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	c.Attack(bullet.ID)
	c.SettleAndCollect(func(int32, []byte) {})
	if !events.at("minecraft:entity_damage", bullet.X, bullet.Y, bullet.Z) {
		t.Errorf("entity_damage events: %v, want one at the bullet %v,%v,%v", events.seen["minecraft:entity_damage"], bullet.X, bullet.Y, bullet.Z)
	}
}

// TestAShulkerBulletThatHitsABlockPostsAnEntityDamageEvent: the bullet's
// burst on a block is a vibration too.
// vanilla: ShulkerBullet.onHit
func TestAShulkerBulletThatHitsABlockPostsAnEntityDamageEvent(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	bx, bz := int32(math.Floor(at.X))+2, int32(math.Floor(at.Z))+2
	cy := int32(math.Floor(y))
	id := srv.SpawnMob("minecraft:shulker", float64(bx)-6.5, y, float64(bz)+0.5)
	for _, d := range [][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}} {
		srv.SetBlock(bx+d[0], cy+d[1], bz+d[2], 1)
	}
	c.Settle()
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	srv.LaunchShulkerBullet(float64(bx)+0.5, y+0.5, float64(bz)+0.5, id, 1)
	srv.TickEntities(60)
	if n := len(events.seen["minecraft:entity_damage"]); n != 1 {
		t.Errorf("%d entity_damage events after the bullet hit a block, want 1", n)
	}
}

// TestASprintingBlowOnAShulkerBulletEndsTheSprint: a full-strength sprinting
// blow shoves its target, so the attacker stops sprinting, as for any blow.
// vanilla: Player.attack, causeExtraKnockback
func TestASprintingBlowOnAShulkerBulletEndsTheSprint(t *testing.T) {
	t.Parallel()
	srv, c, self := servertest.JoinAs(t, servertest.SpawningTestConfig(), "Watcher")
	servertest.AwaitChunksSettled(t, srv)
	srv.StopTick()
	srv.SetDifficulty(2)
	c.Settle()
	bullet := bulletBeside(t, srv, srv.PlayerPosition("Watcher").Y)
	c.Settle()
	c.SendSprint(self, true)
	c.Settle()
	if !srv.PlayerSprinting("Watcher") {
		t.Fatal("the player is not sprinting before the blow")
	}
	c.Attack(bullet.ID)
	c.Settle()
	if srv.PlayerSprinting("Watcher") {
		t.Error("the player still sprints after a sprinting blow on a bullet")
	}
}

// TestASnowballBreaksAShulkerBullet: a thrown body that meets a bullet
// destroys it, though a snowball deals no damage.
// vanilla: ShulkerBullet.hurtServer, Projectile.onHit
func TestASnowballBreaksAShulkerBullet(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	bullet := bulletBeside(t, srv, y)
	c.Settle()
	srv.ThrowThrown(entity.ThrownSnowball, bullet.X-1, bullet.Y+0.25, bullet.Z, -90, 0, 0, 1)
	srv.TickEntities(1)
	removed, hurt, crits := sawBulletBreak(c, t, bullet.ID)
	if !removed || !hurt || crits != 15 {
		t.Errorf("bullet removal told %v, hurt sound heard %v, %d crit particles, want both and 15", removed, hurt, crits)
	}
	if n := len(bulletsIn(srv)); n != 0 {
		t.Errorf("%d bullets still fly after the snowball, want none", n)
	}
}
