package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The squid tests: the body's own half of the water categories, driven the
// way the summon and combat tests drive the server — through a real
// connection whose operator summons into a hand-laid pond, with the loop
// stopped and the ticks driven by hand so the swims are the same every run.

// The pond: water two blocks deep, dug into the stone of the flat test
// world, with a stone ledge standing dry at its north end for the striking
// player. The player's ledge and the pond's north edge sit within a
// swing's reach of each other.
const (
	pondY            = 62
	pondXLo          = 6
	pondXHi          = 10
	pondZLo          = -12
	pondZHi          = -4
	pondCentreX      = 8.5
	pondNorth        = -4.5
	playerX, playerZ = 8.5, -2.5
)

// squidTests is the squid behaviour tests' fixture: the flat world, an
// operator to run the summons, and the pond dug into its stone. The loop is
// stopped, so the ticks the tests drive by hand are the only ones the body
// lives, and the clock stands at noon, where nothing burns.
func squidTests(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	const name = "Striker"
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, name, servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID(name).String(),
		Name:  name,
		Level: 4,
	}))
	c.MoveToGround(playerX, 64, playerZ, true)
	c.Settle()
	srv.SetDayTime(servertest.Noon)
	srv.StopTick()
	layPond(t, srv)
	return srv, c
}

// layPond digs the pond the squid tests summon into: still water two blocks
// deep into the surface of the flat stone, over the whole pond rectangle.
func layPond(t *testing.T, srv *server.Server) {
	t.Helper()
	water, ok := registry.FindState("minecraft:water", map[string]string{"level": "0"})
	if !ok {
		t.Fatal("the block registry has no still water state")
	}
	for x := int32(pondXLo); x <= pondXHi; x++ {
		for z := int32(pondZLo); z <= pondZHi; z++ {
			if !srv.SetBlock(x, pondY, z, water) || !srv.SetBlock(x, pondY+1, z, water) {
				t.Fatalf("the pond's chunk at (%d, %d) was not loaded", x, z)
			}
		}
	}
}

// summonSquid summons one of the two squids into the pond at the block
// centre given and returns its entity ID, the success line read on the way.
func summonSquid(t *testing.T, c *servertest.Client, kind string, entityType int32, x, z float64) int32 {
	t.Helper()
	return summonSquidTagged(t, c, kind, entityType, x, z, "")
}

// summonSquidTagged is summonSquid with a compound tag after the position,
// read onto the body as any summon's tag is.
func summonSquidTagged(t *testing.T, c *servertest.Client, kind string, entityType int32, x, z float64, tag string) int32 {
	t.Helper()
	return summonSquidSpawn(t, c, kind, entityType, x, z, tag).EntityID
}

// summonSquidSpawn is summonSquidTagged returning the spawn as read.
func summonSquidSpawn(t *testing.T, c *servertest.Client, kind string, entityType int32, x, z float64, tag string) servertest.SpawnedMob {
	t.Helper()
	command := "summon " + kind + " " + fmt.Sprintf("%g", x) + " 62 " + fmt.Sprintf("%g", z)
	if tag != "" {
		command += " " + tag
	}
	c.ChatCommand(command)
	servertest.AwaitSummonFeedback(t, c, kind, "entity.minecraft."+kind[len("minecraft:"):])
	return c.ExpectMobSpawn(entityType)
}

// TestASquidSquirtsInkAndFleesWhenHurt strikes each kind of squid once in
// its pond and reads the hurt back: the squirt's own sound and the ink
// cloud the blow squeezes out of the body, and the flee — the swim the
// hurt turns away from the striker — carrying it a full block down the
// pond and further inside the hundred ticks its memory of the blow lasts.
func TestASquidSquirtsInkAndFleesWhenHurt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind      string
		entityTyp int32
		squirt    string
		ink       int32
	}{
		{"minecraft:squid", protocol.EntityTypeSquid,
			"minecraft:entity.squid.squirt", protocol.ParticleSquidInk},
		{"minecraft:glow_squid", protocol.EntityTypeGlowSquid,
			"minecraft:entity.glow_squid.squirt", protocol.ParticleGlowSquidInk},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			srv, c := squidTests(t)
			id := summonSquid(t, c, tc.kind, tc.entityTyp, pondCentreX, pondNorth)
			c.Settle()

			squirted, inked := false, false
			var before server.TestMob
			for _, m := range srv.Mobs() {
				if m.ID == id {
					before = m
				}
			}
			c.Attack(id)
			c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
				switch packetID {
				case protocol.IDPlaySoundEffect:
					r := protocol.NewReader(data)
					sound := r.ReadVarInt()
					name, _ := servertest.SoundEventName(sound - 1)
					if sound == 0 {
						name = r.ReadString(32767)
					}
					if name == tc.squirt {
						squirted = true
						if got := r.ReadVarInt(); got != protocol.SoundCategoryNeutral {
							t.Errorf("the squirt played in category %d, want the neutral bracket", got)
						}
					}
				case protocol.IDPlayLevelParticles:
					r := protocol.NewReader(data)
					pkind := r.ReadVarInt()
					ptail := servertest.ReadParticleTail(t, r)
					count := ptail.Count
					if got := pkind; got == tc.ink {
						if count <= 0 {
							t.Errorf("the ink cloud carried %d particles, want the squirt's burst", count)
						}
						inked = true
					}
				}
				return squirted && inked
			})
			if squirted && inked {
				return
			}
			// The packets above drain the hurt; the flee runs in the ticks
			// after, whether or not both of its tellings arrived.
			for range 100 {
				srv.TickMobs(1)
			}
			if !squirted {
				t.Error("the hurt never played the squirt's own sound")
			}
			if !inked {
				t.Error("the hurt never squeezed the ink cloud out")
			}
			if before.ID == id {
				for _, m := range srv.Mobs() {
					if m.ID != id {
						continue
					}
					if before.Z-m.Z < 1.0 {
						t.Errorf("the struck squid sits %.2f blocks further down the pond after a hundred ticks, want the flee's whole block and more", before.Z-m.Z)
					}
					return
				}
				t.Fatal("the struck squid is gone from the world a hundred ticks after the blow")
			}
		})
	}
}
