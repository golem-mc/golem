package combat

import (
	"bytes"
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The thrown potions as the wire carries them. A splash shatters on
// impact with the glass sound's world event and poisons whoever its spray
// reaches, the distance scaling the poison's duration; a lingering one
// leaves an area effect cloud that keeps poisoning the bodies standing
// inside it while it shrinks, and then expires and poisons no one any
// more.

const potionDeadlineTicks = 120

// potionStack writes a stack of the item given carrying a brewed potion
// and nothing else: the potion by its ID, no custom colour, no custom
// effects and no custom name — the shape of the value the wire's
// potion_contents component holds.
func potionStack(t *testing.T, item string, potion int32, count int32) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(potion)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: servertest.MustItemID(t, item), Count: count,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// poisonBrewing is the ID of the plain poison brewing; poisonEffect the
// status effect it hands out, poisonFlags the flags the drinking tests'
// packet carries.
var (
	poisonBrewing       = registry.MustPotion("minecraft:poison")
	poisonEffect        = registry.MustMobEffect("minecraft:poison")
	instantDamageEffect = registry.MustMobEffect("minecraft:instant_damage")
	instantHealthEffect = registry.MustMobEffect("minecraft:instant_health")
	absorptionEffect    = registry.MustMobEffect("minecraft:absorption")
	saturationEffect    = registry.MustMobEffect("minecraft:saturation")
	poisonFlags         = uint8(0x02 | 0x04 | 0x08)
)

// poisonColour is the opaque ARGB the poison brewing's particles travel
// with: the brewing's one effect's own colour, at full alpha.
func poisonColour() int32 {
	opaque := int32(0xFF)
	return opaque<<24 | int32(registry.MobEffectOf(poisonEffect).Color&0xFFFFFF)
}

// worldEffect is one World Event packet: the event number, the block it
// plays at and the data the potion's colour rides as.
type worldEffect struct {
	event   int32
	x, y, z int32
	data    int32
}

func worldEffectFor(data []byte) worldEffect {
	r := protocol.NewReader(data)
	w := worldEffect{event: r.ReadI32()}
	w.x, w.y, w.z = r.ReadPosition()
	w.data = r.ReadI32()
	return w
}

// cloudMeta is what one Set Entity Metadata packet said about an area
// effect cloud: the entries it carried, each with whether it was there.
type cloudMeta struct {
	entityID     int32
	hasRadius    bool
	radius       float32
	hasWaiting   bool
	waiting      bool
	hasParticle  bool
	particleType int32
	colour       int32
}

// readCloudMeta parses one Set Entity Metadata packet of a cloud the test
// hears; ok is false for anyone else's metadata.
func readCloudMeta(c *servertest.Client, data []byte, clouds map[int32]bool) (cloudMeta, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := cloudMeta{entityID: r.ReadVarInt()}
	if !clouds[m.entityID] {
		return m, false
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexCloudRadius:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("cloud radius field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			m.radius, m.hasRadius = r.ReadF32(), true
		case protocol.MetadataIndexCloudWaiting:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("cloud waiting field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			m.waiting, m.hasWaiting = r.ReadU8() != 0, true
		case protocol.MetadataIndexCloudParticle:
			if fieldType != protocol.MetadataParticle {
				c.T.Fatalf("cloud particle field type = %d, want %d (a particle)", fieldType, protocol.MetadataParticle)
			}
			m.particleType, m.colour, m.hasParticle = r.ReadVarInt(), r.ReadI32(), true
		default:
			servertest.SkipBodyEntry(c, r, fieldType)
		}
	}
	if r.Err() != nil {
		c.T.Fatalf("cloud metadata: %v", r.Err())
	}
	if r.Remaining() != 0 {
		c.T.Errorf("cloud metadata has %d bytes left over", r.Remaining())
	}
	return m, true
}

// potionWitness collects what a potion's throw and its aftermath told the
// wire: the sounds, the thrown body's and the cloud's spawns, the world
// events, the thrown bodies' item metadata and removals, and the living
// bodies' effect tells — the poison particles a body's metadata carried,
// the mob-effect packets the players were sent, and the magic bites'
// damage events with the healths. It steps over the rest.
type potionWitness struct {
	sounds      []servertest.ThrownSound
	spawns      []servertest.ThrownSpawn
	metas       []servertest.ThrownMeta
	events      []servertest.ThrownEvent
	worldEvents []worldEffect
	mobEffects  []servertest.MobEffect
	cloudMetas  []cloudMeta
	healths     map[int32][]float32
	// particleTells holds the body's effect particles in the order their
	// metadata told of them, one entry per packet that carried the entry.
	particleTells map[int32][][][2]int32
	// magicBites counts the magic damage events, by the body each bit.
	magicBites map[int32]int
	removes    map[int32]int
	bodies     map[int32]bool
	clouds     map[int32]bool
	thrownIDs  map[int32]bool
}

func newPotionWitness() *potionWitness {
	return &potionWitness{
		healths:       map[int32][]float32{},
		particleTells: map[int32][][][2]int32{},
		magicBites:    map[int32]int{},
		removes:       map[int32]int{},
		bodies:        map[int32]bool{},
		clouds:        map[int32]bool{},
		thrownIDs:     map[int32]bool{},
	}
}

// hears marks a living body whose effect metadata the witness reads.
func (w *potionWitness) hears(entityID int32) {
	w.bodies[entityID] = true
}

// listenWhile reads the packets the window leaves and keeps the potion's
// parts of them.
func (w *potionWitness) listenWhile(srv *server.Server, c *servertest.Client, ticks int) {
	c.T.Helper()
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		w.note(c, gotID, data)
		return false
	})
}

// note keeps one packet's potion-relevant parts.
func (w *potionWitness) note(c *servertest.Client, packetID int32, data []byte) {
	c.T.Helper()
	switch packetID {
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
		w.sounds = append(w.sounds, servertest.ThrownSound{
			Name: name, Category: category,
			X: x, Y: y, Z: z, Volume: volume, Pitch: pitch,
		})
	case protocol.IDPlaySpawnEntity:
		spawn := servertest.ReadThrownSpawn(c, data)
		w.spawns = append(w.spawns, spawn)
		switch spawn.EntityType {
		case protocol.EntityTypeAreaEffectCloud:
			w.clouds[spawn.EntityID] = true
		case protocol.EntityTypeSplashPotion, protocol.EntityTypeLingeringPotion:
			w.thrownIDs[spawn.EntityID] = true
		}
	case protocol.IDPlayEntityMetadata:
		m, mine := servertest.ReadBodyMetadata(c, data, w.bodies)
		if mine {
			if m.HasParticles {
				w.particleTells[m.EntityID] = append(w.particleTells[m.EntityID], m.Particles)
			}
			if m.HasHealth {
				w.healths[m.EntityID] = append(w.healths[m.EntityID], m.Health)
			}
			return
		}
		if cm, mine := readCloudMeta(c, data, w.clouds); mine {
			w.cloudMetas = append(w.cloudMetas, cm)
			return
		}
		if tm, mine := readThrownItemMeta(c, data, w.thrownIDs); mine {
			w.metas = append(w.metas, tm)
		}
	case protocol.IDPlayEntityEvent:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		w.events = append(w.events, servertest.ThrownEvent{Id: id, Status: r.ReadU8()})
	case protocol.IDPlayRemoveEntities:
		for _, id := range removeEntitiesOf(c, data) {
			w.removes[id]++
		}
	case protocol.IDPlayWorldEvent:
		w.worldEvents = append(w.worldEvents, worldEffectFor(data))
	case protocol.IDPlayUpdateMobEffect:
		w.mobEffects = append(w.mobEffects, servertest.MobEffectFor(data))
	case protocol.IDPlayDamageEvent:
		r := protocol.NewReader(data)
		victim := r.ReadVarInt()
		if kind := r.ReadVarInt(); kind == registry.MustID("minecraft:damage_type", "minecraft:magic") {
			w.magicBites[victim]++
		}
	}
}

// removeEntitiesOf reads one Remove Entities packet's IDs.
func removeEntitiesOf(c *servertest.Client, data []byte) []int32 {
	c.T.Helper()
	r := protocol.NewReader(data)
	var ids []int32
	for count := r.ReadVarInt(); count > 0; count-- {
		ids = append(ids, r.ReadVarInt())
	}
	return ids
}

// readThrownItemMeta parses the item-slot metadata of one thrown body the
// test hears, reporting whether the packet was its.
func readThrownItemMeta(c *servertest.Client, data []byte, thrownIDs map[int32]bool) (servertest.ThrownMeta, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := servertest.ThrownMeta{Id: r.ReadVarInt()}
	if !thrownIDs[m.Id] {
		return m, false
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		if index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
			stack, ok := r.ReadSlot()
			if !ok {
				c.T.Fatalf("the thrown body's item slot did not read")
			}
			m.Count, m.Item = stack.Count, stack.ItemID
		} else {
			servertest.SkipBodyEntry(c, r, fieldType)
		}
	}
	if r.Err() != nil {
		c.T.Fatalf("thrown body metadata: %v", r.Err())
	}
	if r.Remaining() != 0 {
		c.T.Errorf("thrown body metadata has %d bytes left over", r.Remaining())
	}
	return m, m.Item != 0
}

// spawnOfType is the first spawn of the entity type given, nil if none
// came.
func (w *potionWitness) spawnOfType(entityType int32) *servertest.ThrownSpawn {
	for i := range w.spawns {
		if w.spawns[i].EntityType == entityType {
			return &w.spawns[i]
		}
	}
	return nil
}

// soundNamed is the first sound of the name given, nil if none played.
func (w *potionWitness) soundNamed(name string) *servertest.ThrownSound {
	for i := range w.sounds {
		if w.sounds[i].Name == name {
			return &w.sounds[i]
		}
	}
	return nil
}

// throwSounds checks a throw's own sound whole: the name given, at the
// category given, from the thrower's feet, at the vanilla half, at the
// roll's pitch.
func (w *potionWitness) throwSounds(t *testing.T, name string, category int32) {
	t.Helper()
	sound := w.soundNamed(name)
	if sound == nil {
		t.Fatalf("no %s was played", name)
	}
	if sound.Category != category {
		t.Errorf("the throw's sound category = %d, want %d", sound.Category, category)
	}
	if sound.X != 0.5 || sound.Y != 64 || sound.Z != 0.5 {
		t.Errorf("the throw's sound played from (%v %v %v), want the thrower's feet", sound.X, sound.Y, sound.Z)
	}
	if sound.Volume != 0.5 {
		t.Errorf("the throw's sound volume = %v, want the vanilla half", sound.Volume)
	}
	if sound.Pitch < 0.3 || sound.Pitch > 0.51 {
		t.Errorf("the throw's sound pitch = %v, want the roll's third to a half", sound.Pitch)
	}
}

// TestASplashPoisonPotionShattersAndPoisonsTheCowBesideIt uses a splash
// Poison potion aimed at the thrower's own feet and walks the whole
// shatter over the wire: the throw's spawn and sound, the glass break's
// world event with the poison's colour at the impact's block, no
// entity-status event a thrown potion never plays, one removal, and the
// poison landing in the cow a block and a half away — the particles its
// metadata tells the watchers of, and the magic bite the rhythm lands
// within the window. The hand threw one of its two potions and kept no
// glass bottle: thrown potions vanish whole.
func TestASplashPoisonPotionShattersAndPoisonsTheCowBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Splasher")
	c.Configure()
	playerID := c.EnterWorldAs("Splasher")
	c.Settle()

	cow := srv.SpawnMob("minecraft:cow", 2.0, 64, 0.5)
	cowSpawn := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cowSpawn.EntityID != cow {
		t.Fatalf("the planted cow spawned as id %d, want %d", cowSpawn.EntityID, cow)
	}
	srv.SetMobNoAI(cow)

	splash := potionStack(t, "minecraft:splash_potion", poisonBrewing, 2)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Splasher", 36, splash)
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	w := newPotionWitness()
	w.hears(cow)
	w.listenWhile(srv, c, potionDeadlineTicks)

	thrown := w.spawnOfType(protocol.EntityTypeSplashPotion)
	if thrown == nil {
		t.Fatal("no splash potion came into the world for the throw")
	}
	if thrown.OwnerID != playerID {
		t.Errorf("the potion's data field names entity %d, want the thrower's %d", thrown.OwnerID, playerID)
	}
	if d := thrown.Y - (64 + 1.62 - 0.1); d < -0.01 || d > 0.01 {
		t.Errorf("the potion was born at y=%v, want the thrower's eye less its tenth", thrown.Y)
	}
	if d := thrown.X - 0.5; d < -0.02 || d > 0.02 {
		t.Errorf("the potion was born at x=%v, want the thrower's own", thrown.X)
	}
	// The potion leaves at the power of a half, pitched down.
	speed := thrown.Vx*thrown.Vx + thrown.Vy*thrown.Vy + thrown.Vz*thrown.Vz
	if speed < 0.2 || speed > 0.3 {
		t.Errorf("the potion flies at %v blocks a tick squared, want the half power's square's neighbourhood", speed)
	}
	if thrown.Vy >= 0 {
		t.Errorf("a throw pitched down flew up: vy=%v", thrown.Vy)
	}
	w.throwSounds(t, "minecraft:entity.splash_potion.throw", protocol.SoundCategoryPlayer)

	// The item the body is a stack of: one splash potion, twice — the
	// spawn's telling and the tracked entity's first pass.
	metas := 0
	for i := range w.metas {
		if w.metas[i].Id == thrown.EntityID {
			metas++
			if w.metas[i].Count != 1 || w.metas[i].Item != servertest.MustItemID(t, "minecraft:splash_potion") {
				t.Errorf("metadata %d carried %d of item %d, want the one splash potion", i, w.metas[i].Count, w.metas[i].Item)
			}
		}
	}
	if metas != 2 {
		t.Fatalf("the potion's metadata arrived %d times, want the spawn's and the first pass's", metas)
	}

	// The shatter: the glass break's particle world event at the impact's
	// block,
	// carrying the poison's colour. No entity event beside it: the
	// status-three the egg and the snowball play is their own classes'.
	splashEvents := 0
	for _, ev := range w.worldEvents {
		if ev.event != 2002 || ev.data != poisonColour() {
			continue
		}
		if ev.x != 0 || ev.z != 0 || ev.y < 63 || ev.y > 64 {
			t.Errorf("the splash played at block (%d %d %d), want the thrower's feet", ev.x, ev.y, ev.z)
			continue
		}
		splashEvents++
	}
	if splashEvents != 1 {
		t.Errorf("the splash's world events with the poison's colour = %d, want the impact's one", splashEvents)
	}
	if len(w.events) != 0 {
		t.Errorf("the potion's entity events = %#v, want none: the shatter's telling is the world event's alone", w.events)
	}
	if w.removes[thrown.EntityID] != 1 {
		t.Errorf("the potion was taken out of the world %d times, want the shatter's once", w.removes[thrown.EntityID])
	}

	// The poison in the cow: the particles its metadata tells the
	// watchers of, and the bite the rhythm lands within the window.
	tells := w.particleTells[cow]
	if len(tells) == 0 {
		t.Fatal("the splash never poisoned the cow")
	}
	if got := tells[0]; len(got) != 1 || got[0] != servertest.PoisonParticles[0] {
		t.Errorf("the poisoned cow's particles = %v, want %v", got, servertest.PoisonParticles)
	}
	if w.magicBites[cow] == 0 {
		t.Error("the poison the splash left in the cow never bit as magic")
	}
	if healths := w.healths[cow]; len(healths) == 0 || healths[len(healths)-1] >= 10 {
		t.Errorf("the poisoned cow's healths = %v, want the bite's tell under ten", healths)
	}

	// The survival hand threw one of its two potions and no bottle crept
	// into its place.
	if left := srv.InventorySlot("Splasher", 36); left.ItemID != splash.ItemID || left.Count != 1 {
		t.Errorf("the hand holds %+v after the throw, want one thrown potion poorer and no bottle", left)
	}
}

// TestASplashPoisonPotionScalesItsPoisonByDistance throws a splash Poison
// potion at the thrower's own feet and reads the two updates the poison
// comes as: the thrower, whose box holds the shatter, takes the brewing's
// whole 900 ticks, while the player standing two blocks east takes the
// fraction its distance leaves.
func TestASplashPoisonPotionScalesItsPoisonByDistance(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Splasher")
	a.Configure()
	aID := a.EnterWorldAs("Splasher")

	b := connect()
	b.Login("Bystander")
	b.Configure()
	bID := b.EnterWorldAs("Bystander")
	b.MoveTo(2.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	srv.GiveSlot("Splasher", 36, potionStack(t, "minecraft:splash_potion", poisonBrewing, 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	awaitEffect := func(c *servertest.Client, entityWant int32) servertest.MobEffect {
		c.T.Helper()
		var got servertest.MobEffect
		if !c.ReadTicksSkipping(srv, potionDeadlineTicks, nil, func(id int32, data []byte) bool {
			if id != protocol.IDPlayUpdateMobEffect {
				return false
			}
			e := servertest.MobEffectFor(data)
			if e.Entity == entityWant && e.Effect == poisonEffect {
				got = e
				return true
			}
			return false
		}) {
			c.T.Fatalf("no poison was given to entity %d", entityWant)
		}
		return got
	}

	mine := awaitEffect(a, aID)
	theirs := awaitEffect(b, bID)
	if mine.Amplifier != 0 || mine.Duration != 900 || mine.Flags != poisonFlags {
		t.Errorf("the thrower's poison = %+v, want the brewing's whole 900 ticks at the drinking flags", mine)
	}
	// Two blocks east: the box gap to the shatter is under a block, the
	// margin the hit test's adds taking it to one — the brewing's 900
	// ticks scaled by somewhere near a quarter off.
	if theirs.Amplifier != 0 || theirs.Flags != poisonFlags {
		t.Errorf("the bystander's poison = %+v, want the poison at the drinking flags", theirs)
	}
	if theirs.Duration <= 300 || theirs.Duration >= mine.Duration {
		t.Errorf("the bystander %d east got %d of poison, want the distance-scaled share below the thrower's %d",
			bID, theirs.Duration, mine.Duration)
	}
}

// TestALingeringPotionLeavesAShrinkingCloudThatPoisonsAndExpires throws a
// lingering Poison potion at the thrower's feet and follows the cloud it
// leaves: the spawn of minecraft:area_effect_cloud where the potion
// shattered, its metadata carrying only the coloured particle its defaults
// leave out, the glass break's world event beside it, and then the poison
// itself — a cow teleported into the cloud gets it, a cleared poison comes
// back within the cloud's reapplication, and once the shrunk cloud is done
// the poison clears for good.
func TestALingeringPotionLeavesAShrinkingCloudThatPoisonsAndExpires(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Lingerer"))
	c := connect()
	c.Login("Lingerer")
	c.Configure()
	c.EnterWorldAs("Lingerer")
	c.Settle()

	// The cow starts well clear of the cloud the throw will leave, and
	// walks in only once the cloud is stood up.
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, 0.5)
	cowSpawn := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cowSpawn.EntityID != cow {
		t.Fatalf("the planted cow spawned as id %d, want %d", cowSpawn.EntityID, cow)
	}
	srv.SetMobNoAI(cow)
	// Held from the throw on: the thrower stands where the cloud comes up,
	// and a free loop's extra ticks before the stop below would let the
	// cloud's pass touch it and spend half a block of the radius.
	servertest.HoldTicks(t, srv)

	srv.GiveSlot("Lingerer", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	// The stream up to the cloud's arrival: everything the throw and the
	// shatter told on the way, one cloud pairing at its end — the spawn
	// and the metadata that follows it. The tick loop stops behind the
	// pairing, so the pass's calendar is the test's own from here.
	w := newPotionWitness()
	w.hears(cow)
	if !c.ReadTicksSkipping(srv, potionDeadlineTicks, nil, func(id int32, data []byte) bool {
		w.note(c, id, data)
		for _, m := range w.cloudMetas {
			if m.hasParticle {
				return true
			}
		}
		return false
	}) {
		t.Fatal("no area effect cloud arrived for the lingering throw")
	}
	srv.StopTick()
	// The loop's last ticks may have told their own metadata before the
	// stop landed — waiting's flip among them — so the settle that waits
	// for the server to settle collects instead of dropping: whatever the
	// cloud told is the witness's to check.
	c.SettleAndCollect(func(id int32, data []byte) { w.note(c, id, data) })

	thrown := w.spawnOfType(protocol.EntityTypeLingeringPotion)
	if thrown == nil {
		t.Fatal("no lingering potion came into the world for the throw")
	}
	w.throwSounds(t, "minecraft:entity.lingering_potion.throw", protocol.SoundCategoryNeutral)
	splashEvents := 0
	for _, ev := range w.worldEvents {
		if ev.event == 2002 && ev.data == poisonColour() && ev.x == 0 && ev.z == 0 && ev.y >= 63 && ev.y <= 64 {
			splashEvents++
		}
	}
	if splashEvents != 1 {
		t.Errorf("the shatter's world events with the poison's colour = %d, want the impact's one", splashEvents)
	}
	if len(w.events) != 0 {
		t.Errorf("the potion's entity events = %#v, want none: a thrown potion plays no status of its own", w.events)
	}
	if w.removes[thrown.EntityID] != 1 {
		t.Errorf("the potion was taken out of the world %d times, want the shatter's once", w.removes[thrown.EntityID])
	}
	metas := 0
	for i := range w.metas {
		if w.metas[i].Id == thrown.EntityID {
			metas++
			if w.metas[i].Count != 1 || w.metas[i].Item != servertest.MustItemID(t, "minecraft:lingering_potion") {
				t.Errorf("metadata %d carried %d of item %d, want the one lingering potion", i, w.metas[i].Count, w.metas[i].Item)
			}
		}
	}
	if metas != 2 {
		t.Fatalf("the potion's metadata arrived %d times, want the spawn's and the first pass's", metas)
	}

	cloud := w.spawnOfType(protocol.EntityTypeAreaEffectCloud)
	if d := cloud.X - 0.5; d < -0.1 || d > 0.1 {
		t.Errorf("the cloud stands at x=%v, want the shatter point's own", cloud.X)
	}
	if d := cloud.Y - 64; d < -0.11 || d > 0.1 {
		t.Errorf("the cloud stands at y=%v, want the shatter point's own", cloud.Y)
	}
	if d := cloud.Z - 0.5; d < -0.1 || d > 0.1 {
		t.Errorf("the cloud stands at z=%v, want the shatter point's own", cloud.Z)
	}
	// The cloud's first metadata: only the particle it rains is no
	// default — the radius and the waiting flag stay the client's own
	// 3.0 and clear until they change.
	spawnMetas := 0
	for _, m := range w.cloudMetas {
		if m.entityID != cloud.EntityID || !m.hasParticle {
			continue
		}
		spawnMetas++
		if m.particleType != protocol.ParticleEntityEffect || m.colour != poisonColour() {
			t.Errorf("the cloud's particle = (%d, %#x), want the poison's entity effect", m.particleType, m.colour)
		}
		if m.hasRadius || m.hasWaiting {
			t.Errorf("the cloud's first metadata carried a radius %v or waiting %v, want only the particle", m.hasRadius, m.hasWaiting)
		}
	}
	if spawnMetas != 1 {
		t.Fatalf("the cloud's particle metadatas = %d, want the spawn's one", spawnMetas)
	}

	// The thrower steps clear of the cloud before the pass's calendar
	// runs again: the player it stands on would be its first body, and
	// every touch of one costs the cloud half a block of its radius.
	c.ChatCommand("tp Lingerer -6.0 64 0.5")

	// A tick of the cloud and a tick of the cows: the cloud's pass runs
	// only every fifth tick, so anything less than five ticks after a
	// change is soon.
	tick := func() {
		srv.TickClouds(1)
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) { w.note(c, id, data) })
	}
	poisoned := func() bool {
		tells := w.particleTells[cow]
		return len(tells) > 0 && len(tells[len(tells)-1]) > 0
	}
	cleared := func() bool {
		tells := w.particleTells[cow]
		return len(tells) > 0 && len(tells[len(tells)-1]) == 0
	}

	// The cloud's first tick turns the waiting flag on, held behind the
	// wait its brewing's cloud enters the world on — told live at the
	// stop's edge or by the first hand tick, whichever reached the wire.
	tick()
	waitingTold := false
	for _, m := range w.cloudMetas {
		if m.entityID == cloud.EntityID && m.hasWaiting && m.waiting {
			waitingTold = true
		}
	}
	if !waitingTold {
		t.Error("the cloud's waiting flag was never told, want the first tick's true")
	}

	// The cow walks into the cloud and the cloud's pass poisons it once
	// the wait's ten ticks are spent and a five-tick pass falls.
	c.ChatCommand("tp @e[type=minecraft:cow] 2.0 64 0.5")
	for range 15 {
		tick()
		if poisoned() {
			break
		}
	}
	if !poisoned() {
		t.Fatal("the cloud never poisoned the cow that walked into it")
	}

	// The cloud's reapplication: a poison cleared from the cow comes back
	// within the twenty ticks the cloud waits between its touches.
	w.particleTells[cow] = nil
	c.ChatCommand("effect clear @e[type=minecraft:cow]")
	for range 8 {
		tick()
		if cleared() {
			break
		}
	}
	if !cleared() {
		t.Fatal("the cloud's poison was never cleared from the cow")
	}
	cameBack := false
	for range 30 {
		tick()
		if poisoned() {
			cameBack = true
			break
		}
	}
	if !cameBack {
		t.Fatal("the cleared poison never came back, want the cloud to poison its bodies again")
	}
	if tells := w.particleTells[cow]; len(tells) == 0 || !slices.Equal(tells[len(tells)-1], servertest.PoisonParticles) {
		t.Errorf("the cloud's repoisoned particles = %v, want %v", tells[len(tells)-1], servertest.PoisonParticles)
	}

	// The cow walks out again. Then a late joiner is shown the standing
	// cloud whole — the spawn, the particle, and the radius the shrink
	// has carried so far — through the same send-off every body of the
	// dimension gets at a join or an arrival.
	c.ChatCommand("tp @e[type=minecraft:cow] 9.0 64 0.5")
	tick()
	w.particleTells[cow] = nil
	radii := w.radiusMetas(cloud.EntityID)

	// The join runs on the wall clock with the world frozen: the watcher
	// joins into the cloud's own place, and the ticks a slow join takes
	// would have the cloud's pass touch it there.
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	late := connect()
	late.Login("Watcher")
	late.Configure()
	// The cloud's pairing rides among the join's own send-offs, so the
	// join's read collects rather than drops it.
	w2 := newPotionWitness()
	late.ReadJoin(func(id int32, data []byte) { w2.note(late, id, data) })
	late.MarkLoaded()
	late.ReadTicksSkipping(srv, potionDeadlineTicks, nil, func(id int32, data []byte) bool {
		w2.note(late, id, data)
		for _, m := range w2.cloudMetas {
			if m.hasParticle && m.particleType == protocol.ParticleEntityEffect && m.colour == poisonColour() {
				return true
			}
		}
		return false
	})
	if crux := w2.spawnOfType(protocol.EntityTypeAreaEffectCloud); crux == nil {
		srv.StopTick()
		t.Fatal("the late joiner was not shown the standing cloud")
	} else if d := crux.X - cloud.X; d < -0.01 || d > 0.01 {
		srv.StopTick()
		t.Errorf("the late joiner's cloud stands at x=%v, want the living one's %v", crux.X, cloud.X)
	}
	srv.StopTick()
	srv.SetTickFrozen(false) // the hand ticks below are the world's own again
	// The watcher joined into the cloud's own place, and every body its
	// pass touches costs the radius half a block: they step clear too,
	// so the shrink's descent is the shrink's alone.
	c.ChatCommand("tp Watcher -8.0 64 0.5")
	tick()

	// The cloud shrinks to its expiry: the radius's metadata tells a
	// smaller cloud each tick past the wait, the removal takes the cloud
	// out of the world, and a cleared poison stays cleared — the expired
	// cloud poisons no one any more.
	removed := false
	for range 480 {
		tick()
		if w.removes[cloud.EntityID] > 0 {
			removed = true
			break
		}
	}
	if !removed {
		t.Fatal("the cloud never expired, want the shrunk radius's removal before the duration's own")
	}
	now := w.radiusMetas(cloud.EntityID)
	if len(now) <= len(radii) {
		t.Fatalf("the cloud's radius was told %d times over its life, want the shrinkage's every tick", len(now))
	}
	if last := now[len(now)-1]; last >= 0.55 || last < 0.495 {
		t.Errorf("the last radius told = %v, want the half-block floor the cloud dies at", last)
	}
	for _, r := range now[:min(3, len(now))] {
		if r <= 2.0 {
			t.Errorf("the cloud's radius began at %v, want the three the shatter sets", r)
		}
	}
	c.ChatCommand("effect clear @e[type=minecraft:cow]")
	for range 8 {
		tick()
		if cleared() {
			break
		}
	}
	if !cleared() {
		t.Fatal("the cow's poison was never cleared")
	}
	stayedCleared := true
	for range 40 {
		tick()
		if poisoned() {
			stayedCleared = false
			break
		}
	}
	if !stayedCleared {
		t.Error("the expired cloud poisoned the cow again after its removal")
	}
	if left := srv.InventorySlot("Lingerer", 36); left.Count != 0 {
		t.Errorf("the hand holds %+v after the throw, want the last potion spent", left)
	}
}

// radiusMetas is the cloud's radius tells, in the order their metadata
// arrived.
func (w *potionWitness) radiusMetas(entityID int32) []float32 {
	var out []float32
	for _, m := range w.cloudMetas {
		if m.entityID == entityID && m.hasRadius {
			out = append(out, m.radius)
		}
	}
	return out
}

// TestALingeringPotionScalesItsCloudEffects uses a lingering Poison potion
// and reads the poison the cloud rains into a cow: the item's own
// potion_duration_scale of a quarter brings the brewing's 900 ticks down to
// 225, where the un-scaled cloud would give the drinking strength.
func TestALingeringPotionScalesItsCloudEffects(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Lingerer")
	a.Configure()
	a.EnterWorldAs("Lingerer")
	b := connect()
	b.Login("Victim")
	b.Configure()
	bID := b.EnterWorldAs("Victim")
	b.MoveTo(2.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	srv.GiveSlot("Lingerer", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	var poison servertest.MobEffect
	if !b.ReadTicksSkipping(srv, potionDeadlineTicks, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		e := servertest.MobEffectFor(data)
		if e.Entity == bID && e.Effect == poisonEffect {
			poison = e
			return true
		}
		return false
	}) {
		t.Fatal("the cloud never poisoned the body standing in it")
	}
	if poison.Duration != 225 {
		t.Errorf("the cloud's poison lasts %d ticks, want the quarter of the brewing's 900", poison.Duration)
	}
}

// TestALingeringPotionWithoutEffectsLeavesNoCloud throws a lingering water
// and a lingering mundane potion: neither has an effect to rain, so neither
// leaves an area effect cloud, though each still plays its shatter's world
// events and is taken out of the world.
func TestALingeringPotionWithoutEffectsLeavesNoCloud(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Waterer")
	a.Configure()
	a.EnterWorldAs("Waterer")
	b := connect()
	b.Login("Mundaner")
	b.Configure()
	b.EnterWorldAs("Mundaner")
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	b.MoveTo(10.5, 64, 0.5, -90, 90)
	a.Settle()
	b.Settle()

	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Waterer", 36, potionStack(t, "minecraft:lingering_potion", registry.MustPotion("minecraft:water"), 1))
	srv.GiveSlot("Mundaner", 36, potionStack(t, "minecraft:lingering_potion", registry.MustPotion("minecraft:mundane"), 1))
	servertest.UseHeld(a, -90, 90)
	servertest.UseHeld(b, -90, 90)

	w := newPotionWitness()
	w.listenWhile(srv, a, potionDeadlineTicks)
	if cloud := w.spawnOfType(protocol.EntityTypeAreaEffectCloud); cloud != nil {
		t.Errorf("a lingering potion with no effects left a cloud: %+v", cloud)
	}
	counts := map[int32]int{}
	for _, ev := range w.worldEvents {
		counts[ev.event]++
	}
	if len(w.worldEvents) != 4 || counts[protocol.WorldEventPotionSplash] != 2 || counts[protocol.WorldEventPotionBreakSound] != 2 {
		t.Errorf("the two shatters played world events %v, want each one's 2002 and 1053", counts)
	}
}

// TestASplashWaterPotionDousesFireAndPutsOutTheBurning throws a splash
// water bottle at the thrower's own feet with a fire block beside them and
// the thrower alight: the water puts the fire out and douses the thrower,
// and leaves no cloud of any kind.
func TestASplashWaterPotionDousesFireAndPutsOutTheBurning(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Douser")
	c.Configure()
	c.EnterWorldAs("Douser")
	c.Settle()

	fire := registry.MustState("minecraft:fire", nil)
	if !srv.SetBlock(0, 64, 1, fire) {
		t.Fatal("the fire beside the thrower could not be placed")
	}
	srv.SetPlayerFire("Douser", 200)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Douser", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	w := newPotionWitness()
	w.listenWhile(srv, c, potionDeadlineTicks)
	if cloud := w.spawnOfType(protocol.EntityTypeAreaEffectCloud); cloud != nil {
		t.Errorf("a water splash left a cloud: %+v", cloud)
	}
	if got := srv.BlockAt(0, 64, 1); got != 0 {
		t.Errorf("the fire the water fell beside = state %d, want it doused to air", got)
	}
	if got := srv.PlayerFire("Douser"); got > 0 {
		t.Errorf("the burning thrower kept %d ticks of fire, want the water to put them out", got)
	}
	if w.soundNamed("minecraft:entity.generic.extinguish_fire") == nil {
		t.Error("the doused thrower's fizz was not heard")
	}
}

// TestASplashHarmingPotionScalesItsInstantDamageByDistance reads a splash
// Strong Harming's instant damage by distance: the thrower under the shatter
// takes the whole twelve of the brewing's second level, while a body two
// blocks away takes the distance-scaled share under it.
func TestASplashHarmingPotionScalesItsInstantDamageByDistance(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Thrower")
	a.Configure()
	a.EnterWorldAs("Thrower")
	b := connect()
	b.Login("Victim")
	b.Configure()
	b.EnterWorldAs("Victim")
	b.MoveTo(2.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	srv.SetHealth("Thrower", 20)
	srv.SetHealth("Victim", 20)
	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:strong_harming"), 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	servertest.UntilTicks(t, srv, potionDeadlineTicks, "condition", func() bool {
		return srv.PlayerHealth("Thrower") < 20 && srv.PlayerHealth("Victim") < 20
	})
	whole := 20 - srv.PlayerHealth("Thrower")
	scaled := 20 - srv.PlayerHealth("Victim")
	if whole < 11.9 || whole > 12.1 {
		t.Errorf("the thrower took %v damage, want the whole 12 of Strong Harming", whole)
	}
	if scaled <= 0 || scaled >= whole {
		t.Errorf("the body two blocks away took %v, want the distance-scaled share below the thrower's %v", scaled, whole)
	}
}

// TestASplashHarmingPotionKillsWithIndirectMagic splashes a Strong Harming
// at a body left on 2 health beside the shatter: the death names the thrower
// (death.attack.indirectMagic), and the instant harm is applied in place, so
// the victim is never sent an Update Mob Effect for it.
// vanilla: ThrownSplashPotion.onHitAsPotion, HealOrHarmMobEffect
func TestASplashHarmingPotionKillsWithIndirectMagic(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg)
	a := connect()
	a.JoinAs("Thrower")
	b := connect()
	bID := b.JoinAs("Victim")
	b.MoveTo(1.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	srv.SetHealth("Victim", 2)
	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:strong_harming"), 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	var sentInstant bool
	var chat []byte
	if !b.PumpTicks(srv, uint64(potionDeadlineTicks), func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayUpdateMobEffect:
			if e := servertest.MobEffectFor(data); e.Entity == bID && e.Effect == instantDamageEffect {
				sentInstant = true
			}
		case protocol.IDPlaySystemChat:
			chat = data
			return true
		}
		return false
	}) {
		t.Fatal("the victim never died of the splash")
	}
	if sentInstant {
		t.Error("the victim was sent an Update Mob Effect for instant_damage")
	}
	want := deathMessage("Victim", servertest.PlayerDisplayComponent("Thrower"))
	if !bytes.Equal(chat, want) {
		t.Errorf("death message = % X, want indirectMagic naming Victim and Thrower % X", chat, want)
	}
}

// deathMessage is the System Chat payload of an indirect magic death of the
// player named victim, by killer.
func deathMessage(victim string, killer protocol.Text) []byte {
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "death.attack.indirectMagic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(victim), killer},
	})
	want.PutBool(false)
	return want.Bytes()
}

// cloudHere stands an ownerless cloud over the spawn and drives the cloud
// pass by hand until done reports true; the loop must be stopped.
func cloudHere(t *testing.T, srv *server.Server, specs []protocol.PotionEffectSpec, done func() bool) {
	t.Helper()
	srv.SpawnOwnerlessCloud(0.5, 64, 0.5, specs)
	for range 20 {
		srv.TickClouds(1)
		if done() {
			return
		}
	}
	t.Fatal("the cloud never landed its effects")
}

// TestAnOwnerlessCloudAppliesItsEffectsInOrder drives a cloud with no owner
// whose harm precedes its absorption over a player on four health: the harm
// lands first and kills them, as the effects' order has it, with indirect
// magic whose death message names the cloud alone.
// vanilla: AreaEffectCloud.serverTick, HealOrHarmMobEffect.applyInstantaneousEffect
func TestAnOwnerlessCloudAppliesItsEffectsInOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Victim")
	srv.StopTick()
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	c.Settle()
	srv.SetHealth("Victim", 4)

	cloudHere(t, srv, []protocol.PotionEffectSpec{
		{ID: instantDamageEffect, Amplifier: 1, Duration: 1, ShowParticles: true, ShowIcon: true},
		{ID: absorptionEffect, Duration: 600, ShowParticles: true, ShowIcon: true},
	}, func() bool { return srv.PlayerHealth("Victim") != 4 })
	if h := srv.PlayerHealth("Victim"); h > 0 {
		t.Fatalf("the victim is on %v health, want the harm to land before the absorption and kill them", h)
	}
	var chat []byte
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			chat = data
		}
	})
	want := deathMessage("Victim", protocol.Text{Translate: "entity.minecraft.area_effect_cloud"})
	if !bytes.Equal(chat, want) {
		t.Errorf("death message = % X, want indirectMagic naming Victim and the cloud % X", chat, want)
	}
}

// TestACloudsSaturationLeavesMobsAlone drives a cloud of instant health and
// saturation over an injured cow and an undead zombie: only the health
// reaches them, healing the cow its share of 4 and harming the zombie its
// share of 6.
// vanilla: SaturationMobEffect.applyEffectTick, MobEffect.applyInstantaneousEffect
func TestACloudsSaturationLeavesMobsAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")
	srv.StopTick()
	cow := srv.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	zombie := srv.SpawnMob("minecraft:zombie", -0.5, 64, 0.5)
	c.Settle()
	srv.SetMobHealth(cow, 4)
	srv.SetMobHealth(zombie, 10)

	cloudHere(t, srv, []protocol.PotionEffectSpec{
		{ID: saturationEffect, Duration: 1, ShowParticles: true, ShowIcon: true},
		{ID: instantHealthEffect, Duration: 1, ShowParticles: true, ShowIcon: true},
	}, func() bool { h, _ := srv.MobHealth(cow); return h != 4 })
	if h, _ := srv.MobHealth(cow); h != 6 {
		t.Errorf("the cow is on %v health, want 6: healed by the instant health alone", h)
	}
	if h, _ := srv.MobHealth(zombie); h != 7 {
		t.Errorf("the zombie is on %v health, want 7: harmed by the instant health alone", h)
	}
}

// TestACloudsMagicBypassesAMobsArmour drives a healing cloud over a zombie in
// a diamond chestplate: the harm is the unarmoured share, since indirect
// magic bypasses armour.
// vanilla: DamageTypeTagsProvider (BYPASSES_ARMOR), HealOrHarmMobEffect.applyInstantaneousEffect
func TestACloudsMagicBypassesAMobsArmour(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")
	srv.StopTick()
	zombie := srv.SpawnMob("minecraft:zombie", -0.5, 64, 0.5)
	c.Settle()
	srv.SetMobHealth(zombie, 10)
	srv.SetMobWorn(zombie, 2, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:diamond_chestplate"), Count: 1})

	cloudHere(t, srv, []protocol.PotionEffectSpec{
		{ID: instantHealthEffect, Duration: 1, ShowParticles: true, ShowIcon: true},
	}, func() bool { h, _ := srv.MobHealth(zombie); return h != 10 })
	if h, _ := srv.MobHealth(zombie); h != 7 {
		t.Errorf("the armoured zombie is on %v health, want 7: armour does not soften magic", h)
	}
}

// TestAnOwnerlessCloudTeleportsEndermen drives an ownerless harming cloud
// over endermen with fixed seeds: with no living cause, each rolls a
// nine-in-ten teleport after the damage, so some leave their spot.
// vanilla: EnderMan.hurtServer
func TestAnOwnerlessCloudTeleportsEndermen(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")
	servertest.HoldTicks(t, srv)
	ids := make([]int32, 8)
	for i := range ids {
		ids[i] = srv.SpawnMobSeeded("minecraft:enderman", 0.5+float64(i%4)*0.2, 64, 1.5+float64(i/4)*0.2, int64(i+1))
		srv.SetMobNoAI(ids[i])
	}
	// One tick hands each enderman its world, which a hurt teleport needs.
	servertest.RunTicks(t, srv, 1)
	srv.StopTick()
	c.Settle()
	x0 := make([]float64, len(ids))
	for i, id := range ids {
		x0[i], _, _, _ = srv.MobPos(id)
	}

	cloudHere(t, srv, []protocol.PotionEffectSpec{
		{ID: instantDamageEffect, Duration: 1, ShowParticles: true, ShowIcon: true},
	}, func() bool { h, _ := srv.MobHealth(ids[0]); return h != 40 })
	moved := 0
	for i, id := range ids {
		if x, _, _, _ := srv.MobPos(id); x != x0[i] {
			moved++
		}
	}
	if moved == 0 {
		t.Error("no enderman teleported from an ownerless harming cloud, want the hurt roll to move most of them")
	}
}

// TestASplashPotionDoesNotSplashASpectator splashes a Poison potion on a
// body in spectator mode: the thrower beside the shatter is poisoned, the
// spectator no projectile or splash may touch is not.
func TestASplashPotionDoesNotSplashASpectator(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Splasher"))
	a := connect()
	a.Login("Splasher")
	a.Configure()
	aID := a.EnterWorldAs("Splasher")
	b := connect()
	b.Login("Ghost")
	b.Configure()
	bID := b.EnterWorldAs("Ghost")
	b.MoveTo(1.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	a.ChatCommand("gamemode spectator Ghost")
	a.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator
	})
	a.Settle()
	b.Settle()

	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Splasher", 36, potionStack(t, "minecraft:splash_potion", poisonBrewing, 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)

	if !a.PumpTicks(srv, uint64(potionDeadlineTicks), func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		e := servertest.MobEffectFor(data)
		return e.Entity == aID && e.Effect == poisonEffect
	}) {
		t.Fatal("the splash never poisoned the thrower under the shatter")
	}
	b.PumpTicks(srv, uint64(potionDeadlineTicks), func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		e := servertest.MobEffectFor(data)
		if e.Entity == bID {
			t.Error("a splash potion landed on a spectator no splash may touch")
			return true
		}
		return false
	})
}

// TestALingeringCloudDoesNotRainOnASpectator throws a lingering Poison
// potion at a survival body standing in the cloud beside a spectator: the
// cloud rains on the living body, and never on the spectator.
func TestALingeringCloudDoesNotRainOnASpectator(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Lingerer"))
	a := connect()
	a.Login("Lingerer")
	a.Configure()
	aID := a.EnterWorldAs("Lingerer")
	b := connect()
	b.Login("Ghost")
	b.Configure()
	bID := b.EnterWorldAs("Ghost")
	b.MoveTo(1.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()
	servertest.HoldTicks(t, srv)

	// Settled: the held loop runs its ticks as fast as they are asked
	// for, so all of them could pass before the command is read.
	a.ChatCommand("gamemode spectator Ghost")
	a.Settle()
	servertest.UntilTicks(t, srv, 1200, "condition", func() bool {
		return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator
	})
	a.Settle()
	b.Settle()

	srv.GiveSlot("Lingerer", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)
	a.Settle()

	if !a.PumpTicks(srv, uint64(potionDeadlineTicks), func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		e := servertest.MobEffectFor(data)
		return e.Entity == aID && e.Effect == poisonEffect
	}) {
		t.Fatal("the cloud never rained on the living body standing in it")
	}
	b.PumpTicks(srv, uint64(potionDeadlineTicks), func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		e := servertest.MobEffectFor(data)
		if e.Entity == bID {
			t.Error("a cloud rained on a spectator no splash may touch")
			return true
		}
		return false
	})
}

// TestACloudDoesNotStepOnItsShatterTick stands a lingering potion's cloud
// up with the tick loop stopped and drives the cloud pass by hand: the first
// pass is the one after the shatter, so the waiting flag is not told until
// the second.
func TestACloudDoesNotStepOnItsShatterTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Lingerer")
	c.Configure()
	c.EnterWorldAs("Lingerer")
	c.Settle()

	srv.GiveSlot("Lingerer", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	srv.StopTick()
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)
	c.Settle()
	srv.TickEntities(5)

	w := newPotionWitness()
	c.SettleAndCollect(func(id int32, data []byte) { w.note(c, id, data) })
	cloud := w.spawnOfType(protocol.EntityTypeAreaEffectCloud)
	if cloud == nil {
		t.Fatal("no area effect cloud came into the world for the lingering throw")
	}
	waiting := func() bool {
		for _, m := range w.cloudMetas {
			if m.entityID == cloud.EntityID && m.hasWaiting && m.waiting {
				return true
			}
		}
		return false
	}
	srv.TickClouds(1)
	c.SettleAndCollect(func(id int32, data []byte) { w.note(c, id, data) })
	if waiting() {
		t.Fatal("the cloud stepped on the very tick its potion shattered")
	}
	srv.TickClouds(1)
	c.SettleAndCollect(func(id int32, data []byte) { w.note(c, id, data) })
	if !waiting() {
		t.Fatal("the cloud never stepped on the tick after its shatter")
	}
}

// shatterOrder is the packet order a shatter's telling is checked in: the
// first index of each kind named by a test.
type shatterOrder struct {
	ids []int32
	// spawnTypes carries the entity type each Spawn Entity named, -1 for
	// every other packet.
	spawnTypes []int32
}

func newShatterOrder() *shatterOrder { return &shatterOrder{} }

func (o *shatterOrder) note(c *servertest.Client, id int32, data []byte) {
	et := int32(-1)
	if id == protocol.IDPlaySpawnEntity {
		et = servertest.ReadThrownSpawn(c, data).EntityType
	}
	o.ids = append(o.ids, id)
	o.spawnTypes = append(o.spawnTypes, et)
}

func (o *shatterOrder) first(id int32) int {
	for i := range o.ids {
		if o.ids[i] == id {
			return i
		}
	}
	return -1
}

func (o *shatterOrder) firstSpawn(entityType int32) int {
	for i := range o.ids {
		if o.ids[i] == protocol.IDPlaySpawnEntity && o.spawnTypes[i] == entityType {
			return i
		}
	}
	return -1
}

// TestAPotionShatterTellsInVanillaOrder drives a splash and a lingering
// shatter with the tick loop stopped and reads the packets' order: a splash
// tells the bodies' effects before the world event and the removal, and a
// lingering potion spawns its cloud before the world event and the removal.
func TestAPotionShatterTellsInVanillaOrder(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	splash := connect()
	splash.Login("Splasher")
	splash.Configure()
	splash.EnterWorldAs("Splasher")
	linger := connect()
	linger.Login("Lingerer")
	linger.Configure()
	linger.EnterWorldAs("Lingerer")
	linger.MoveTo(16.5, 64, 0.5, -90, 90)
	splash.Settle()
	linger.Settle()

	srv.GiveSlot("Splasher", 36, potionStack(t, "minecraft:splash_potion", poisonBrewing, 1))
	srv.GiveSlot("Lingerer", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	srv.StopTick()

	order := newShatterOrder()
	splash.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(splash, -90, 90)
	splash.Settle()
	srv.TickEntities(5)
	splash.SettleAndCollect(func(id int32, data []byte) { order.note(splash, id, data) })

	effect := order.first(protocol.IDPlayUpdateMobEffect)
	event := order.first(protocol.IDPlayWorldEvent)
	remove := order.first(protocol.IDPlayRemoveEntities)
	if effect < 0 || event < 0 || remove < 0 {
		t.Fatalf("the splash missed a shatter packet: effect %d, event %d, remove %d", effect, event, remove)
	}
	if effect >= event || event >= remove {
		t.Errorf("the splash's shatter order = effect %d, event %d, remove %d; want effect, event, removal", effect, event, remove)
	}

	dir := newShatterOrder()
	servertest.UseHeld(linger, -90, 90)
	linger.Settle()
	srv.TickEntities(5)
	linger.SettleAndCollect(func(id int32, data []byte) { dir.note(linger, id, data) })
	cloud := dir.firstSpawn(protocol.EntityTypeAreaEffectCloud)
	cevent := dir.first(protocol.IDPlayWorldEvent)
	cremove := dir.first(protocol.IDPlayRemoveEntities)
	if cloud < 0 || cevent < 0 || cremove < 0 {
		t.Fatalf("the lingering shatter missed a packet: cloud %d, event %d, remove %d", cloud, cevent, cremove)
	}
	if cloud >= cevent || cevent >= cremove {
		t.Errorf("the lingering shatter order = cloud %d, event %d, remove %d; want cloud, event, removal", cloud, cevent, cremove)
	}
}

// TestASplashWaterPotionThatHitsABodyDousesNoFire throws a splash water
// bottle point-blank at a body with lit fires at its side: vanilla douses
// only from onHitBlock, so a potion that broke on the body leaves every
// fire burning.
func TestASplashWaterPotionThatHitsABodyDousesNoFire(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Thrower")
	a.Configure()
	a.EnterWorldAs("Thrower")
	b := connect()
	b.Login("Victim")
	b.Configure()
	b.EnterWorldAs("Victim")
	b.MoveTo(1.0, 64, 0.5, 270, 0)
	a.Settle()
	b.Settle()

	fire := registry.MustState("minecraft:fire", nil)
	srv.SetBlock(0, 64, 1, registry.MustState("minecraft:stone", nil))
	srv.SetBlock(0, 64, -1, registry.MustState("minecraft:stone", nil))
	cells := [][3]int32{{0, 65, 1}, {0, 65, -1}}
	for _, cell := range cells {
		if !srv.SetBlock(cell[0], cell[1], cell[2], fire) {
			t.Fatalf("the fire at %v could not be placed", cell)
		}
	}
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	a.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.UseHeld(a, -90, 0)

	w := newPotionWitness()
	w.listenWhile(srv, a, potionDeadlineTicks)
	for _, cell := range cells {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got == 0 {
			t.Errorf("a water splash that broke on a body doused the fire at %v", cell)
		}
	}
}

// vanilla: AbstractThrownPotion.onHitBlock
func TestASplashWaterPotionThatHitsABodyLeavesTheOriginsCampfireLit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Thrower")
	a.Configure()
	a.EnterWorldAs("Thrower")
	b := connect()
	b.Login("Victim")
	b.Configure()
	b.EnterWorldAs("Victim")
	b.MoveTo(7, 64, 6.5, 270, 0)
	a.Settle()
	b.Settle()

	lit := registry.MustState("minecraft:campfire", map[string]string{"lit": "true"})
	if !srv.SetBlock(0, 0, 0, lit) {
		t.Fatal("the origin's campfire could not be placed")
	}
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	a.MoveTo(6.5, 64, 6.5, -90, 0)
	servertest.UseHeld(a, -90, 0)

	w := newPotionWitness()
	w.listenWhile(srv, a, potionDeadlineTicks)
	if got := srv.BlockAt(0, 0, 0); got != lit {
		t.Errorf("the origin's campfire = state %d, want lit state %d", got, lit)
	}
}

// TestASplashWaterPotionReachesOnlyWithinFourBlocks douses the burning
// bodies a splash water bottle reaches and no further: a body two blocks
// from the shatter is put out, one past the four-block centre-to-centre
// reach is left burning.
func TestASplashWaterPotionReachesOnlyWithinFourBlocks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	a := connect()
	a.Login("Thrower")
	a.Configure()
	a.EnterWorldAs("Thrower")
	near := connect()
	near.Login("Near")
	near.Configure()
	near.EnterWorldAs("Near")
	far := connect()
	far.Login("Far")
	far.Configure()
	far.EnterWorldAs("Far")
	near.MoveTo(2.5, 64, 0.5, 270, 0)
	far.MoveTo(4.7, 64, 0.5, 270, 0)
	a.Settle()
	near.Settle()
	far.Settle()

	// The loop is stopped and ticked by hand: the burn counts down per
	// tick, so a throw or a poll that a loaded machine delays must not let
	// the far body's own fire run out between the shatter and the read.
	srv.StopTick()
	srv.SetPlayerFire("Near", 200)
	srv.SetPlayerFire("Far", 200)
	srv.GiveSlot("Thrower", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	a.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(a, -90, 90)
	a.Settle()

	// A doused body's burn falls into the douse cooldown, twenty below
	// nought, rather than counting down from zero. The flight is a few
	// ticks; the far body's 200 outlast the budget, so it still burns
	// unless the splash reached it too.
	const flightBudget = 40
	for range flightBudget {
		if srv.PlayerFire("Near") <= 0 {
			break
		}
		srv.RunTick()
	}
	if got := srv.PlayerFire("Near"); got > 0 {
		t.Fatalf("the splash did not put out the near body: burn %d", got)
	}
	if got := srv.PlayerFire("Far"); got <= 0 {
		t.Errorf("a burning body past the four-block reach was doused: burn %d", got)
	}
}

// TestASplashWaterPotionDousesACandleWithItsSound throws a splash water
// bottle at the thrower's feet with a lit candle beside them: the candle is
// turned unlit and the block.candle.extinguish fizz is heard.
func TestASplashWaterPotionDousesACandleWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Douser")
	c.Configure()
	c.EnterWorldAs("Douser")
	c.Settle()

	srv.SetBlock(0, 64, 1, servertest.BlockStateWith(t, "minecraft:candle", "candles=1,lit=true,waterlogged=false"))
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Douser", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	w := newPotionWitness()
	w.listenWhile(srv, c, potionDeadlineTicks)
	if got, want := srv.BlockAt(0, 64, 1), servertest.BlockStateWith(t, "minecraft:candle", "candles=1,lit=false,waterlogged=false"); got != want {
		t.Errorf("the candle the water fell beside = %d, want %d (out)", got, want)
	}
	if w.soundNamed("minecraft:block.candle.extinguish") == nil {
		t.Error("the doused candle's fizz was not heard")
	}
}

// TestASplashWaterPotionPutsOutABurningMobWithItsSound throws a splash
// water bottle beside a burning mob: the mob's fire is put out and the
// neutral-category fizz is heard.
func TestASplashWaterPotionPutsOutABurningMobWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Douser")
	c.Configure()
	c.EnterWorldAs("Douser")
	c.Settle()

	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	if cow == 0 {
		t.Fatal("the cow could not be spawned")
	}
	srv.SetMobNoAI(cow)
	srv.SetMobFire(cow, 200)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Douser", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	w := newPotionWitness()
	w.listenWhile(srv, c, potionDeadlineTicks)
	fizz := w.soundNamed("minecraft:entity.generic.extinguish_fire")
	if fizz == nil {
		t.Fatal("the doused mob's fizz was not heard")
	}
	if fizz.Category != protocol.SoundCategoryNeutral {
		t.Errorf("the mob's fizz category = %d, want the neutral %d", fizz.Category, protocol.SoundCategoryNeutral)
	}
}

// TestASplashWaterPotionPutsOutABurningMonsterWithItsSound is the monster
// case of the doused-mob fizz: a monster speaks in the hostile bracket,
// the category extinguishFire takes from the body's own getSoundSource.
func TestASplashWaterPotionPutsOutABurningMonsterWithItsSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Douser")
	c.Configure()
	c.EnterWorldAs("Douser")
	c.Settle()

	zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	if zombie == 0 {
		t.Fatal("the zombie could not be spawned")
	}
	srv.SetMobNoAI(zombie)
	srv.SetMobFire(zombie, 200)
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Douser", 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)

	w := newPotionWitness()
	w.listenWhile(srv, c, potionDeadlineTicks)
	fizz := w.soundNamed("minecraft:entity.generic.extinguish_fire")
	if fizz == nil {
		t.Fatal("the doused monster's fizz was not heard")
	}
	if fizz.Category != protocol.SoundCategoryHostile {
		t.Errorf("the monster's fizz category = %d, want the hostile %d", fizz.Category, protocol.SoundCategoryHostile)
	}
}

// splashWaterAt gives a thrower a splash water bottle and throws it straight
// down at (0.5, 64, 0.5), then runs the ticks the flight and shatter take.
func splashWaterAt(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	srv.GiveSlot(name, 36, potionStack(t, "minecraft:splash_potion", registry.MustPotion("minecraft:water"), 1))
	c.MoveTo(0.5, 64, 0.5, -90, 90)
	servertest.UseHeld(c, -90, 90)
	c.ReadTicks(srv, 40, func(int32, []byte) bool { return false })
}

// TestASplashWaterPotionHurtsAndTeleportsAnEnderman splashes water beside an
// enderman: it loses one health to the indirect magic blow and blinks away.
// vanilla: AbstractThrownPotion.onHitAsWater, EnderMan.hurtServer
func TestASplashWaterPotionHurtsAndTeleportsAnEnderman(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Splasher")
	c.Configure()
	c.EnterWorldAs("Splasher")
	c.Settle()
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob("minecraft:enderman", 2.0, 64, 0.5)
	srv.SetMobNoAI(id)
	before, _ := srv.MobHealth(id)
	x0, y0, z0, _ := srv.MobPos(id)
	splashWaterAt(t, srv, c, "Splasher")

	after, _ := srv.MobHealth(id)
	if got := before - after; got != 1 {
		t.Errorf("a water splash cost the enderman %v health, want 1", got)
	}
	if x, y, z, _ := srv.MobPos(id); x == x0 && y == y0 && z == z0 {
		t.Error("a water splash left the enderman where it stood, want it teleported")
	}
}

// TestASplashWaterPotionHurtsEachWaterSensitiveMob splashes water beside
// each water-sensitive kind: one health lost to the indirect magic blow.
// vanilla: AbstractThrownPotion.onHitAsWater
func TestASplashWaterPotionHurtsEachWaterSensitiveMob(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:blaze", "minecraft:snow_golem", "minecraft:strider"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Dial(t, servertest.MobTestConfig())
			c.Login("Splasher")
			c.Configure()
			c.EnterWorldAs("Splasher")
			c.Settle()
			servertest.HoldTicks(t, srv)

			id := srv.SpawnMob(kind, 2.0, 64, 0.5)
			srv.SetMobNoAI(id)
			before, _ := srv.MobHealth(id)
			splashWaterAt(t, srv, c, "Splasher")
			if after, _ := srv.MobHealth(id); before-after != 1 {
				t.Errorf("a water splash cost the %s %v health, want 1", kind, before-after)
			}
		})
	}
}

// TestASplashWaterPotionSparesAWaterInsensitiveCow splashes water beside a
// cow, which is not hurt by water.
func TestASplashWaterPotionSparesAWaterInsensitiveCow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Splasher")
	c.Configure()
	c.EnterWorldAs("Splasher")
	c.Settle()
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob("minecraft:cow", 2.0, 64, 0.5)
	before, _ := srv.MobHealth(id)
	splashWaterAt(t, srv, c, "Splasher")
	if after, _ := srv.MobHealth(id); after != before {
		t.Errorf("a water splash took a cow from %v to %v health", before, after)
	}
}

// TestALingeringPotionThatHitsAMobLeavesItsCloudAtTheMobsFeet throws a
// lingering potion level into a cow's side: the cloud is put at the cow's
// position on the ground, not at the impact point up on its box.
// vanilla: ThrownLingeringPotion.onHitAsPotion
func TestALingeringPotionThatHitsAMobLeavesItsCloudAtTheMobsFeet(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Lobber", servertest.OpsListing(t, "Lobber"))

	cow := srv.SpawnMob("minecraft:cow", 3.0, 64, 0.5)
	cowSpawn := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cowSpawn.EntityID != cow {
		t.Fatalf("the planted cow spawned as id %d, want %d", cowSpawn.EntityID, cow)
	}
	srv.SetMobNoAI(cow)
	servertest.HoldTicks(t, srv)

	srv.GiveSlot("Lobber", 36, potionStack(t, "minecraft:lingering_potion", poisonBrewing, 1))
	// Pitched down past the throw's twenty-degree lift, the flight dips
	// into the cow's side, above the ground.
	c.MoveTo(0.5, 64, 0.5, -90, 35)
	servertest.UseHeld(c, -90, 35)

	var cloud servertest.ThrownSpawn
	if !c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		if _, typ := servertest.SpawnEntityHead(data); typ != protocol.EntityTypeAreaEffectCloud {
			return false
		}
		cloud = servertest.ReadThrownSpawn(c, data)
		return true
	}) {
		t.Fatal("no area effect cloud arrived for the lingering throw")
	}
	if cloud.X != cowSpawn.X || cloud.Y != cowSpawn.Y || cloud.Z != cowSpawn.Z {
		t.Errorf("the cloud stands at (%v, %v, %v), want the struck cow's (%v, %v, %v)",
			cloud.X, cloud.Y, cloud.Z, cowSpawn.X, cowSpawn.Y, cowSpawn.Z)
	}
	if cloud.Y != 64 {
		t.Errorf("the cloud stands at y=%v, want the ground the cow stands on, 64", cloud.Y)
	}
}
