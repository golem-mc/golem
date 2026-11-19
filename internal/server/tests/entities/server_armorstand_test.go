package entities

// The armor stand tests: the item's placement — facing its placer, the yaw
// snapped to the 45 degrees between; /summon's stand carrying the tagged
// state the way a saved stand's wake reads it; a punch's shake and the
// second punch within the window taking the stand down with its drops, the
// creative hand's one punch alone; and the interact's slot swap, the click
// offset picking the slot and the DisabledSlots mask locking it.

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// armorStandItemID is the item a placed stand comes from and a broken one
// leaves behind.
var armorStandItemID = servertest.RegistryItemID("minecraft:armor_stand")

// armorStandAt is where the stand with the given entity ID stands in the
// overworld.
func armorStandAt(t *testing.T, srv *server.Server, id int32) server.TestArmorStand {
	t.Helper()
	for _, ent := range srv.ArmorStandsInDimension(world.DimensionName) {
		if ent.ID == id {
			return ent
		}
	}
	t.Fatalf("no stand with entity ID %d stands in the overworld", id)
	return server.TestArmorStand{}
}

// TestASummonedArmorStandCarriesItsTag summons a stand whose tag names
// every field a save keeps — the four tool-mode flags, the invisibility,
// the slot mask and a pose — and reads each back off the spawn: the
// metadata word for word, the rotation entries at the indices they ride.
func TestASummonedArmorStandCarriesItsTag(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:armor_stand 1.5 64 1.5 {Small:1b,ShowArms:1b,` +
		`NoBasePlate:1b,Invisible:1b,DisabledSlots:1048576,` +
		`Pose:{Head:[90f,10f,20f],LeftArm:[-25f,0f,45f]}}`)
	spawn := c.ExpectArmorStandSpawn()

	if spawn.X != 1.5 || spawn.Y != 64 || spawn.Z != 1.5 {
		t.Errorf("the stand stands at (%v, %v, %v), want (1.5, 64, 1.5)", spawn.X, spawn.Y, spawn.Z)
	}
	if spawn.Yaw != 0 {
		t.Errorf("the summoned stand faces yaw %v, want 0", spawn.Yaw)
	}
	if got, want := spawn.Flags, uint8(protocol.EntityFlagInvisible); got != want {
		t.Errorf("the stand's shared flags = %#02x, want the invisible bit %#02x", got, want)
	}
	if !spawn.HasHealth || spawn.Health != 20 {
		t.Errorf("the stand's health travelled as %v at %v present, want 20", spawn.Health, spawn.HasHealth)
	}
	const wantStandFlags = protocol.ArmorStandFlagSmall | protocol.ArmorStandFlagArms | protocol.ArmorStandFlagNoBasePlate
	if spawn.StandFlags != wantStandFlags {
		t.Errorf("the stand's own flags = %#02x, want %#02x", spawn.StandFlags, wantStandFlags)
	}
	head, ok := spawn.Pose[protocol.MetadataIndexArmorStandHead]
	if !ok || head != [3]float32{90, 10, 20} {
		t.Errorf("the Head rotation = %v at index 16, want (90, 10, 20)", head)
	}
	arm, ok := spawn.Pose[protocol.MetadataIndexArmorStandLeftArm]
	if !ok || arm != [3]float32{-25, 0, 45} {
		t.Errorf("the LeftArm rotation = %v at index 18, want (-25, 0, 45)", arm)
	}
	if _, sent := spawn.Pose[protocol.MetadataIndexArmorStandBody]; sent {
		t.Errorf("the default Body rotation was sent, want it left out")
	}
	if _, sent := spawn.Pose[protocol.MetadataIndexArmorStandRightLeg]; sent {
		t.Errorf("the default RightLeg rotation was sent, want it left out")
	}
	// The whole equipment arrives, six slots empty: a stand bare of it.
	if len(spawn.Equipment) != 6 {
		t.Fatalf("the stand's equipment carries %d entries, want all 6 slots", len(spawn.Equipment))
	}
	for _, e := range spawn.Equipment {
		if !e.Stack.Empty() {
			t.Errorf("slot %d of a fresh stand's equipment holds %v, want nothing", e.Slot, e.Stack)
		}
	}
	if servertest.StandsStanding(srv) != 1 {
		t.Errorf("the server holds %d armor stands, want the one that was summoned", servertest.StandsStanding(srv))
	}
	// The success line names what stood up the way Entity.getDisplayName
	// does: the type's own translation, with the UUID insertion and the
	// show-entity hover.
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:armor_stand", "entity.minecraft.armor_stand", spawn.Identity))
}

// TestArmorStandsStandFacingTheirPlacer places one from the creative hand
// and reads the yaw off the spawn: the placer's own turned about to face
// them, snapped to the 45 degrees between. A placer facing yaw 100 has the
// stand face 270.
func TestArmorStandsStandFacingTheirPlacer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: armorStandItemID, Count: 1})
	// Two blocks south of the stand's cell, looking north toward it, so
	// the placer's own box is out of the stand's and well within the
	// placement's reach.
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()

	// The top face of the ground: the stand stands in the cell above it,
	// centred.
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	if spawn.X != 3.5 || spawn.Y != 64 || spawn.Z != 3.5 {
		t.Errorf("the stand stands at (%v, %v, %v), want the cell's centre (3.5, 64, 3.5)",
			spawn.X, spawn.Y, spawn.Z)
	}
	// Placer facing 100 → the stand faces 100+180 = 280, snapped to the
	// nearest 45: 270.
	if got, want := int32(spawn.Yaw), int32(270); got != want {
		t.Errorf("the placed stand faces yaw %d, want %d", got, want)
	}
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.place", protocol.SoundCategoryBlock, 0.75, 0.8, 3.5, 64, 3.5)
	if servertest.StandsStanding(srv) != 1 {
		t.Errorf("the server holds %d armor stands, want the one that was placed", servertest.StandsStanding(srv))
	}
}

// TestAPlacedStandCarriesItsItemsEntityData holds an armor stand item whose
// entity_data component — a TypedEntityData, the armor_stand type id then
// the compound — names the small flag and places it: the stand reads the
// compound's fields, as a summon's tag would.
func TestAPlacedStandCarriesItsItemsEntityData(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	fromComponent := protocol.Text{Text: "FromTheComponent"}
	stack := protocol.ItemStack{ItemID: armorStandItemID, Count: 1, CustomName: &fromComponent,
		Raw: []protocol.RawComponent{entityDataComponent(t, protocol.EntityTypeArmorStand,
			nbt.Compound{"Small": nbt.Byte(1)})}}
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()

	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	if spawn.StandFlags&protocol.ArmorStandFlagSmall == 0 {
		t.Errorf("the placed stand's flags = %#02x, want the Small bit the item's entity_data names", spawn.StandFlags)
	}
	if !reflect.DeepEqual(spawn.Name, protocol.TextTag(fromComponent)) {
		t.Errorf("the placed stand's name = %v, want the item's custom_name", spawn.Name)
	}
}

// TestAPlacedStandIgnoresAnotherTypesEntityData holds an armor stand item
// whose entity_data names the item type and then the small flag: the data
// names no armor stand, so the stand is placed without it.
func TestAPlacedStandIgnoresAnotherTypesEntityData(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	stack := protocol.ItemStack{ItemID: armorStandItemID, Count: 1,
		Raw: []protocol.RawComponent{entityDataComponent(t, protocol.EntityTypeItem,
			nbt.Compound{"Small": nbt.Byte(1)})}}
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()

	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	if spawn.StandFlags&protocol.ArmorStandFlagSmall != 0 {
		t.Errorf("the placed stand's flags = %#02x, want the item type's entity_data ignored", spawn.StandFlags)
	}
}

// TestAPlacedStandsEntityDataNameWins holds an armor stand item whose
// custom_name component names one thing and whose entity_data CustomName
// names another: the entity_data is applied after the components, so its
// name is the one the stand carries.
func TestAPlacedStandsEntityDataNameWins(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	fromComponent := protocol.Text{Text: "FromTheComponent"}
	stack := protocol.ItemStack{ItemID: armorStandItemID, Count: 1, CustomName: &fromComponent,
		Raw: []protocol.RawComponent{entityDataComponent(t, protocol.EntityTypeArmorStand,
			nbt.Compound{"CustomName": nbt.String("FromTheEntityData")})}}
	c.SetCreativeSlot(player.HotbarSlot, stack)
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()

	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	if !reflect.DeepEqual(spawn.Name, nbt.Tag(nbt.String("FromTheEntityData"))) {
		t.Errorf("the placed stand's name = %v, want the entity_data's FromTheEntityData", spawn.Name)
	}
}

// entityDataComponent builds a minecraft:entity_data component's raw value:
// the TypedEntityData of an entity type id and then the network compound.
func entityDataComponent(t *testing.T, entityType int32, compound nbt.Compound) protocol.RawComponent {
	t.Helper()
	tag, err := nbt.EncodeNetwork(compound)
	if err != nil {
		t.Fatalf("encoding an entity_data compound: %v", err)
	}
	var b protocol.Buffer
	b.PutVarInt(entityType)
	b.PutRaw(tag)
	return protocol.RawComponent{Type: protocol.DataComponentEntityData, Data: b.Bytes()}
}

// TestASurvivorsPunchesShakeThenBreakAnArmorStand punches a survival
// player's stand twice: the first lands the shake of entity event 32 and
// the second, within its window, breaks the stand out of the world — the
// removal, the break sound and the drops, both the equipment the stand
// was dressed in and the item it is. A stand punched again after more than
// the window stands the shake again instead of breaking. The tick loop is
// stepped by hand, so the window is counted in game ticks however long the
// packets between the punches take.
func TestASurvivorsPunchesShakeThenBreakAnArmorStand(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Attacker", servertest.OpsListing(t, "Attacker"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})

	// A stand dressed before it is punched: the summon's tag carries a
	// chestplate worth breaking.
	chestplate := servertest.RegistryItemID("minecraft:diamond_chestplate")
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	spawn := c.ExpectArmorStandSpawn()
	if got := servertest.EquipEntry(spawn.Equipment, 4); got.ItemID != chestplate {
		t.Fatalf("the summoned stand's chest holds item %d, want the chestplate %d", got.ItemID, chestplate)
	}

	// In reach of the stand.
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	// Past the world's first ticks, where vanilla's lastHit starting at
	// nought reads a punch as already within the window.
	servertest.StepTicks(t, srv, clock, 10)

	// The first punch: the shake, the stand left standing.
	c.Attack(spawn.EntityID)
	c.ExpectEntityEvent(spawn.EntityID, protocol.EntityStatusArmorStandShake)
	if got := servertest.StandsStanding(srv); got != 1 {
		t.Fatalf("the server holds %d armor stands after the shake, want the standing one", got)
	}

	// The second punch within the window: the break, with the drops.
	c.Attack(spawn.EntityID)
	c.ExpectRemoval(spawn.EntityID)
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.break", protocol.SoundCategoryNeutral, 1.0, 1.0, 3.5, 64, 3.5)
	drops := map[int32]int32{}
	for range 2 {
		item := c.ExpectDroppedItem()
		drops[item.Stack.ItemID] += item.Stack.Count
	}
	if drops[chestplate] != 1 {
		t.Errorf("the broken stand's drops hold %d chestplates, want its one", drops[chestplate])
	}
	if drops[armorStandItemID] != 1 {
		t.Errorf("the broken stand's drops hold %d armor stand items, want its one", drops[armorStandItemID])
	}
	servertest.WaitFor(t, srv, 200, func() bool { return servertest.StandsStanding(srv) == 0 })
}

// TestAnArrowBreaksAnArmorStandAndLaysItsDrops shoots a survival stand
// dressed in a chestplate: an arrow's damage is of the
// always_kills_armor_stands tag, so the stand breaks at once — the removal,
// the break sound, and the item it is and its equipment laid behind.
// vanilla: ArmorStand.hurtServer, DamageTypeTags.ALWAYS_KILLS_ARMOR_STANDS
func TestAnArrowBreaksAnArmorStandAndLaysItsDrops(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	chestplate := servertest.RegistryItemID("minecraft:diamond_chestplate")
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	spawn := c.ExpectArmorStandSpawn()
	if got := servertest.EquipEntry(spawn.Equipment, 4); got.ItemID != chestplate {
		t.Fatalf("the summoned stand's chest holds item %d, want the chestplate %d", got.ItemID, chestplate)
	}

	// The archer's own body stands clear of the line the shot runs.
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	if id := srv.ShootArrow(1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}
	c.ExpectRemoval(spawn.EntityID)
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.break", protocol.SoundCategoryNeutral, 1.0, 1.0, 3.5, 64, 3.5)
	drops := map[int32]int32{}
	for range 2 {
		item := c.ExpectDroppedItem()
		drops[item.Stack.ItemID] += item.Stack.Count
	}
	if drops[chestplate] != 1 {
		t.Errorf("the broken stand's drops hold %d chestplates, want its one", drops[chestplate])
	}
	if drops[armorStandItemID] != 1 {
		t.Errorf("the broken stand's drops hold %d armor stand items, want its one", drops[armorStandItemID])
	}
	// A landed plain shaft plays its own hit voice before it is spent.
	// vanilla: AbstractArrow.onHitEntity
	servertest.ExpectSoundEvent(t, c, "minecraft:entity.arrow.hit")
	servertest.WaitFor(t, srv, 200, func() bool { return servertest.StandsStanding(srv) == 0 })
}

// TestAWindChargeBreaksAnArmorStandAndLaysItsDrops throws a player's wind
// charge at a stand two blocks ahead: wind_charge is in the
// always_kills_armor_stands tag, so the stand breaks and lays its item.
// vanilla: AbstractWindCharge.onHitEntity, ArmorStand.hurtServer
func TestAWindChargeBreaksAnArmorStandAndLaysItsDrops(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 0.5`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	c.Settle()

	servertest.UseHeld(c, -90, 0)
	traffic := servertest.ReadThrownTraffic(c, srv, 40)
	if traffic.SpawnOfType(protocol.EntityTypeWindCharge) == nil {
		t.Fatal("no wind charge came into the world for the throw")
	}
	if !traffic.RemovedOnce(stand.EntityID) {
		t.Fatal("the stand the wind charge hit was not taken out of the world within 40 ticks")
	}
	item := traffic.SpawnOfType(protocol.EntityTypeItem)
	if item == nil {
		t.Fatal("the broken stand laid no item within 40 ticks")
	}
	if metas := traffic.MetasOf(item.EntityID); len(metas) == 0 || metas[0].Item != armorStandItemID {
		t.Errorf("the laid item's metadata = %#v, want the armor stand item %d", metas, armorStandItemID)
	}
}

// TestASnowballBreaksOnAnArmorStand throws a snowball at a stand: the ball
// ends its flight on the stand, which a snowball's hurt does not break. The
// ball's place is read on the server each tick: the client is told a thrown
// body's place only every few ticks.
// vanilla: Projectile.canHitEntity, ArmorStand.isPickable
func TestASnowballBreaksOnAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 16})

	const standX = 2.5
	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 0.5`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	servertest.UseHeld(c, -90, 0)
	c.Settle()
	var ballID int32
	lastX := math.NaN()
	servertest.UntilTicks(t, srv, 40, "the snowball flies and ends", func() bool {
		if flying := srv.ThrownInDimension("minecraft:overworld"); len(flying) > 0 {
			ballID, lastX = flying[0].ID, flying[0].X
			return false
		}
		return ballID != 0
	})
	if lastX >= standX {
		t.Errorf("the snowball was last at x=%v, want it on the thrower's side of the stand at x=%v", lastX, standX)
	}
	traffic := servertest.ReadThrownTraffic(c, srv, 1)
	if !traffic.RemovedOnce(ballID) {
		t.Error("the client was not told once of the snowball's removal")
	}
	if n := traffic.RemovalCount(stand.EntityID); n != 0 {
		t.Errorf("the stand was removed %d times, want a snowball to leave it standing", n)
	}
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the server holds %d armor stands after the snowball, want the one", n)
	}
}

// TestATridentBouncesOffAnInvisibleStandAndBreaksAVisibleOne throws a
// trident at an invisible stand first: the hurt is refused, the stand
// stands and the trident deflects back off it; then at a dressed visible
// stand, which the always-kills hurt breaks while the trident itself is
// never spent, playing its own item.trident.hit voice.
// vanilla: ArmorStand.hurtServer, ThrownTrident.onHitEntity
func TestATridentBouncesOffAnInvisibleStandAndBreaksAVisibleOne(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Thrower", servertest.OpsListing(t, "Thrower"))

	chestplate := servertest.RegistryItemID("minecraft:diamond_chestplate")
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Invisible:1b}`)
	c.ExpectArmorStandSpawn()
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	visible := c.ExpectArmorStandSpawn()
	if got := servertest.EquipEntry(visible.Equipment, 4); got.ItemID != chestplate {
		t.Fatalf("the summoned stand's chest holds item %d, want the chestplate %d", got.ItemID, chestplate)
	}

	c.MoveToGrounded(0.5, 64, 7.5, 0, 0, true)
	c.Settle()

	// The invisible stand refuses the hurt: it stands, and the trident
	// deflects off it rather than being spent.
	refused := srv.ShootTrident(1.5, 64.5, 3.5, 10, 0, 0)
	if refused == 0 {
		t.Fatal("the first trident never flew")
	}
	servertest.ExpectSoundEvent(t, c, "minecraft:item.trident.hit")
	if !srv.ArrowAlive(refused) {
		t.Error("the trident was spent on the invisible stand, want it deflected and still in the world")
	}
	if n := servertest.StandsStanding(srv); n != 2 {
		t.Fatalf("the server holds %d armor stands after the refused throw, want both standing", n)
	}

	// The visible stand takes the same hurt as the always-kills break.
	thrown := srv.ShootTrident(1.5, 64.5, 5.5, 10, 0, 0)
	if thrown == 0 {
		t.Fatal("the second trident never flew")
	}
	c.ExpectRemoval(visible.EntityID)
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.break", protocol.SoundCategoryNeutral, 1.0, 1.0, 5.5, 64, 5.5)
	drops := map[int32]int32{}
	for range 2 {
		item := c.ExpectDroppedItem()
		drops[item.Stack.ItemID] += item.Stack.Count
	}
	if drops[chestplate] != 1 || drops[armorStandItemID] != 1 {
		t.Errorf("the trident's break laid %v, want the chestplate and the stand item", drops)
	}
	// The trident's own voice, not the arrow's, before it flies on.
	servertest.ExpectSoundEvent(t, c, "minecraft:item.trident.hit")
	if !srv.ArrowAlive(thrown) {
		t.Error("the trident was spent on the stand, want it deflected and still in the world")
	}
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the server holds %d armor stands after the throw, want the invisible one left", n)
	}
}

// TestAnArrowBouncesOffAnInvisibleStand shoots a stand made invisible: the
// hurt is refused, so the stand stands and the shaft bounces off it, the
// equipment it wears left on it.
// vanilla: ArmorStand.hurtServer
func TestAnArrowBouncesOffAnInvisibleStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Invisible:1b,equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	shaft := srv.ShootArrow(1.5, 64.5, 3.5, 10, 0, 0)
	if shaft == 0 {
		t.Fatal("the arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the server holds %d armor stands, want the invisible one untouched", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it bounced and still in the world")
	}
	if got := srv.ItemCount(); got != 0 {
		t.Errorf("items standing = %d, want none laid by a refused hurt", got)
	}
}

// TestACreativePlayersArrowBreaksAnArmorStandWithoutDropping shoots a
// creative hand's own shaft into a dressed stand: the hurt takes it down
// the same, while the drop of it skips the item and the equipment for a
// shooter whose infinite materials leave nothing to lay.
// vanilla: ArmorStand.hurtServer
func TestACreativePlayersArrowBreaksAnArmorStandWithoutDropping(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	spawn := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	if id := srv.ShootPlayerArrow("Archer", 1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the archer's arrow never flew")
	}
	c.ExpectRemoval(spawn.EntityID)
	c.ExpectArmorStandSound("minecraft:entity.armor_stand.break", protocol.SoundCategoryNeutral, 1.0, 1.0, 3.5, 64, 3.5)
	if servertest.ASpawnOfWithinTicks(c, srv, 40, protocol.EntityTypeItem) {
		t.Error("a creative shooter's shot laid the stand's drops")
	}
	if n := servertest.StandsStanding(srv); n != 0 {
		t.Errorf("the server holds %d armor stands after the shot, want none", n)
	}
}

// TestAnAdventurePlayersArrowCannotBreakAnArmorStand switches an operator
// to adventure and shoots their own shaft at a stand: the hurt is refused
// for a hand that may not build, so the stand stands and the shaft bounces
// off it.
// vanilla: ArmorStand.hurtServer
func TestAnAdventurePlayersArrowCannotBreakAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	c.ChatCommand("gamemode adventure Archer")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Archer") == protocol.GameModeAdventure })

	shaft := srv.ShootPlayerArrow("Archer", 1.5, 64.5, 3.5, 10, 0, 0)
	if shaft == 0 {
		t.Fatal("the archer's arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the server holds %d armor stands, want the adventure archer's shaft refused", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it bounced and still in the world")
	}
}

// TestASpectatorsArrowCannotBreakAnArmorStand switches an operator to
// spectator and shoots their own shaft at a stand: a hand that may not
// build is refused the same as adventure, so the stand stands and the
// shaft bounces off it.
// vanilla: ArmorStand.hurtServer
func TestASpectatorsArrowCannotBreakAnArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	c.ChatCommand("gamemode spectator Archer")
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Archer") == protocol.GameModeSpectator })

	shaft := srv.ShootPlayerArrow("Archer", 1.5, 64.5, 3.5, 10, 0, 0)
	if shaft == 0 {
		t.Fatal("the archer's arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the server holds %d armor stands, want the spectator's shaft refused", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it bounced and still in the world")
	}
}

// TestASkeletonsArrowCannotBreakAnArmorStandWithMobGriefingOff flies a
// mob's own shaft into a stand with mob_griefing off: the hurt is refused
// the way a mob's block griefing is — the stand stands, nothing laid — and
// the shaft bounces off rather than dying on the hit.
// vanilla: ArmorStand.hurtServer
func TestASkeletonsArrowCannotBreakAnArmorStandWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Witness", servertest.OpsListing(t, "Witness"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	c.ExpectArmorStandSpawn()
	srv.SetGameRule("mob_griefing", false)
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	skeleton := srv.SpawnMob("minecraft:skeleton", 0.5, 64, 0.5)
	if skeleton == 0 {
		t.Fatal("the skeleton never stood up")
	}
	shaft := srv.ShootMobArrowFrom(skeleton, 1.5, 64.5, 3.5, 10, 0, 0)
	if shaft == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := servertest.StandsStanding(srv); n != 1 {
		t.Errorf("the skeleton's arrow broke the stand with mob_griefing off: %d left", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it bounced and still in the world")
	}
}

// TestASecondArrowFliesOnPastAStandBrokenThisTick lays two shafts of one
// tick into one stand: the first breaks it and is spent, the second finds
// it gone and flies on rather than being used up on a body no longer there.
// vanilla: AbstractArrow.onHitEntity
func TestASecondArrowFliesOnPastAStandBrokenThisTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()

	if id := srv.ShootArrow(1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the first arrow never flew")
	}
	if id := srv.ShootArrow(1.5, 64.5, 3.5, 10, 0, 0); id == 0 {
		t.Fatal("the second arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := servertest.StandsStanding(srv); n != 0 {
		t.Errorf("the server holds %d armor stands, want the one the first shaft broke", n)
	}
	if got := srv.ArrowCount(); got != 1 {
		t.Errorf("arrows standing = %d, want the second shaft flying on past the gone stand", got)
	}
}

// TestAShakenStandKeepsStandingAfterTheWindow punches a stand, waits out
// the five ticks its window stands, and punches again: the second shake,
// the stand still in the world.
func TestAShakenStandKeepsStandingAfterTheWindow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Attacker", servertest.OpsListing(t, "Attacker"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	spawn := c.ExpectArmorStandSpawn()

	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	// Past the world's first ticks, where vanilla's lastHit starting at
	// nought reads a punch as already within the window.
	servertest.AwaitTicks(t, srv, 10)

	c.Attack(spawn.EntityID)
	c.ExpectEntityEvent(spawn.EntityID, protocol.EntityStatusArmorStandShake)
	// The window is five ticks; the sixth punch is a first one again.
	servertest.AwaitTicks(t, srv, 10)
	c.Attack(spawn.EntityID)
	c.ExpectEntityEvent(spawn.EntityID, protocol.EntityStatusArmorStandShake)
	if got := servertest.StandsStanding(srv); got != 1 {
		t.Fatalf("the server holds %d armor stands after the second shake, want the standing one", got)
	}
}

// TestACreativePunchBreaksAnArmorStandAtOnce swings a creative hand's one
// punch at a stand: no shake and no drops, the stand gone.
func TestACreativePunchBreaksAnArmorStandAtOnce(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Creator", servertest.OpsListing(t, "Creator"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	spawn := c.ExpectArmorStandSpawn()

	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	c.Attack(spawn.EntityID)
	c.ExpectRemoval(spawn.EntityID)
	servertest.WaitFor(t, srv, 200, func() bool { return servertest.StandsStanding(srv) == 0 })
}

// TestAnArmorStandTakesAHeldPieceAtItsOwnSlot holds a helmet and clicks
// the stand's feet: the piece goes to its own slot, the head, wherever the
// click landed. An empty hand clicking the head takes it back.
func TestAnArmorStandTakesAHeldPieceAtItsOwnSlot(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	helmet := servertest.RegistryItemID("minecraft:diamond_helmet")
	// The helmet in hand, the click at the feet: it lands on the head, and
	// a creative hand keeps its stack.
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.InteractAt(stand.EntityID, protocol.HandMain, 0, 0.2, 0)
	slot, held, onStand := c.ReadSwapAnswer(stand.EntityID, 5)
	if slot != int16(player.HotbarSlot) || held.ItemID != helmet {
		t.Errorf("the clicking hand's slot %d holds %v, want the creative hand keeping its helmet", slot, held)
	}
	if onStand.ItemID != helmet || onStand.Count != 1 {
		t.Errorf("the stand's head slot holds %v after the click at the feet, want the helmet", onStand)
	}

	// A bare hand clicking the head takes it back.
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{})
	c.InteractAt(stand.EntityID, protocol.HandMain, 0, 1.7, 0)
	slot, held, onStand = c.ReadSwapAnswer(stand.EntityID, 5)
	if slot != int16(player.HotbarSlot) || held.ItemID != helmet {
		t.Errorf("the clicking hand's slot %d holds item %d after the take-back, want the helmet", slot, held.ItemID)
	}
	if !onStand.Empty() {
		t.Errorf("the stand's head slot holds %v after the take-back, want nothing", onStand)
	}
}

// TestAnArmlessStandRefusesAHandItem holds a sword to a stand that shows no
// arms: the hand slot is refused and nothing answers. With the arms shown,
// the sword lands in the main hand.
func TestAnArmlessStandRefusesAHandItem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	armless := c.ExpectArmorStandSpawn()
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 3.5 {ShowArms:1b}`)
	armed := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	sword := servertest.RegistryItemID("minecraft:diamond_sword")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	c.InteractAt(armless.EntityID, protocol.HandMain, 0.3, 1.2, 0)
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayContainerSlot))

	// The stand that shows its arms takes it.
	c.InteractAt(armed.EntityID, protocol.HandMain, 0.3, 1.2, 0)
	slot, _, onStand := c.ReadSwapAnswer(armed.EntityID, 0)
	if slot != int16(player.HotbarSlot) || onStand.ItemID != sword {
		t.Errorf("the armed stand's main hand holds %v, want the sword", onStand)
	}
}

// TestAnArmorStandSplitsAStackOfTwo holds two helmets to a stand in
// survival: one goes on the head and one stays in the hand.
func TestAnArmorStandSplitsAStackOfTwo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	helmet := servertest.RegistryItemID("minecraft:diamond_helmet")
	srv.GiveSlot("Dresser", player.HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 2})
	c.InteractAt(stand.EntityID, protocol.HandMain, 0, 1.7, 0)
	slot, held, onStand := c.ReadSwapAnswer(stand.EntityID, 5)
	if slot != int16(player.HotbarSlot) || held.ItemID != helmet || held.Count != 1 {
		t.Errorf("the hand holds %v after the split, want the one helmet left", held)
	}
	if onStand.ItemID != helmet || onStand.Count != 1 {
		t.Errorf("the stand's head holds %v, want the one helmet put down", onStand)
	}
}

// TestALockedHeadSlotRefusesTheClick summons a stand whose head is disabled
// by the mask's +0 bit — 1<<4, the head's EquipmentSlot id, which is not
// its wire index — and holds a helmet to it: nothing answers.
func TestALockedHeadSlotRefusesTheClick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {DisabledSlots:16}`)
	locked := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	helmet := servertest.RegistryItemID("minecraft:diamond_helmet")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.InteractAt(locked.EntityID, protocol.HandMain, 0, 1.7, 0)
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayContainerSlot))
}

// TestAWeakBlastStillKillsAVisibleStand fires a blast whose falloff leaves
// only a few points of damage at the stand: a visible stand is killed by it
// whatever the amount, where a plain hurt would leave it standing.
func TestAWeakBlastStillKillsAVisibleStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"))

	// High in the air, so the blast finds no blocks to drop and the weak
	// falloff is what the stand is measured against.
	c.ChatCommand(`summon minecraft:armor_stand 3.5 100 3.5`)
	c.ExpectArmorStandSpawn()
	srv.StopTick()
	srv.BlastAt(10.5, 100, 3.5, 4)

	if got := servertest.StandsStanding(srv); got != 0 {
		t.Errorf("a weak blast left %d stands standing, want the visible one killed outright", got)
	}
}

// TestABlastLeavesAnInvisibleStandAlone fires a lethal blast at an invisible
// stand: it is untouched.
func TestABlastLeavesAnInvisibleStandAlone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 100 3.5 {Invisible:1b}`)
	c.ExpectArmorStandSpawn()
	srv.StopTick()
	srv.BlastAt(3.5, 100, 3.5, 2)

	if got := servertest.StandsStanding(srv); got != 1 {
		t.Errorf("a blast took the invisible stand, want it untouched")
	}
}

// TestABlastWithMobGriefingOffLeavesAStand fires a lethal mob blast at a
// visible stand with mob_griefing turned off: the blast affects no blocklike
// entity, so the stand is left standing.
func TestABlastWithMobGriefingOffLeavesAStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 100 3.5`)
	c.ExpectArmorStandSpawn()
	srv.SetGameRule("mob_griefing", false)
	srv.StopTick()
	srv.BlastAtMob(3.5, 100, 3.5, 2)

	if got := servertest.StandsStanding(srv); got != 1 {
		t.Errorf("a mob_griefing-off blast left %d stands standing, want the visible one untouched", got)
	}
}

// TestABlastKillsBothStandsInRange fires a lethal blast at two visible stands
// side by side: both are killed, the one that shifts into the other's slot
// included.
func TestABlastKillsBothStandsInRange(t *testing.T) {
	t.Parallel()
	// No tick runs between the summons and the blast, so the stands cannot
	// fall out of the blast's range however late the loop would have run.
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Blaster", servertest.OpsListing(t, "Blaster"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})

	c.ChatCommand(`summon minecraft:armor_stand 3.5 100 3.5`)
	c.ExpectArmorStandSpawn()
	c.ChatCommand(`summon minecraft:armor_stand 5.5 100 3.5`)
	c.ExpectArmorStandSpawn()
	srv.StopTick()
	srv.BlastAt(4.5, 100, 3.5, 2)

	if got := servertest.StandsStanding(srv); got != 0 {
		t.Errorf("a blast left %d of two stands standing, want both killed outright", got)
	}
}

// TestANoGravityStandDoesNotGlide summons a no-gravity stand with a sideways
// speed and steps it by hand: ArmorStand.travel returns before the move, so
// the impulse carries it nowhere, where the same impulse moves a stand with
// gravity.
// vanilla: ArmorStand.travel, Entity.isNoGravity
func TestANoGravityStandDoesNotGlide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 70 3.5 {NoGravity:1b,Motion:[0.5d,0d,0d]}`)
	held := c.ExpectArmorStandSpawn()
	if !held.NoGravity {
		t.Fatalf("the summoned stand's no-gravity = false, want the flag the summon named")
	}
	// The same impulse on a stand with gravity, as the control: it must move,
	// so a held stand is not just one the ticks never reached.
	c.ChatCommand(`summon minecraft:armor_stand 5.5 70 3.5 {Motion:[0.5d,0d,0d]}`)
	free := c.ExpectArmorStandSpawn()

	srv.StopTick()
	heldBefore := armorStandAt(t, srv, held.EntityID)
	freeBefore := armorStandAt(t, srv, free.EntityID)
	srv.TickArmorStands(40)
	heldAfter := armorStandAt(t, srv, held.EntityID)
	freeAfter := armorStandAt(t, srv, free.EntityID)

	if math.Abs(heldAfter.X-heldBefore.X) > 0.01 || math.Abs(heldAfter.Y-heldBefore.Y) > 0.01 {
		t.Errorf("the no-gravity stand drifted from %v,%v to %v,%v over forty ticks, want it held",
			heldBefore.X, heldBefore.Y, heldAfter.X, heldAfter.Y)
	}
	if math.Abs(freeAfter.X-freeBefore.X) < 0.01 {
		t.Errorf("the stand with gravity stayed at x=%v over forty ticks, want its impulse to carry it", freeBefore.X)
	}
}

// TestAnArmorStandPlacedOnABottomSlabRestsOnItsTop stands one on a bottom
// slab: EntityType.getYOffset drops the stand onto the slab's half-block
// top, so it spawns at the slab's top rather than the cell's floor.
func TestAnArmorStandPlacedOnABottomSlabRestsOnItsTop(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	srv.SetBlock(3, 63, 3, registry.MustState("minecraft:stone_slab", map[string]string{"type": "bottom"}))

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: armorStandItemID, Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	spawn := c.ExpectArmorStandSpawn()
	if spawn.Y != 63.5 {
		t.Errorf("the stand on the slab stands at y %v, want the slab's top 63.5", spawn.Y)
	}
}

// TestAnArmorStandRefusesACeilingedPlacement places a stone in the cell the
// stand's box would fill and tries to stand one there: the placement is
// refused and nothing stands.
func TestAnArmorStandRefusesACeilingedPlacement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Placer")

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry carries no stone")
	}
	srv.SetBlock(3, 65, 3, stone)

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: armorStandItemID, Count: 1})
	c.MoveToGrounded(3.5, 64, 5.5, 100, 40, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySpawnEntity))
	if got := servertest.StandsStanding(srv); got != 0 {
		t.Errorf("the server holds %d armor stands, want none stood into the ceiling", got)
	}
}

// TestAnArmorStandSurvivesARestart summons one dressed and posed, saves
// the world and stands a server back up on it: the entity file holds the
// flags, the pose and the equipment the vanilla save writes, and the stand
// comes back at the same identity and the same state.
func TestAnArmorStandSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Dresser", servertest.OpsListing(t, "Dresser"))

	chestplate := servertest.RegistryItemID("minecraft:diamond_chestplate")
	c.ChatCommand(`summon minecraft:armor_stand 1.5 64 1.5 {Small:1b,ShowArms:1b,` +
		`Pose:{Head:[90f,10f,20f]},equipment:{chest:{id:"minecraft:diamond_chestplate",count:1}}}`)
	left := c.ExpectArmorStandSpawn()
	if got := servertest.EquipEntry(left.Equipment, 4); got.ItemID != chestplate {
		t.Fatalf("the summoned stand's chest holds item %d, want the chestplate %d", got.ItemID, chestplate)
	}

	// The save runs while the chunk stands loaded — the player leaving
	// would give it up to its entity file without a write.
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	// On disk the compound is the vanilla one: the flag set, the pose's
	// parts, the slot-less mask left out and the equipment by its slots'
	// names — the save a vanilla server would hand back.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:armor_stand", func(compound nbt.Compound) {
		identity, ok := nbt.Get[nbt.IntArray](compound, "UUID")
		if !ok || len(identity) != 4 {
			t.Fatal("the saved stand has no four-word UUID")
		}
		want := servertest.UuidWords(left.Identity)
		for i := range want {
			if identity[i] != want[i] {
				t.Errorf("saved UUID = %v, want %v (the summoned stand's)", identity, want)
				break
			}
		}
		// The five flags and the mask are always written, the marker
		// alone left out while clear.
		for key, want := range map[string]nbt.Tag{
			"Small":         nbt.Byte(1),
			"ShowArms":      nbt.Byte(1),
			"Invisible":     nbt.Byte(0),
			"NoBasePlate":   nbt.Byte(0),
			"DisabledSlots": nbt.Int(0),
		} {
			if got := compound[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("the saved stand's %s = %v, want %v", key, got, want)
			}
		}
		if _, marked := compound["Marker"]; marked {
			t.Error("the saved stand wrote a Marker it does not carry")
		}
		pose, ok := nbt.Get[nbt.Compound](compound, "Pose")
		if !ok {
			t.Fatal("the saved stand carries no Pose")
		}
		head, ok := nbt.Get[nbt.List](pose, "Head")
		if !ok || len(head.Items) != 3 ||
			head.Items[0] != nbt.Tag(nbt.Float(90)) || head.Items[1] != nbt.Tag(nbt.Float(10)) ||
			head.Items[2] != nbt.Tag(nbt.Float(20)) {
			t.Errorf("the saved stand's Head pose = %v, want (90, 10, 20)", head.Items)
		}
		equipment, ok := nbt.Get[nbt.Compound](compound, "equipment")
		if !ok {
			t.Fatal("the saved stand carries no equipment compound")
		}
		chest, ok := nbt.Get[nbt.Compound](equipment, "chest")
		if !ok || string(chest["id"].(nbt.String)) != "minecraft:diamond_chestplate" {
			t.Errorf("the saved chest slot = %v, want the diamond chestplate", chest)
		}
	})

	// A restarted server wakes it back as it was, the same identity
	// included.
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeArmorStand: "stand",
	})
	back := servertest.CapturedOr(t, again, shown, "stand", protocol.EntityTypeArmorStand)
	servertest.ExpectUUID(t, back.Uuid, left.Identity, "armor stand")
	if back.X != 1.5 || back.Y != 64 || back.Z != 1.5 {
		t.Errorf("the restored stand stands at (%v, %v, %v), want (1.5, 64, 1.5)", back.X, back.Y, back.Z)
	}
	flags := servertest.MetaOf(back.Meta, protocol.MetadataIndexArmorStandFlags)
	if flags == nil || flags.ByteVal != protocol.ArmorStandFlagSmall|protocol.ArmorStandFlagArms {
		t.Errorf("the restored stand's own flags = %v, want its small and arms bits", flags)
	}
	headPose := servertest.MetaOf(back.Meta, protocol.MetadataIndexArmorStandHead)
	if headPose == nil || headPose.RotVal != [3]float32{90, 10, 20} {
		t.Errorf("the restored stand's Head rotation = %v, want (90, 10, 20)", headPose)
	}
	if againSrv.ArmorStandCount() != 1 {
		t.Errorf("the restarted server holds %d armor stands, want the one that was saved", againSrv.ArmorStandCount())
	}
}

// TestASummonedStandStartsInTheAir summons a stand onto the floor with an
// upward and sideways Motion and steps it by hand: a fresh entity is not on
// the ground, so its first tick takes the air's 0.91 drag alone, without the
// floor's 0.6 friction under it.
// vanilla: LivingEntity.travelInAir, Entity.onGround
func TestASummonedStandStartsInTheAir(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	srv.StopTick()
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Motion:[0.4d,0.4d,0.0d]}`)
	id := c.ExpectArmorStandSpawn().EntityID
	start := armorStandAt(t, srv, id)
	srv.TickArmorStands(2)
	after := armorStandAt(t, srv, id)
	// Tick one moves 0.4 and leaves 0.4*0.91; tick two moves that.
	if want := start.X + 0.4 + 0.4*0.91; math.Abs(after.X-want) > 1e-6 {
		t.Errorf("after two ticks the stand stands at x=%v, want %v (first tick in the air)", after.X, want)
	}
	// 3.18 is the travel a vanilla server gave the same summon.
	srv.TickArmorStands(100)
	if travel := armorStandAt(t, srv, id).X - start.X; math.Abs(travel-3.18) > 0.01 {
		t.Errorf("the stand travelled %v blocks, want vanilla's 3.18", travel)
	}
}

// TestASlowStandSettles summons stands onto the floor with sideways speeds
// either side of 0.003: the slower one's speed is zeroed before it moves at
// all, where the faster one takes one step before its drag carries it under.
// A stand in the air with a slow rise has the rise zeroed before its first
// move.
// vanilla: LivingEntity.aiStep
func TestASlowStandSettles(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	srv.StopTick()
	c.ChatCommand(`summon minecraft:armor_stand 8.5 70 3.5 {Motion:[0.0d,0.0029d,0.0d]}`)
	rising := c.ExpectArmorStandSpawn().EntityID
	risingBefore := armorStandAt(t, srv, rising)
	srv.TickArmorStands(1)
	if risingAfter := armorStandAt(t, srv, rising); risingAfter.Y != risingBefore.Y {
		t.Errorf("the stand rising 0.0029 a tick moved from y=%v to %v on its first tick, want it held", risingBefore.Y, risingAfter.Y)
	}

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {Motion:[0.0029d,0.0d,-0.0029d]}`)
	slow := c.ExpectArmorStandSpawn().EntityID
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 6.5 {Motion:[0.0031d,0.0d,0.0d]}`)
	fast := c.ExpectArmorStandSpawn().EntityID
	slowBefore := armorStandAt(t, srv, slow)
	fastBefore := armorStandAt(t, srv, fast)
	srv.TickArmorStands(40)
	slowAfter := armorStandAt(t, srv, slow)
	fastAfter := armorStandAt(t, srv, fast)

	if slowAfter.X != slowBefore.X || slowAfter.Z != slowBefore.Z {
		t.Errorf("the stand at 0.0029 a tick crept from %v,%v to %v,%v, want it held",
			slowBefore.X, slowBefore.Z, slowAfter.X, slowAfter.Z)
	}
	if moved := fastAfter.X - fastBefore.X; math.Abs(moved-0.0031) > 1e-9 {
		t.Errorf("the stand at 0.0031 a tick moved %v, want the one step of 0.0031", moved)
	}
}

// TestASummonedStandKeepsItsOnGroundTag summons a stand onto the floor with
// OnGround:1b and a sideways Motion: its first tick takes the floor's
// friction, where the same summon without the tag takes the air's drag alone.
// vanilla: Entity.load
func TestASummonedStandKeepsItsOnGroundTag(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	srv.StopTick()
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {OnGround:1b,Motion:[0.4d,0.0d,0.0d]}`)
	grounded := c.ExpectArmorStandSpawn().EntityID
	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 6.5 {Motion:[0.4d,0.0d,0.0d]}`)
	airborne := c.ExpectArmorStandSpawn().EntityID
	groundedStart := armorStandAt(t, srv, grounded)
	airborneStart := armorStandAt(t, srv, airborne)
	srv.TickArmorStands(2)
	groundedMoved := armorStandAt(t, srv, grounded).X - groundedStart.X
	airborneMoved := armorStandAt(t, srv, airborne).X - airborneStart.X
	if want := 0.4 + 0.4*0.6*0.91; math.Abs(groundedMoved-want) > 1e-6 {
		t.Errorf("the OnGround:1b stand moved %v in two ticks, want %v (floor friction on tick one)", groundedMoved, want)
	}
	if want := 0.4 + 0.4*0.91; math.Abs(airborneMoved-want) > 1e-6 {
		t.Errorf("the stand without the tag moved %v in two ticks, want %v", airborneMoved, want)
	}
}

// TestAnArmorStandBelowTheWorldIsRemoved: a stand 64 below the world's floor
// is removed on its next tick with nothing laid down, a marker as well.
func TestAnArmorStandBelowTheWorldIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Voider", servertest.OpsListing(t, "Voider"))

	for _, tag := range []string{"", " {Marker:1b}"} {
		c.ChatCommand("summon minecraft:armor_stand 3.5 -200 3.5" + tag)
		spawn := c.ExpectArmorStandSpawn()
		removed := c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
			if id == protocol.IDPlaySpawnEntity && servertest.ParseItemSpawn(data).EntityID != 0 {
				t.Fatalf("summon%s: an item spawned as the stand fell out of the world", tag)
			}
			return id == protocol.IDPlayRemoveEntities && slices.Contains(servertest.RemovedIDs(t, data), spawn.EntityID)
		})
		if !removed {
			t.Fatalf("summon%s: the stand at y -200 was not removed within 5 ticks", tag)
		}
		// A break's drops follow its removal; a void removal has none.
		c.ExpectNoneWithinTicks(srv, 1, func(id int32, data []byte) bool {
			return id == protocol.IDPlaySpawnEntity && servertest.ParseItemSpawn(data).EntityID != 0
		})
	}
}

// TestAnArmedStandTakesAShieldInItsOffHand holds a shield to a stand with
// arms: its equippable slot is the off hand, which the stand can use.
func TestAnArmedStandTakesAShieldInItsOffHand(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {ShowArms:1b}`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	shield := servertest.RegistryItemID("minecraft:shield")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: shield, Count: 1})
	c.InteractAt(stand.EntityID, protocol.HandMain, 0.3, 1.2, 0)
	slot, _, onStand := c.ReadSwapAnswer(stand.EntityID, 1)
	if slot != int16(player.HotbarSlot) || onStand.ItemID != shield {
		t.Errorf("the stand's off hand holds %v, want the shield", onStand)
	}
}

// TestALockedHeadSendsAHelmetToAnArmedStandsMainHand holds a helmet to an
// armed stand whose head is disabled: the helmet falls back to the main hand.
func TestALockedHeadSendsAHelmetToAnArmedStandsMainHand(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Dresser", servertest.OpsListing(t, "Dresser"))

	c.ChatCommand(`summon minecraft:armor_stand 3.5 64 3.5 {ShowArms:1b,DisabledSlots:16}`)
	stand := c.ExpectArmorStandSpawn()
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()

	helmet := servertest.RegistryItemID("minecraft:diamond_helmet")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
	c.InteractAt(stand.EntityID, protocol.HandMain, 0, 1.7, 0)
	slot, _, onStand := c.ReadSwapAnswer(stand.EntityID, 0)
	if slot != int16(player.HotbarSlot) || onStand.ItemID != helmet {
		t.Errorf("the stand's main hand holds %v, want the helmet", onStand)
	}
}

// TestAPunchOnAnArmorStandWearsTheBladeAndCountsItsUse punches a stand with
// an iron sword: the blade wears one point, the item-used stat rises, the
// hand tires, and the blow's own sweep leaves the stand shaken, not broken.
func TestAPunchOnAnArmorStandWearsTheBladeAndCountsItsUse(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Attacker", servertest.OpsListing(t, "Attacker"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGrounded(3.5, 64, 5.5, 180, 40, true)
	c.Settle()
	servertest.StepTicks(t, srv, clock, 10)
	srv.GiveSlot("Attacker", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1})
	stand := srv.SpawnArmorStand(3.5, 64, 3.5)
	servertest.StepTicks(t, srv, clock, 20)
	before := srv.PlayerExhaustion("Attacker")
	c.Attack(stand)
	c.Settle()
	if got := srv.InventorySlot("Attacker", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the sword's damage is %d after the punch, want 1", got)
	}
	if got := srv.StatValue("Attacker", registry.StatUsed, "minecraft:iron_sword"); got != 1 {
		t.Errorf("the sword's use count is %d, want 1", got)
	}
	if d := srv.PlayerExhaustion("Attacker") - before; math.Abs(d-0.1) > 1e-6 {
		t.Errorf("the punch cost %v exhaustion, want 0.1", d)
	}
	if got := srv.ArmorStandCount(); got != 1 {
		t.Errorf("%d armor stands stand after one punch, want 1", got)
	}
}

// witherSkullAtStand summons a stand at x=4.5 and an owned wither skull
// flying at it from x=0.5, and waits for the skull's wither_skull hurt to
// break the stand. The skull's blast is too small to reach the owner.
func witherSkullAtStand(t *testing.T, srv *server.Server, owner uuid.UUID) {
	t.Helper()
	words := servertest.UuidWords(owner)
	srv.RunLine(`summon minecraft:armor_stand 4.5 64 0.5`)
	srv.RunLine(fmt.Sprintf(`summon minecraft:wither_skull 0.5 64.5 0.5 {Owner:[I;%d,%d,%d,%d],Motion:[0.5d,0d,0d]}`,
		words[0], words[1], words[2], words[3]))
	servertest.UntilTicks(t, srv, 40, "the wither skull breaks the stand", func() bool {
		return servertest.StandsStanding(srv) == 0
	})
}

// TestAWitherSkullBreakingAStandHealsItsMobOwner flies a pig's wither skull
// into a stand: the always-kills hurt leaves the stand dead, so the pig
// regains 5 health.
// vanilla: WitherSkull.onHitEntity, ArmorStand.hurtServer
func TestAWitherSkullBreakingAStandHealsItsMobOwner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	c.MoveToGrounded(0.5, 64, -6.5, 0, 0, true)
	c.Settle()

	srv.RunLine(`summon minecraft:pig 0.5 64 6.5 {NoAI:1b,Health:4f,UUID:[I;1,2,3,4]}`)
	mobs := srv.Mobs()
	if len(mobs) != 1 {
		t.Fatalf("%d mobs stand, want the summoned pig", len(mobs))
	}
	witherSkullAtStand(t, srv, servertest.FixtureUUID(1, 2, 3, 4))

	if health, ok := srv.MobHealth(mobs[0].ID); !ok || health != 9 {
		t.Errorf("the pig's health after its skull broke the stand = %v (alive %v), want 4+5 = 9", health, ok)
	}
}

// TestAWitherSkullBreakingAStandHealsItsPlayerOwner is the same blow with a
// player as the skull's owner, natural regeneration off so only the heal
// moves the player's health.
// vanilla: WitherSkull.onHitEntity
func TestAWitherSkullBreakingAStandHealsItsPlayerOwner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Owner", servertest.OpsListing(t, "Owner"))
	c.MoveToGrounded(0.5, 64, -6.5, 0, 0, true)
	c.Settle()
	srv.RunLine("gamerule natural_health_regeneration false")
	srv.SetHealth("Owner", 4)

	witherSkullAtStand(t, srv, protocol.OfflineUUID("Owner"))

	if health := srv.PlayerHealth("Owner"); health != 9 {
		t.Errorf("the owner's health after the skull broke the stand = %v, want 4+5 = 9", health)
	}
}
