package entities

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The fishing rod as the wire tells it: a use of the rod casts a bobber —
// the spawn the client draws its line from — a second use reels it in,
// and what the reel finds decides what the world sees next: the item and
// the orb a catch pays out, the shove a hooked body takes toward its
// angler.

// fishingMeta is one Set Entity Metadata packet the hook's own two fields
// came in: the body it is caught in — one on the wire past the ID — and
// the bite, each told only where the packet carried it.
type fishingMeta struct {
	id         int32
	hooked     int32
	hookedTold bool
	biting     bool
	bitingTold bool
}

// fishingRead collects the wire while the ticks run (the loop must be
// held), keeping the traffic a
// hook's life is made of — the sounds, the spawns, the metadata of the
// two fields, the motions, the entity events and the removals — and
// stepping over the rest.
func fishingRead(c *servertest.Client, srv *server.Server, ticks int) (sounds []servertest.ThrownSound, spawns []servertest.ThrownSpawn,
	metas []fishingMeta, motions []servertest.ThrownVelocity, events []servertest.ThrownEvent, removes [][]int32) {
	c.T.Helper()
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		{
			switch gotID {
			case protocol.IDPlaySoundEffect:
				r := protocol.NewReader(data)
				wireID := r.ReadVarInt()
				category := r.ReadVarInt()
				x := float64(r.ReadI32()) / 8
				y := float64(r.ReadI32()) / 8
				z := float64(r.ReadI32()) / 8
				volume := r.ReadF32()
				pitch := r.ReadF32()
				name := ""
				if wireID > 0 {
					if n, ok := servertest.SoundEventName(wireID - 1); ok {
						name = n
					}
				}
				sounds = append(sounds, servertest.ThrownSound{
					Name: name, Category: category,
					X: x, Y: y, Z: z, Volume: volume, Pitch: pitch,
				})
			case protocol.IDPlaySpawnEntity:
				spawns = append(spawns, servertest.ReadThrownSpawn(c, data))
			case protocol.IDPlayEntityMetadata:
				r := protocol.NewReader(data)
				id := r.ReadVarInt()
				meta := fishingMeta{id: id}
				for r.Err() == nil {
					index := r.ReadU8()
					if index == protocol.MetadataEnd {
						break
					}
					fieldType := r.ReadVarInt()
					switch {
					case index == protocol.MetadataIndexHookedEntity && fieldType == protocol.MetadataVarInt:
						meta.hooked = r.ReadVarInt() - 1
						meta.hookedTold = true
					case index == protocol.MetadataIndexBite && fieldType == protocol.MetadataBoolean:
						meta.biting = r.ReadBool()
						meta.bitingTold = true
					default:
						servertest.SkipMetadataField(c, r, fieldType)
					}
				}
				metas = append(metas, meta)
			case protocol.IDPlaySetEntityMotion:
				r := protocol.NewReader(data)
				id := r.ReadVarInt()
				vx, vy, vz := r.ReadLpVec3()
				motions = append(motions, servertest.ThrownVelocity{Id: id, Vx: vx, Vy: vy, Vz: vz})
			case protocol.IDPlayEntityEvent:
				r := protocol.NewReader(data)
				id := r.ReadI32()
				status := r.ReadU8()
				events = append(events, servertest.ThrownEvent{Id: id, Status: status})
			case protocol.IDPlayRemoveEntities:
				r := protocol.NewReader(data)
				count := r.ReadVarInt()
				var ids []int32
				for range count {
					ids = append(ids, r.ReadVarInt())
				}
				removes = append(removes, ids)
			}
		}
		return false
	})
	return sounds, spawns, metas, motions, events, removes
}

// spawnsOf keeps the spawns of the entity type named.
func spawnsOf(spawns []servertest.ThrownSpawn, entityType int32) []servertest.ThrownSpawn {
	var out []servertest.ThrownSpawn
	for _, s := range spawns {
		if s.EntityType == entityType {
			out = append(out, s)
		}
	}
	return out
}

// givenRod stands the survival angler named with a rod in hand, freshly
// cast and wanting it.
func givenRod(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), name)
	srv.GiveSlot(name, 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	servertest.HoldTicks(t, srv)
	return srv, c, id
}

// TestACastPutsABobberOnTheWire aims a rod level and south and reads the
// cast whole: the throw's sound at the angler's feet, the Spawn Entity of
// the fishing bobber type — at the eye, a third of a block along the
// look, leaving on the line's own speed — and the angler's entity ID in
// the data field. A second use reels the hook back: the retrieval's
// sound, the hook out of the world, the rod untouched either way.
func TestACastPutsABobberOnTheWire(t *testing.T) {
	t.Parallel()
	srv, c, playerID := givenRod(t, "Angler")
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(c, 0, 0)
	// The flight crosses the whole render distance before it lands; ten
	// ticks carry the cast and its first passes.
	sounds, spawns, _, _, _, _ := fishingRead(c, srv, 10)

	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	bobber := cast[0]
	if bobber.OwnerID != playerID {
		t.Errorf("the bobber's data field names entity %d, want the angler's %d", bobber.OwnerID, playerID)
	}
	// Born at the eye — 1.62 over the feet — and a third of a block along
	// the southbound look.
	if d := bobber.Y - (64 + 1.62); d < -0.01 || d > 0.01 {
		t.Errorf("the bobber was born at y=%v, want the angler's eye", bobber.Y)
	}
	if d := bobber.X - 0.5; d < -0.01 || d > 0.01 {
		t.Errorf("the bobber was born at x=%v, want the angler's own", bobber.X)
	}
	if d := bobber.Z - (0.5 + 0.3); d < -0.01 || d > 0.01 {
		t.Errorf("the bobber was born at z=%v, want a third of a block south", bobber.Z)
	}
	// Leaving on the look's own line: no share east, the 1.1 the six
	// tenths and the half both scale the southbound ray by, each axis's
	// triangle running to a hundredth past.
	if bobber.Vx != 0 {
		t.Errorf("a level cast moved east at %v, want the line's own zero", bobber.Vx)
	}
	if bobber.Vy != 0 {
		t.Errorf("a level cast moved at %v vertically, want the line's own zero", bobber.Vy)
	}
	if bobber.Vz < 1.08 || bobber.Vz > 1.12 {
		t.Errorf("the cast left at vz=%v, want the 1.1 the line's share gives", bobber.Vz)
	}

	var throwSound *servertest.ThrownSound
	for i := range sounds {
		if sounds[i].Name == "minecraft:entity.fishing_bobber.throw" {
			throwSound = &sounds[i]
		}
	}
	if throwSound == nil {
		t.Fatal("no fishing bobber throw sound was played")
	}
	if throwSound.Category != protocol.SoundCategoryNeutral {
		t.Errorf("the throw's sound category = %d, want the neutral's", throwSound.Category)
	}
	if throwSound.X != 0.5 || throwSound.Y != 64 || throwSound.Z != 0.5 {
		t.Errorf("the throw's sound played from (%v %v %v), want the angler's feet", throwSound.X, throwSound.Y, throwSound.Z)
	}
	if throwSound.Volume != 0.5 {
		t.Errorf("the throw's sound volume = %v, want the vanilla half", throwSound.Volume)
	}
	if throwSound.Pitch < 0.3 || throwSound.Pitch > 0.51 {
		t.Errorf("the throw's sound pitch = %v, want the roll's third to a half", throwSound.Pitch)
	}

	// The second use reels: the hook back, the sound of it, and no new
	// bobber in the world of either.
	servertest.UseHeld(c, 0, 0)
	sounds, spawns, _, _, _, removes := fishingRead(c, srv, 10)
	if after := spawnsOf(spawns, protocol.EntityTypeFishingBobber); len(after) != 0 {
		t.Errorf("the reel spawned %d more bobbers, want none", len(after))
	}
	var retrieveSound *servertest.ThrownSound
	for i := range sounds {
		if sounds[i].Name == "minecraft:entity.fishing_bobber.retrieve" {
			retrieveSound = &sounds[i]
		}
	}
	if retrieveSound == nil {
		t.Fatal("no fishing bobber retrieve sound was played")
	}
	if retrieveSound.Volume != 1.0 {
		t.Errorf("the retrieval's sound volume = %v, want the whole block", retrieveSound.Volume)
	}
	hookBack := false
	for _, ids := range removes {
		for _, one := range ids {
			if one == bobber.EntityID {
				hookBack = true
			}
		}
	}
	if !hookBack {
		t.Error("the reeled hook was not taken out of the world")
	}
	if got := srv.InventorySlot("Angler", 36); got.Count != 1 {
		t.Errorf("the rod's count = %d after cast and reel, want the one it began as", got.Count)
	}
}

// TestReelingInABittenHookPaysTheCatch casts a rod into a pool, winds the
// water's countdown to the bite's door and reads the bite come — the
// splash and the catchable flag on the wire — then reels in and checks
// the catch paid out: the loot of the fishing table tossed from the hook
// as an item entity, an orb of experience at the angler's feet, and the
// one wear the catch costs the rod.
func TestReelingInABittenHookPaysTheCatch(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Fisher")
	// A three-by-three pool, a cell deep, south of where the angler
	// stands: the cast in the follow aims down at its middle.
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)

	// The cast: a hook into the pool.
	servertest.UseHeld(c, 0, 40)
	_, castSpawns, _, _, _, _ := fishingRead(c, srv, 20)
	cast := spawnsOf(castSpawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID, hookX := cast[0].EntityID, cast[0].X

	// The water's countdown wound to the bite's door: the bite reads
	// as the metadata's catchable flag and the splash of a fish
	// taking the hook.
	if !srv.WindBobberBite("Fisher") {
		t.Fatal("no hook was out to wind")
	}
	var splash *servertest.ThrownSound
	bit := false
	for range 30 {
		if bit {
			break
		}
		readSounds, _, metas, _, _, _ := fishingRead(c, srv, 2)
		for _, m := range metas {
			if m.id == hookID && m.bitingTold && m.biting {
				bit = true
			}
		}
		for i := range readSounds {
			if readSounds[i].Name == "minecraft:entity.fishing_bobber.splash" {
				splash = &readSounds[i]
			}
		}
	}
	if !bit {
		t.Fatal("the wound hook never bit")
	}

	// The reel, inside the nibble's window: the catch is tossed from
	// the hook, an orb stands at the angler's feet, the retrieval's
	// sound plays, and the rod wears the one a catch costs.
	servertest.UseHeld(c, 0, 40)
	sounds, spawns, _, _, _, removes := fishingRead(c, srv, 40)
	var retrieveSound *servertest.ThrownSound
	for i := range sounds {
		if sounds[i].Name == "minecraft:entity.fishing_bobber.retrieve" {
			retrieveSound = &sounds[i]
		}
	}
	hookBack := false
	for _, ids := range removes {
		for _, one := range ids {
			if one == hookID {
				hookBack = true
			}
		}
	}

	if splash == nil {
		t.Error("the bite played no splash")
	} else if splash.Volume != 0.25 {
		t.Errorf("the splash's volume = %v, want the vanilla quarter", splash.Volume)
	}
	if retrieveSound == nil {
		t.Error("the catch's reel played no retrieval sound")
	}
	if !hookBack {
		t.Error("the reeled hook was not taken out of the world")
	}

	loot := map[string]bool{
		"minecraft:cod": true, "minecraft:salmon": true,
		"minecraft:pufferfish": true, "minecraft:tropical_fish": true,
		"minecraft:lily_pad": true, "minecraft:leather_boots": true,
		"minecraft:leather": true, "minecraft:bone": true,
		"minecraft:string": true, "minecraft:fishing_rod": true,
		"minecraft:bowl": true, "minecraft:stick": true,
		"minecraft:ink_sac": true, "minecraft:tripwire_hook": true,
		"minecraft:rotten_flesh": true, "minecraft:bamboo": true,
		"minecraft:potion":   true,
		"minecraft:name_tag": true, "minecraft:saddle": true,
		"minecraft:nautilus_shell": true, "minecraft:bow": true,
		"minecraft:enchanted_book": true,
	}
	// The catch, tossed from the hook and already on its way to the
	// angler: taken to hand by the time the test reads, or lying where
	// the water caught it — one item of the fishing table either way.
	var caughtAt [3]float64
	for _, s := range spawnsOf(spawns, protocol.EntityTypeItem) {
		caughtAt = [3]float64{s.X, s.Y, s.Z}
	}
	if d := caughtAt[0] - hookX; d < -4 || d > 4 {
		t.Errorf("the catch was tossed to x=%v, want it near the hook's %v", caughtAt[0], hookX)
	}
	ofTheLoot := ""
	rod := servertest.MustItem("minecraft:fishing_rod")
	if items := srv.ItemsInDimension(world.DimensionName); len(items) == 1 {
		ofTheLoot = items[0].Item
	} else {
		// The pickup reached it first: whatever slot filled is the catch.
		for slot := 0; slot <= 45; slot++ {
			if stack := srv.InventorySlot("Fisher", slot); !stack.Empty() && stack.ItemID != rod {
				ofTheLoot, _ = registry.ItemName(stack.ItemID)
			}
		}
	}
	if ofTheLoot == "" {
		t.Fatal("the catch neither lies in the world nor sits in the angler's hands")
	}
	if !loot[ofTheLoot] {
		t.Errorf("the catch was a %q, want an item of the fishing loot", ofTheLoot)
	}

	orbs := spawnsOf(spawns, protocol.EntityTypeExperienceOrb)
	if len(orbs) != 1 {
		t.Fatalf("the catch stood %d experience orbs, want the one a stack of loot gives", len(orbs))
	}
	if d := orbs[0].X - 2.5; d < -0.3 || d > 0.3 {
		t.Errorf("the orb stood at x=%v, want it beside the angler", orbs[0].X)
	}
	if d := orbs[0].Z - (1.5 + 0.5); d < -0.3 || d > 0.3 {
		t.Errorf("the orb stood at z=%v, want it half a block south of the angler", orbs[0].Z)
	}
	if d := orbs[0].Y - (64 + 0.5); d < -0.3 || d > 0.3 {
		t.Errorf("the orb stood at y=%v, want it half a block over the angler's feet", orbs[0].Y)
	}
	// The rod wears one a catch.
	if got := srv.InventorySlot("Fisher", 36); got.Damage != 1 {
		t.Errorf("the rod's damage = %d after a catch, want the one a catch wears", got.Damage)
	}
}

// TestReelingInAHookOnAMobPullsIt casts at a cow standing frozen on the
// beam, reads the hook take it — the hooked-entity field naming the cow
// on the wire — and reels in: the cow takes the pull toward its angler,
// told as its motion at once, and the rod wears the five a body's hook
// costs.
func TestReelingInAHookOnAMobPullsIt(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Puller")
	cowID := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	if !srv.SetMobNoAI(cowID) {
		t.Fatal("the cow never stood still")
	}
	// East of the angler, pitched down onto the body's box: the hook
	// crosses it on the third tick of its flight.
	c.MoveTo(0.5, 64, 0.5, -90, 10)
	servertest.UseHeld(c, -90, 10)
	sounds, spawns, metas, _, _, _ := fishingRead(c, srv, 40)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID := cast[0].EntityID
	var thrown bool
	for i := range sounds {
		if sounds[i].Name == "minecraft:entity.fishing_bobber.throw" {
			thrown = true
		}
	}
	if !thrown {
		t.Error("the cast played no throw sound")
	}

	var hooked bool
	for _, m := range metas {
		if m.id == hookID && m.hookedTold && m.hooked == cowID {
			hooked = true
		}
	}
	if !hooked {
		t.Fatalf("the hook never named the cow on the wire")
	}

	// The reel: the cow takes the pull — a tenth of the way to the angler
	// — and the wire hears of it at once.
	servertest.UseHeld(c, -90, 10)
	_, _, _, motions, events, removes := fishingRead(c, srv, 40)
	var pull *servertest.ThrownVelocity
	for i := range motions {
		if motions[i].Id == cowID {
			pull = &motions[i]
			break
		}
	}
	if pull == nil {
		t.Fatal("the hooked cow was never told of its pull")
	}
	if pull.Vx > -0.15 {
		t.Errorf("the pull's x = %v, want the tenth of the angler's way west", pull.Vx)
	}
	if pull.Vx > 0 {
		t.Errorf("the pull's x = %v, want it toward the angler west", pull.Vx)
	}
	var pullEvent bool
	for _, ev := range events {
		if ev.Id == hookID && ev.Status == protocol.EntityStatusFishingPull {
			pullEvent = true
		}
	}
	if !pullEvent {
		t.Error("the pull was not told of as the hook's entity event")
	}
	hookBack := false
	for _, ids := range removes {
		for _, one := range ids {
			if one == hookID {
				hookBack = true
			}
		}
	}
	if !hookBack {
		t.Error("the reeled hook was not taken out of the world")
	}
	if got := srv.InventorySlot("Puller", 36); got.Damage != 5 {
		t.Errorf("the rod's damage = %d after the pull, want the five a body's hook wears", got.Damage)
	}
}

// TestASpectatorsRodCastsNothing reads the gate the other use handlers
// keep: an operator flying as a spectator with a rod in hand casts
// nothing — no bobber, no throw sound — and the same use back in
// survival casts.
func TestASpectatorsRodCastsNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
	srv.GiveSlot("Ghost", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	c.MoveTo(0.5, 64, 0.5, 0, 0)

	servertest.HoldTicks(t, srv)
	c.SendChangeGameMode(protocol.GameModeSpectator)
	servertest.RunTicks(t, srv, 4)
	servertest.UseHeld(c, 0, 0)
	sounds, spawns, _, _, _, _ := fishingRead(c, srv, 10)
	if after := spawnsOf(spawns, protocol.EntityTypeFishingBobber); len(after) != 0 {
		t.Fatalf("a spectator's rod spawned %d bobbers, want none", len(after))
	}
	for i := range sounds {
		if sounds[i].Name == "minecraft:entity.fishing_bobber.throw" {
			t.Error("a spectator's rod played the throw sound")
		}
	}

	c.SendChangeGameMode(protocol.GameModeSurvival)
	servertest.RunTicks(t, srv, 4)
	servertest.UseHeld(c, 0, 0)
	_, spawns, _, _, _, _ = fishingRead(c, srv, 10)
	if after := spawnsOf(spawns, protocol.EntityTypeFishingBobber); len(after) != 1 {
		t.Errorf("the rod back in a surviving hand spawned %d bobbers, want the cast's one", len(after))
	}
}

// vanilla: FishingHook.shouldStopFishing, Entity.canInteractWithLevel
func TestTheHookGoesWhenTheAnglerBecomesASpectator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
	rod := protocol.ItemStack{ItemID: int32(registry.ItemFishingRod), Count: 1}
	srv.GiveSlot("Ghost", 36, rod)
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(c, 0, 0)
	_, spawns, _, _, _, removes := fishingRead(c, srv, 1)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want one", len(cast))
	}
	hookID := cast[0].EntityID
	for _, ids := range removes {
		for _, id := range ids {
			if id == hookID {
				t.Fatal("the hook was removed while its angler was in survival")
			}
		}
	}

	c.SendChangeGameMode(protocol.GameModeSpectator)
	_, _, _, _, _, removes = fishingRead(c, srv, 1)
	if got := srv.InventorySlot("Ghost", 36); got.ItemID != rod.ItemID || got.Count != 1 || got.Damage != 0 {
		t.Fatalf("the spectator's held rod = %+v, want an unworn rod", got)
	}
	for _, ids := range removes {
		if slices.Contains(ids, hookID) {
			return
		}
	}
	t.Fatal("the hook survived its owner's first tick as a spectator")
}

// TestACreativeReelWearsNoRod checks the wear's other side: the reel of a
// hook grounded dry, the two a surviving rod takes for it, costs a creative
// one none — the instabuild cloak wearHeld wears for every other use.
func TestACreativeReelWearsNoRod(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Muse", servertest.OpsListing(t, "Muse"))
	srv.GiveSlot("Muse", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	servertest.HoldTicks(t, srv)
	c.SendChangeGameMode(protocol.GameModeCreative)
	c.MoveTo(0.5, 64, 0.5, 0, 30)
	servertest.UseHeld(c, 0, 30)
	// The flight lands the hook on the ground a few blocks off; the reel
	// after it is the grounded two-wear one.
	servertest.RunTicks(t, srv, 60)
	fishingRead(c, srv, 20)
	servertest.UseHeld(c, 0, 30)
	fishingRead(c, srv, 20)
	if got := srv.InventorySlot("Muse", 36); got.Damage != 0 {
		t.Errorf("the creative rod's damage = %d after the reel, want the none of instabuild", got.Damage)
	}
}

// TestALatecomerIsShownTheFloatingHook leaves an angler's hook bobbing on
// its water and has another joiner arrive into the world: the join's
// send-off shows the floating hook to the newcomer, named with its
// angler, where it floats — the same send-off a crossing arrival gives.
func TestALatecomerIsShownTheFloatingHook(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	angler := connect()
	anglerID := angler.JoinAs("Angler")
	srv.GiveSlot("Angler", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	// A pool for the hook to hold on: bobbing in water, a hook stays out
	// until its angler reels it in.
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	angler.MoveTo(2.5, 64, 1.5, 0, 40)
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(angler, 0, 40)
	_, spawns, _, _, _, _ := fishingRead(angler, srv, 20)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d bobbers, want the one rod's", len(cast))
	}

	// The joiner's own read of their join: the send-off's spawn is among
	// the world's own packets, before the chunks.
	late := connect()
	late.Login("Watchman")
	late.Configure()
	var seen *servertest.ThrownSpawn
	late.ReadJoin(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySpawnEntity {
			return
		}
		if s := servertest.ReadThrownSpawn(late, data); s.EntityType == protocol.EntityTypeFishingBobber {
			seen = &s
		}
	})
	late.MarkLoaded()
	if seen == nil {
		t.Fatal("the joiner was shown no fishing bobber")
	}
	if seen.OwnerID != anglerID {
		t.Errorf("the shown hook names entity %d in its data field, want its angler's %d", seen.OwnerID, anglerID)
	}
	// The hook shown where it floats: on the pool it was cast on, its
	// flight having carried it a few blocks south of the angler's feet.
	if seen.X < 0 || seen.X > 4 || seen.Z < 1 || seen.Z > 6 {
		t.Errorf("the shown hook floats at (x=%v, z=%v), want the pool the cast put it in", seen.X, seen.Z)
	}
}

// fishingParticle is one Level Particles packet the hook's water gave off:
// the type, the place, the spread, the speed and the count.
type fishingParticle struct {
	particle          int32
	x, y, z           float64
	dx, dy, dz, speed float32
	count             int32
}

// fishingParticles collects the Level Particles packets while the ticks run,
// stepping over the rest.
func fishingParticles(c *servertest.Client, srv *server.Server, ticks int) []fishingParticle {
	c.T.Helper()
	var out []fishingParticle
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		{
			if gotID != protocol.IDPlayLevelParticles {
				return false
			}
			r := protocol.NewReader(data)
			particle := r.ReadVarInt()
			tail := servertest.ReadParticleTail(c.T, r)
			x, y, z, dx, dy, dz, speed, count := tail.X, tail.Y, tail.Z, tail.DX, tail.DY, tail.DZ, tail.Speed, tail.Count
			out = append(out, fishingParticle{
				particle: particle, x: x, y: y, z: z,
				dx: dx, dy: dy, dz: dz, speed: speed, count: count,
			})
		}
		return false
	})
	return out
}

// TestABittenHookSendsItsParticles winds a hook to the bite and checks the
// water's two particles: the bubble and the fishing ripples around it.
func TestABittenHookSendsItsParticles(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Bubbler")
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 4; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	servertest.UseHeld(c, 0, 40)
	fishingRead(c, srv, 20)
	if !srv.WindBobberBite("Bubbler") {
		t.Fatal("no hook was out to wind")
	}
	var bubble, fishing bool
	for range 30 {
		if bubble && fishing {
			break
		}
		for _, p := range fishingParticles(c, srv, 2) {
			switch p.particle {
			case protocol.ParticleBubble:
				bubble = true
			case protocol.ParticleFishing:
				fishing = true
			}
		}
	}
	if !bubble {
		t.Error("the bite sent no bubble particles")
	}
	if !fishing {
		t.Error("the bite sent no fishing particles")
	}
}

// TestAHookTakesAnArmorStand casts at a summoned armor stand: alive and
// pickable, it is a body the hook may take, named on the wire, and reeling
// tells its pull and wears the rod the five a body's hook costs.
func TestAHookTakesAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Stander")
	srv.RunLine("summon minecraft:armor_stand 3.5 64 0.5")
	servertest.AwaitTicks(t, srv, 2)
	c.MoveTo(0.5, 64, 0.5, -90, 10)
	servertest.UseHeld(c, -90, 10)
	_, spawns, metas, _, _, _ := fishingRead(c, srv, 40)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID := cast[0].EntityID
	var hooked bool
	for _, m := range metas {
		if m.id == hookID && m.hookedTold && m.hooked != 0 {
			hooked = true
		}
	}
	if !hooked {
		t.Fatal("the hook never named the armor stand on the wire")
	}
	servertest.UseHeld(c, -90, 10)
	_, _, _, _, events, _ := fishingRead(c, srv, 40)
	var pull bool
	for _, ev := range events {
		if ev.Id == hookID && ev.Status == protocol.EntityStatusFishingPull {
			pull = true
		}
	}
	if !pull {
		t.Error("the armor stand's hook played no pull event")
	}
	if got := srv.InventorySlot("Stander", 36); got.Damage != 5 {
		t.Errorf("the rod's damage = %d after the body's pull, want the five", got.Damage)
	}
}

// TestReelingInAHookOnAnItemWearsTheRodThree hooks a dropped item and
// checks the wear: the three a reel of an item-body costs.
func TestReelingInAHookOnAnItemWearsTheRodThree(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Picker")
	// A stone platform east of the angler for the item to rest on, flat so
	// the low pick-up height stays in the cast's path.
	for x := int32(2); x <= 5; x++ {
		for z := int32(-2); z <= 3; z++ {
			srv.SetBlock(x, 64, z, servertest.MustState(t, "minecraft:stone", nil))
		}
	}
	id := srv.SpawnItemEntityDelay(3.5, 65.5, 0.5, "minecraft:dirt", 1, 1000000)
	if id == 0 {
		t.Fatal("SpawnItemEntityDelay came to nothing")
	}
	servertest.AwaitTicks(t, srv, 2)
	c.MoveTo(0.5, 64, 0.5, -90, 5)
	servertest.UseHeld(c, -90, 5)
	_, spawns, metas, _, _, _ := fishingRead(c, srv, 40)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID := cast[0].EntityID
	var hooked bool
	for _, m := range metas {
		if m.id == hookID && m.hookedTold && m.hooked == id {
			hooked = true
		}
	}
	if !hooked {
		t.Fatalf("the hook never took the item %d", id)
	}
	servertest.UseHeld(c, -90, 5)
	fishingRead(c, srv, 40)
	if got := srv.InventorySlot("Picker", 36); got.Damage != 3 {
		t.Errorf("the rod's damage = %d after the item's pull, want the three", got.Damage)
	}
}

// TestAGroundedReelWearsTheRodTwo casts onto dry ground, steps the hook by
// hand until it lies on the ground — the loop held, so the resting
// hook's alternating ground contact cannot slip out from under the reel —
// and reels: the grounded two a surviving rod takes.
func TestAGroundedReelWearsTheRodTwo(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Beacher")
	c.MoveTo(0.5, 64, 0.5, 0, 30)
	servertest.UseHeld(c, 0, 30)
	c.Settle()
	for range 400 {
		if on, ok := srv.BobberOnGround("Beacher"); ok && on {
			break
		}
		servertest.RunTicks(t, srv, 1)
	}
	if on, ok := srv.BobberOnGround("Beacher"); !ok || !on {
		t.Fatal("the hook never lay grounded to reel")
	}
	servertest.UseHeld(c, 0, 30)
	c.Settle() // the reel is acted on before a tick can lift the hook off the ground
	fishingRead(c, srv, 20)
	if got := srv.InventorySlot("Beacher", 36); got.Damage != 2 {
		t.Errorf("the rod's damage = %d after a grounded reel, want the two", got.Damage)
	}
}

// TestTheHookGoesWhenTheAnglerWalksPastItsRange walks the angler past the
// thirty-two blocks the line is held by and checks the hook leaves the
// world.
func TestTheHookGoesWhenTheAnglerWalksPastItsRange(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Wanderer")
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(c, 0, 0)
	_, spawns, _, _, _, _ := fishingRead(c, srv, 10)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID := cast[0].EntityID
	c.MoveToGround(40.5, 64, 0.5, true)
	gone := false
	for range 30 {
		if gone {
			break
		}
		_, _, _, _, _, removes := fishingRead(c, srv, 2)
		for _, ids := range removes {
			for _, one := range ids {
				if one == hookID {
					gone = true
				}
			}
		}
	}
	if !gone {
		t.Error("the hook stayed out past its angler's range")
	}
}

// TestTheHookGoesWhenTheRodLeavesTheHand puts the rod out of the angler's
// hands and checks the line goes: no rod held, no hook.
func TestTheHookGoesWhenTheRodLeavesTheHand(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Stasher")
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(c, 0, 0)
	_, spawns, _, _, _, _ := fishingRead(c, srv, 10)
	cast := spawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	hookID := cast[0].EntityID
	srv.GiveSlot("Stasher", 36, protocol.ItemStack{})
	gone := false
	for range 30 {
		if gone {
			break
		}
		_, _, _, _, _, removes := fishingRead(c, srv, 2)
		for _, ids := range removes {
			for _, one := range ids {
				if one == hookID {
					gone = true
				}
			}
		}
	}
	if !gone {
		t.Error("the hook stayed out with the rod out of hand")
	}
}

// TestAnEnchantedRodsCastCarriesItsLuckAndLure gives the angler a rod
// enchanted with Luck of the Sea III and Lure II and checks the cast's
// hook carries what the rod reads: three luck and the two hundred ticks
// off the wait.
func TestAnEnchantedRodsCastCarriesItsLuckAndLure(t *testing.T) {
	t.Parallel()
	srv, c, _ := givenRod(t, "Enchanter")
	luck, ok := registry.EnchantmentID("minecraft:luck_of_the_sea")
	if !ok {
		t.Fatal("the enchantment registry has no luck_of_the_sea")
	}
	lure, ok := registry.EnchantmentID("minecraft:lure")
	if !ok {
		t.Fatal("the enchantment registry has no lure")
	}
	rod := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1,
		Enchantments: map[int32]int32{luck: 3, lure: 2}}
	srv.GiveSlot("Enchanter", 36, rod)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(c, 0, 0)
	fishingRead(c, srv, 10)
	gotLuck, gotLure, ok := srv.BobberLuckLure("Enchanter")
	if !ok {
		t.Fatal("no hook was out to read")
	}
	if gotLuck != 3 {
		t.Errorf("the cast's hook carries luck %d, want the rod's three", gotLuck)
	}
	if gotLure != 200 {
		t.Errorf("the cast's hook carries %d lure ticks, want the rod's two hundred", gotLure)
	}
}
