package entity

import "github.com/golem-mc/golem/nbt"

// The setters below are the mob kinds' applyImplicitComponent for the
// mob-variant item components. Each does nothing for a kind that does not
// read the component.

// SetCollarColour is Wolf and Cat.applyImplicitComponent for wolf/collar and
// cat/collar.
func (m *Mob) SetCollarColour(colour string, kind MobType) {
	if m.Type == kind {
		m.SetMetadataCollar(colour)
	}
}

// SetSheepColour is Sheep.applyImplicitComponent for sheep/color.
func (m *Mob) SetSheepColour(colour string) {
	if m.Type == TypeSheep {
		m.SetMetadataColour(colour)
	}
}

// SetShulkerDye is Shulker.applyImplicitComponent for shulker/color: a dye id.
func (m *Mob) SetShulkerDye(dye uint8) {
	if m.Type == TypeShulker {
		m.SetShulkerColor(dye)
	}
}

// SetFoxVariant is Fox.applyImplicitComponent for fox/variant.
func (m *Mob) SetFoxVariant(id int32) {
	if m.Type == TypeFox {
		m.SetMetadataFoxVariant(id)
	}
}

// SetMooshroomVariant is MushroomCow.applyImplicitComponent for
// mooshroom/variant: "red" or "brown".
func (m *Mob) SetMooshroomVariant(name string) {
	if m.Type == TypeMooshroom {
		m.SetMetadataMushroom(name)
	}
}

// SetHorseColour is Horse.applyImplicitComponent for horse/variant: the
// colour changes and the markings stay.
// vanilla: Horse.setVariant
func (m *Mob) SetHorseColour(id int32) {
	if m.Type == TypeHorse {
		m.SetMetadataHorseVariant(id&0xFF | m.Horse.Variant&^0xFF)
	}
}

// SetLlamaVariant is Llama.applyImplicitComponent for llama/variant.
func (m *Mob) SetLlamaVariant(id int32) {
	if IsLlama(m.Type) {
		m.SetMetadataHorseVariant(id)
	}
}

// SetVillagerVariant is Villager and ZombieVillager.applyImplicitComponent
// for villager/variant: the type changes and the data counts as finalized.
func (m *Mob) SetVillagerVariant(name string) {
	if m.Type == TypeVillager || m.Type == TypeZombieVillager {
		m.SetMetadataVillagerType(name)
		m.SetMetadataVillagerFinalized(true)
	}
}

// SetVariantEntry is the applyImplicitComponent of a kind whose variant is a
// registry entry: Wolf, Cat, Pig, Cow, Chicken, Frog and ZombieNautilus.
func (m *Mob) SetVariantEntry(kind MobType, name string) {
	if m.Type == kind {
		m.Variant = name
	}
}

// SetSoundVariantEntry is SetVariantEntry for a sound variant.
func (m *Mob) SetSoundVariantEntry(kind MobType, name string) {
	if m.Type == kind {
		m.SoundVariant = name
	}
}

// SetSalmonVariant is Salmon.applyImplicitComponent for salmon/size.
func (m *Mob) SetSalmonVariant(size int) {
	if m.Type == TypeSalmon {
		m.SalmonSize = size
	}
}

// SetTropicalPart is TropicalFish.applyImplicitComponent: the pattern, base
// colour or pattern colour changes and the other two stay.
func (m *Mob) SetTropicalPart(mask, shift, value int32) {
	if m.Type == TypeTropicalFish {
		m.SetMetadataTropicalVariant(m.TropicalVariant&^(mask<<shift) | value&mask<<shift)
	}
}

// SetCustomData is Entity.applyImplicitComponent for custom_data.
func (m *Mob) SetCustomData(data nbt.Compound) {
	m.CustomData = nil
	if len(data) > 0 {
		m.CustomData = nbt.Copy(data).(nbt.Compound)
	}
}
