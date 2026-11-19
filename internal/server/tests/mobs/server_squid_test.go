package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestASummonedSquidSwimsInItsPond summons a squid into the pond and
// watches its body over a hundred hand-driven ticks: the tentacle pump
// carries it through the water — never out of the pond — and the entity
// event its wrapped cycle broadcasts restarts the client's tentacle
// animation at least once on the way.
func TestASummonedSquidSwimsInItsPond(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)

	id := summonSquid(t, c, "minecraft:squid", protocol.EntityTypeSquid, pondCentreX, -8.5)
	start := srv.Mobs()
	if len(start) != 1 || start[0].ID != id {
		t.Fatalf("%d mobs stand after the summon, want the one squid (id %d)", len(start), id)
	}

	tentacles := 0
	for range 100 {
		c.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID != protocol.IDPlayEntityEvent {
				return
			}
			r := protocol.NewReader(data)
			tickView := r.ReadI32()
			status := r.ReadU8()
			if tickView == id && status == protocol.EntityStatusSquidTentacles {
				tentacles++
			}
		})
		srv.TickMobs(1)
	}
	c.Settle()

	m := srv.Mobs()
	if len(m) != 1 {
		t.Fatalf("%d mobs stand after the swim, want the one squid", len(m))
	}
	moved := math.Hypot(m[0].X-start[0].X, m[0].Z-start[0].Z)
	if moved < 0.25 {
		t.Errorf("the squid swam %.2f blocks in a hundred ticks, want the tentacle pump's wandering", moved)
	}
	if x, z := int32(math.Floor(m[0].X)), int32(math.Floor(m[0].Z)); x < pondXLo || x > pondXHi || z < pondZLo || z > pondZHi {
		t.Errorf("the squid swam to (%d, %d), outside the pond it was summoned into", x, z)
	}
	if tentacles == 0 {
		t.Error("no tentacle restart event in the pond's first hundred ticks, want the wrapped cycle's broadcast")
	}
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

// TestASoftenedBlowStillSquirtsInk strikes a squid twice inside the
// invulnerability window — a whole bare-fisted blow first, then a sword's
// larger one the window softens to its excess — and expects the softened
// blow, which tells of no hurt's damage or sound, to squeeze the ink out
// all the same.
// vanilla: Squid.hurtServer, LivingEntity.hurtServer
func TestASoftenedBlowStillSquirtsInk(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:squid", protocol.EntityTypeSquid, pondCentreX, pondNorth)

	// The whole first blow and its own ink are read past with the settle.
	c.Attack(id)
	c.Settle()

	// The sword goes on and the swing comes back to full: the window a
	// blow opens is ticked by the mobs, which the loop's stop leaves
	// still, while the arm recovers on the survival ticks. The second
	// blow's larger damage then lands only its excess.
	srv.GiveSlot("Striker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	for range 20 {
		srv.TickSurvival()
	}
	c.Attack(id)

	squirted, inked := false, false
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			name, _ := servertest.SoundEventName(sound - 1)
			if sound == 0 {
				name = r.ReadString(32767)
			}
			if name == "minecraft:entity.squid.squirt" {
				squirted = true
			}
		case protocol.IDPlayLevelParticles:
			r := protocol.NewReader(data)
			pkind := r.ReadVarInt()
			servertest.ReadParticleTail(t, r)
			if pkind == protocol.ParticleSquidInk {
				inked = true
			}
		}
		return squirted && inked
	})
	if !squirted {
		t.Error("the softened blow never played the squirt's own sound")
	}
	if !inked {
		t.Error("the softened blow never squeezed the ink cloud out")
	}
}

// TestTheDeadSquidLaysItsInkSacDown kills each kind of squid with a
// diamond sword and reads the death's loot: the ink sac — one to three of
// them, the glow squid's own sac glowing. The squid is summoned on one
// heart so the one blow kills it: a struck squid flees, and the pond is
// too short to catch it again before the memory of the blow fades.
func TestTheDeadSquidLaysItsInkSacDown(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind      string
		entityTyp int32
		sac       string
	}{
		{"minecraft:squid", protocol.EntityTypeSquid, "minecraft:ink_sac"},
		{"minecraft:glow_squid", protocol.EntityTypeGlowSquid, "minecraft:glow_ink_sac"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			srv, c := squidTests(t)

			id := summonSquidTagged(t, c, tc.kind, tc.entityTyp, pondCentreX, pondNorth, "{Health:1f}")
			srv.GiveSlot("Striker", player.HotbarSlot,
				protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})

			sac, ok := registry.ItemID(tc.sac)
			if !ok {
				t.Fatalf("the item registry has no %s", tc.sac)
			}
			var drops []servertest.ItemDrop
			removed := false
			for tick := 0; tick < 200 && (!removed || len(drops) == 0); tick++ {
				if srv.AttackTicks("Striker") >= servertest.SwingRecovered {
					c.Attack(id)
				}
				srv.TickSurvival()
				srv.TickMobs(1)
				c.SettleAndCollect(func(packetID int32, data []byte) {
					switch packetID {
					case protocol.IDPlaySpawnEntity:
						if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
							c.ReadDropStack(&d)
							drops = append(drops, d)
						}
					case protocol.IDPlayRemoveEntities:
						r := protocol.NewReader(data)
						for range r.ReadVarInt() {
							if r.ReadVarInt() == id {
								removed = true
							}
						}
					}
				})
			}
			if !removed {
				t.Fatal("the squid was never struck down by the sword's swings")
			}
			if len(drops) == 0 {
				t.Fatal("the dead squid laid nothing down")
			}
			for _, d := range drops {
				name, _ := registry.ItemName(d.Stack.ItemID)
				if d.Stack.ItemID != sac {
					t.Errorf("the dead squid dropped a %s, want its ink sac the table names", name)
					continue
				}
				if d.Stack.Count < 1 || d.Stack.Count > 3 {
					t.Errorf("the dead squid dropped %d %s, want the table's one to three", d.Stack.Count, name)
				}
			}
		})
	}
}
