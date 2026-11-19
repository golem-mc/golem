package protocol

import (
	"github.com/golem-mc/golem/nbt"
	"slices"
)

// blockEntityTypeIDs maps every type a chunk entry or command tag may name,
// modelled or carried, to this version's minecraft:block_entity_type ID.
var blockEntityTypeIDs = map[string]int32{
	"minecraft:furnace":                 BlockEntityFurnace,
	"minecraft:chest":                   BlockEntityChest,
	"minecraft:trapped_chest":           BlockEntityTrappedChest,
	"minecraft:ender_chest":             BlockEntityEnderChest,
	"minecraft:jukebox":                 BlockEntityJukebox,
	"minecraft:dispenser":               BlockEntityDispenser,
	"minecraft:dropper":                 BlockEntityDropper,
	"minecraft:sign":                    BlockEntitySign,
	"minecraft:hanging_sign":            BlockEntityHangingSign,
	"minecraft:mob_spawner":             BlockEntitySpawner,
	"minecraft:creaking_heart":          BlockEntityCreakingHeart,
	"minecraft:piston":                  BlockEntityPiston,
	"minecraft:brewing_stand":           BlockEntityBrewingStand,
	"minecraft:enchanting_table":        BlockEntityEnchantingTable,
	"minecraft:end_portal":              BlockEntityEndPortal,
	"minecraft:beacon":                  BlockEntityBeacon,
	"minecraft:skull":                   BlockEntitySkull,
	"minecraft:daylight_detector":       BlockEntityDaylightDetector,
	"minecraft:hopper":                  BlockEntityHopper,
	"minecraft:comparator":              BlockEntityComparator,
	"minecraft:banner":                  BlockEntityBanner,
	"minecraft:structure_block":         BlockEntityStructureBlock,
	"minecraft:end_gateway":             BlockEntityEndGateway,
	"minecraft:command_block":           BlockEntityCommandBlock,
	"minecraft:shulker_box":             BlockEntityShulkerBox,
	"minecraft:conduit":                 BlockEntityConduit,
	"minecraft:barrel":                  BlockEntityBarrel,
	"minecraft:smoker":                  BlockEntitySmoker,
	"minecraft:blast_furnace":           BlockEntityBlastFurnace,
	"minecraft:lectern":                 BlockEntityLectern,
	"minecraft:bell":                    BlockEntityBell,
	"minecraft:jigsaw":                  BlockEntityJigsaw,
	"minecraft:campfire":                BlockEntityCampfire,
	"minecraft:beehive":                 BlockEntityBeehive,
	"minecraft:sculk_sensor":            BlockEntitySculkSensor,
	"minecraft:calibrated_sculk_sensor": BlockEntityCalibratedSculkSensor,
	"minecraft:sculk_catalyst":          BlockEntitySculkCatalyst,
	"minecraft:sculk_shrieker":          BlockEntitySculkShrieker,
	"minecraft:chiseled_bookshelf":      BlockEntityChiseledBookshelf,
	"minecraft:shelf":                   BlockEntityShelf,
	"minecraft:brushable_block":         BlockEntityBrushableBlock,
	"minecraft:decorated_pot":           BlockEntityDecoratedPot,
	"minecraft:crafter":                 BlockEntityCrafter,
	"minecraft:trial_spawner":           BlockEntityTrialSpawner,
	"minecraft:vault":                   BlockEntityVault,
	"minecraft:test_block":              BlockEntityTestBlock,
	"minecraft:test_instance_block":     BlockEntityTestInstanceBlock,
	"minecraft:copper_golem_statue":     BlockEntityCopperGolemStatue,
	"minecraft:potent_sulfur":           BlockEntityPotentSulfur,
}

// blockEntityTypeNames is blockEntityTypeIDs inverted, indexed by ID.
var blockEntityTypeNames = func() []string {
	names := make([]string, RegistrySizeBlockEntityType)
	for name, id := range blockEntityTypeIDs {
		names[id] = name
	}
	return names
}()

// BlockEntityTypeID returns a block entity type's minecraft:block_entity_type
// ID, false for a name the registry does not carry.
func BlockEntityTypeID(name string) (int32, bool) {
	id, ok := blockEntityTypeIDs[name]
	return id, ok
}

// BlockEntityTypeName is BlockEntityTypeID's reverse, false for an ID past the
// registry.
func BlockEntityTypeName(id int32) (string, bool) {
	if id < 0 || int(id) >= len(blockEntityTypeNames) || blockEntityTypeNames[id] == "" {
		return "", false
	}
	return blockEntityTypeNames[id], true
}

// SetBlockEntityData lays minecraft:block_entity_data on the stack: the
// block entity type's registry ID and the network NBT compound a placed
// block reads its data from. It reports false for a type the registry does
// not name or a compound that cannot be encoded.
// vanilla: BlockItem.setBlockEntityData, TypedEntityData.STREAM_CODEC
func (s *ItemStack) SetBlockEntityData(entityType int32, tag nbt.CompoundTag) bool {
	return s.setTypedEntityData(DataComponentBlockEntityData, RegistrySizeBlockEntityType, entityType, tag)
}

// SetEntityData is SetBlockEntityData for minecraft:entity_data, naming an
// entity type.
func (s *ItemStack) SetEntityData(entityType int32, tag nbt.CompoundTag) bool {
	return s.setTypedEntityData(DataComponentEntityData, RegistrySizeEntityType, entityType, tag)
}

func (s *ItemStack) setTypedEntityData(component, registrySize, entityType int32, tag nbt.CompoundTag) bool {
	if entityType < 0 || entityType >= registrySize {
		return false
	}
	encoded, err := nbt.EncodeNetwork(withoutEntityID(tag))
	if err != nil {
		return false
	}
	var value Buffer
	value.PutVarInt(entityType)
	value.PutRaw(encoded)
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: component, Data: value.Bytes()})
	return true
}

// BlockEntityDataOf returns the block entity type and compound the stack's
// minecraft:block_entity_data component carries.
func (s ItemStack) BlockEntityDataOf() (int32, nbt.Compound, bool) {
	id, tag, ok := s.BlockEntityDataTag()
	view, _ := nbt.CompoundView(tag)
	return id, view, ok
}

// EntityDataOf returns the entity type and compound the stack's
// minecraft:entity_data component carries.
func (s ItemStack) EntityDataOf() (int32, nbt.Compound, bool) {
	id, tag, ok := s.EntityDataTag()
	view, _ := nbt.CompoundView(tag)
	return id, view, ok
}

// BlockEntityDataTag reads block entity data retaining compound insertion history.
func (s ItemStack) BlockEntityDataTag() (int32, nbt.CompoundTag, bool) {
	return s.typedEntityDataOf(DataComponentBlockEntityData)
}

// EntityDataTag reads entity data retaining compound insertion history.
func (s ItemStack) EntityDataTag() (int32, nbt.CompoundTag, bool) {
	return s.typedEntityDataOf(DataComponentEntityData)
}

func (s ItemStack) typedEntityDataOf(component int32) (int32, nbt.CompoundTag, bool) {
	for _, raw := range s.Raw {
		if raw.Type != component {
			continue
		}
		r := NewReader(raw.Data)
		entityType := r.ReadVarInt()
		if r.Err() != nil {
			return 0, nil, false
		}
		tag, _, err := nbt.DecodeNetworkOrdered(r.Rest())
		if err != nil {
			return 0, nil, false
		}
		compound, ok := tag.(nbt.CompoundTag)
		if !ok {
			return 0, nil, false
		}
		return entityType, withoutEntityID(compound), true
	}
	return 0, nil, false
}

// vanilla: TypedEntityData.stripId
func withoutEntityID(tag nbt.CompoundTag) nbt.CompoundTag {
	if tag == nil {
		return nbt.NewOrderedCompound(0)
	}
	if _, hasID := tag.Entries()["id"]; !hasID {
		return tag
	}
	out := nbt.ShallowCopyCompound(tag)
	out.Delete("id")
	return out
}
