package mobs

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The spider and the creeper tests: the second and third monsters, driven
// the way the zombie's are — through a real connection, in the flat world
// the harness grows, where every mob is one the test planted. The spider
// hunts in the dark and ignores a player it is not struck by in the light,
// climbs the wall it is pressed against, and drops its loot when a player
// kills it; the creeper swells when its target comes close, goes up at the
// end of the fuse — the blast destroying the blocks around it and running
// the player's hurt pipeline — stops swelling when the target walks
// out of reach, and lights its fuse at a creeper_igniters item whatever
// its targets do.

// blastSkip is the background a watching client hears while a monster is
// being watched: the world's own traffic. The movement packets are kept —
// the climb test reads a spider's rise out of them — and so are the sound
// effects, the priming of a fuse being one of the things the tests watch
// for.
func blastSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse:
		return true
	}
	return false
}

// TestASpiderHuntsAPlayerAtNight walks the spider's hunt over the wire: at
// midnight the spider put five blocks from a survival player takes them for
// its target — the aggressive bit of the Mob flags rising with the hunt —
// and lands its blow through the same pipeline a player's own hits run.
func TestASpiderHuntsAPlayerAtNight(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	// The night the hunt waits for is set by a /time, whose runner is
	// listed as an operator.
	srv, c, playerID := servertest.JoinAs(t, cfg, "Prey", servertest.OpsListing(t, "Prey"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	c.ChatCommand("time set midnight")
	c.Settle()

	spiderID := srv.SpawnMob("minecraft:spider", 4.5, 64, 4.5)
	spider := c.ExpectMobSpawn(protocol.EntityTypeSpider)
	if spider.EntityID != spiderID {
		t.Fatalf("spawned spider's entity ID = %d, want the one SpawnMob reported (%d)", spider.EntityID, spiderID)
	}
	if spider.Health != 16 {
		t.Errorf("spawned spider's health = %v, want the vanilla sixteen", spider.Health)
	}
	if spider.SpeedAttribute != float64(float32(0.3)) {
		t.Errorf("spawned spider's speed = %v, want the vanilla 0.3", spider.SpeedAttribute)
	}

	aggressive := false
	hurt := false
	damageEvent := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadMonsterMetadata(c, data)
			if m.EntityID != spiderID {
				return false // another body's state, read past
			}
			if m.HasFlags && m.Flags&protocol.EntityFlagAggressive != 0 {
				aggressive = true
			}
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			// The first blow's own health update is the one asserted; the
			// regeneration and the further blows after it say nothing
			// about the first.
			if h.Health < 20 && !hurt {
				hurt = true
				if h.Health != 18 {
					c.T.Errorf("the spider's blow left the player at %v, want the easy difficulty's 18 of 20", h.Health)
				}
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			if victim == playerID {
				if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", rules.DamageTypeMobAttack.Name()); got != want {
					c.T.Errorf("damage type ID = %d, want %d", got, want)
				}
				if got := r.ReadVarInt(); got != spiderID+1 {
					c.T.Errorf("damage cause ID = %d, want the spider's %d", got, spiderID+1)
				}
				if got := r.ReadVarInt(); got != spiderID+1 {
					c.T.Errorf("damage direct ID = %d, want the spider's %d", got, spiderID+1)
				}
				if got := r.ReadBool(); got {
					c.T.Error("damage event carries a position, which only /damage gives")
				}
				damageEvent = true
			}
		}
		return aggressive && hurt && damageEvent
	})
	if !aggressive {
		t.Error("the hunt never raised the aggressive bit of the Mob flags")
	}
	if !hurt {
		t.Error("the spider never landed its blow")
	}
	if !damageEvent {
		t.Error("the blow never came with its damage event")
	}
}

// TestASpiderInDaylightIgnoresAPlayer checks the light's own word: at noon
// the same spider beside the same survival player acquires no target of its
// own — no aggressive bit, no blow — for as long as the test cares to
// watch, the light-dependent value of the hour standing at or past the half
// the spider's hunt refuses to cross.
func TestASpiderInDaylightIgnoresAPlayer(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	// The daylight the test stands the spider in is set by a /time, whose
	// runner is listed as an operator.
	srv, c := servertest.Join(t, cfg, "Daylight", servertest.OpsListing(t, "Daylight"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	c.ChatCommand("time set noon")
	c.Settle()

	spiderID := srv.SpawnMob("minecraft:spider", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeSpider)

	pumpTicks(c, srv, 200, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadMonsterMetadata(c, data)
			if m.EntityID != spiderID {
				return false
			}
			if m.HasFlags && m.Flags&protocol.EntityFlagAggressive != 0 {
				t.Error("a spider in the noonday sun turned aggressive on a player it was not struck by")
				return true
			}
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				t.Errorf("the daylight spider hurt the player down to %v, want them untouched", h.Health)
				return true
			}
		}
		return false
	})
}

// TestASpiderClimbsAWall checks the climb: a spider whose target stands the
// far side of a wall three blocks high is pressed against it by the
// straight drive its navigation falls back to when no path can be found,
// and rises — the climbing bit in its metadata while it does.
func TestASpiderClimbsAWall(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	// The midnight the test waits out is set by a /time, whose runner is
	// listed as an operator.
	srv, c := servertest.Join(t, cfg, "Wallflower", servertest.OpsListing(t, "Wallflower"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	c.ChatCommand("time set midnight")
	c.Settle()

	// The wall: three blocks high on the column x=3, running thirty blocks
	// each way along Z, so no path the spider's search ranges at can walk
	// around it and the only way at the player behind it is over the top.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for dz := int32(-30); dz <= 30; dz++ {
		for dy := int32(64); dy <= 66; dy++ {
			srv.SetBlock(3, dy, dz, stone)
		}
	}

	// The player crosses to the far side of the wall before the spider is
	// planted on this one, so the only way at them is through the stone.
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	c.Settle()

	spiderID := srv.SpawnMob("minecraft:spider", 0.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeSpider)

	// The spider sees no player through the wall, so the hunt is the
	// retaliation's to start: one blow of the player's hand, the grudge
	// the HurtByTargetGoal answers to.
	c.Attack(spiderID)

	rose := func() bool {
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:spider" && m.Y > 64.5 {
				return true
			}
		}
		return false
	}
	climbing := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntityMetadata {
			m := servertest.ReadMonsterMetadata(c, data)
			if m.EntityID == spiderID && m.HasClimb && m.Climbing {
				climbing = true
			}
		}
		return rose() && climbing
	})
	if !rose() {
		t.Error("the spider pressed against the wall never rose")
	}
	if !climbing {
		t.Error("the climbing bit never stood in the spider's metadata while it rose")
	}
}

// TestACreeperSwellsAndExplodes walks the whole of a creeper's week over
// the wire: the fuse begun beside a survival player — the swell direction
// in its metadata and the priming sound with it — the Explosion packet at
// the end of the thirty ticks, the blocks around it set to air, the player
// hurt by the blast through the pipeline, and the body gone without so
// much as a corpse.
func TestACreeperSwellsAndExplodes(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, playerID := servertest.JoinAs(t, cfg, "Bystander")

	// A ring of stone at the creeper's own feet level, a block out from
	// where it will stand: the blast's first food, its crater's chew.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for dx := int32(1); dx <= 3; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx != 2 || dz != 0 { // the creeper's own cell stays open
				srv.SetBlock(dx, 64, dz, stone)
			}
		}
	}

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if creeper.EntityID != creeperID {
		t.Fatalf("spawned creeper's entity ID = %d, want the one SpawnMob reported (%d)", creeper.EntityID, creeperID)
	}
	if creeper.Health != 20 {
		t.Errorf("spawned creeper's health = %v, want the vanilla twenty", creeper.Health)
	}
	if creeper.SpeedAttribute != 0.25 {
		t.Errorf("spawned creeper's speed = %v, want the vanilla 0.25", creeper.SpeedAttribute)
	}

	swelling := false
	primed := false
	exploded := false
	var blast servertest.Explosion
	hurt := false
	damageEvent := false
	removed := false
	blocksGone := 0
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			// The crater's drops spawn items whose metadata is no
			// monster's; only the creeper's own packets are parsed.
			if protocol.NewReader(data).ReadVarInt() != creeperID {
				return false
			}
			m := servertest.ReadMonsterMetadata(c, data)
			if m.HasSwell && m.Swell == 1 {
				swelling = true
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767) // an inline definition
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:entity.creeper.primed" {
				primed = true
			}
		case protocol.IDPlayExplosion:
			blast = servertest.ReadExplosion(c, data)
			if blast.Radius != 3.0 {
				c.T.Errorf("explosion radius = %v, want the creeper's 3", blast.Radius)
			}
			exploded = true
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 && !hurt {
				hurt = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			if victim == playerID {
				if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", rules.DamageTypePlayerExplosion.Name()); got != want {
					c.T.Errorf("damage type ID = %d, want %d", got, want)
				}
				if got := r.ReadVarInt(); got != creeperID+1 {
					c.T.Errorf("damage cause ID = %d, want the creeper's %d", got, creeperID+1)
				}
				if got := r.ReadVarInt(); got != creeperID+1 {
					c.T.Errorf("damage direct ID = %d, want the creeper's %d", got, creeperID+1)
				}
				damageEvent = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 1 {
				c.T.Errorf("removed entities = %d, want 1", got)
			}
			if got := r.ReadVarInt(); got != creeperID {
				c.T.Errorf("removed entity = %d, want the creeper's %d", got, creeperID)
			}
			removed = true
		}
		// The crater: the four stone blocks standing directly beside the
		// creeper's feet cell are the first cells the rays out of the
		// centre reach, and the blast's chew on them is what the world
		// reads back as air.
		if exploded {
			blocksGone = 0
			for _, pos := range [][3]int32{{1, 64, 0}, {3, 64, 0}, {2, 64, 1}, {2, 64, -1}} {
				if srv.BlockAt(pos[0], pos[1], pos[2]) == 0 {
					blocksGone++
				}
			}
			return swelling && primed && hurt && damageEvent && removed && blocksGone >= 3
		}
		return false
	})
	if !swelling {
		t.Error("the creeper never put the swell direction in its metadata")
	}
	if !primed {
		t.Error("the fuse's first tick never played the priming")
	}
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	if blast.X != 2.5 || blast.Y != 64 || blast.Z != 0.5 {
		t.Errorf("the blast stood at (%v %v %v), want the creeper's own feet (2.5 64 0.5)", blast.X, blast.Y, blast.Z)
	}
	if !hurt {
		t.Error("the blast never hurt the player beside it")
	}
	if !damageEvent {
		t.Error("the blast never came with its damage event")
	}
	if !removed {
		t.Error("the exploding creeper's body was never taken out of the world")
	}
	if blocksGone < 3 {
		t.Errorf("the blast left %d of the four neighbouring blocks standing, want the crater's chew", blocksGone)
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the world keeps %d mobs, want the exploded creeper gone from the herd too", n)
	}
}

// TestACreeperBlastKillsWithTheExplosionMessage checks the lethal end of
// the same blast: on hard, a creeper at arm's length deals enough that the
// death message says the vanilla words — "was blown up by", with the
// creeper named.
func TestACreeperBlastKillsWithTheExplosionMessage(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(3) // hard
	c := connect()
	playerID := c.JoinAs("CloseBy")

	servertest.HoldTicks(t, srv)
	creeperID := srv.SpawnMob("minecraft:creeper", 1.5, 64, 0.5)
	identity, ok := srv.MobUUID(creeperID)
	if !ok {
		t.Fatal("creeper has no UUID")
	}

	// The message, as the Combat Death packet carries it and the chat
	// announces it: the explosion's own words with the creeper named.
	message := protocol.Text{
		Translate: "death.attack.explosion.player",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("CloseBy"), servertest.EntityDisplayComponent("minecraft:creeper", "entity.minecraft.creeper", uuid.MustParse(identity))},
	}
	var wantDeath protocol.Buffer
	wantDeath.PutVarInt(playerID)
	wantDeath.PutText(message)
	var wantChat protocol.Buffer
	wantChat.PutText(message)
	wantChat.PutBool(false) // chat, not the action bar

	deathScreen := false
	announcement := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayCombatDeath:
			if string(data) != string(wantDeath.Bytes()) {
				c.T.Errorf("combat death = % X, want % X", data, wantDeath.Bytes())
			}
			deathScreen = true
		case protocol.IDPlaySystemChat:
			if string(data) != string(wantChat.Bytes()) {
				c.T.Errorf("system chat = % X, want % X", data, wantChat.Bytes())
			}
			announcement = true
		}
		return deathScreen && announcement
	})
	if !deathScreen {
		t.Error("the blast never put the player on the respawn screen")
	}
	if !announcement {
		t.Error("the death was never announced in chat")
	}
}

// TestABlastKillingAPlayerLeavesTheirDrops checks the death drops of the
// blast's own victim: a survival player killed by the creeper's blast drops
// their inventory and experience where they fell, and the blast that killed
// them leaves it all lying there — what it may destroy was decided before
// the first hurt landed, so nothing a death drops during the pass is among
// the bodies it goes on to hurt.
func TestABlastKillingAPlayerLeavesTheirDrops(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(3) // hard
	c := connect()
	c.JoinAs("Unlucky")

	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	log, ok := registry.ItemID("minecraft:oak_log")
	if !ok {
		t.Fatal("the item registry has no oak log")
	}
	carried := []protocol.ItemStack{
		{ItemID: dirt, Count: 64},
		{ItemID: log, Count: 3},
	}
	srv.GiveSlot("Unlucky", player.HotbarSlot, carried[0])
	srv.GiveSlot("Unlucky", player.HotbarSlot+1, carried[1])
	// A level's worth of experience, whose orbs the death drops with the
	// inventory.
	srv.SetExperienceLevels("Unlucky", 1)

	srv.SpawnMob("minecraft:creeper", 1.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	var drops []servertest.ItemDrop
	removed := map[int32]bool{}
	deathScreen := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayCombatDeath:
			deathScreen = true
		case protocol.IDPlaySpawnEntity:
			if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
				c.ReadDropStack(&d)
				drops = append(drops, d)
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed[r.ReadVarInt()] = true
			}
		}
		return deathScreen && len(drops) >= len(carried)
	})
	if !deathScreen {
		t.Fatal("the blast never put the player on the respawn screen")
	}
	// The stacks on the ground are the ones the inventory held.
	for _, want := range carried {
		name, _ := registry.ItemName(want.ItemID)
		if !slices.ContainsFunc(drops, func(d servertest.ItemDrop) bool {
			return d.Stack.ItemID == want.ItemID && d.Stack.Count == want.Count
		}) {
			t.Errorf("the death never dropped %d %s, its drops being %d stacks", want.Count, name, len(drops))
		}
	}
	// A few ticks past the blast, so the item tick's own pass has run
	// against what the blast left: nothing the death dropped may have been
	// taken back out of the world by it.
	c.PumpTicks(srv, 10, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed[r.ReadVarInt()] = true
			}
		}
		return false
	})
	for _, d := range drops {
		name, _ := registry.ItemName(d.Stack.ItemID)
		if removed[d.EntityID] {
			t.Errorf("the blast took the dropped %s back out of the world", name)
		}
	}
	if n := srv.OrbCount(); n == 0 {
		t.Error("the death's experience never reached the ground as orbs")
	}
}

// TestACreeperBlastLeavesTheCowsLoot checks the same against a mob's death:
// a creeper swelling at a player takes a cow standing beside it with it, and
// the beef the cow's death rolls lies on the ground afterwards — the loot of
// a body the blast killed is not among what the blast destroys.
func TestACreeperBlastLeavesTheCowsLoot(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Herder")

	srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	beef, ok := registry.ItemID("minecraft:beef")
	if !ok {
		t.Fatal("the item registry has no beef")
	}

	var drops []servertest.ItemDrop
	removed := map[int32]bool{}
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
				c.ReadDropStack(&d)
				drops = append(drops, d)
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed[r.ReadVarInt()] = true
			}
		}
		return slices.ContainsFunc(drops, func(d servertest.ItemDrop) bool {
			return d.Stack.ItemID == beef
		})
	})

	if !slices.ContainsFunc(drops, func(d servertest.ItemDrop) bool {
		return d.Stack.ItemID == beef
	}) {
		t.Fatal("the blast-killed cow laid no beef down")
	}
	// A few ticks past the blast, so the item tick's own pass has run
	// against what the blast left.
	c.PumpTicks(srv, 10, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed[r.ReadVarInt()] = true
			}
		}
		return false
	})
	for _, d := range drops {
		if removed[d.EntityID] {
			name, _ := registry.ItemName(d.Stack.ItemID)
			t.Errorf("the blast took the cow's dropped %s back out of the world", name)
		}
	}
}

// TestASummonedChargedCreeperBlastsWideAfterItsLongFuse summons the creeper
// /summon {powered:1b,Fuse:60s,ExplosionRadius:6b} makes: the charged
// metadata off the tag, the fuse burning the tag's sixty ticks rather than
// the vanilla thirty, and the doubled twelve-block blast — wide enough to
// take a block the plain three could not reach.
func TestASummonedChargedCreeperBlastsWideAfterItsLongFuse(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Target", servertest.OpsListing(t, "Target"))

	// A stone block six and a half out: within the charged twelve, far
	// past the plain three.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	srv.SetBlock(9, 64, 0, stone)

	c.ChatCommand("summon minecraft:creeper 2.5 64 0.5 {powered:1b,Fuse:60s,ExplosionRadius:6b}")
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if !creeper.HasPowered || !creeper.Powered {
		t.Fatalf("the summoned creeper's metadata = %+v, want the charged flag", creeper)
	}
	creeperID := creeper.EntityID

	swellSeen, exploded := false, false
	var swellTick uint64
	var blast servertest.Explosion
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() != creeperID {
				return false
			}
			m := servertest.ReadMonsterMetadata(c, data)
			if m.HasSwell && m.Swell == 1 && !swellSeen {
				swellSeen, swellTick = true, srv.TicksRun()
			}
		case protocol.IDPlayExplosion:
			blast = servertest.ReadExplosion(c, data)
			exploded = true
		}
		return swellSeen && exploded
	})
	if !exploded {
		t.Fatal("the charged creeper never exploded")
	}
	if !swellSeen {
		t.Fatal("the creeper never began swelling")
	}
	if elapsed := srv.TicksRun() - swellTick; elapsed < 50 || elapsed > 90 {
		t.Errorf("the fuse burned %d ticks, want the tag's sixty rather than the vanilla thirty", elapsed)
	}
	if blast.Radius != 12.0 {
		t.Errorf("the charged blast radius = %v, want the doubled twelve", blast.Radius)
	}
	if srv.BlockAt(9, 64, 0) != 0 {
		t.Error("the block six and a half out survived, want the charged blast to reach it")
	}
}

// TestASummonedChargedCreeperKeepsItsTags plants the creeper a summon tag
// makes, lets the world save it, and finds the powered, Fuse and
// ExplosionRadius tags in its compound, the charged metadata back on its
// body when the chunk comes back, and the same tags still on the creeper
// its reload saves again — the load reading what the save wrote.
func TestASummonedChargedCreeperKeepsItsTags(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")

	c.ChatCommand("summon minecraft:creeper 2.5 64 0.5 {powered:1b,Fuse:60s,ExplosionRadius:6b,NoAI:1b}")
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if !creeper.HasPowered || !creeper.Powered {
		t.Fatalf("the summoned creeper's metadata = %+v, want the charged flag", creeper)
	}

	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the world saved %d mobs, want the creeper", len(saved))
	}
	for _, compound := range saved {
		if got := compound["powered"]; got != nbt.Byte(1) {
			t.Errorf("saved powered = %v, want the tag's 1b", got)
		}
		if got := compound["Fuse"]; got != nbt.Short(60) {
			t.Errorf("saved Fuse = %v, want the tag's 60s", got)
		}
		if got := compound["ExplosionRadius"]; got != nbt.Byte(6) {
			t.Errorf("saved ExplosionRadius = %v, want the tag's 6b", got)
		}
	}

	second := connect()
	second.Login("Keeper")
	second.Configure()
	back, ok := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCreeper)
	if !ok {
		t.Fatal("the creeper was not restored with its chunk")
	}
	if !back.HasPowered || !back.Powered {
		t.Errorf("the returned creeper's metadata = %+v, want the charged flag", back)
	}

	// The load read the tags back in: what the reloaded creeper saves
	// again is the live body's own state, so the summon's values only
	// come back where the second load's reads kept them.
	second.Disconnect()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	saved = servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the reloaded world saved %d mobs, want the creeper", len(saved))
	}
	for _, compound := range saved {
		if got := compound["powered"]; got != nbt.Byte(1) {
			t.Errorf("the reloaded creeper's saved powered = %v, want the 1b it came back with", got)
		}
		if got := compound["Fuse"]; got != nbt.Short(60) {
			t.Errorf("the reloaded creeper's saved Fuse = %v, want the tag's 60s still", got)
		}
		if got := compound["ExplosionRadius"]; got != nbt.Byte(6) {
			t.Errorf("the reloaded creeper's saved ExplosionRadius = %v, want the tag's 6b still", got)
		}
	}
}

// TestASummonedCreeperKeepsItsNoughtFuseAndRadius summons the creeper
// /summon {Fuse:0s,ExplosionRadius:0b} makes: the vanilla read takes the
// named noughts as they stand, a body that goes up the moment it is stood
// in and a blast that breaks nothing — not the thirty-tick, three-block
// default the noughts would keep under a `> 0` read.
func TestASummonedCreeperKeepsItsNoughtFuseAndRadius(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Hasty", servertest.OpsListing(t, "Hasty"))

	// A stone block half a step out: the default blast would take it,
	// the tag's nought leaves it standing.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	srv.SetBlock(3, 64, 0, stone)

	c.ChatCommand("summon minecraft:creeper 2.5 64 0.5 {Fuse:0s,ExplosionRadius:0b}")

	// The blast rides the spawn's heels — the fuse burns none of the
	// wait — so the spawn is watched beside it rather than awaited.
	spawned, exploded := false, false
	var spawnTick uint64
	var blast servertest.Explosion
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt() // the entity ID
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeCreeper {
				spawned, spawnTick = true, srv.TicksRun()
			}
		case protocol.IDPlayExplosion:
			blast = servertest.ReadExplosion(c, data)
			exploded = true
		}
		return spawned && exploded
	})
	if !spawned {
		t.Fatal("the summoned creeper never arrived")
	}
	if !exploded {
		t.Fatal("the creeper with the nought fuse never exploded")
	}
	if elapsed := srv.TicksRun() - spawnTick; elapsed > 10 {
		t.Errorf("the blast came %d ticks after the spawn, want the nought fuse's first tick", elapsed)
	}
	if blast.Radius != 0.0 {
		t.Errorf("the blast radius = %v, want the tag's nought", blast.Radius)
	}
	if srv.BlockAt(3, 64, 0) == 0 {
		t.Error("the block half a step out was broken, want the nought-radius blast to break nothing")
	}
}

// TestASummonedCreeperWrapsAFusePastTheShort summons the fuse a short
// cannot hold: the vanilla read casts Fuse to a short, so the
// seventy-thousand the Int carries reads as the 4464 the cast keeps — what
// the creeper's own save holds, not the 32767 a clamp would write.
func TestASummonedCreeperWrapsAFusePastTheShort(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Wrapper", servertest.OpsListing(t, "Wrapper"))

	c.ChatCommand("summon minecraft:creeper 2.5 64 0.5 {Fuse:70000}")
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	c.Disconnect()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != 1 {
		t.Fatalf("the world saved %d mobs, want the creeper", len(saved))
	}
	for _, compound := range saved {
		if got := compound["Fuse"]; got != nbt.Short(4464) {
			t.Errorf("saved Fuse = %v, want the short cast's 4464s", got)
		}
	}
}

// TestACreeperStopsSwellingWhenItsTargetWalksOff checks the fuse's other
// end: a creeper swelling at a player who then walks past the seven blocks
// the swell tolerates deflates — the swell direction back to idle in its
// metadata — before whatever the chase brings next.
func TestACreeperStopsSwellingWhenItsTargetWalksOff(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Runner")

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	sawSwell := false
	deflated := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadMonsterMetadata(c, data)
			if m.EntityID != creeperID {
				return false
			}
			if m.HasSwell && m.Swell == 1 {
				sawSwell = true
			}
			if sawSwell && m.HasSwell && m.Swell == -1 {
				deflated = true
			}
		}
		if sawSwell && !deflated {
			// The target's walk out of reach: once the fuse has been seen
			// running, the player steps past the seven blocks the swell
			// keeps it running inside of.
			c.MoveTo(12.5, 64, 0.5, 0, 0)
		}
		return sawSwell && deflated
	})
	if !sawSwell {
		t.Error("the creeper never began swelling beside its target")
	}
	if !deflated {
		t.Error("the fuse never deflated when the target walked out of the seven")
	}
}

// TestAKilledSpiderDropsItsLoot kills spiders by the sword and watches what
// the deaths leave: the string and the spider's eye of the vanilla loot
// table's two pools, none of it anything else. Eight spiders stand in for
// the table's chances — a string roll of nought and an eye roll short of
// one can leave a single body with nothing at all, and eight bodies make
// the chance of the lot of them coming up empty one the test does not wait
// around for.
func TestAKilledSpiderDropsItsLoot(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Exterminator")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the item registry has no diamond sword")
	}
	srv.GiveSlot("Exterminator", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the killing waits the cooldown out before the first blow, so
	// every swing lands whole.
	servertest.AwaitFullSwing(t, srv, "Exterminator")

	// The eight, strung along the X axis clear of each other's reach.
	spiderIDs := make([]int32, 8)
	for i := range spiderIDs {
		spiderIDs[i] = srv.SpawnMob("minecraft:spider", float64(2+i*3)+0.5, 64, 20.5)
	}
	for range 8 {
		c.ExpectMobSpawn(protocol.EntityTypeSpider)
	}

	// Three swings of the sword carry the spider's sixteen points of
	// health, spaced past the half-second of invulnerability a landed hit
	// buys. The loop is stopped and each recovery ticked by hand, so the
	// row wanders for those ticks alone.
	srv.StopTick()
	for i, id := range spiderIDs {
		for range 3 {
			servertest.RecoverByHand(t, srv, "Exterminator")
			// The killing hand stands within the entity interaction's reach
			// of the spider: vanilla refuses a blow past the reach. The aim
			// is read afresh each swing, the knockback of the last blow and
			// the stroll of the recovery's hand ticks having carried the
			// spider on from where the row was read.
			x, _, z, ok := srv.MobPos(id)
			if !ok {
				t.Fatalf("spider %d of the row is gone before its swing", i)
			}
			// No settle between the step and the blow: the two ride the one
			// connection in order, and a settle would read past the drops
			// the spiders before this one left, which the read after the
			// row wants.
			c.MoveToGround(x, 64, z-1.5, true)
			c.Attack(id)
			servertest.SwingLanded(t, srv, "Exterminator")
		}
	}
	srv.StartTick()

	var drops []protocol.ItemStack
	for spent := 0; spent < 400 && len(drops) == 0; spent += 60 {
		pumpTicks(c, srv, 60, blastSkip, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySpawnEntity {
				return false
			}
			if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
				c.ReadDropStack(&d)
				drops = append(drops, d.Stack)
			}
			return false
		})
	}
	if len(drops) == 0 {
		t.Fatal("eight dead spiders laid nothing down")
	}
	str, ok := registry.ItemID("minecraft:string")
	if !ok {
		t.Fatal("the item registry has no string")
	}
	eye, _ := registry.ItemID("minecraft:spider_eye")
	for _, stack := range drops {
		if stack.ItemID != str && stack.ItemID != eye {
			name, _ := registry.ItemName(stack.ItemID)
			t.Errorf("the dead spiders dropped %s, want only the string and the spider eye the table's plain path rolls", name)
		}
		if stack.Count < 1 || stack.Count > 2 {
			t.Errorf("the dead spiders dropped %d of an item, want the table's range of one to two", stack.Count)
		}
	}
}

// TestAKilledCreeperDropsGunpowder kills creepers by the sword and watches
// what the deaths leave: the gunpowder of the vanilla loot table, and
// nothing else — the music discs of the charged-creeper pool staying in it,
// as nothing here charges a creeper or shoots one.
func TestAKilledCreeperDropsGunpowder(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Defuser")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the item registry has no diamond sword")
	}
	srv.GiveSlot("Defuser", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the killing waits the cooldown out before the first blow, so
	// every swing lands whole.
	servertest.AwaitFullSwing(t, srv, "Defuser")

	// The eight, strung along the X axis clear of each other's reach —
	// and clear of the player, a swelling creeper being nobody's to stand
	// next to.
	creeperIDs := make([]int32, 8)
	for i := range creeperIDs {
		creeperIDs[i] = srv.SpawnMob("minecraft:creeper", float64(2+i*3)+0.5, 64, 20.5)
	}
	for range 8 {
		c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	}

	// Three swings of the sword are the creeper's twenty points of
	// health, spaced past the half-second of invulnerability a landed hit
	// buys. The loop is stopped and each recovery ticked by hand, so the
	// row wanders for those ticks alone.
	srv.StopTick()
	for i, id := range creeperIDs {
		for range 3 {
			servertest.RecoverByHand(t, srv, "Defuser")
			// The killing hand stands within the entity interaction's reach
			// of the creeper: vanilla refuses a blow past the reach. The
			// aim is read afresh each swing, the knockback of the last blow
			// and the stroll of the recovery's hand ticks having carried
			// the creeper on from where the row was read. The mode is
			// creative, so no creeper here targets the hand to swell at it.
			x, _, z, ok := srv.MobPos(id)
			if !ok {
				t.Fatalf("creeper %d of the row is gone before its swing", i)
			}
			// No settle between the step and the blow: the two ride the one
			// connection in order, and a settle would read past the drops
			// the creepers before this one left, which the read after the
			// row wants.
			c.MoveToGround(x, 64, z-1.5, true)
			c.Attack(id)
			servertest.SwingLanded(t, srv, "Defuser")
		}
	}
	srv.StartTick()

	var drops []protocol.ItemStack
	for spent := 0; spent < 400 && len(drops) == 0; spent += 60 {
		pumpTicks(c, srv, 60, blastSkip, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySpawnEntity {
				return false
			}
			if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
				c.ReadDropStack(&d)
				drops = append(drops, d.Stack)
			}
			return false
		})
	}
	if len(drops) == 0 {
		t.Fatal("eight dead creepers laid nothing down")
	}
	gunpowder, _ := registry.ItemID("minecraft:gunpowder")
	for _, stack := range drops {
		if stack.ItemID != gunpowder {
			name, _ := registry.ItemName(stack.ItemID)
			t.Errorf("the dead creepers dropped %s, want only the gunpowder the table's plain path rolls", name)
		}
		if stack.Count < 1 || stack.Count > 2 {
			t.Errorf("the dead creepers dropped %d gunpowder, want the table's range of one to two", stack.Count)
		}
	}
}

// TestFlintAndSteelLightsACreeper checks the igniters on a creeper: flint
// and steel clicked on one lights the fuse — the swell direction and the
// ignited boolean up in the metadata, the priming with it — and the blast
// waits out the vanilla thirty ticks of fuse before it takes the body. The
// clicker is creative, so the creeper hunts nobody: the lit fuse is the only
// one running.
func TestFlintAndSteelLightsACreeper(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Pyromaniac")

	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Pyromaniac", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	creeper := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if creeper.EntityID != creeperID {
		t.Fatalf("spawned creeper's entity ID = %d, want the one SpawnMob reported (%d)", creeper.EntityID, creeperID)
	}

	// The fuse is stepped by hand rather than on the wall clock: a loop that
	// catches up after a stall runs its missed ticks back to back, so a blast
	// can land in the settle tail of a count that has already stopped.
	srv.StopTick()
	c.UseOn(creeperID)
	// The connection reads one packet at a time, so the ping's answer means
	// the click has lit the fuse before the first stepped tick.
	c.Settle()

	var (
		swelling bool
		lit      bool
		primed   bool
		exploded bool
		removed  bool
	)
	record := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			// The blast's cobblestone drops carry their stacks in
			// metadata of their own; only the creeper's state is this
			// test's.
			r := protocol.NewReader(data)
			if r.ReadVarInt() != creeperID {
				return
			}
			m := servertest.ReadMonsterMetadata(c, data)
			if m.HasSwell && m.Swell == 1 {
				swelling = true
			}
			if m.HasIgnite && m.Ignited {
				lit = true
			}
		case protocol.IDPlaySoundEffect:
			if servertest.SoundIs(data, "minecraft:entity.creeper.primed") {
				primed = true
			}
		case protocol.IDPlayExplosion:
			blast := servertest.ReadExplosion(c, data)
			if blast.Radius != 3.0 {
				c.T.Errorf("explosion radius = %v, want the creeper's 3", blast.Radius)
			}
			exploded = true
		case protocol.IDPlayRemoveEntities:
			// A removal naming the creeper is the blast's; the drops'
			// own pickups and merges are background.
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				if r.ReadVarInt() == creeperID {
					removed = true
				}
			}
		}
	}
	// Twenty-nine ticks: the lit fuse swells, but the blast waits for the
	// thirtieth.
	srv.TickMobs(29)
	c.SettleAndCollect(record)
	if !swelling {
		t.Error("the lit fuse never showed the swell direction in the metadata")
	}
	if !lit {
		t.Error("the lit fuse never showed the ignited boolean in the metadata")
	}
	if !primed {
		t.Error("the lighting never played the priming")
	}
	if exploded {
		t.Fatal("the blast came before the fuse's thirty ticks had run")
	}
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if !exploded {
		t.Fatal("the lit creeper never exploded")
	}
	if !removed {
		t.Error("the exploding creeper's body was never taken out of the world")
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the world keeps %d mobs, want the exploded creeper gone from the herd too", n)
	}
}

// TestFlintAndSteelWearsOnACreeper checks the survival price of the
// lighting: the flint and steel the fuse is lit with takes the vanilla one
// wear, the way Creeper.mobInteract's hurtAndBreak does.
func TestFlintAndSteelWearsOnACreeper(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig() // survival, the mode that pays wear

	srv, c, _ := servertest.JoinAs(t, cfg, "Lighter")

	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Lighter", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})

	// The creeper stands an arm past the swell's own three blocks, so the
	// lighting is the only fuse this click can begin.
	creeperID := srv.SpawnMob("minecraft:creeper", 4.0, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	c.UseOn(creeperID)
	c.Settle()

	if got := srv.InventorySlot("Lighter", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the steel's wear after the lighting = %d, want the vanilla one", got.Damage)
	}
}

// TestAFireChargeLightsACreeper checks the fire charge's own way down
// Creeper.mobInteract: the use sound it plays is the fire charge's, gone to
// everyone but the clicker at the vanilla volume and the mob's pitch range,
// and the stack pays the vanilla one — survival shrinking it, where the
// flint and steel would wear.
func TestAFireChargeLightsACreeper(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig() // survival, the mode that pays the stack

	srv, connect := servertest.Start(t, cfg)

	// The witness stands the hearing the clicker is excluded from: the
	// use sound goes to everyone but the hand that struck it, within the
	// sixteen blocks a sound of volume 1 carries, and clear of the blast.
	witness := connect().Join("Onlooker")
	witness.MoveTo(0.5, 64, 12.5, 0, 0)
	witness.Settle()

	c := connect().Join("Bombardier")

	fireID, ok := registry.ItemID("minecraft:fire_charge")
	if !ok {
		t.Fatal("the item registry has no fire charge")
	}
	srv.GiveSlot("Bombardier", player.HotbarSlot, protocol.ItemStack{ItemID: fireID, Count: 3})

	// The creeper stands an arm past the swell's own three blocks, so the
	// lighting is the only fuse this click can begin.
	creeperID := srv.SpawnMob("minecraft:creeper", 4.0, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	c.UseOn(creeperID)
	c.Settle()

	if got := srv.InventorySlot("Bombardier", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the fire charge's stack after the lighting = %d, want the two the vanilla shrink leaves", got.Count)
	}

	heard := false
	pumpTicks(witness, srv, 400, blastSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		sound := r.ReadVarInt()
		var name string
		if sound == 0 {
			name = r.ReadString(32767) // an inline definition
		} else {
			name, _ = servertest.SoundEventName(sound - 1)
		}
		if name != "minecraft:item.firecharge.use" {
			return false
		}
		if got := r.ReadVarInt(); got != protocol.SoundCategoryHostile {
			t.Errorf("the use sound's category = %d, want the hostile one the mob speaks in", got)
		}
		r.ReadI32() // x, in eighths of a block
		r.ReadI32() // y
		r.ReadI32() // z
		if got := r.ReadF32(); got != 1.0 {
			t.Errorf("the use sound's volume = %v, want the vanilla 1.0", got)
		}
		// vanilla: getRandom().nextFloat() * 0.4 + 0.8, off the mob's own
		// random source
		if got := r.ReadF32(); got < 0.8 || got >= 1.2 {
			t.Errorf("the use sound's pitch = %v, want the vanilla range [0.8, 1.2)", got)
		}
		heard = true
		return true
	})
	if !heard {
		t.Error("the onlooker never heard the fire charge's use sound")
	}

	// The clicker's own stream: no igniter's use sound on it, the client
	// predicting its own strike.
	stray := false
	c.PumpTicks(srv, 3, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect &&
			(servertest.SoundIs(data, "minecraft:item.firecharge.use") || servertest.SoundIs(data, "minecraft:item.flintandsteel.use")) {
			stray = true
		}
		return false
	})
	if stray {
		t.Error("the clicker was sent the igniter's use sound its own client predicts")
	}
}

// chargedHeadsAfterBlast detonates an ignited creeper at x=20 beside the
// victims, far from the player, and returns the head items the world holds
// once the blast has gone off and the dead mobs' drops have landed.
func chargedHeadsAfterBlast(t *testing.T, powered bool, victims ...string) []string {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Target", servertest.OpsListing(t, "Target"))

	for i, victim := range victims {
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s %.1f 64 0.5 {NoAI:1b,Health:4f,PersistenceRequired:1b}", victim, 21.5+float64(i)))
		c.ExpectMobSpawn(creeperVictimType(t, victim))
	}
	charge := "0b"
	if powered {
		charge = "1b"
	}
	c.ChatCommand("summon minecraft:creeper 20.5 64 0.5 {powered:" + charge + ",ignited:1b,Fuse:5s,ExplosionRadius:3b,NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	exploded := false
	pumpTicks(c, srv, 400, blastSkip, func(id int32, _ []byte) bool {
		exploded = exploded || id == protocol.IDPlayExplosion
		return exploded
	})
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	// The deaths report on the tick after the blast; give them a few.
	until := srv.TicksRun() + 40
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.TicksRun() >= until })
	var heads []string
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if strings.HasSuffix(it.Item, "_head") || strings.HasSuffix(it.Item, "_skull") {
			heads = append(heads, it.Item)
		}
	}
	return heads
}

func creeperVictimType(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:" + name)
	if !ok {
		t.Fatalf("no entity type %s", name)
	}
	return id
}

// A charged creeper's kill drops the victim's head; an uncharged one's does
// not.
func TestAChargedCreeperBlastDropsTheVictimsHead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ victim, want string }{
		{"zombie", "minecraft:zombie_head"},
		{"skeleton", "minecraft:skeleton_skull"},
		{"wither_skeleton", "minecraft:wither_skeleton_skull"},
		{"creeper", "minecraft:creeper_head"},
	} {
		t.Run(tc.victim, func(t *testing.T) {
			t.Parallel()
			if got := chargedHeadsAfterBlast(t, true, tc.victim); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("heads = %v, want [%s]", got, tc.want)
			}
		})
	}
	t.Run("uncharged", func(t *testing.T) {
		t.Parallel()
		if got := chargedHeadsAfterBlast(t, false, "zombie"); len(got) != 0 {
			t.Errorf("heads = %v, want none", got)
		}
	})
	t.Run("headless victim killed first", func(t *testing.T) {
		t.Parallel()
		got := chargedHeadsAfterBlast(t, true, "cow", "zombie")
		if !slices.Equal(got, []string{"minecraft:zombie_head"}) {
			t.Errorf("heads = %v, want [minecraft:zombie_head]", got)
		}
	})
	t.Run("one creeper drops one head", func(t *testing.T) {
		t.Parallel()
		if got := chargedHeadsAfterBlast(t, true, "zombie", "skeleton"); len(got) != 1 {
			t.Errorf("heads = %v, want exactly one", got)
		}
	})
}
