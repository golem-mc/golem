package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The mooshroom, checked against the decompiled MushroomCow against
// the cow it extends: the bowl vanilla's mobInteract fills with mushroom
// stew, the shears that turn it into a cow with the five mushrooms of its
// variant, the Type the brown is summoned and saved under, and the breeding
// that passes the variant on.

// mooshroomShearSkip is the background a shear's reading steps over: the
// world's stream, everything a moving body sends, and the pairing packets of
// the bodies it is not reading whole — the drops' stacks reach it inside
// readDropStack, and the new cow's state and pace are the world's, not the
// shearing's.
func mooshroomShearSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
		protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
		protocol.IDPlayTeleportEntity, protocol.IDPlayContainerSlot,
		protocol.IDPlayEntityEquipment, protocol.IDPlayEntityAnimation,
		protocol.IDPlaySetEntityMotion, protocol.IDPlaySystemChat,
		protocol.IDPlayPickupItem, protocol.IDPlayPingResponse,
		protocol.IDPlayEntityEvent, protocol.IDPlayDamageEvent,
		protocol.IDPlayHurtAnimation, protocol.IDPlayEntityMetadata,
		protocol.IDPlayUpdateAttributes, protocol.IDPlayLevelParticles:
		return true
	}
	return false
}

// TestBowlOnAMooshroomFillsWithMushroomStew checks MushroomCow.mobInteract's
// bowl branch: an empty bowl on a grown mooshroom is filled with one
// mushroom stew, the milking plays from the mooshroom's own body, and the
// mooshroom is still standing there afterwards. The bucket after it answers
// AbstractCow's mobInteract behind it: the same milking a cow gives.
func TestBowlOnAMooshroomFillsWithMushroomStew(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Filler")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bowl := servertest.ItemIDOrDie(t, "minecraft:bowl")
	stew := servertest.ItemIDOrDie(t, "minecraft:mushroom_stew")
	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	milk := servertest.ItemIDOrDie(t, "minecraft:milk_bucket")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bowl, Count: 1})
	c.Settle()

	srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	// The red variant is the metadata client's default, and a spawn says
	// a default by leaving it out — the pairing below reads every field it
	// carries, so reaching its end says the variant was not among them.
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	slot, stack := c.ExpectContainerSlot()
	if slot != int16(player.HotbarSlot) {
		t.Errorf("the stew went to slot %d, want the hand's %d", slot, player.HotbarSlot)
	}
	if stack.ItemID != stew || stack.Count != 1 {
		t.Errorf("the filled bowl = %+v, want one mushroom stew (item %d)", stack, stew)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != stew || got.Count != 1 {
		t.Errorf("the server's slot holds %+v, want the mushroom stew", got)
	}
	// vanilla: entity.mooshroom.milk, played from the body in the neutral
	// category a mob's own voice speaks in.
	stewSounds := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, category, x, y, z, volume, _ := servertest.ReadAmbient(t, data)
		switch name {
		case "minecraft:entity.mooshroom.milk":
			stewSounds++
			if category != protocol.SoundCategoryNeutral {
				t.Errorf("the mooshroom's milking played in category %d, want the neutral its voice speaks in", category)
			}
			if x != 44 || y != 512 || z != 44 {
				t.Errorf("the mooshroom's milking played at (%d,%d,%d), want its feet (44,512,44)", x, y, z)
			}
			if volume != 1 {
				t.Errorf("the mooshroom's milking played at volume %v, want 1", volume)
			}
		case "minecraft:entity.cow.milk":
			t.Error("the stew's milking played the cow's sound, want the mooshroom's own")
		}
	})
	if stewSounds != 1 {
		t.Errorf("the bowl played %d mooshroom milkings, want one", stewSounds)
	}

	// The bucket behind the bowl: AbstractCow.mobInteract's milk bucket.
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	c.Settle()
	c.UseOn(mooshroom.EntityID)
	_, stack = c.ExpectContainerSlot()
	if stack.ItemID != milk || stack.Count != 1 {
		t.Errorf("the filled bucket = %+v, want one milk bucket (item %d)", stack, milk)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != milk {
		t.Errorf("the server's slot holds item %d, want the milk bucket %d", got.ItemID, milk)
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("the world holds %d mobs after the bowl and the bucket, want the one milked mooshroom", got)
	}
}

// TestShearingAMooshroomTurnsItIntoACow checks MushroomCow.shear: shears
// turn a mooshroom into a cow — the old body out of the world, a cow in its
// place — with the five mushrooms of minecraft:shearing/mooshroom/red laid
// where it stood, the shear's own sound, and a wear paid by the shears.
func TestShearingAMooshroomTurnsItIntoACow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	red := servertest.ItemIDOrDie(t, "minecraft:red_mushroom")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()

	srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	var drops []servertest.ItemDrop
	shearSound, cowSpawned, mooshroomGone := false, false, false
	cowID := int32(0)
	c.ReadTicks(srv, 200, func(gotID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			id := r.ReadVarInt()
			r.ReadUUID()
			switch got := r.ReadVarInt(); got {
			case protocol.EntityTypeCow:
				if id == mooshroom.EntityID {
					t.Fatal("the cow arrived under the mooshroom's own entity ID")
				}
				cowID, cowSpawned = id, true
			case protocol.EntityTypeItem:
				drop := servertest.ParseItemSpawn(data)
				c.ReadDropStack(&drop)
				drops = append(drops, drop)
			}
		case protocol.IDPlayEntitySoundEffect:
			shearSound = shearSound || servertest.SoundIs(data, "minecraft:entity.mooshroom.shear")
		case protocol.IDPlayRemoveEntities:
			for count := r.ReadVarInt(); count > 0; count-- {
				if r.ReadVarInt() == mooshroom.EntityID {
					mooshroomGone = true
				}
			}
		default:
			if !mooshroomShearSkip(gotID) {
				t.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
			}
		}
		return len(drops) == 5 && shearSound && cowSpawned && mooshroomGone
	})
	if got := len(drops); got != 5 {
		t.Fatalf("the shearing dropped %d stacks, want vanilla's five mushrooms", got)
	}
	for _, drop := range drops {
		if drop.Stack.ItemID != red || drop.Stack.Count != 1 {
			t.Errorf("a shearing drop = %+v, want one red mushroom (item %d)", drop.Stack, red)
		}
	}
	if !shearSound {
		t.Error("the shearing never played entity.mooshroom.shear")
	}
	if !cowSpawned || !mooshroomGone {
		t.Errorf("the conversion gave a cow (%v) and removed the mooshroom (%v), want both", cowSpawned, mooshroomGone)
	}

	// The world's own answer: one cow standing where the mooshroom was, and
	// the five mushrooms it shed lying around it.
	if got := srv.MobCount(); got != 1 {
		t.Fatalf("the world holds %d mobs after the shearing, want the one cow", got)
	}
	mob := srv.Mobs()[0]
	if mob.Kind != "minecraft:cow" || mob.ID != cowID {
		t.Errorf("the world holds a %s of ID %d, want the cow of ID %d the wire showed", mob.Kind, mob.ID, cowID)
	}
	if mob.X != 5.5 || mob.Z != 5.5 {
		t.Errorf("the cow stands at (%v,%v), want the mooshroom's (5.5,5.5)", mob.X, mob.Z)
	}
	var reds int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:red_mushroom" {
			reds += int(it.Count)
		}
	}
	if reds != 5 {
		t.Errorf("the ground holds %d red mushrooms, want vanilla's five", reds)
	}

	// The shears paid a wear of their own.
	after := srv.InventorySlot("Shearer", player.HotbarSlot)
	if after.ItemID != shears {
		t.Errorf("the held slot holds item %d after the shearing, want the shears", after.ItemID)
	}
}

// TestShearingABrownMooshroomDropsBrownMushrooms checks the variant's own
// shearing: a brown is summoned under the Type it is saved as, shows the
// brown's metadata its pairing carries, and drops the five brown mushrooms
// its shearing table names.
func TestShearingABrownMooshroomDropsBrownMushrooms(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Shearer", servertest.OpsListing(t, "Shearer"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	brown := servertest.ItemIDOrDie(t, "minecraft:brown_mushroom")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()

	c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Type:"brown"}`)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if !mooshroom.HasMooshroomVariant || mooshroom.MooshroomVariant != 1 {
		t.Fatalf("the brown's pairing carried variant %v (found %v), want the VarInt 1 of the brown", mooshroom.MooshroomVariant, mooshroom.HasMooshroomVariant)
	}

	c.UseOn(mooshroom.EntityID)
	var drops []servertest.ItemDrop
	shearSound, cowSpawned, mooshroomGone := false, false, false
	c.ReadTicksSkipping(srv, 200, mooshroomShearSkip, func(gotID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			id := r.ReadVarInt()
			r.ReadUUID()
			switch got := r.ReadVarInt(); got {
			case protocol.EntityTypeCow:
				cowSpawned = id != 0
			case protocol.EntityTypeItem:
				drop := servertest.ParseItemSpawn(data)
				c.ReadDropStack(&drop)
				drops = append(drops, drop)
			}
		case protocol.IDPlayEntitySoundEffect:
			shearSound = shearSound || servertest.SoundIs(data, "minecraft:entity.mooshroom.shear")
		case protocol.IDPlayRemoveEntities:
			for count := r.ReadVarInt(); count > 0; count-- {
				if r.ReadVarInt() == mooshroom.EntityID {
					mooshroomGone = true
				}
			}
		}
		return len(drops) == 5 && shearSound && cowSpawned && mooshroomGone
	})
	if got := len(drops); got != 5 {
		t.Fatalf("the brown's shearing dropped %d stacks, want vanilla's five mushrooms", got)
	}
	for _, drop := range drops {
		if drop.Stack.ItemID != brown || drop.Stack.Count != 1 {
			t.Errorf("a shearing drop = %+v, want one brown mushroom (item %d)", drop.Stack, brown)
		}
	}
	if !shearSound || !cowSpawned || !mooshroomGone {
		t.Fatalf("the brown's shearing gave sound %v, cow %v, removal %v; want all", shearSound, cowSpawned, mooshroomGone)
	}
	if got := srv.MobCount(); got != 1 {
		t.Fatalf("the world holds %d mobs after the shearing, want the one cow", got)
	}
	if mob := srv.Mobs()[0]; mob.Kind != "minecraft:cow" {
		t.Errorf("the world holds a %s, want the cow the shearing made", mob.Kind)
	}
}

// TestMooshroomsBreedABrownCalf checks MushroomCow.getBreedOffspring: two
// loving mooshrooms breed a baby mooshroom — not the cow their kind extends
// — carrying the parents' variant.
func TestMooshroomsBreedABrownCalf(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Rancher", servertest.OpsListing(t, "Rancher"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Rancher", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat"), Count: 64})

	c.ChatCommand(`summon minecraft:mooshroom 1.5 64 2.5 {Type:"brown"}`)
	mom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if !mom.HasMooshroomVariant || mom.MooshroomVariant != 1 {
		t.Errorf("the summoned mother carries variant %v (found %v), want the brown's 1", mom.MooshroomVariant, mom.HasMooshroomVariant)
	}
	c.ChatCommand(`summon minecraft:mooshroom 2.5 64 2.5 {Type:"brown"}`)
	dad := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if dad.EntityID == mom.EntityID {
		t.Fatal("the two summoned mooshrooms came as one")
	}
	if !dad.HasMooshroomVariant || dad.MooshroomVariant != 1 {
		t.Errorf("the summoned father carries variant %v (found %v), want the brown's 1", dad.MooshroomVariant, dad.HasMooshroomVariant)
	}

	c.UseOn(mom.EntityID)
	c.AwaitEntityEvent(mom.EntityID, loveHearts)
	c.UseOn(dad.EntityID)
	c.AwaitEntityEvent(dad.EntityID, loveHearts)
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.InventorySlot("Rancher", player.HotbarSlot).Count == 62
	})

	// The orb the breeding drops goes out ahead of the child.
	kind, _ := c.AwaitNextSpawn()
	if kind != protocol.EntityTypeExperienceOrb {
		t.Fatalf("the breeding's first arrival was entity type %d, want the experience orb before the child", kind)
	}
	calf := c.AwaitSpawn(protocol.EntityTypeMooshroom)
	fields := c.AwaitMetadataFor(calf.EntityID)
	if baby := servertest.MetaOf(fields, protocol.MetadataIndexBaby); baby == nil || !baby.FlagVal {
		t.Error("the bred mooshroom is not a baby, want the newborn it is")
	}
	if variant := servertest.MetaOf(fields, protocol.MetadataIndexMooshroomVariant); variant == nil || variant.VarintVal != 1 {
		t.Errorf("the calf carries variant %v, want the brown of its parents", variant)
	}
	if got := srv.MobCount(); got != 3 {
		t.Errorf("the world holds %d mooshrooms, want the parents and the calf", got)
	}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:mooshroom" {
			t.Errorf("a baby %s was born of two mooshrooms, want the mooshroom of their own kind", m.Kind)
		}
	}
}

// TestABabyMooshroomFillsNoBowl: the bowl branch is for the grown body
// alone (MushroomCow.mobInteract's !isBaby), so a calf leaves the bowl empty
// and nothing sounding.
func TestABabyMooshroomFillsNoBowl(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Filler", servertest.OpsListing(t, "Filler"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	bowl := servertest.ItemIDOrDie(t, "minecraft:bowl")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bowl, Count: 2})
	c.Settle()

	c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Age:-24000}`)
	calf := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if !calf.Baby {
		t.Fatal("the summoned calf is a baby, want the age read to make it one")
	}
	c.UseOn(calf.EntityID)
	slots := 0
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayContainerSlot {
			slots++
		}
	})
	if slots != 0 {
		t.Errorf("the calf's bowl sent %d slot changes, want none", slots)
	}
	if got := srv.InventorySlot("Filler", player.HotbarSlot); got.ItemID != bowl || got.Count != 2 {
		t.Errorf("the hand after the calf's bowl = %+v, want the two bowls untouched", got)
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("the world holds %d mobs, want the one calf", got)
	}
}

// mooshroomParticle reads a Level Particles packet's particle type, the
// first field; its options are not read.
func mooshroomParticle(t *testing.T, data []byte) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	return r.ReadVarInt()
}

// TestADispenserShearsALeashedMooshroomLeadFirst checks
// ShearsDispenseItemBehavior.tryShearEntity: the first dispense cuts the
// mooshroom's lead and leaves it a mooshroom; only the next converts it.
func TestADispenserShearsALeashedMooshroomLeadFirst(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer", servertest.OpsListing(t, "Shearer"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()
	servertest.HoldTicks(t, srv)

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	giveLead(t, srv, "Shearer", 2)
	mooshroomID := srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	// Tie it to the player, then to the fence's knot.
	c.UseOn(mooshroomID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != mooshroomID || holding != knot.EntityID {
		t.Fatalf("the fence links (%d, %d), want the mooshroom %d on the knot %d", attached, holding, mooshroomID, knot.EntityID)
	}

	// The lead goes first, as a player's shears would cut it first too.
	c.ChatCommand("setblock 5 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	if !srv.SetInventoryAt(5, 64, 4, 0, protocol.ItemStack{ItemID: shears, Count: 1}) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	mobs := srv.Mobs()
	if len(mobs) != 1 || mobs[0].Kind != "minecraft:mooshroom" {
		t.Fatalf("mobs after the first dispense = %+v, want the one mooshroom", mobs)
	}
	if holder, ok := srv.MobLeashHolder(mooshroomID); ok && holder != 0 {
		t.Errorf("the mooshroom still has a lead held by %d after the first dispense", holder)
	}

	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	mobs = srv.Mobs()
	if len(mobs) != 1 || mobs[0].Kind != "minecraft:cow" {
		t.Fatalf("mobs after the second dispense = %+v, want the one cow", mobs)
	}
}

// TestShearingASeatedMooshroomKeepsTheCowInTheBoat checks the SINGLE
// conversion's seat: a mooshroom in a boat shears into a cow that takes the
// old body's seat instead of stepping out of the hull.
func TestShearingASeatedMooshroomKeepsTheCowInTheBoat(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer", servertest.OpsListing(t, "Shearer"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.TickBoats(1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	mooshroomID := srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	srv.TickBoats(1)
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != mooshroomID {
		t.Fatalf("the boat's passengers = %v, want the mooshroom %d seated", got, mooshroomID)
	}

	c.UseOn(mooshroomID)
	servertest.WaitFor(t, srv, 1200, func() bool {
		mobs := srv.Mobs()
		return len(mobs) == 1 && mobs[0].Kind == "minecraft:cow"
	})
	cow := srv.Mobs()[0]
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != cow.ID {
		t.Errorf("the boat's passengers = %v, want the cow %d in the mooshroom's seat", got, cow.ID)
	}
}

// TestACreativeBowlOnAMooshroomFillsEveryClick checks MushroomCow.mobInteract's
// createFilledResult(..., false): a creative click keeps the bowl and gives a
// stew every time, not only when the inventory holds none.
func TestACreativeBowlOnAMooshroomFillsEveryClick(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Filler", servertest.OpsListing(t, "Filler"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bowl := servertest.ItemIDOrDie(t, "minecraft:bowl")
	stew := servertest.ItemIDOrDie(t, "minecraft:mushroom_stew")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bowl, Count: 1})
	c.Settle()
	mooshroomID := srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	stews := func() int {
		n := 0
		for slot := 0; slot <= player.OffHandSlot; slot++ {
			if s := srv.InventorySlot("Filler", slot); s.ItemID == stew {
				n += int(s.Count)
			}
		}
		return n
	}
	for click := 1; click <= 2; click++ {
		c.UseOn(mooshroomID)
		servertest.WaitFor(t, srv, 1200, func() bool { return stews() >= click })
		if got := srv.InventorySlot("Filler", player.HotbarSlot).ItemID; got != bowl {
			t.Errorf("after click %d the hand holds item %d, want the bowl a creative click keeps", click, got)
		}
	}
	if got := stews(); got != 2 {
		t.Errorf("two creative clicks gave %d mushroom stew, want one each", got)
	}
}

// TestASilentMooshroomFillsTheBowlInSilence checks Entity.playSound's
// isSilent gate on the mooshroom's milking: the bowl still fills, and the
// body says nothing.
func TestASilentMooshroomFillsTheBowlInSilence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Filler", servertest.OpsListing(t, "Filler"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bowl := servertest.ItemIDOrDie(t, "minecraft:bowl")
	stew := servertest.ItemIDOrDie(t, "minecraft:mushroom_stew")
	srv.GiveSlot("Filler", player.HotbarSlot, protocol.ItemStack{ItemID: bowl, Count: 1})
	c.Settle()
	mooshroomID := srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if !srv.SetMobSilent(mooshroomID, true) {
		t.Fatal("SetMobSilent found no mooshroom")
	}

	c.UseOn(mooshroomID)
	filled, milked := false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt() // window ID
			r.ReadVarInt() // state ID
			r.ReadI16()
			if stack, ok := r.ReadSlot(); ok && stack.ItemID == stew {
				filled = true
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.mooshroom.milk" {
				milked = true
			}
		}
	})
	if !filled {
		t.Error("the silent mooshroom filled no bowl, want the stew still given")
	}
	if milked {
		t.Error("a silent mooshroom played its milking, want it held in")
	}
}

// TestAMooshroomsShearingLaysItsMushroomsAtItsFeet checks MushroomCow.shear's
// own drop: each mushroom lies at the body's exact x and z — the
// four-argument ItemEntity, no scatter — and the conversion puffs one
// EXPLOSION at its middle.
func TestAMooshroomsShearingLaysItsMushroomsAtItsFeet(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()
	srv.SpawnMob("minecraft:mooshroom", 5.5, 64, 5.5)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	skip := func(id int32) bool {
		return id != protocol.IDPlayLevelParticles && mooshroomShearSkip(id)
	}
	var drops []servertest.ItemDrop
	explosion := false
	c.ReadTicksSkipping(srv, 200, skip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if drop := servertest.ParseItemSpawn(data); drop.EntityID != 0 {
				c.ReadDropStack(&drop)
				drops = append(drops, drop)
			}
		case protocol.IDPlayLevelParticles:
			explosion = explosion || mooshroomParticle(t, data) == protocol.ParticleExplosion
		}
		return len(drops) == 5 && explosion
	})
	if got := len(drops); got != 5 {
		t.Fatalf("the shearing dropped %d stacks, want vanilla's five mushrooms", got)
	}
	for _, drop := range drops {
		if servertest.Abs64(drop.X-5.5) > 1e-9 || servertest.Abs64(drop.Z-5.5) > 1e-9 {
			t.Errorf("a mushroom dropped at (%v,%v), want the mooshroom's own x and z (5.5,5.5)", drop.X, drop.Z)
		}
	}
	if !explosion {
		t.Error("the shearing puffed no EXPLOSION")
	}
}

// TestShearingAMooshroomDoesNotDropItsGear checks the SINGLE conversion's
// keepEquipment false: the copy is skipped, and the old body, discarded with
// no loot, takes the sword it held with it — nothing is left on the ground.
func TestShearingAMooshroomDoesNotDropItsGear(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Shearer", servertest.OpsListing(t, "Shearer"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()
	c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {equipment:{mainhand:{id:"minecraft:diamond_sword",count:1}}}`)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)

	c.UseOn(mooshroom.EntityID)
	servertest.WaitFor(t, srv, 1200, func() bool {
		mobs := srv.Mobs()
		return len(mobs) == 1 && mobs[0].Kind == "minecraft:cow"
	})
	c.Settle()
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == "minecraft:diamond_sword" {
			t.Error("the shearing laid the mooshroom's sword on the ground, want it gone with the discarded body")
		}
	}
}

// TestADispenserShearsAMooshroom checks the dispenser's shear on the
// mooshroom: the five mushrooms come off where it stood and the body
// converts to a cow.
func TestADispenserShearsAMooshroom(t *testing.T) {
	t.Parallel()
	srv, w := servertest.Dispensing(t)

	srv.SpawnMob("minecraft:mooshroom", 4.5, 64, 1.5)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	w.DrainRecorded()

	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	w.DrainRecorded()
	var mushrooms int
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item != "minecraft:red_mushroom" {
			continue
		}
		mushrooms += int(it.Count)
		if servertest.Abs64(it.X-4.5) > 1e-9 || servertest.Abs64(it.Z-1.5) > 1e-9 {
			t.Errorf("a mushroom lay at (%v,%v), want the mooshroom's own x and z (4.5,1.5)", it.X, it.Z)
		}
		if it.PickupDelay != 0 {
			t.Errorf("a mushroom's pickup delay = %d, want the ItemEntity constructor's nought", it.PickupDelay)
		}
	}
	if mushrooms != 5 {
		t.Errorf("the dispenser's shear left %d red mushrooms, want the five", mushrooms)
	}
	if got := srv.MobCount(); got != 1 {
		t.Fatalf("the world holds %d mobs after the dispenser's shear, want the one cow", got)
	}
	if mob := srv.Mobs()[0]; mob.Kind != "minecraft:cow" {
		t.Errorf("the dispenser's shear made a %s, want the cow", mob.Kind)
	}
}

// TestABrownMooshroomIsSavedAsItsTypeAndReturnsBrown checks the file's half
// of the variant: a brown leaves with its chunk as Type "brown", the tag
// MushroomCow.adds beside the rest of it, and comes back as the brown it
// was.
func TestABrownMooshroomIsSavedAsItsTypeAndReturnsBrown(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")
	c.Settle()

	c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Type:"brown"}`)
	mooshroom := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	if !mooshroom.HasMooshroomVariant || mooshroom.MooshroomVariant != 1 {
		t.Fatalf("the summoned brown carries variant %v (found %v), want its 1", mooshroom.MooshroomVariant, mooshroom.HasMooshroomVariant)
	}

	// Leaving takes the chunks away and the tick writes the mob standing
	// in the out-of-world chunk into its entity file.
	c.Disconnect()
	servertest.WaitFor(t, srv, 100, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:mooshroom", func(compound nbt.Compound) {
		kind, ok := nbt.Get[nbt.String](compound, "Type")
		if !ok || string(kind) != "brown" {
			t.Errorf("the saved mooshroom's Type = %q (found %v), want the brown of its variant", string(kind), ok)
		}
	})

	// Back in, the file's brown is the brown that left: the pairing the
	// join reads carries the variant's own byte.
	second := connect()
	second.Login("Keeper")
	second.Configure()
	returned, back := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeMooshroom)
	if !back {
		t.Fatal("the brown mooshroom was not restored with its chunk")
	}
	if !returned.HasMooshroomVariant || returned.MooshroomVariant != 1 {
		t.Errorf("the brown came back as variant %v (found %v), want the 1 the file kept of it", returned.MooshroomVariant, returned.HasMooshroomVariant)
	}
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:mooshroom" {
			t.Errorf("the reloaded world holds a %s, want the brown mooshroom", m.Kind)
		}
	}
}

// vanilla: MushroomCow.mobInteract
func TestBrownMooshroomFlowerMakesOneSuspiciousStew(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.GameMode = mode
			srv, c := servertest.Join(t, cfg, "Florist", servertest.OpsListing(t, "Florist"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Type:"brown",NoAI:1b}`)
			cow := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
			flower := servertest.ItemIDOrDie(t, "minecraft:allium")
			srv.GiveSlot("Florist", player.HotbarSlot, protocol.ItemStack{ItemID: flower, Count: 3})
			c.Settle()
			for click := range 2 {
				if click == 1 {
					flower = servertest.ItemIDOrDie(t, "minecraft:poppy")
					srv.GiveSlot("Florist", player.HotbarSlot, protocol.ItemStack{ItemID: flower, Count: 3})
					c.Settle()
				}
				c.UseOn(cow.EntityID)
				eats, particles := 0, 0
				c.SettleAndCollect(func(id int32, data []byte) {
					if id == protocol.IDPlaySoundEffect {
						name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
						if name == "minecraft:entity.mooshroom.eat" {
							eats++
							if category != protocol.SoundCategoryNeutral || x != 44 || y != 512 || z != 44 || volume != 2 || pitch != 1 {
								t.Errorf("eat sound = category %d at (%d,%d,%d), volume %v pitch %v", category, x, y, z, volume, pitch)
							}
						}
					}
					if id == protocol.IDPlayLevelParticles {
						want := int32(protocol.ParticleSmoke)
						if click == 0 {
							want, _, _ = registry.ParticleID("minecraft:effect")
						}
						if mooshroomParticle(t, data) == want {
							particles++
						}
					}
				})
				wantEats, wantParticles := 1, 4
				if click == 1 {
					wantEats, wantParticles = 0, 2
				}
				if eats != wantEats || particles != wantParticles {
					t.Errorf("click %d: eat sounds %d particles %d, want %d and %d", click, eats, particles, wantEats, wantParticles)
				}
				wantCount := int32(2)
				if mode == "creative" || click == 1 {
					wantCount = 3
				}
				if got := srv.InventorySlot("Florist", player.HotbarSlot); got.ItemID != flower || got.Count != wantCount {
					t.Fatalf("click %d: flower stack %+v, want count %d", click, got, wantCount)
				}
			}
			for bowl := range 2 {
				srv.GiveSlot("Florist", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bowl", 1))
				c.Settle()
				c.UseOn(cow.EntityID)
				wantItem, wantSound := registry.ItemMushroomStew, "minecraft:entity.mooshroom.milk"
				if bowl == 0 {
					wantItem, wantSound = registry.ItemSuspiciousStew, "minecraft:entity.mooshroom.suspicious_milk"
				}
				found, sounds := false, 0
				c.SettleAndCollect(func(id int32, data []byte) {
					switch id {
					case protocol.IDPlayContainerSlot:
						r := protocol.NewReader(data)
						r.ReadVarInt()
						r.ReadVarInt()
						r.ReadI16()
						stack, ok := r.ReadSlot()
						if ok && stack.ItemID == int32(wantItem) {
							found = true
							if bowl == 0 {
								assertAlliumStew(t, stack)
							}
						}
					case protocol.IDPlaySoundEffect:
						name, _, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
						if name == wantSound {
							sounds++
							if volume != 1 || pitch != 1 {
								t.Errorf("milk volume %v pitch %v, want 1, 1", volume, pitch)
							}
						}
					}
				})
				if !found || sounds != 1 {
					t.Fatalf("bowl %d: stew found %v, milk sounds %d", bowl, found, sounds)
				}
			}
		})
	}
}

func assertAlliumStew(t *testing.T, stack protocol.ItemStack) {
	t.Helper()
	effects := stack.SuspiciousStewEffectsOf()
	if stack.ItemID != int32(registry.ItemSuspiciousStew) || len(effects) != 1 || effects[0].Effect != registry.MustMobEffect("minecraft:fire_resistance") || effects[0].Duration != 60 {
		t.Fatalf("stew %+v effects %+v, want fire resistance for 60 ticks", stack, effects)
	}
}

func TestRedAndBabyMooshroomsIgnoreFlowers(t *testing.T) {
	t.Parallel()
	for _, tags := range []string{`{Type:"red",NoAI:1b}`, `{Type:"brown",Age:-24000,NoAI:1b}`} {
		t.Run(tags, func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Florist", servertest.OpsListing(t, "Florist"))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 ` + tags)
			cow := c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
			flower := servertest.ItemStack(t, "minecraft:allium", 2)
			srv.GiveSlot("Florist", player.HotbarSlot, flower)
			c.Settle()
			c.UseOn(cow.EntityID)
			c.SettleAndCollect(func(id int32, data []byte) {
				if id == protocol.IDPlaySoundEffect {
					if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.mooshroom.eat" {
						t.Error("ignored flower played eat sound")
					}
				}
			})
			if got := srv.InventorySlot("Florist", player.HotbarSlot); got.ItemID != flower.ItemID || got.Count != flower.Count {
				t.Fatalf("ignored flower stack = %+v, want %+v", got, flower)
			}
		})
	}
}

func TestMooshroomFlowerEffectsSurviveReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect().Join("Keeper")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand(`summon minecraft:mooshroom 5.5 64 5.5 {Type:"brown",NoAI:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeMooshroom)
	srv.GiveSlot("Keeper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:allium", 1))
	c.Settle()
	c.UseOn(srv.Mobs()[0].ID)
	c.ExpectContainerSlot()
	c.Settle()
	c.Disconnect()
	servertest.WaitFor(t, srv, 100, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:mooshroom", func(tag nbt.Compound) {
		list, ok := nbt.Get[nbt.List](tag, "stew_effects")
		if !ok || len(list.Items) != 1 {
			t.Fatalf("saved stew_effects = %+v, found %v", list, ok)
		}
		effect, ok := list.Items[0].(nbt.Compound)
		if !ok || effect["id"] != nbt.String("minecraft:fire_resistance") || effect["duration"] != nbt.Int(60) {
			t.Errorf("saved effect = %+v, want fire resistance 60", effect)
		}
	})
	second := connect()
	second.Login("Keeper")
	second.Configure()
	cow, ok := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeMooshroom)
	if !ok {
		t.Fatal("mooshroom did not reload")
	}
	second.MarkLoaded()
	second.MoveToGround(2.5, 64, 5.5, true)
	srv.GiveSlot("Keeper", player.HotbarSlot, servertest.ItemStack(t, "minecraft:bowl", 1))
	second.Settle()
	second.UseOn(cow.EntityID)
	_, stack := second.ExpectContainerSlot()
	assertAlliumStew(t, stack)
}
