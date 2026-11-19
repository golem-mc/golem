package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob tests for status effects: /effect gives a mob an effect and the
// body runs its rhythm — poison bites on vanilla's twenty-five ticks — with
// the effect's particles reaching whoever tracks the mob in its metadata and
// no effect packet sent to anyone, a tired mob clearing or outliving its
// effects, the undead and the spiders refusing what they refuse, a wither
// and a venomous blow biting, a mob's effects saved to active_effects and
// standing up with the mob again, and a tipped arrow leaving its effect in
// the body it strikes.

// healthsWhileTicking drives n mob ticks a test at a time and collects the
// health the metadata told of the given body, in the order it was told.
func healthsWhileTicking(c *servertest.Client, srv *server.Server, entityID int32, ticks int) []float32 {
	c.T.Helper()
	var healths []float32
	bodies := map[int32]bool{entityID: true}
	for range ticks {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata {
				return
			}
			if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasHealth {
				healths = append(healths, m.Health)
			}
		})
	}
	return healths
}

// summonedNoAI stands a motionless mob of the kind before the reader, the
// way a summon with NoAI does. With midnight keeping the sun off it, a
// body that neither walks nor hunts is one whose health only an effect can
// move, which is what the effect tests want.
func summonedNoAI(c *servertest.Client, kind string, entityType int32, where string) servertest.SpawnedMob {
	c.T.Helper()
	c.ChatCommand("summon " + kind + " " + where + " {NoAI:1b}")
	return c.ExpectMobSpawn(entityType)
}

// TestEffectGivePoisonsAMobOnItsRhythm gives a poison to /effect's target
// and watches the body run it: the tracking player gets the effect
// particles in the mob's own metadata and no effect packet at all, and the
// poison bites a heart every twenty-five ticks, down to the one it leaves a
// body at and no further. The zombie given the same poison keeps none: an
// undead body refuses it, and the command answers with the give's failed.
func TestEffectGivePoisonsAMobOnItsRhythm(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Poisoner", servertest.OpsListing(t, "Poisoner"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	cowSpawn := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cowSpawn.EntityID != cow || cowSpawn.Health != 10 {
		t.Fatalf("the planted cow spawned as %+v (id %d), want the ten of health it was planted with", cowSpawn, cow)
	}
	zombie := summonedNoAI(c, "minecraft:zombie", protocol.EntityTypeZombie, "12.5 64 0.5")
	srv.StopTick()
	c.Settle()

	refused := c.Give("effect give @e[type=minecraft:zombie] minecraft:poison 10", zombie.EntityID)
	if !bytes.Contains(refused.Chat, []byte("commands.effect.give.failed")) {
		t.Errorf("giving poison to a zombie answered % X, want commands.effect.give.failed", refused.Chat)
	}
	if len(refused.Effects) != 0 || len(refused.Particles) != 0 {
		t.Errorf("the zombie that refused its poison was sent %d effect packets and %d particle metadata",
			len(refused.Effects), len(refused.Particles))
	}
	if got := healthsWhileTicking(c, srv, zombie.EntityID, 10); len(got) != 0 {
		t.Errorf("the zombie that refused its poison lost health %v over 10 ticks", got)
	}

	given := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 60", cow)
	if !bytes.Contains(given.Chat, []byte("commands.effect.give.success")) {
		t.Errorf("giving poison to a cow answered % X, want the give's success", given.Chat)
	}
	if len(given.Effects) != 0 {
		t.Errorf("the poisoned cow was sent %d effect packets, which a mob's riders alone would hear", len(given.Effects))
	}
	if got := given.Particles[cow]; len(got) != 1 || got[0] != servertest.PoisonParticles[0] {
		t.Errorf("the poisoned cow's particles = %v, want %v", got, servertest.PoisonParticles)
	}

	// The rhythm: a bite on the very tick the poison counts down from,
	// then one every twenty-five, to the single heart the poison leaves a
	// body at.
	healths := healthsWhileTicking(c, srv, cow, 225)
	if len(healths) != 9 {
		t.Fatalf("the poisoned cow's health over 225 ticks = %v, want nine bites", healths)
	}
	if healths[0] != 9 || healths[8] != 1 {
		t.Errorf("the poisoned cow's healths = %v, want it bitten from 10 down to 1", healths)
	}
	if more := healthsWhileTicking(c, srv, cow, 50); len(more) != 0 {
		t.Errorf("the poisoned cow at one heart kept losing health %v", more)
	}
}

// TestInstantHealthAndHarmTurnOnTheUndead checks the two instant effects
// against the minecraft:inverted_healing_and_harm the zombie carries: a
// health the zombie is given takes six off it as magic, a harm gives four
// back, and the regeneration it is also given never lands at all — the
// undead keep none of it.
func TestInstantHealthAndHarmTurnOnTheUndead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Alchemist", servertest.OpsListing(t, "Alchemist"))
	c.Settle()
	srv.SetDayTime(midnight)

	zombie := summonedNoAI(c, "minecraft:zombie", protocol.EntityTypeZombie, "4.5 64 0.5")
	srv.StopTick()
	c.Settle()

	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")
	zombieRefuses := func(w *servertest.EffectsWitness, effect string) {
		t.Helper()
		if !bytes.Contains(w.Chat, []byte("commands.effect.give.failed")) {
			t.Errorf("giving %s to the zombie answered % X, want commands.effect.give.failed", effect, w.Chat)
		}
		if len(w.Effects) != 0 {
			t.Errorf("the zombie that refused %s was sent %d effect packets", effect, len(w.Effects))
		}
	}
	for _, give := range []struct {
		effect   string
		refused  bool
		want     float32
		wantBite bool
	}{
		{"minecraft:regeneration", true, 20, false},
		{"minecraft:instant_health", false, 14, true},
		{"minecraft:instant_damage", false, 18, false},
	} {
		w := c.Give("effect give @e[type=minecraft:zombie] " + give.effect)
		if give.refused {
			zombieRefuses(w, give.effect)
			continue
		}
		if !bytes.Contains(w.Chat, []byte("commands.effect.give.success")) {
			t.Errorf("giving %s to the zombie answered % X, want the give's success", give.effect, w.Chat)
		}
		if len(w.Effects) != 0 {
			t.Errorf("giving %s to the zombie sent %d effect packets, which a mob never gets", give.effect, len(w.Effects))
		}
		var healths []float32
		bitten := false
		zombieBodies := map[int32]bool{zombie.EntityID: true}
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == zombie.EntityID && kind == magic {
					bitten = true
				}
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, zombieBodies); mine && m.HasHealth {
					healths = append(healths, m.Health)
				}
			}
		})
		if len(healths) != 1 || healths[0] != give.want {
			t.Errorf("%s left the zombie's health at %v, want %v", give.effect, healths, give.want)
		}
		if give.wantBite && !bitten {
			t.Error("the instant health the zombie took as a bite sent no magic damage")
		}
	}
}

// TestAMobsEffectIsSavedIntoItsFile gives a cow a poison that outlasts the
// save and reads the entity file back: the effect under active_effects in
// the shape vanilla's MobEffectInstance codec writes. A server started on
// the world again stands the cow up with the effect still on it, told to
// whoever joins to see it.
func TestAMobsEffectIsSavedIntoItsFile(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")
	c.Settle()

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()
	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 60 2", cow); len(w.Effects) != 0 {
		t.Fatalf("giving the cow a poison sent %d effect packets, which a mob never gets", len(w.Effects))
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		list, ok := nbt.Get[nbt.List](compound, "active_effects")
		if !ok || len(list.Items) != 1 {
			t.Fatalf("the saved cow's active_effects = %v, %v; want one effect", list, ok)
		}
		saved := list.Items[0].(nbt.Compound)
		if saved["id"] != nbt.String("minecraft:poison") || saved["amplifier"] != nbt.Byte(2) ||
			saved["duration"] != nbt.Int(1200) || saved["show_icon"] != nbt.Byte(1) || len(saved) != 4 {
			t.Errorf("the saved cow's effect = %v, want a poison III of 1200 ticks with its icon shown", saved)
		}
	})
	srv.Shutdown()

	// The cow stands back up with the poison still on it: whoever joins
	// is told of the effect beside the cow's own packets, and the cow's
	// swarm of poison particles rides its metadata in.
	_, connect = servertest.Start(t, cfg)
	c = connect()
	c.Login("Loader")
	c.Configure()
	var woken int32
	w := servertest.NewEffectsWitness()
	c.ReadJoin(func(packetID int32, data []byte) {
		w.NoteCow(c, packetID, data, &woken)
	})
	c.MarkLoaded()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		if woken == 0 {
			// The cow's pairing is still on its way; the settle
			// reads on until it arrives.
			c.SettleAndCollect(func(id int32, data []byte) { w.NoteCow(c, id, data, &woken) })
		}
		return woken != 0
	})
	if len(w.Effects) != 0 {
		t.Errorf("the woken cow was sent %d effect packets, which a mob never gets", len(w.Effects))
	}
	if got := w.Particles[woken]; len(got) != 1 || got[0] != servertest.PoisonParticles[0] {
		t.Errorf("the woken cow's particles = %v, want %v", got, servertest.PoisonParticles)
	}
}

// TestATippedArrowBitesItsEffectOntoAMob plants a tipped arrow from an
// entity file on a collision course with a planted cow and reads what the
// shaft leaves in the body: the effect the tip carries told to whoever
// sees the cow, and the poison's bite landing by the rhythm the body runs
// it on.
func TestATippedArrowBitesItsEffectOntoAMob(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		nbt.Compound{
			"id":                  nbt.String("minecraft:cow"),
			"UUID":                servertest.UuidWords(servertest.FixtureUUID(3, 1, 4, 1)),
			"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(64), nbt.Double(0.5)}},
			"Health":              nbt.Float(10),
			"NoAI":                nbt.Byte(1),
			"PersistenceRequired": nbt.Byte(1),
		},
		nbt.Compound{
			"id":   nbt.String("minecraft:arrow"),
			"UUID": servertest.UuidWords(servertest.FixtureUUID(5, 9, 2, 6)),
			"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(4.5), nbt.Double(64.7), nbt.Double(0.5),
			}},
			"Motion": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
				nbt.Double(3.0), nbt.Double(0.0), nbt.Double(0.0),
			}},
			"Rotation": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(0), nbt.Float(0)}},
			"pickup":   nbt.Byte(0),
			"item": nbt.Compound{
				"id":    nbt.String("minecraft:arrow"),
				"count": nbt.Int(1),
				"components": nbt.Compound{
					"minecraft:potion_contents": nbt.Compound{
						"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
							nbt.Compound{"id": nbt.String("minecraft:poison"), "duration": nbt.Int(600)},
						}},
					},
				},
			},
		},
	)

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Archer")
	c.Configure()
	var woken int32
	w := servertest.NewEffectsWitness()
	c.ReadJoin(func(packetID int32, data []byte) {
		w.NoteCow(c, packetID, data, &woken)
	})
	c.MarkLoaded()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		if woken == 0 || len(w.Particles[woken]) == 0 {
			// The cow and the shaft's effect are still on their
			// way; the settle reads on until both arrive.
			c.SettleAndCollect(func(id int32, data []byte) { w.NoteCow(c, id, data, &woken) })
		}
		return len(w.Particles[woken]) > 0
	})
	if len(w.Effects) != 0 {
		t.Errorf("the tipped arrow's poison was sent to the cow's trackers in %d effect packets, which a mob never gets", len(w.Effects))
	}
	if got := w.Particles[woken]; len(got) != 1 || got[0] != servertest.PoisonParticles[0] {
		t.Errorf("the poisoned cow's particles = %v, want %v", got, servertest.PoisonParticles)
	}

	// The poison the tip left bites on the body's own rhythm: the magic
	// damage event and the health it tells, once the arrow's own
	// invulnerability window has let the bite through.
	srv.StopTick()
	c.Settle()
	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")
	biteWindow := map[int32]bool{woken: true}
	var bitten, told int
	for range 50 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == woken && kind == magic {
					bitten++
				}
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, biteWindow); mine && m.HasHealth {
					told++
				}
			}
		})
	}
	if bitten == 0 || told == 0 {
		t.Errorf("the poison the arrow left bit %d times and told %d healths, want the tip's bite to land", bitten, told)
	}
}

// TestEffectGiveRefusesASpidersPoison checks Spider.canBeAffected over the
// command: neither the spider nor the cave spider that extends it takes
// poison, each answering the give's failed with no packet or metadata sent,
// while a cow beside them does take it.
func TestEffectGiveRefusesASpidersPoison(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Arachnid", servertest.OpsListing(t, "Arachnid"))
	c.Settle()
	srv.SetDayTime(midnight)
	// Held before the spawns: a live tick lets the spider close the four
	// blocks and bite the player while the next spawn is waited for.
	servertest.HoldTicks(t, srv)

	caveSpiderType, _ := registry.EntityTypeID("minecraft:cave_spider")
	spider := srv.SpawnMob("minecraft:spider", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeSpider)
	caveSpider := srv.SpawnMob("minecraft:cave_spider", 8.5, 64, 0.5)
	c.ExpectMobSpawn(caveSpiderType)
	cow := srv.SpawnMob("minecraft:cow", 12.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Settle()

	for _, kind := range []struct {
		name string
		id   int32
		typ  string
	}{{"spider", spider, "minecraft:spider"}, {"cave spider", caveSpider, "minecraft:cave_spider"}} {
		w := c.Give("effect give @e[type="+kind.typ+"] minecraft:poison 10", kind.id)
		if !bytes.Contains(w.Chat, []byte("commands.effect.give.failed")) {
			t.Errorf("giving poison to the %s answered % X, want commands.effect.give.failed", kind.name, w.Chat)
		}
		if len(w.Effects) != 0 || len(w.Particles) != 0 {
			t.Errorf("the %s that refused its poison was sent %d effect packets and %d particle metadata",
				kind.name, len(w.Effects), len(w.Particles))
		}
	}
	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 10", cow); len(w.Particles[cow]) == 0 {
		t.Error("the cow beside the spiders refused its poison")
	}
}

// TestEffectClearTakesAMobsEffects gives a cow a poison and clears it, once
// by name and once whole: each clear answers success and the cow's metadata
// comes back with the particles cleared and no effect packet sent.
func TestEffectClearTakesAMobsEffects(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Purge", servertest.OpsListing(t, "Purge"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 600", cow); len(w.Particles[cow]) == 0 {
		t.Fatal("the cow never took the poison")
	}
	byName := c.Give("effect clear @e[type=minecraft:cow] minecraft:poison", cow)
	if !bytes.Contains(byName.Chat, []byte("commands.effect.clear.specific.success")) {
		t.Errorf("clearing the cow's poison answered % X, want the specific clear's success", byName.Chat)
	}
	if len(byName.Effects) != 0 {
		t.Errorf("clearing the cow's poison sent %d effect packets, which a mob never gets", len(byName.Effects))
	}
	if got := byName.Particles[cow]; len(got) != 0 {
		t.Errorf("the cleared cow still shows particles %v", got)
	}

	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 600", cow); len(w.Particles[cow]) == 0 {
		t.Fatal("the cow never took the poison again")
	}
	everything := c.Give("effect clear @e[type=minecraft:cow]", cow)
	if !bytes.Contains(everything.Chat, []byte("commands.effect.clear.everything.success")) {
		t.Errorf("clearing everything on the cow answered % X, want the everything clear's success", everything.Chat)
	}
	if got := everything.Particles[cow]; len(got) != 0 {
		t.Errorf("the fully cleared cow still shows particles %v", got)
	}
}

// TestAMobsEffectExpires gives a cow a two-second poison, runs it out, and
// watches the expiry: one bite on the way, then the effect ends with the
// particles cleared and its further ticks gone.
func TestAMobsEffectExpires(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Timer", servertest.OpsListing(t, "Timer"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:poison 2", cow); len(w.Particles[cow]) == 0 {
		t.Fatal("the cow never took the poison")
	}
	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")
	bodies := map[int32]bool{cow: true}
	var bites int
	cleared, poisoned := false, true
	for range 41 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cow && kind == magic {
					bites++
				}
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles {
					poisoned = poisoned && len(m.Particles) > 0
					if len(m.Particles) == 0 {
						cleared = true
					}
				}
			}
		})
	}
	if bites != 1 {
		t.Errorf("the two-second poison bit %d times, want the one its twenty-five-tick rhythm allows", bites)
	}
	if poisoned {
		t.Error("the poison was still showing particles after it ran out")
	}
	if !cleared {
		t.Error("the expired poison's particles were never cleared")
	}
}

// TestAWitherBiteTakesAMobsHealth withers a cow and watches its own rhythm:
// a bite of the wither damage type every forty ticks, with no single-heart
// floor the poison leaves a body at.
func TestAWitherBiteTakesAMobsHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Witherer", servertest.OpsListing(t, "Witherer"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if w := c.Give("effect give @e[type=minecraft:cow] minecraft:wither 5", cow); len(w.Particles[cow]) == 0 {
		t.Fatal("the cow never took the wither")
	}
	wither := registry.MustID("minecraft:damage_type", "minecraft:wither")
	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")
	bodies := map[int32]bool{cow: true}
	var withers, magics int
	var healths []float32
	for range 100 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				victim, kind := r.ReadVarInt(), r.ReadVarInt()
				if victim != cow {
					return
				}
				if kind == wither {
					withers++
				}
				if kind == magic {
					magics++
				}
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasHealth {
					healths = append(healths, m.Health)
				}
			}
		})
	}
	if withers != 2 || magics != 0 {
		t.Errorf("the wither bit %d times as wither and %d as magic, want two wither bites alone", withers, magics)
	}
	if len(healths) == 0 || healths[len(healths)-1] != 8 {
		t.Errorf("the withered cow's healths = %v, want it bitten from 10 down to 8", healths)
	}
}

// TestAWitherRoseWithersAMob stands a cow in a wither rose and watches the
// flower plant its own wither: the effect's particles on the body's metadata
// and the wither damage its rhythm lands, with no /effect command behind it.
// The flower plants after the body's effects tick, so the effect appears a
// tick before its first bite.
func TestAWitherRoseWithersAMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()
	if !srv.SetMobNoAI(cow) {
		t.Fatal("the cow could not be held in the flower")
	}

	rose := servertest.BlockState(t, "minecraft:wither_rose")
	if !srv.SetBlock(4, 64, 0, rose) {
		t.Fatal("the wither rose's chunk was not loaded")
	}

	wither := registry.MustID("minecraft:damage_type", "minecraft:wither")
	bodies := map[int32]bool{cow: true}
	var withered bool
	var bites int
	step := func() {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles && len(m.Particles) > 0 {
					withered = true
				}
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				victim, kind := r.ReadVarInt(), r.ReadVarInt()
				if victim == cow && kind == wither {
					bites++
				}
			}
		})
	}

	step()
	if !withered {
		t.Error("the cow standing in the wither rose never took the wither")
	}
	if bites != 0 {
		t.Errorf("the wither bit the cow %d times on the tick it was planted, want the bite a tick later", bites)
	}

	step()
	if bites != 1 {
		t.Errorf("the wither bit the cow %d times over two ticks, want the first bite a tick after it was planted", bites)
	}
}

// TestAWitherRoseSparesAnInvulnerableMob checks the flower's gate on the mob
// path: a body invulnerable to the wither damage takes neither the flower's
// wither nor its bite, though it stands in it.
func TestAWitherRoseSparesAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Botanist", servertest.OpsListing(t, "Botanist"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()
	if !srv.SetMobNoAI(cow) {
		t.Fatal("the cow could not be held in the flower")
	}
	if !srv.SetMobInvulnerable(cow) {
		t.Fatal("the cow could not be made invulnerable")
	}

	rose := servertest.BlockState(t, "minecraft:wither_rose")
	if !srv.SetBlock(4, 64, 0, rose) {
		t.Fatal("the wither rose's chunk was not loaded")
	}

	wither := registry.MustID("minecraft:damage_type", "minecraft:wither")
	bodies := map[int32]bool{cow: true}
	var withered bool
	var bites int
	for range 5 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityMetadata:
				if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles && len(m.Particles) > 0 {
					withered = true
				}
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				victim, kind := r.ReadVarInt(), r.ReadVarInt()
				if victim == cow && kind == wither {
					bites++
				}
			}
		})
	}
	if withered {
		t.Error("the invulnerable cow standing in the wither rose took the wither")
	}
	if bites != 0 {
		t.Errorf("the wither the rose planted bit the invulnerable cow %d times, want none", bites)
	}
}

// TestAVenomousBlowPoisonsAMob turns a cave spider on a cow with the cow's
// own arrow, then walks the bite over the wire: the spider's melee leaves
// its poison as the cow's particles, and a poison tick lands as a magic
// damage the body's own rhythm says.
func TestAVenomousBlowPoisonsAMob(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(rules.DifficultyNormal)
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn

	c := connect().Join("Herder")

	caveSpiderType, _ := registry.EntityTypeID("minecraft:cave_spider")
	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	caveSpider := srv.SpawnMob("minecraft:cave_spider", 6.0, 64, 0.5)
	c.ExpectMobSpawn(caveSpiderType)
	srv.SetMobNoAI(cow)

	if arrowID := srv.ShootMobArrowAt(cow, caveSpider); arrowID == 0 {
		t.Fatal("the cow's arrow was never laid in the world")
	}
	servertest.UntilTicks(t, srv, 120, "condition", func() bool { return srv.MobTargetID(caveSpider) == cow })

	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")
	bodies := map[int32]bool{cow: true}
	poisoned, bitten := false, false
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles && len(m.Particles) > 0 {
				poisoned = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cow && kind == magic {
				bitten = true
			}
		}
		return poisoned && bitten
	})
	if !poisoned {
		t.Error("the cave spider's venomous blow left no poison on the cow")
	}
	if !bitten {
		t.Error("the poison the blow left never bit")
	}
}

// mobFlagsOnTick drives one mob tick and returns the shared flags byte that
// tick's metadata pass carried for the entity, and whether it carried one.
func mobFlagsOnTick(c *servertest.Client, srv *server.Server, entityID int32) (uint8, bool) {
	c.T.Helper()
	srv.TickMobs(1)
	var flags uint8
	var found bool
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return
		}
		if f, ok := servertest.ReadEntityFlags(r); ok {
			flags, found = f, true
		}
	})
	return flags, found
}

// TestInvisibilityAndGlowingFlagsReachAMobsTrackers gives a cow the
// invisibility and the glowing effect over the command and reads the shared
// flags each change tells: 0x20 once the invisibility is on, the 0x40 added
// with the glowing, each bit cleared again by the clear.
func TestInvisibilityAndGlowingFlagsReachAMobsTrackers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Vanisher", servertest.OpsListing(t, "Vanisher"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:invisibility 600", cow); !ok ||
		got&protocol.EntityFlagInvisible == 0 {
		t.Errorf("the cow's flags from the invisibility = %#02x (told %v), want the invisible bit", got, ok)
	}
	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow); !ok ||
		got&protocol.EntityFlagInvisible == 0 || got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("the cow's flags from the glowing = %#02x (told %v), want both bits", got, ok)
	}

	if got, ok := c.MobFlagsFromTell("effect clear @e[type=minecraft:cow] minecraft:invisibility", cow); !ok ||
		got&protocol.EntityFlagInvisible != 0 {
		t.Errorf("the cow's flags after clearing invisibility = %#02x (told %v), want the invisible bit clear", got, ok)
	}
	if got, ok := c.MobFlagsFromTell("effect clear @e[type=minecraft:cow] minecraft:glowing", cow); !ok ||
		got&protocol.EntityFlagGlowing != 0 {
		t.Errorf("the cow's flags after clearing glowing = %#02x (told %v), want the glowing bit clear", got, ok)
	}
}

// TestMobInvisibilityAndGlowingFlagsOnALateJoin gives a cow both effects and
// joins a watcher after them: the spawn metadata that watcher's join carries
// for the cow already has the invisible and glowing bits set.
func TestMobInvisibilityAndGlowingFlagsOnALateJoin(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Vanisher"))

	c := connect().Join("Vanisher")
	c.Settle()

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Give("effect give @e[type=minecraft:cow] minecraft:invisibility 600", cow)
	c.Give("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow)

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	if got := watcher.EnterWorldSeeingMobFlags(cow); got&protocol.EntityFlagInvisible == 0 ||
		got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("the late watcher's spawn flags = %#02x for the cow, want the invisible and glowing bits", got)
	}
}

// TestMobGlowingFlagRidesTheEffectChange gives a cow the glowing effect and
// reads the shared flags from the metadata that effect change sent, with the
// mobs not ticked at all: the 0x40 bit is told with the change, so a client
// draws the outline without waiting on the next tick's metadata pass.
func TestMobGlowingFlagRidesTheEffectChange(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Lantern", servertest.OpsListing(t, "Lantern"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow); !ok ||
		got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("the cow's flags from the give = %#02x (told %v), want the glowing bit", got, ok)
	}
}

// TestMobGlowingFlagIsNotToldTwice gives a cow the glowing effect and reads
// the flags with the change, then ticks the mob: the tick's own metadata pass
// says nothing, because the tell moved the body's baseline with it.
func TestMobGlowingFlagIsNotToldTwice(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Twice", servertest.OpsListing(t, "Twice"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow); !ok ||
		got&protocol.EntityFlagGlowing == 0 {
		t.Fatalf("the cow's flags from the give = %#02x (told %v), want the glowing bit", got, ok)
	}
	if got, ok := mobFlagsOnTick(c, srv, cow); ok {
		t.Errorf("the tick after the give told the flags again as %#02x, want no second flags packet", got)
	}
}

// TestMobBurningFlagsKeptWhenGlowingGiven lights a cow and gives it glowing:
// the change tells the whole shared byte, 0x41, so the on-fire bit it already
// carried is not dropped.
func TestMobBurningFlagsKeptWhenGlowingGiven(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Ember", servertest.OpsListing(t, "Ember"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	srv.SetMobFire(cow, 600)
	want := uint8(protocol.EntityFlagOnFire | protocol.EntityFlagGlowing)
	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow); !ok || got != want {
		t.Errorf("the burning cow's flags from the give = %#02x (told %v), want %#02x", got, ok, want)
	}
}

// TestMobGlowingFlagClearsWhenTheEffectExpires runs a one-second glowing out
// on a cow and watches the bit clear on its own: once the change gave the bit
// and moved the baseline, the tick that ends the effect tells it clear in
// exactly one more flags packet.
func TestMobGlowingFlagClearsWhenTheEffectExpires(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Lamp", servertest.OpsListing(t, "Lamp"))
	c.Settle()
	srv.SetDayTime(midnight)

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 1", cow); !ok ||
		got&protocol.EntityFlagGlowing == 0 {
		t.Fatalf("the cow's flags from the give = %#02x (told %v), want the glowing bit", got, ok)
	}
	var clears int
	var last uint8
	for range 25 {
		if f, ok := mobFlagsOnTick(c, srv, cow); ok {
			clears++
			last = f
		}
	}
	if last&protocol.EntityFlagGlowing != 0 {
		t.Errorf("the cow's flags = %#02x after the glowing ran out, want the glowing bit clear", last)
	}
	if clears != 1 {
		t.Errorf("the glowing's expiry told the flags in %d packets, want exactly one", clears)
	}
}

// TestMobGlowingTagOutlivesTheEffectAndIsSaved summons a cow with Glowing:1b
// and checks the bit reaches a late watcher, stays set when the Glowing
// effect is given and cleared, and is written to the entity file.
func TestMobGlowingTagOutlivesTheEffectAndIsSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Tagger"))
	c := connect().Join("Tagger")
	c.Settle()

	c.ChatCommand("summon minecraft:cow 4.5 64 0.5 {Glowing:1b,NoAI:1b}")
	spawned := c.ExpectMobSpawn(protocol.EntityTypeCow)
	cow := spawned.EntityID
	if spawned.BaseFlags&protocol.EntityFlagGlowing == 0 {
		t.Errorf("the summoned cow's spawn flags = %#02x, want the glowing bit", spawned.BaseFlags)
	}
	srv.StopTick()
	c.Settle()

	if got, ok := c.MobFlagsFromTell("effect give @e[type=minecraft:cow] minecraft:glowing 600", cow); ok &&
		got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("flags with tag and effect = %#02x, want the glowing bit", got)
	}
	if got, ok := c.MobFlagsFromTell("effect clear @e[type=minecraft:cow] minecraft:glowing", cow); ok &&
		got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("flags told after clearing the effect = %#02x, want the glowing bit kept by the tag", got)
	}

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	if got := watcher.EnterWorldSeeingMobFlags(cow); got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("the late watcher's spawn flags = %#02x, want the glowing bit", got)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if compound["Glowing"] != nbt.Byte(1) {
			t.Errorf("the saved cow's Glowing = %v, want 1b", compound["Glowing"])
		}
	})
}

// TestAbsorptionOnAMobIsNeverTold gives a zombie Absorption II and checks no
// index-17 entry is sent (on a mob that index is another field and the
// client kicks on the type mismatch), then that the hearts are saved.
// vanilla: Player.DATA_PLAYER_ABSORPTION_ID, LivingEntity.addAdditionalSaveData
func TestAbsorptionOnAMobIsNeverTold(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Padder", servertest.OpsListing(t, "Padder"))
	c.Settle()
	srv.SetDayTime(midnight)

	zombie := summonedNoAI(c, "minecraft:zombie", protocol.EntityTypeZombie, "12.5 64 0.5")
	srv.StopTick()
	c.Settle()

	bodies := map[int32]bool{zombie.EntityID: true}
	told, seen := 0, 0
	collect := func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine {
			seen++
			if m.HasAbsorb {
				told++
			}
		}
	}
	c.ChatCommand("effect give @e[type=minecraft:zombie] minecraft:absorption 600 1")
	srv.TickMobs(1)
	c.SettleAndCollect(collect)
	if seen == 0 {
		t.Fatal("no metadata told for the effect, so the check proved nothing")
	}
	if told != 0 {
		t.Errorf("%d absorption entries told for a mob, want none", told)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:zombie", func(compound nbt.Compound) {
		if compound["AbsorptionAmount"] != nbt.Float(8) {
			t.Errorf("the saved zombie's AbsorptionAmount = %v, want 8f", compound["AbsorptionAmount"])
		}
	})
}

// TestAbsorptionLeavesAMobWithItsHearts gives a zombie Absorption I for
// 600 seconds and hits it for five: the effect goes on the next effect tick
// rather than running out its time. Vanilla sends a mob's Remove Mob Effect
// only to a player riding it, so the trackers see it in the particles
// clearing, and /data no longer lists it.
// vanilla: AbsorptionMobEffect.applyEffectTick, LivingEntity.onEffectsRemoved
func TestAbsorptionLeavesAMobWithItsHearts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Spender", servertest.OpsListing(t, "Spender"))
	c.Settle()
	srv.SetDayTime(midnight)

	zombie := summonedNoAI(c, "minecraft:zombie", protocol.EntityTypeZombie, "12.5 64 0.5")
	srv.StopTick()
	c.Settle()

	if w := c.Give("effect give @e[type=minecraft:zombie] minecraft:absorption 600 0", zombie.EntityID); len(w.Particles[zombie.EntityID]) == 0 {
		t.Fatal("the zombie never took the absorption")
	}
	srv.TickMobs(1)
	c.Settle()
	if _, ok := nbt.Get[nbt.List](entityCompound(t, c, "@e[type=minecraft:zombie,limit=1]"), "active_effects"); !ok {
		t.Fatal("/data lists no active_effects before the hit, so the check proves nothing")
	}

	c.ChatCommand("damage @e[type=minecraft:zombie,limit=1] 5 minecraft:magic")
	c.Settle()
	bodies := map[int32]bool{zombie.EntityID: true}
	cleared := false
	for range 2 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata {
				return
			}
			if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles && len(m.Particles) == 0 {
				cleared = true
			}
		})
	}
	if !cleared {
		t.Error("the spent absorption's particles were not cleared within 2 ticks")
	}
	if list, ok := nbt.Get[nbt.List](entityCompound(t, c, "@e[type=minecraft:zombie,limit=1]"), "active_effects"); ok {
		t.Errorf("/data still lists active_effects %v after the extra hearts were spent", list)
	}
}
