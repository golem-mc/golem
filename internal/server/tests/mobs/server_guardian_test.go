package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The guardian tests reuse the squid tests' pond with the loop stopped, so
// the guardian lives only the ticks the test runs. The player is the striker
// of the squid fixture, standing on the ledge at the pond's north end.

const (
	guardianPlayer = "Striker"
	lakeXLo        = -12
	lakeXHi        = 28
	lakeZLo        = -40
)

// guardianTests is squidTests with the difficulty given, returning the
// player's entity ID.
func guardianTests(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.SpawningTestConfig()
	srv, c, playerID := servertest.JoinAs(t, cfg, guardianPlayer, servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID(guardianPlayer).String(),
		Name:  guardianPlayer,
		Level: 4,
	}))
	c.MoveToGround(playerX, 64, playerZ, true)
	c.Settle()
	srv.SetDayTime(servertest.Noon)
	srv.StopTick()
	layPond(t, srv)
	srv.SetDifficulty(difficulty)
	return srv, c, playerID
}

func guardianType(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID(name)
	if !ok {
		t.Fatalf("no entity type %s", name)
	}
	return id
}

// spawnGuardian stands a guardian in the pond with the seed of its position,
// and returns its entity ID with the spawn packet read.
func spawnGuardian(t *testing.T, srv *server.Server, c *servertest.Client, kind string, x, y, z float64) int32 {
	t.Helper()
	id := srv.SpawnMob(kind, x, y, z)
	srv.TickMobs(1)
	got := c.ExpectMobSpawn(guardianType(t, kind))
	if got.EntityID != id {
		t.Fatalf("the spawn packet names entity %d, want %d", got.EntityID, id)
	}
	return id
}

// beamWatch follows one guardian's metadata and the player's health tick by
// tick.
type beamWatch struct {
	id         int32
	meta       servertest.SpawnedMob
	targetedAt int
	hurtAt     int
	lost       float64
}

// run ticks the mob pass until the player is first hurt or limit ticks pass.
func (w *beamWatch) run(t *testing.T, srv *server.Server, c *servertest.Client, limit int) {
	t.Helper()
	w.targetedAt, w.hurtAt = -1, -1
	for tick := range limit {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata || protocol.NewReader(data).ReadVarInt() != w.id {
				return
			}
			c.ReadMobMetadataInto(&w.meta, data)
			if w.meta.GuardianTarget != 0 && w.targetedAt < 0 {
				w.targetedAt = tick
			}
		})
		if hp := srv.PlayerHealth(guardianPlayer); hp < 20 {
			w.hurtAt, w.lost = tick, 20-hp
			return
		}
	}
}

func newBeamWatch(t *testing.T, srv *server.Server, c *servertest.Client, id int32, kind string) *beamWatch {
	t.Helper()
	return &beamWatch{id: id, meta: servertest.SpawnedMob{EntityID: id, TypeID: guardianType(t, kind)}}
}

// TestAGuardianChargesItsBeamForEightyTicks: a guardian that has the player
// in sight tells the client whom its beam is on, and the beam's damage
// lands exactly eighty ticks after that and not before. On normal the
// magic hit is 1 and the guardian's own blow of 6 lands its excess of 5
// through the window the beam opened.
// vanilla: Guardian.GuardianAttackGoal.tick
func TestAGuardianChargesItsBeamForEightyTicks(t *testing.T) {
	t.Parallel()
	srv, c, playerID := guardianTests(t, rules.DifficultyNormal)
	id := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 62, -8.5)
	w := newBeamWatch(t, srv, c, id, "minecraft:guardian")
	w.run(t, srv, c, 600)

	if w.targetedAt < 0 {
		t.Fatal("the guardian never told its beam target in 600 ticks")
	}
	if w.meta.GuardianTarget != playerID {
		t.Errorf("the beam target is entity %d, want the player %d", w.meta.GuardianTarget, playerID)
	}
	if w.hurtAt < 0 {
		t.Fatal("the beam never hurt the player")
	}
	if got := w.hurtAt - w.targetedAt; got != 80 {
		t.Errorf("the beam landed %d ticks after its target was told, want 80", got)
	}
	if w.lost != 6 {
		t.Errorf("the beam and the blow cost %v health, want 6 (1 magic + 5 left of the blow)", w.lost)
	}
	// The hunt ends with the blow: the target is let go.
	srv.TickMobs(2)
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
			c.ReadMobMetadataInto(&w.meta, data)
		}
	})
	if w.meta.GuardianTarget != 0 {
		t.Errorf("the beam target is still %d after the blow, want it cleared", w.meta.GuardianTarget)
	}
}

// TestAnElderGuardianChargesSixtyTicksAndHitsHarder: on hard the elder's beam
// is 1 + 2 (hard) + 2 (elder) = 5, scaled by 1.5 to 7.5, and its blow of 8
// scales to 12, so the pair costs 12 and lands after sixty ticks.
// vanilla: ElderGuardian.getAttackDuration, GuardianAttackGoal.tick
func TestAnElderGuardianChargesSixtyTicksAndHitsHarder(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyHard)
	id := spawnGuardian(t, srv, c, "minecraft:elder_guardian", pondCentreX, 62, -8.5)
	w := newBeamWatch(t, srv, c, id, "minecraft:elder_guardian")
	w.run(t, srv, c, 600)

	if w.targetedAt < 0 || w.hurtAt < 0 {
		t.Fatalf("the elder's beam was told at tick %d and landed at %d, want both", w.targetedAt, w.hurtAt)
	}
	if got := w.hurtAt - w.targetedAt; got != 60 {
		t.Errorf("the elder's beam landed %d ticks after its target was told, want 60", got)
	}
	if w.lost != 12 {
		t.Errorf("the elder's beam and blow cost %v health, want 12", w.lost)
	}
}

// TestAGuardianPricksTheOneWhoStrikesItWithSpikesOut: a guardian that is not
// swimming to a point hurts its melee attacker for 2; one that is swimming
// has its spikes in and does not.
// vanilla: Guardian.hurtServer
func TestAGuardianPricksTheOneWhoStrikesItWithSpikesOut(t *testing.T) {
	t.Parallel()
	prick := func(t *testing.T, still bool) float64 {
		srv, c, _ := guardianTests(t, rules.DifficultyNormal)
		// The stroll ranks water over air, so it swims only where a lake
		// offers cells; a beam that lands on the way is not the prick.
		layLake(t, srv)
		// Within three blocks of the ledge, so the guardian starts out unhunted.
		id := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 63, pondNorth)
		w := newBeamWatch(t, srv, c, id, "minecraft:guardian")
		if still {
			srv.SetMobNoAI(id)
		} else {
			for tick := 0; !w.meta.GuardianMoving; tick++ {
				if tick == 400 {
					t.Fatal("the guardian never swam anywhere in 400 ticks")
				}
				srv.TickMobs(1)
				c.SettleAndCollect(func(pid int32, data []byte) {
					if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
						c.ReadMobMetadataInto(&w.meta, data)
					}
				})
			}
		}
		before := srv.PlayerHealth(guardianPlayer)
		for srv.AttackTicks(guardianPlayer) < servertest.SwingRecovered {
			srv.TickSurvival()
		}
		c.Attack(id)
		c.Settle()
		return before - srv.PlayerHealth(guardianPlayer)
	}
	t.Run("spikes out", func(t *testing.T) {
		if got := prick(t, true); got != 2 {
			t.Errorf("striking a still guardian cost %v health, want its 2", got)
		}
	})
	t.Run("swimming", func(t *testing.T) {
		if got := prick(t, false); got != 0 {
			t.Errorf("striking a swimming guardian cost %v health, want none", got)
		}
	})
}

// TestAnElderGuardianCursesThePlayersAroundIt: every 1200 ticks the elder
// gives players within 50 blocks mining fatigue III for 6000 ticks and shows
// them the apparition with game event 10; a silent elder shows it with 0.
// vanilla: ElderGuardian.customServerAiStep
func TestAnElderGuardianCursesThePlayersAroundIt(t *testing.T) {
	t.Parallel()
	for _, silent := range []bool{false, true} {
		name := "sounding"
		want := float32(1)
		if silent {
			name, want = "silent", 0
		}
		t.Run(name, func(t *testing.T) {
			srv, c, _ := guardianTests(t, rules.DifficultyNormal)
			// A pool forty blocks from the player: out of the elder's
			// sixteen-block follow range, inside its curse's fifty.
			water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
			for x := int32(46); x <= 50; x++ {
				for z := int32(-12); z <= -4; z++ {
					for y := int32(pondY); y <= pondY+1; y++ {
						if !srv.SetBlock(x, y, z, water) {
							t.Fatalf("the pool's chunk at (%d, %d) was not loaded", x, z)
						}
					}
				}
			}
			id := srv.SpawnMob("minecraft:elder_guardian", 48.5, 62, -8.5)
			srv.TickMobs(1)
			c.ExpectMobSpawn(guardianType(t, "minecraft:elder_guardian"))
			if silent {
				srv.SetMobSilent(id, true)
			}
			if _, _, ok := srv.PlayerEffect(guardianPlayer, "minecraft:mining_fatigue"); ok {
				t.Fatal("the player carries mining fatigue before the curse")
			}
			// The curse falls on the tick the elder's age plus its ID
			// reaches a multiple of 1200; the spawn's own tick was the first.
			var events []float32
			cursedAt := -1
			for tick := 2; tick <= 1200; tick++ {
				srv.TickMobs(1)
				c.SettleAndCollect(func(pid int32, data []byte) {
					if pid != protocol.IDPlayGameEvent {
						return
					}
					r := protocol.NewReader(data)
					if r.ReadU8() == protocol.GameEventGuardianElderEffect {
						events = append(events, r.ReadF32())
						cursedAt = tick
					}
				})
			}
			if wantTick := 1200 - int(id)%1200; cursedAt != wantTick {
				t.Errorf("the curse fell on tick %d, want %d (1200 less the elder's ID %d)", cursedAt, wantTick, id)
			}
			duration, amp, ok := srv.PlayerEffect(guardianPlayer, "minecraft:mining_fatigue")
			if !ok || duration != 6000 || amp != 2 {
				t.Errorf("the player's mining fatigue is (%d, %d, %v) after 1200 ticks, want 6000 ticks of amplifier 2", duration, amp, ok)
			}
			if len(events) != 1 || events[0] != want {
				t.Errorf("the apparition events were %v, want exactly one of %v", events, want)
			}
		})
	}
}

// TestACursedPlayerOutOfReachIsSpared: the same elder, with the player more
// than fifty blocks away, curses nobody.
func TestACursedPlayerOutOfReachIsSpared(t *testing.T) {
	t.Parallel()
	srv, _, _ := guardianTests(t, rules.DifficultyNormal)
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	for x := int32(-56); x <= -50; x++ {
		for z := int32(-12); z <= -4; z++ {
			for y := int32(pondY); y <= pondY+1; y++ {
				if !srv.SetBlock(x, y, z, water) {
					t.Fatalf("the pool's chunk at (%d, %d) was not loaded", x, z)
				}
			}
		}
	}
	// The player stands at x = 8.5; the elder 61 blocks west.
	srv.SpawnMob("minecraft:elder_guardian", -52.5, 62, -8.5)
	for range 1200 {
		srv.TickMobs(1)
	}
	if _, _, ok := srv.PlayerEffect(guardianPlayer, "minecraft:mining_fatigue"); ok {
		t.Error("a player 61 blocks from the elder was cursed")
	}
}

// TestAGuardianSwimsAboutItsLakeAndNeverDrowns: a guardian nobody hunts
// swims through its lake without leaving it, and holds its breath under
// water for longer than a land mob's 300 ticks.
// vanilla: Guardian.aiStep, GuardianMoveControl
func TestAGuardianSwimsAboutItsLakeAndNeverDrowns(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyNormal)
	// A creative player is nobody's target.
	c.ChatCommand("gamemode creative")
	c.Settle()
	// The stroll picks the best of ten random cells, and water outranks air:
	// the little pond rarely holds one, so the guardian swims in a lake.
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	for x := int32(lakeXLo); x <= lakeXHi; x++ {
		for z := int32(lakeZLo); z <= pondZHi; z++ {
			for y := int32(60); y <= pondY+1; y++ {
				if srv.SetBlock(x, y, z, water); srv.BlockAt(x, y, z) != water {
					t.Fatalf("the lake's chunk at (%d, %d) was not loaded", x, z)
				}
			}
		}
	}
	id := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 62, -8.5)
	travelled, moving := 0.0, 0
	last, _ := fishAt(srv, id)
	for range 450 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
				var m servertest.SpawnedMob
				m.EntityID, m.TypeID = id, guardianType(t, "minecraft:guardian")
				c.ReadMobMetadataInto(&m, data)
				if m.GuardianMoving {
					moving++
				}
			}
		})
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatal("the guardian left the world")
		}
		if x, z := int32(math.Floor(m.X)), int32(math.Floor(m.Z)); x < lakeXLo || x > lakeXHi || z < lakeZLo || z > pondZHi || m.Y < 60 || m.Y >= pondY+2 {
			t.Fatalf("the guardian swam out of the lake to (%.2f, %.2f, %.2f)", m.X, m.Y, m.Z)
		}
		travelled += math.Hypot(m.X-last.X, m.Z-last.Z)
		last = m
	}
	if travelled < 3 {
		t.Errorf("the guardian swam %.2f blocks in 450 ticks, want it wandering", travelled)
	}
	if moving == 0 {
		t.Error("the guardian never told its spikes in while swimming")
	}
	if hp, _ := srv.MobHealth(id); hp != 30 {
		t.Errorf("the guardian has %.1f health after 450 ticks under water, want its 30", hp)
	}
}

// TestAnElderGuardianDropsASpongeAndExperience: killed by a player, the elder
// always leaves a wet sponge, and a guardian's experience is 10.
// vanilla: entities/elder_guardian.json, Guardian's xpReward
func TestAnElderGuardianDropsASpongeAndExperience(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyNormal)
	id := spawnGuardian(t, srv, c, "minecraft:elder_guardian", pondCentreX, 63, pondNorth)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 1)
	srv.GiveSlot(guardianPlayer, player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	servertest.RecoverByHand(t, srv, guardianPlayer)
	c.Attack(id)
	c.Settle()
	srv.TickMobs(1)
	sponges := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:wet_sponge" {
			sponges += int(it.Count)
		}
	}
	if sponges != 1 {
		t.Errorf("the dead elder left %d wet sponges, want 1", sponges)
	}
	xp := 0
	for _, o := range srv.OrbsInWorld() {
		xp += o.XP
	}
	if xp != 10 {
		t.Errorf("the dead elder left %d experience, want 10", xp)
	}
}

// TestAnElderGuardianHomesToItsSpawnBlock: on its first AI step an elder
// guardian sets its home to the block it is in, radius 16, so its walks keep
// to its monument room; a plain guardian gets no home.
// vanilla: ElderGuardian.customServerAiStep
func TestAnElderGuardianHomesToItsSpawnBlock(t *testing.T) {
	t.Parallel()
	srv, c, _ := guardianTests(t, rules.DifficultyNormal)
	elder := spawnGuardian(t, srv, c, "minecraft:elder_guardian", pondCentreX, 62, -8.5)
	plain := spawnGuardian(t, srv, c, "minecraft:guardian", pondCentreX, 62, -11.5)
	srv.TickMobs(2)

	x, y, z, radius, set, ok := srv.MobHome(elder)
	if !ok || !set {
		t.Fatalf("the elder has no home (ok=%v set=%v)", ok, set)
	}
	if x != 8 || y != 62 || z != -9 || radius != 16 {
		t.Errorf("the elder's home is (%d, %d, %d) radius %d, want (8, 62, -9) radius 16", x, y, z, radius)
	}
	if _, _, _, _, set, ok := srv.MobHome(plain); !ok || set {
		t.Errorf("the plain guardian's home is set=%v (ok=%v), want none", set, ok)
	}
}
