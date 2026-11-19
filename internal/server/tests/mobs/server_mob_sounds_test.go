package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mob voice on the wire: the sound variant a farm animal's finalize
// chose decides its ambient, hurt and death events, a wound and a death
// speak at getSoundVolume and getVoicePitch, a sound reaches sixteen blocks
// and no farther, and the Silent tag holds a body's voice in.

// TestAPlantedMoodyCowSpeaksItsVariantVoice plants a cow saved with the
// moody sound variant — what a vanilla world holds — and hears all three of
// its voices: the idle mutter, the fire's hurt and the death, each the
// variant's event, never the classic cow's.
func TestAPlantedMoodyCowSpeaksItsVariantVoice(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(3, 1, 4, 1)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:cow"),
		"UUID": servertest.UuidWords(id),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5),
		}},
		"Health":        nbt.Float(10),
		"NoAI":          nbt.Byte(1),
		"variant":       nbt.String("minecraft:temperate"),
		"sound_variant": nbt.String("minecraft:moody"),
	})

	srv, connect := servertest.Start(t, cfg)
	// Midnight, so the cow the test sets burning is the fire's and not the
	// sun's.
	srv.SetDayTime(18000)
	c := connect()
	c.Login("Herder")
	c.Configure()
	c.JoinSeeingMobs("Herder", id)
	srv.StopTick()

	// The idle voice, before any fire: the moody mutter and never the
	// classic one.
	moaned := false
	for tick := 0; tick < servertest.AmbientTicks && !moaned; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
			if packetID != protocol.IDPlaySoundEffect {
				return false
			}
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			switch name {
			case "minecraft:entity.cow_moody.ambient":
				moaned = true
			case "minecraft:entity.cow.ambient":
				t.Error("the moody cow moaned the classic voice")
			}
			return false
		})
	}
	if !moaned {
		t.Fatalf("the moody cow never moaned within %d ticks", servertest.AmbientTicks)
	}

	// The fire's voice: the wound as it lands and the death at the end.
	if !srv.SetBlock(4, 64, 0, servertest.BlockState(t, "minecraft:fire")) {
		t.Fatal("the fire's chunk is not loaded")
	}
	hurt, died := false, false
	for tick := 0; tick < 300 && !died; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
			if packetID != protocol.IDPlaySoundEffect {
				return false
			}
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			switch name {
			case "minecraft:entity.cow_moody.hurt":
				hurt = true
			case "minecraft:entity.cow_moody.death":
				died = true
			case "minecraft:entity.cow.hurt", "minecraft:entity.cow.death":
				t.Errorf("the moody cow spoke the classic voice: %s", name)
			}
			return false
		})
	}
	if !hurt || !died {
		t.Errorf("the moody cow hurt %v and died %v, want its variant's hurt and death", hurt, died)
	}
}

// TestACowsHurtAndDeathPlayAtItsQuietVoice burns a classic cow and holds
// the fire's hurt and the killing death to AbstractCow.getSoundVolume's 0.4
// and the pitch getVoicePitch rolls, never the fixed one and one.
func TestACowsHurtAndDeathPlayAtItsQuietVoice(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(9, 3, 9, 3)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:cow"),
		"UUID": servertest.UuidWords(id),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5),
		}},
		"Health":        nbt.Float(10),
		"NoAI":          nbt.Byte(1),
		"sound_variant": nbt.String("minecraft:classic"),
	})

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	c := connect()
	c.Login("Herder")
	c.Configure()
	c.JoinSeeingMobs("Herder", id)
	srv.StopTick()
	if !srv.SetBlock(4, 64, 0, servertest.BlockState(t, "minecraft:fire")) {
		t.Fatal("the fire's chunk is not loaded")
	}

	hurt, died := false, false
	for tick := 0; tick < 300 && !died; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
			if packetID != protocol.IDPlaySoundEffect {
				return false
			}
			name, _, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			switch name {
			case "minecraft:entity.cow.hurt", "minecraft:entity.cow_moody.hurt":
				hurt = true
				checkCowVoice(t, "hurt", volume, pitch)
			case "minecraft:entity.cow.death", "minecraft:entity.cow_moody.death":
				died = true
				checkCowVoice(t, "death", volume, pitch)
			}
			return false
		})
	}
	if !hurt || !died {
		t.Fatalf("the cow hurt %v and died %v, want the fire to land both", hurt, died)
	}
}

// checkCowVoice holds one cow voice to the kind's quiet volume and the
// voice's rolled pitch.
func checkCowVoice(t *testing.T, what string, volume, pitch float32) {
	t.Helper()
	if volume != 0.4 {
		t.Errorf("the cow's %s played at volume %v, want AbstractCow.getSoundVolume's 0.4", what, volume)
	}
	if pitch == 1.0 {
		t.Errorf("the cow's %s played at the fixed pitch 1, want getVoicePitch's rolled one", what)
	}
	if pitch < 0.79 || pitch > 1.21 {
		t.Errorf("the cow's %s played at pitch %v, want the voice's spread around one", what, pitch)
	}
}

// TestAMobsSoundDoesNotReachPastSixteenBlocks puts one player beside the
// cow and another two and a half chunks off, kills the cow at the near one,
// and holds the death sound to PlayerList.broadcast's earshot: the near
// player hears it, the far one does not.
func TestAMobsSoundDoesNotReachPastSixteenBlocks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Near"))
	near := connect().Join("Near")
	far := connect().Join("Far")

	// Well past the sixteen blocks a sound reaches; the settle puts the
	// step where the server reads it.
	far.MoveTo(40.5, 64, 0.5, 0, 0)
	far.Settle()
	near.Settle()

	// A cow born at the near player and killed the same command, so the
	// death sound plays a couple of blocks off that player.
	near.ChatCommand("execute summon minecraft:cow run kill @s")

	soundsOnly := func(id int32) bool { return id != protocol.IDPlaySoundEffect }
	nearHeard := pumpTicks(near, srv, 40, soundsOnly, func(_ int32, data []byte) bool {
		name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		return strings.HasPrefix(name, "minecraft:entity.cow") && strings.HasSuffix(name, ".death")
	})
	farHeard := pumpTicks(far, srv, 40, soundsOnly, func(_ int32, data []byte) bool {
		name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
		return strings.HasPrefix(name, "minecraft:entity.cow")
	})
	if !nearHeard {
		t.Error("the cow's death sound never reached the player who killed it")
	}
	if farHeard {
		t.Error("the cow's sound reached a player more than sixteen blocks off")
	}
}

// TestASilentMobMakesNoSoundAndKeepsItsTag plants a cow tagged Silent and
// holds the tag's two halves: the idle voice never plays however long the
// body lives, and the flag is written back to the entity file.
func TestASilentMobMakesNoSoundAndKeepsItsTag(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(6, 2, 6, 2)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:cow"),
		"UUID": servertest.UuidWords(id),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5),
		}},
		"Health": nbt.Float(10),
		"NoAI":   nbt.Byte(1),
		"Silent": nbt.Byte(1),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Quiet")
	c.Configure()
	c.JoinSeeingMobs("Quiet", id)
	srv.StopTick()

	for tick := 0; tick < servertest.AmbientTicks; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
			if packetID != protocol.IDPlaySoundEffect {
				return false
			}
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); strings.HasPrefix(name, "minecraft:entity.cow") {
				t.Fatalf("a Silent cow spoke %s", name)
			}
			return false
		})
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if silent, _ := nbt.Get[nbt.Byte](compound, "Silent"); silent != 1 {
			t.Errorf("the Silent cow saved Silent = %v, want 1", compound["Silent"])
		}
	})
}

// The Silent tag on the paths the ambient test does not take: a player's
// swing, an arrow, a thrown thing, the fire's bite and the burn it leaves, a
// landed fall and the death any of them brings. Each reaches the wire through
// a different wrapper — broadcastVoice for the blows, mobVoiceSend for the
// body's own hurts and death, the lava's own burn — so each gets a silent body
// of its own.

// TestASilentMobStruckByAPlayerSpeaksNothing swings a sword at a Silent cow
// and holds the hurt to silence: the blow lands — the damage event names the
// cow — but no cow voice rides it, the broadcastVoice path a player's own
// swing takes.
func TestASilentMobStruckByAPlayerSpeaksNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Striker")
	c.MoveToGround(2.5, 64, 2.5, true)
	c.Settle()

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the item registry has no diamond sword")
	}
	srv.GiveSlot("Striker", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	servertest.AwaitFullSwing(t, srv, "Striker")

	cowID := srv.SpawnMob("minecraft:cow", 4.5, 64, 2.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.SetMobSilent(cowID, true) {
		t.Fatal("the spawned cow is nowhere to silence")
	}
	c.Attack(cowID)

	hurt, voices := false, []string{}
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == cowID {
				hurt = true
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); strings.HasPrefix(name, "minecraft:entity.cow") {
				voices = append(voices, name)
			}
		}
	})
	if !hurt {
		t.Error("the silent cow's hurt never landed, want the blow to tell of it")
	}
	if len(voices) > 0 {
		t.Errorf("the silent cow spoke %v, want no voice from a Silent body", voices)
	}
}

// TestASilentMobStruckByAnArrowSpeaksNothing flies a skeleton's arrow into a
// Silent cow and holds the wound to silence: the arrow lands — the damage
// event names the cow — but no cow voice rides it.
func TestASilentMobStruckByAnArrowSpeaksNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Archer")

	skeletonID := srv.SpawnMob("minecraft:skeleton", 6.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeSkeleton)
	cowID := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.SetMobSilent(cowID, true) {
		t.Fatal("the spawned cow is nowhere to silence")
	}
	if arrowID := srv.ShootMobArrowAt(skeletonID, cowID); arrowID == 0 {
		t.Fatal("the arrow was never laid in the world")
	}

	hurt, voices := false, []string{}
	pumpTicks(c, srv, 60, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == cowID {
				hurt = true
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); strings.HasPrefix(name, "minecraft:entity.cow") {
				voices = append(voices, name)
			}
		}
		return false
	})
	if !hurt {
		t.Error("the silent cow's arrow hurt never landed, want the shaft to tell of it")
	}
	if len(voices) > 0 {
		t.Errorf("the silent cow spoke %v, want no voice from a Silent body", voices)
	}
}

// TestASilentMobStruckByAThrownItemSpeaksNothing throws a snowball into a
// Silent cow and holds the knock to silence: the thrown blow lands — the
// damage event names the cow — but no cow voice rides it.
func TestASilentMobStruckByAThrownItemSpeaksNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Thrower")

	cowID := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.SetMobSilent(cowID, true) {
		t.Fatal("the spawned cow is nowhere to silence")
	}
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 16})

	c.MoveTo(0.5, 64, 0.5, -90, 25)
	// The cow pinned where the aim points, the stroll its own mind wanders on
	// unable to walk it out of the way of a ball that takes two ticks across.
	srv.MoveMob(cowID, 2.5, 0.5)
	servertest.UseHeld(c, -90, 25)

	traffic := servertest.ReadThrownTraffic(c, srv, 80)
	if len(traffic.DamageEventsOf(cowID)) == 0 {
		t.Error("the silent cow's thrown hurt never landed, want the ball to tell of it")
	}
	var voices []string
	for _, s := range traffic.Sounds {
		if strings.HasPrefix(s.Name, "minecraft:entity.cow") {
			voices = append(voices, s.Name)
		}
	}
	if len(voices) > 0 {
		t.Errorf("the silent cow spoke %v, want no voice from a Silent body", voices)
	}
}

// TestASilentMobBurntAndKilledSpeaksNothing stands a Silent NoAI cow in a
// pool of lava with the loop stopped and the ticks driven by hand until the
// burn kills it: every bite lands — the lava damage events name the cow and
// the corpse is taken away — but neither the lava's generic burn beside the
// bite, nor the cow's own hurts, nor its death sound reaches the wire.
func TestASilentMobBurntAndKilledSpeaksNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Scalded", servertest.OpsListing(t, "Scalded"))

	// The pool: a stone floor at y=63 with its centre cell holding the lava
	// the cow is to stand in.
	stone := servertest.BlockState(t, "minecraft:stone")
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	srv.SetBlock(4, 64, 4, lava)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.SetMobSilent(cow.EntityID, true) {
		t.Fatal("the summoned cow is nowhere to silence")
	}

	lavaID := registry.MustID("minecraft:damage_type", "minecraft:lava")
	hurts := []int32{}
	voices := []string{}
	burned := false
	gone := false
	for tick := 0; tick < 300 && !gone; tick++ {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cow.EntityID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlaySoundEffect:
				name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
				if name == "minecraft:entity.generic.burn" {
					burned = true
				}
				if strings.HasPrefix(name, "minecraft:entity.cow") {
					voices = append(voices, name)
				}
			case protocol.IDPlayRemoveEntities:
				r := protocol.NewReader(data)
				for n := r.ReadVarInt(); n > 0; n-- {
					if r.ReadVarInt() == cow.EntityID {
						gone = true
					}
				}
			}
		})
	}
	if len(hurts) == 0 || hurts[0] != lavaID {
		t.Errorf("the pool told %v of the silent cow's burns, want the lava's own bite", hurts)
	}
	if !gone {
		t.Error("the silent cow outlived the pool it stood in, want the burn to kill it")
	}
	if burned {
		t.Error("the silent cow's lava bite played the generic burn, want the Silent tag to hold it in")
	}
	if len(voices) > 0 {
		t.Errorf("the silent cow spoke %v, want no voice from a Silent body", voices)
	}
}

// TestASilentMobLandingAFallSpeaksNothing drops a Silent cow from ten blocks
// up with the loop stopped and the ticks driven by hand: the fall lands — the
// fall damage event names the cow — but no cow voice rides the landing, the
// mobVoiceSend path a fall takes.
func TestASilentMobLandingAFallSpeaksNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Skyfall")

	// The loop stopped before the drop, so the fall is the hand-driven ticks'
	// alone to run.
	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.SetMobSilent(cowID, true) {
		t.Fatal("the spawned cow is nowhere to silence")
	}

	fallID := registry.MustID("minecraft:damage_type", "minecraft:fall")
	hurts := []int32{}
	voices := []string{}
	for range 40 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cowID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlaySoundEffect:
				if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); strings.HasPrefix(name, "minecraft:entity.cow") {
					voices = append(voices, name)
				}
			}
		})
	}
	if len(hurts) != 1 || hurts[0] != fallID {
		t.Errorf("the landing told %v of the silent cow's hurt, want the fall's one", hurts)
	}
	if len(voices) > 0 {
		t.Errorf("the silent cow spoke %v, want no voice from a Silent body", voices)
	}
}

// TestAMobsSoundReachesOnlyWithinItsEarshot plants a NoAI cow and kills it
// with the loop stopped: the players just inside PlayerList.broadcast's
// sixteen blocks hear the death, the one just outside does not, and neither
// does a player standing at the same coordinates in another dimension.
func TestAMobsSoundReachesOnlyWithinItsEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Inside"))

	inside := connect().Join("Inside")
	outside := connect().Join("Outside")
	away := connect()
	away.JoinAs("Away")
	srv.MovePlayerToDimension("Away", world.NetherName, 0, 64, 0)

	// The cow stands at x=0.5: the inside player 15.75 blocks off hears, the
	// edge one exactly sixteen off does not, the strict line broadcast reads.
	inside.MoveTo(16.25, 64, 0.5, 0, 0)
	outside.MoveTo(16.5, 64, 0.5, 0, 0)
	inside.Settle()
	outside.Settle()
	away.Settle()

	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 0.5)
	inside.ExpectMobSpawn(protocol.EntityTypeCow)

	near, far, other := []string{}, []string{}, []string{}
	inside.ChatCommand("kill @e[type=cow]")
	// Death sounds arrive during the command, before its settle barrier.
	inside.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				near = append(near, name)
			}
		}
	})
	outside.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				far = append(far, name)
			}
		}
	})
	away.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				other = append(other, name)
			}
		}
	})

	if len(near) == 0 {
		t.Errorf("the cow at entity %d spoke nothing to the player 15.75 blocks off, want the death within earshot", cowID)
	}
	if len(far) > 0 {
		t.Errorf("the cow's voice reached the player exactly sixteen blocks off: %v", far)
	}
	if len(other) > 0 {
		t.Errorf("the cow's voice reached a player in another dimension: %v", other)
	}
}

// TestACubeVoiceRangeScalesWithItsVolume drops a size-four slime (voice
// volume 1.6, earshot 25.6 blocks) with the loop stopped and holds the range
// the volume buys: the player twenty blocks off hears the cube's landing, the
// one thirty blocks off does not.
func TestACubeVoiceRangeScalesWithItsVolume(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())

	near := connect().Join("Near")
	far := connect().Join("Far")

	near.MoveTo(20.5, 64, 0.5, 0, 0)
	far.MoveTo(30.5, 64, 0.5, 0, 0)
	near.Settle()
	far.Settle()

	srv.StopTick()
	slimeID := srv.SpawnMob("minecraft:slime", 0.5, 70, 0.5)
	if !srv.SetMobSize(slimeID, 4) {
		t.Fatal("the spawned slime is nowhere to grow")
	}
	// The fall to the floor and the landing squish it carries; the cube's
	// voice, and no other, travels at the size's volume.
	for range 40 {
		srv.TickMobs(1)
	}

	nearVoices, farVoices := []string{}, []string{}
	pumpTicks(near, srv, 0, func(id int32) bool { return id != protocol.IDPlaySoundEffect },
		func(_ int32, data []byte) bool {
			name, _, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.slime") && volume == 1.6 {
				nearVoices = append(nearVoices, name)
			}
			return false
		})
	pumpTicks(far, srv, 0, func(id int32) bool { return id != protocol.IDPlaySoundEffect },
		func(_ int32, data []byte) bool {
			name, _, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.slime") && volume == 1.6 {
				farVoices = append(farVoices, name)
			}
			return false
		})
	if len(nearVoices) == 0 {
		t.Error("the size-four cube's voice never reached the player twenty blocks off, want the volume-scaled 25.6")
	}
	if len(farVoices) > 0 {
		t.Errorf("the size-four cube's voice reached the player thirty blocks off: %v", farVoices)
	}
}

// TestABabysWoundAndDeathSpeakItsBabyVoice swings at a piglet, a chick and a
// foal and reads the wire: a baby's hurt and death are the baby events its kind's
// getSoundSet swaps in while it isBaby, each at the baby voice's pitch, and a
// piglet grown back is its grown self again, not the baby set.
// vanilla: Pig.getSoundSet, Chicken.getSoundSet, Horse.getHurtSound, LivingEntity.getVoicePitch
func TestABabysWoundAndDeathSpeakItsBabyVoice(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.MoveToGround(2.5, 64, 2.5, true)
	c.Settle()

	for _, tc := range []struct {
		kind       string
		entityType int32
		hurt       string
		death      string
	}{
		{"pig", protocol.EntityTypePig, "minecraft:entity.baby_pig.hurt", "minecraft:entity.baby_pig.death"},
		{"chicken", protocol.EntityTypeChicken, "minecraft:entity.baby_chicken.hurt", "minecraft:entity.baby_chicken.death"},
		{"horse", protocol.EntityTypeHorse, "minecraft:entity.baby_horse.hurt", "minecraft:entity.baby_horse.death"},
	} {
		for _, event := range []string{tc.hurt, tc.death} {
			if _, ok := registry.SoundEventID(event); !ok {
				t.Fatalf("the sound registry carries no %s", event)
			}
		}
		servertest.UntilTicks(t, srv, 40, "the swing winds back up", func() bool {
			return srv.AttackTicks("Keeper") >= servertest.SwingRecovered
		})
		id := srv.SpawnMob("minecraft:"+tc.kind, 4.5, 64, 2.5)
		c.AwaitSpawn(tc.entityType)
		srv.SetMobBaby(id, true)
		c.Attack(id)
		// A bare-handed swing leaves the baby alive, so the wound speaks
		// hurt; the /kill's death follows on the tick after it, both inside
		// the window read below.
		c.ChatCommand("kill @e[type=" + tc.kind + "]")

		pitches := map[string]float32{}
		pumpTicks(c, srv, 40, nil, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data)
			if name == tc.hurt || name == tc.death {
				pitches[name] = pitch
			}
			return false
		})
		for _, event := range []string{tc.hurt, tc.death} {
			pitch, heard := pitches[event]
			if !heard {
				t.Errorf("the %s never spoke its %s within the window", tc.kind, event)
				continue
			}
			// The baby voice's spread, LivingEntity.getVoicePitch's baby
			// half: 1.5 ± 0.2.
			if pitch < 1.29 || pitch > 1.71 {
				t.Errorf("the %s's %s played at pitch %v, want the baby voice's spread around one and a half", tc.kind, event, pitch)
			}
		}
	}

	// A piglet set back to grown: the voice follows the current age, so the
	// grown set plays at the grown spread, 1.0 ± 0.2.
	servertest.UntilTicks(t, srv, 40, "the swing winds back up", func() bool {
		return srv.AttackTicks("Keeper") >= servertest.SwingRecovered
	})
	grownID := srv.SpawnMob("minecraft:pig", 4.5, 64, 2.5)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	// The spawn's finalize dice picked a sound variant, so pin the classic
	// one: the grown set read below is the kind's own for that variant.
	if !srv.SetMobSoundVariant(grownID, "minecraft:classic") {
		t.Fatal("the grown pig is nowhere to set the classic sound variant")
	}
	srv.SetMobBaby(grownID, true)
	srv.SetMobBaby(grownID, false)
	c.Attack(grownID)
	c.ChatCommand("kill @e[type=pig]")

	pitches := map[string]float32{}
	pumpTicks(c, srv, 40, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		name, _, _, _, _, _, pitch := servertest.ReadAmbient(t, data)
		if name == "minecraft:entity.pig.hurt" || name == "minecraft:entity.pig.death" {
			pitches[name] = pitch
		}
		return false
	})
	for _, event := range []string{"minecraft:entity.pig.hurt", "minecraft:entity.pig.death"} {
		pitch, heard := pitches[event]
		if !heard {
			t.Errorf("the grown pig never spoke its %s within the window", event)
			continue
		}
		// The grown voice's spread, LivingEntity.getVoicePitch's grown half.
		if pitch < 0.79 || pitch > 1.21 {
			t.Errorf("the grown pig's %s played at pitch %v, want the voice's spread around one", event, pitch)
		}
	}
}

// TestABurningMobPutOutFizzesInItsOwnCategory stands a burning NoAI mob in
// water or powder snow: a zombie and a cow go out and play the extinguish
// fizz at 0.7 in their own sound category, hostile for the monster and
// neutral for the animal; a fire-immune magma cube and a silent cow play
// nothing.
// vanilla: Entity.applyEffectsFromBlocks, Entity.playEntityOnFireExtinguishedSound
func TestABurningMobPutOutFizzesInItsOwnCategory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		summon   string
		nbt      string
		block    string
		category int32
		fizzes   int
	}{
		{"zombie", "minecraft:zombie", "{NoAI:1b}", "minecraft:water", protocol.SoundCategoryHostile, 1},
		{"cow", "minecraft:cow", "{NoAI:1b}", "minecraft:water", protocol.SoundCategoryNeutral, 1},
		{"cow in powder snow", "minecraft:cow", "{NoAI:1b}", "minecraft:powder_snow", protocol.SoundCategoryNeutral, 1},
		{"fire-immune magma cube", "minecraft:magma_cube", "{NoAI:1b}", "minecraft:water", protocol.SoundCategoryHostile, 0},
		{"silent cow", "minecraft:cow", "{NoAI:1b,Silent:1b}", "minecraft:water", protocol.SoundCategoryNeutral, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Doused", servertest.OpsListing(t, "Doused"))

			stone := servertest.BlockState(t, "minecraft:stone")
			for x := int32(-2); x <= 6; x++ {
				for z := int32(-2); z <= 6; z++ {
					srv.SetBlock(x, 63, z, stone)
				}
			}
			srv.SetBlock(4, 64, 4, servertest.BlockState(t, tc.block))

			srv.StopTick()
			c.ChatCommand("summon " + tc.summon + " 4.5 64 4.5 " + tc.nbt)
			id := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity)).ReadVarInt()
			srv.SetMobFire(id, 100)

			srv.TickMobs(1)
			fizzes := 0
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySoundEffect {
					return
				}
				name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
				if name != "minecraft:entity.generic.extinguish_fire" {
					return
				}
				fizzes++
				if category != tc.category {
					t.Errorf("fizz category %d, want %d", category, tc.category)
				}
				if volume != 0.7 {
					t.Errorf("fizz volume %v, want 0.7", volume)
				}
				if pitch < 1.2 || pitch > 2.0 {
					t.Errorf("fizz pitch %v, want within 1.6 +/- 0.4", pitch)
				}
			})
			if fizzes != tc.fizzes {
				t.Errorf("heard %d fizzes, want %d", fizzes, tc.fizzes)
			}
		})
	}
}
