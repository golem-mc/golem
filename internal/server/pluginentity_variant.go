package server

import (
	"math/rand/v2"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// The variant-bearing mob handles add one ops type to the family they sit in;
// the saved compound carries every field, so a write is a merge.
type (
	sheepOps    struct{ e pluginEntity }
	variantOps  struct{ e pluginEntity }
	collarOps   struct{ e pluginEntity }
	horseOps    struct{ e pluginEntity }
	creeperOps  struct{ e pluginEntity }
	cubeOps     struct{ e pluginEntity }
	fireworkOps struct{ e pluginEntity }

	pluginSheepEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
		sheepOps
	}
	pluginVariantEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
		variantOps
	}
	pluginCollaredEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
		tameOps
		variantOps
		collarOps
	}
	pluginHorseEntity struct {
		pluginEntity
		livingOps
		mobOps
		ageOps
		loveOps
		tameOps
		horseOps
	}
	pluginCreeperEntity struct {
		pluginEntity
		livingOps
		mobOps
		creeperOps
	}
	pluginCubeEntity struct {
		pluginEntity
		livingOps
		mobOps
		cubeOps
	}
	pluginFireworkEntity struct {
		pluginEntity
		projOps
		fireworkOps
	}
)

// pluginVariantOf wraps the mob kinds that have a typed setter, nil for any
// other. It runs only for a mob's handle, so the Breedable and Tameable
// families are as pluginKindHandle finds them.
func pluginVariantOf(e pluginEntity, saved nbt.Compound) plugin.Entity {
	l, m, a, lv := livingOps{e}, mobOps{e}, ageOps{e}, loveOps{e}
	if _, cube := saved["Size"]; cube {
		return pluginCubeEntity{e, l, m, cubeOps{e}}
	}
	switch e.Type() {
	case registry.EntityTypeSheep:
		return pluginSheepEntity{e, l, m, a, lv, sheepOps{e}}
	case registry.EntityTypeWolf, registry.EntityTypeCat:
		return pluginCollaredEntity{e, l, m, a, lv, tameOps{e}, variantOps{e}, collarOps{e}}
	case registry.EntityTypeFrog, registry.EntityTypeCow, registry.EntityTypePig, registry.EntityTypeChicken:
		return pluginVariantEntity{e, l, m, a, lv, variantOps{e}}
	case registry.EntityTypeHorse:
		return pluginHorseEntity{e, l, m, a, lv, tameOps{e}, horseOps{e}}
	case registry.EntityTypeCreeper:
		return pluginCreeperEntity{e, l, m, creeperOps{e}}
	}
	return nil
}

func (e pluginEntity) dye(key string) plugin.DyeColor {
	n, _ := nbt.IntValue(e.saved()[key])
	return plugin.DyeColor(min(max(n, 0), int32(len(registry.DyeNames)-1)))
}

func (e pluginEntity) setDye(key string, c plugin.DyeColor) {
	if int(c) < len(registry.DyeNames) {
		e.merge(nbt.Compound{key: nbt.Byte(uint8(c))})
	}
}

// vanilla: Sheep.addAdditionalSaveData, DyeColor.LEGACY_ID_CODEC
func (o sheepOps) Color() plugin.DyeColor        { return o.e.dye("Color") }
func (o sheepOps) SetColor(c plugin.DyeColor)    { o.e.setDye("Color", c) }
func (o sheepOps) Sheared() bool                 { return o.e.flag("Sheared") }
func (o sheepOps) SetSheared(v bool)             { o.e.setFlag("Sheared", v) }
func (o collarOps) CollarColor() plugin.DyeColor { return o.e.dye("CollarColor") }
func (o collarOps) SetCollarColor(c plugin.DyeColor) {
	o.e.setDye("CollarColor", c)
}

// Variant is the saved registry key; an unknown key is dropped by the load.
// vanilla: VariantUtils.writeVariant
func (o variantOps) Variant() plugin.Key {
	name, _ := nbt.Get[nbt.String](o.e.saved(), "variant")
	k, _ := plugin.ParseKey(string(name))
	return k
}
func (o variantOps) SetVariant(k plugin.Key) {
	o.e.merge(nbt.Compound{"variant": nbt.String(k.String())})
}

// A horse's Variant is its colour in the low byte and its markings above.
// vanilla: Horse.getTypeVariant
func (o horseOps) variant() int32 { n, _ := nbt.IntValue(o.e.saved()["Variant"]); return n }
func (o horseOps) Color() plugin.HorseColor {
	return plugin.HorseColor(o.variant() & 255)
}
func (o horseOps) Markings() plugin.HorseMarkings {
	return plugin.HorseMarkings(o.variant() >> 8 & 255)
}
func (o horseOps) SetColor(c plugin.HorseColor) {
	if c <= plugin.HorseDarkBrown {
		o.e.merge(nbt.Compound{"Variant": nbt.Int(o.variant()&^255 | int32(c))})
	}
}
func (o horseOps) SetMarkings(m plugin.HorseMarkings) {
	if m <= plugin.MarkingsBlackDots {
		o.e.merge(nbt.Compound{"Variant": nbt.Int(o.variant()&255 | int32(m)<<8)})
	}
}

func (o creeperOps) Powered() bool     { return o.e.flag("powered") }
func (o creeperOps) SetPowered(v bool) { o.e.setFlag("powered", v) }

// Size is the saved Size plus one.
// vanilla: AbstractCubeMob.addAdditionalSaveData
func (o cubeOps) Size() int { return o.e.intField("Size") + 1 }
func (o cubeOps) SetSize(n int) {
	if n >= 1 && n <= 127 {
		o.e.merge(nbt.Compound{"Size": nbt.Int(int32(n - 1))})
	}
}

func (o fireworkOps) Item() plugin.ItemStack {
	c, ok := nbt.Get[nbt.CompoundTag](o.e.saved(), "FireworksItem")
	if !ok {
		return pluginStack(protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1})
	}
	st, _ := registry.StoredStack(c)
	return pluginStack(st)
}

// SetItem takes a firework rocket; any other item is ignored.
// vanilla: FireworkRocketEntity.DATA_ID_FIREWORKS_ITEM
func (o fireworkOps) SetItem(s plugin.ItemStack) {
	if st, ok := pluginProtocolStack(s); ok && registry.Item(st.ItemID) == registry.ItemFireworkRocket {
		// vanilla: FireworkRocketEntity.<init> rolls the life from the stack's flight
		flight, _ := fireworkFlight(st)
		o.e.merge(nbt.Compound{"FireworksItem": registry.StackTag(st),
			"LifeTime": nbt.Int(int32(entity.FireworkLifetime(flight, rand.IntN)))})
	}
}
