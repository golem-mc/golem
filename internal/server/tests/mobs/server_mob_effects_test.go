package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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

// TestExtendingColoredMobEffectResendsParticles checks that extending a cow's
// sole visible Speed effect at the same amplifier resends its colored particle
// metadata (both through /effect and through /data merge), without repeating on
// the next tick, while extending a fixed-particle effect (infested) does not.
// vanilla: LivingEntity.updateSynchronizedMobEffectParticles, MobEffect.createParticleOptions
func TestExtendingColoredMobEffectResendsParticles(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	c.Settle()

	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.StopTick()
	c.Settle()

	wantSpeed := [2]int32{protocol.ParticleEntityEffect, -0x1000000 | int32(registry.MobEffectOf(registry.MobEffectSpeed).Color)}
	first := c.Give("effect give @e[type=minecraft:cow] minecraft:speed 30 0", cow)
	if got := first.Particles[cow]; len(got) != 1 || got[0] != wantSpeed {
		t.Fatalf("initial speed particles = %v, want [%v]", got, wantSpeed)
	}

	extended := c.Give("effect give @e[type=minecraft:cow] minecraft:speed 60 0", cow)
	if got := extended.Particles[cow]; len(got) != 1 || got[0] != wantSpeed {
		t.Errorf("extended speed particles = %v, want [%v]", got, wantSpeed)
	}

	// Advancing a tick must not repeat the already-sent particle metadata.
	bodies := map[int32]bool{cow: true}
	var afterTick [][][2]int32
	srv.TickMobs(1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles {
			afterTick = append(afterTick, m.Particles)
		}
	})
	if len(afterTick) != 0 {
		t.Errorf("next tick resent particles = %v, want none", afterTick)
	}

	// A /data merge rewriting active_effects goes through the tracked metadata delta.
	var merged [][][2]int32
	c.ChatCommand(`data merge entity @e[type=minecraft:cow,limit=1] {active_effects:[{id:"minecraft:speed",duration:2400,amplifier:0b}]}`)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasParticles {
			merged = append(merged, m.Particles)
		}
	})
	if len(merged) != 1 || len(merged[0]) != 1 || merged[0][0] != wantSpeed {
		t.Errorf("data merge speed particles = %v, want [[%v]]", merged, wantSpeed)
	}

	// A fixed-particle effect (SimpleParticleType singleton in vanilla) does not
	// resend when refreshed with the same particle list.
	c.Give("effect clear @e[type=minecraft:cow]", cow)
	wantInfested := [2]int32{protocol.ParticleInfested, 0}
	infested := c.Give("effect give @e[type=minecraft:cow] minecraft:infested 30 0", cow)
	if got := infested.Particles[cow]; len(got) != 1 || got[0] != wantInfested {
		t.Fatalf("initial infested particles = %v, want [%v]", got, wantInfested)
	}
	refreshedInfested := c.Give("effect give @e[type=minecraft:cow] minecraft:infested 60 0", cow)
	if got := refreshedInfested.Particles[cow]; len(got) != 0 {
		t.Errorf("extended infested particles = %v, want none", got)
	}
}
