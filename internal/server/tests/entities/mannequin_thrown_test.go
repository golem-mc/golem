package entities

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Snowball.onHitEntity, SmallFireball.onHitEntity, LivingEntity.hurtServer
func TestThrownProjectilesHurtMannequin(t *testing.T) {
	for _, tc := range []struct {
		kind, extra string
		moved, hurt bool
	}{
		{"snowball", "", true, false},
		{"small_fireball", "", false, true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "Thrower")
			c.ChatCommand("summon mannequin 5.5 64 0.5")
			awaitMannequin(t, c, srv)
			c.ChatCommand("summon " + tc.kind + " 1.5 64.4 0.5 {Motion:[1.2d,0d,0d],NoGravity:1b}")
			servertest.RunTicks(t, srv, 30)
			x, okX := mannequinData(t, c, srv, "Pos[0]")
			health, okH := mannequinData(t, c, srv, "Health")
			if !okX || !okH {
				t.Fatal("the mannequin is gone")
			}
			if tc.moved && x < 6.5 {
				t.Errorf("mannequin x = %v, want pushed east beyond 6.5", x)
			}
			if tc.hurt && health >= 20 {
				t.Errorf("mannequin health = %v, want below 20", health)
			}
		})
	}
}

// vanilla: BlockAttachedEntity.hurtServer
func TestThrownProjectileBreaksLeashKnot(t *testing.T) {
	srv, c := joinMannequinOp(t, "KnotBreaker")
	srv.SetBlock(5, 64, 4, registry.BlockOakFence.DefaultState())
	c.ChatCommand("summon leash_knot 5 64 4")
	knot := c.AwaitSpawn(protocol.EntityTypeLeashKnot)
	c.ChatCommand("summon snowball 1.5 64.5 4.5 {Motion:[1.2d,0d,0d],NoGravity:1b}")
	gone := false
	c.ReadTicks(srv, 30, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		for n := r.ReadVarInt(); n > 0; n-- {
			if r.ReadVarInt() == knot.EntityID {
				gone = true
			}
		}
		return gone
	})
	if !gone {
		t.Fatal("the knot stands after a snowball hit it")
	}
}

// vanilla: ServerExplosion.hurtEntities
func TestExplosionKillsMannequin(t *testing.T) {
	srv, c := joinMannequinOp(t, "Blaster")
	for x := int32(8); x <= 16; x++ {
		for z := int32(-3); z <= 3; z++ {
			srv.SetBlock(x, 63, z, registry.BlockObsidian.DefaultState())
		}
	}
	c.ChatCommand("summon mannequin 13.5 64 0.5")
	awaitMannequin(t, c, srv)
	c.ChatCommand("summon tnt 10.5 64 0.5 {fuse:1}")
	servertest.RunTicks(t, srv, 5)
	if health, alive := mannequinData(t, c, srv, "Health"); alive && health > 0 {
		t.Fatalf("mannequin 3 blocks from the blast has health %v, want dead", health)
	}
}

// mannequinData reads a number off the first mannequin; ok is false when
// the answer is an error because the mannequin is gone.
func mannequinData(t *testing.T, c *servertest.Client, srv *server.Server, path string) (float64, bool) {
	t.Helper()
	c.ChatCommand("data get entity @e[type=mannequin,limit=1] " + path)
	var answer string
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if ok && text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			answer = text.With[1].Plain()
			return true
		}
		return false
	})
	if answer == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimRight(answer, "fd"), 64)
	if err != nil {
		t.Fatalf("/data get %s answered %q", path, answer)
	}
	return v, true
}
