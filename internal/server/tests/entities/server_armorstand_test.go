package entities

// The armor stand tests: the item's placement — facing its placer, the yaw
// snapped to the 45 degrees between; /summon's stand carrying the tagged
// state the way a saved stand's wake reads it; a punch's shake and the
// second punch within the window taking the stand down with its drops, the
// creative hand's one punch alone; and the interact's slot swap, the click
// offset picking the slot and the DisabledSlots mask locking it.

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

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
