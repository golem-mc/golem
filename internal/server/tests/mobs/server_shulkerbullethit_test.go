package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
