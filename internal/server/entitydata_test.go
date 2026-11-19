package server

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestArrowPickupTagRoundTripsALargeAmplifier checks that a tipped arrow's
// pickup item keeps an amplifier above 127: vanilla saves it as an
// UNSIGNED_BYTE, so the signed byte NBT keeps must not read back negative.
func TestArrowPickupTagRoundTripsALargeAmplifier(t *testing.T) {
	t.Parallel()
	in := entity.ArrowEffect{Name: "minecraft:poison", Ticks: 100, Amplifier: 200}
	got, ok := arrowPickupOf(nbt.Compound{"item": arrowPickupTag(in)})
	if !ok {
		t.Fatal("the tag did not read back as a pickup arrow")
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("read back %+v, want %+v", got, in)
	}
}

// TestArrowPickupOfRejectsAnUnknownEffectKey checks that an effect compound
// carrying a key the arrow's own save never writes is refused, not accepted
// on its length alone.
func TestArrowPickupOfRejectsAnUnknownEffectKey(t *testing.T) {
	t.Parallel()
	effect := nbt.Compound{
		"id":       nbt.String("minecraft:poison"),
		"duration": nbt.Int(100),
		"bogus":    nbt.Int(1),
	}
	item := nbt.Compound{
		"id":    nbt.String(defaultArrowPickupItem),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{
				"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{effect}},
			},
		},
	}
	if _, ok := arrowPickupOf(nbt.Compound{"item": item}); ok {
		t.Error("an effect with an unknown key was accepted, want it refused")
	}
}

// TestAnItemTagRoundTripsTheUnlimitedAgeAndAZeroHealth writes an item whose
// age is the unlimited-lifetime marker and whose health is an explicit zero,
// then reads the compound back onto a fresh body: both survive, the zero
// rather than taking the absent-key default of five.
// vanilla: ItemEntity.readAdditionalSaveData
func TestAnItemTagRoundTripsTheUnlimitedAgeAndAZeroHealth(t *testing.T) {
	t.Parallel()
	id, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("the item registry has no dirt")
	}
	stack := protocol.ItemStack{ItemID: id, Count: 1}

	saved := &itemEntity{item: entity.NewItem(1, stack, 0, 0, 0, 0, 0, 0, 0, 1)}
	saved.item.Age = entity.ItemUnlimitedAge
	saved.item.Health = 0

	back := &itemEntity{item: entity.NewItem(2, stack, 0, 0, 0, 0, 0, 0, 0, 1)}
	applyItemTag(back, itemTag(saved).Entries())

	if back.item.Age != entity.ItemUnlimitedAge {
		t.Errorf("read back age = %d, want the marker %d", back.item.Age, entity.ItemUnlimitedAge)
	}
	if back.item.Health != 0 {
		t.Errorf("read back health = %d, want the explicit zero kept", back.item.Health)
	}
}

// TestAFireworkTagCarriesTheProjectileFlags writes an owned rocket's
// compound: Projectile.addAdditionalSaveData always writes HasBeenShot and
// ShotAtAngle, the thrower's identity rides Owner, and LeftOwner appears
// once the flight's grown box has left them.
// vanilla: FireworkRocketEntity.addAdditionalSaveData, Projectile.addAdditionalSaveData
func TestAFireworkTagCarriesTheProjectileFlags(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	r := entity.NewFirework(1, 0, 64, 0, 0, 1, 0, 0, 7)
	// One step with no owner to pass over marks the flight clear of one.
	w := world.NewDimensionWorld(t.TempDir(), true, world.NewNetherGenerator(0))
	r.Step(w, nil, nil, nil)
	if !r.LeftOwner() {
		t.Fatal("a rocket with no owner to pass over never marked LeftOwner")
	}
	tag := fireworkTag(&fireworkEntity{rocket: r, uuid: uuid.New(), ownerUUID: owner, shotMark: shotMark{true}}).Entries()
	for _, key := range []string{"HasBeenShot", "ShotAtAngle", "LeftOwner", "Owner"} {
		if _, ok := tag[key]; !ok {
			t.Errorf("an owned rocket that has left its shooter carries no %s", key)
		}
	}
	if tag["HasBeenShot"] != boolTag(true) {
		t.Errorf("a rocket that has flown saves HasBeenShot %v, want 1b", tag["HasBeenShot"])
	}
	fresh := entity.NewFirework(2, 0, 64, 0, 0, 1, 0, 0, 7)
	if fireworkTag(&fireworkEntity{rocket: fresh, uuid: uuid.New()}).Entries()["HasBeenShot"] != boolTag(false) {
		t.Error("a rocket that has not ticked saves HasBeenShot true")
	}
}

// TestACloudTagCarriesTheThrower writes a cloud left by a thrown potion's
// shatter: the thrower's identity rides Owner beside the cloud's own fields.
// vanilla: AreaEffectCloud.addAdditionalSaveData
func TestACloudTagCarriesTheThrower(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	it := &cloudEntity{
		cloud:         entity.NewCloud(1, 0, 64, 0, nil),
		durationScale: 1,
		ownerUUID:     owner,
	}
	if got := cloudTag(it).Entries()["Owner"]; !reflect.DeepEqual(got, nbt.UUIDToIntArray(owner)) {
		t.Errorf("cloud Owner = %v, want %v", got, nbt.UUIDToIntArray(owner))
	}
}

// tippedArrowItemTag is a tipped_arrow item compound brewed as the potion.
func tippedArrowItemTag(potion string) nbt.Compound {
	return nbt.Compound{
		"id":    nbt.String("minecraft:tipped_arrow"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{"potion": nbt.String(potion)},
		},
	}
}

// TestATippedArrowItemLoadsAndSavesWithItsPotion checks that a tipped_arrow
// pickup item loads, gives the shot the potion's effect (a tipped arrow
// scales the duration by an eighth) and saves back as the same item.
// vanilla: AbstractArrow.readAdditionalSaveData, Arrow.doPostHurtEffects
func TestATippedArrowItemLoadsAndSavesWithItsPotion(t *testing.T) {
	t.Parallel()
	item := tippedArrowItemTag("minecraft:poison")
	effect, stack, ok := arrowPickupStackOf(nbt.Compound{"item": item})
	if !ok {
		t.Fatal("the tipped arrow item was refused")
	}
	if effect.Name != "minecraft:poison" || effect.Ticks != 900/8 {
		t.Errorf("the shot carries %+v, want poison for an eighth of 900 ticks", effect)
	}
	ent := &arrowEntity{arrow: entity.NewArrow(1, 0, 0, 0, 0, 0, 0, false, 2, entity.PickupAllowed, 0, 1)}
	ent.arrow.Effect, ent.arrow.PickupStack = effect, stack
	got := arrowTag(ent).Entries()["item"]
	if !nbt.Equal(got, item) {
		t.Errorf("saved item %v, want %v", got, item)
	}
}

// loadedArrow loads the pickup item and entity type onto a fresh arrow.
func loadedArrow(entityType int32, item nbt.Compound) *arrowEntity {
	ent := &arrowEntity{
		entityType: entityType,
		arrow:      entity.NewArrow(1, 0, 0, 0, 0, 0, 0, false, 2, entity.PickupAllowed, 0, 1),
	}
	applyArrowTag(ent, nbt.Compound{"item": item})
	return ent
}

// TestASpectralArrowIgnoresTheItemsPotion checks that a spectral arrow
// whose item is a tipped arrow gives glowing only, and wears no potion
// colour: only an Arrow reads potion contents.
// vanilla: SpectralArrow.doPostHurtEffects, Arrow.getPotionContents
func TestASpectralArrowIgnoresTheItemsPotion(t *testing.T) {
	t.Parallel()
	ent := loadedArrow(protocol.EntityTypeSpectralArrow, tippedArrowItemTag("minecraft:poison"))
	if ent.arrow.PickupStack.Empty() {
		t.Error("the tipped arrow item was not kept as the pickup")
	}
	effects := arrowPostEffects(ent)
	if len(effects) != 1 || effects[0].ID != registry.MustMobEffect("minecraft:glowing") {
		t.Errorf("hit effects %v, want glowing alone", effects)
	}
	if got := arrowColor(ent.arrow.Effect); got != -1 {
		t.Errorf("colour %d, want none", got)
	}
}

// TestAnArrowItemCarryingPotionContentsActsTipped checks that the potion is
// read from the pickup whatever item holds it.
// vanilla: Arrow.getPotionContents
func TestAnArrowItemCarryingPotionContentsActsTipped(t *testing.T) {
	t.Parallel()
	item := tippedArrowItemTag("minecraft:poison")
	item["id"] = nbt.String("minecraft:arrow")
	ent := loadedArrow(protocol.EntityTypeArrow, item)
	if ent.arrow.Effect.Name != "minecraft:poison" {
		t.Errorf("effect %+v, want poison", ent.arrow.Effect)
	}
	if ent.arrow.Effect.Ticks != 900 {
		t.Errorf("ticks %d, want the potion's unscaled 900", ent.arrow.Effect.Ticks)
	}
}

// TestATippedArrowScalesThePotionDuration checks that only the tipped_arrow
// item carries the eighth scale.
// vanilla: Items.TIPPED_ARROW, Arrow.getPotionDurationScale
func TestATippedArrowScalesThePotionDuration(t *testing.T) {
	t.Parallel()
	ent := loadedArrow(protocol.EntityTypeArrow, tippedArrowItemTag("minecraft:poison"))
	if ent.arrow.Effect.Ticks != 112 {
		t.Errorf("ticks %d, want 112", ent.arrow.Effect.Ticks)
	}
}

// TestAnArrowWearsThePotionsCustomColor checks custom_color wins over the
// effect colour.
// vanilla: PotionContents.getColorOr
func TestAnArrowWearsThePotionsCustomColor(t *testing.T) {
	t.Parallel()
	item := tippedArrowItemTag("minecraft:poison")
	item["components"].(nbt.Compound)["minecraft:potion_contents"].(nbt.Compound)["custom_color"] = nbt.Int(16711680)
	ent := loadedArrow(protocol.EntityTypeArrow, item)
	if got := arrowColor(ent.arrow.Effect); got != 16711680 {
		t.Errorf("colour %d, want 16711680", got)
	}
}

// TestATippedArrowOfWaterWearsTheBaseColour checks that potion contents with
// no effect still colour the arrow.
// vanilla: Arrow.updateColor, PotionContents.getColor
func TestATippedArrowOfWaterWearsTheBaseColour(t *testing.T) {
	t.Parallel()
	ent := loadedArrow(protocol.EntityTypeArrow, tippedArrowItemTag("minecraft:water"))
	if ent.arrow.Effect.Name != "" || !ent.arrow.Effect.Tipped {
		t.Errorf("effect %+v, want tipped with no effect", ent.arrow.Effect)
	}
	if got := arrowColor(ent.arrow.Effect); got != -13083194 {
		t.Errorf("colour %d, want -13083194", got)
	}
}

func TestMinecartNameSurvivesSaveAndUnchangedData(t *testing.T) {
	t.Parallel()
	tag := nbt.Compound{"text": nbt.String("Rail rider"), "bold": nbt.Byte(1)}
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		t.Fatal(err)
	}
	cart := &minecartEntity{cart: entity.NewMinecart(1, 4.5, 64.1, 1.625), kind: registry.EntityTypeMinecart, uuid: uuid.New(), custom: customName{wire: string(data)}}
	saved := minecartTag(cart).Entries()
	if !reflect.DeepEqual(saved["CustomName"], tag) {
		t.Fatalf("saved name = %#v", saved["CustomName"])
	}
	back := &minecartEntity{cart: entity.NewMinecart(2, 0, 0, 0), kind: registry.EntityTypeMinecart}
	applyMinecartTag(back, saved)
	if back.custom != cart.custom {
		t.Fatal("saved name did not round trip")
	}
	was := minecartMetaOf(back)
	applyMinecartTag(back, saved)
	now := minecartMetaOf(back)
	if delta := minecartDeltaMetadata(back.cart.ID, &was, &now); delta != nil {
		t.Fatalf("unchanged data sent metadata: %x", delta)
	}
	delete(saved, "CustomName")
	applyMinecartTag(back, saved)
	now = minecartMetaOf(back)
	r := protocol.NewReader(minecartDeltaMetadata(back.cart.ID, &was, &now))
	if r.ReadVarInt() != back.cart.ID || r.ReadU8() != protocol.MetadataIndexCustomName || r.ReadVarInt() != protocol.MetadataOptionalText || r.ReadBool() || r.ReadU8() != protocol.MetadataEnd || r.Err() != nil {
		t.Fatalf("removing the name did not clear its metadata: %v", r.Err())
	}
}

func TestItemThrowerSurvivesSavedData(t *testing.T) {
	identity := uuid.New()
	it := &itemEntity{item: entity.NewItem(1, protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 12}, 0, 62, 0, 0, 0, 0, 40, 1), uuid: uuid.New(), thrower: identity}
	tag := itemTag(it).Entries()
	back := &itemEntity{item: entity.NewItem(2, protocol.ItemStack{}, 0, 0, 0, 0, 0, 0, 0, 2)}
	applyItemTag(back, tag)
	if back.thrower != identity || back.item.PickupDelay != 40 {
		t.Fatalf("thrower %s, delay %d", back.thrower, back.item.PickupDelay)
	}
	delete(tag, "Thrower")
	applyItemTag(back, tag)
	if back.thrower != uuid.Nil {
		t.Fatal("removed thrower survived a data write")
	}
}

// TestStoredAirEquipmentIsEmpty checks an equipment mainhand of air with no
// count loads empty and is omitted when saved.
// vanilla: ItemStack.isEmpty, EntityEquipment.CODEC
func TestStoredAirEquipmentIsEmpty(t *testing.T) {
	mainHand, offHand, worn := heldEquipmentOf(nbt.Compound{"mainhand": nbt.Compound{"id": nbt.String("minecraft:air")}})
	if !mainHand.Empty() {
		t.Fatalf("mainhand = %+v, want empty", mainHand)
	}
	if got := heldEquipmentTag(mainHand, offHand, worn); len(got) != 0 {
		t.Fatalf("saved equipment = %v, want none", got)
	}
}
