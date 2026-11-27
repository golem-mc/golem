package world

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// A dimension's mobs live in their own region files (vanilla's layout since
// 1.17): the chunks' Anvil layout under entities/ instead of region/, one compound per
// chunk with its position and entities. Mobs are saved in vanilla's form (id,
// Pos, Motion, Rotation, Health, UUID and type-specific fields), so vanilla
// opens a world written here with its animals.

// entitiesRegionFilePath is the entity region file for (rx, rz) under dir's
// entities/ folder, named like the chunk regions.
func entitiesRegionFilePath(dir string, rx, rz int32) string {
	return filepath.Join(dir, "entities", fmt.Sprintf("r.%d.%d.mca", rx, rz))
}

// LoadChunkEntities reads a chunk's stored entities; the second result is
// whether the chunk has an entry at all, even an empty one. (nil, false, nil)
// is a chunk with nothing on file, as anywhere nobody has been.
func (w *World) LoadChunkEntities(pos ChunkPos) ([]nbt.Tag, bool, error) {
	if entities, ok := w.pendingChunkEntities(pos); ok {
		return entities, len(entities) > 0, nil
	}
	rx, rz := regionCoords(pos)
	path := entitiesRegionFilePath(w.storage, rx, rz)
	if _, err := os.Stat(path); err != nil {
		return nil, false, nil // no entity region file, so no entities either
	}
	rf, err := w.entityRegion(rx, rz)
	if err != nil {
		return nil, false, err
	}
	defer w.releaseEntityRegion(rf)
	index := regionIndex(pos.X, pos.Z)
	data, err := rf.readChunk(pos)
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return nil, false, nil // no entry for this chunk in its entity region
	}
	root, err := decodeRoot(data)
	if err != nil {
		rf.markUnreadable(index)
		return nil, false, fmt.Errorf("entities of chunk (%d,%d): %w", pos.X, pos.Z, err)
	}
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	if version != 0 && version < entityDataVersionFloor {
		rf.markUnreadable(index) // so the next save keeps the refused record
		return nil, false, fmt.Errorf("entities of chunk (%d,%d): data version %d is below %d",
			pos.X, pos.Z, version, entityDataVersionFloor)
	}
	entities, ok := nbt.Get[nbt.List](root, "Entities")
	if !ok {
		return nil, true, nil // an entry without an Entities list holds nothing
	}
	upgradeEntities(entities, int32(version))
	return entities.Items, true, nil
}

// upgradeEntities brings the entities of a record written at version to the
// shapes this server reads.
func upgradeEntities(entities nbt.List, version int32) {
	if version == 0 {
		return
	}
	for _, e := range entities.Items {
		c, ok := e.(nbt.CompoundTag)
		if !ok {
			continue
		}
		if version < attributeIdPrefixVersion {
			upgradeLegacyEntity(c, version)
		}
		if version < equipmentFormatVersion {
			upgradeEquipment(c, version)
		}
		if version < tooltipDisplayVersion {
			fixStoredComponents(c, version)
			fixEntityTexts(c, version)
		}
		if version < villagerFinalizedVersion {
			upgradeEntityShapes(c, version)
		}
	}
}

// UpgradePlayerData brings a player file written at an older data version to
// the shapes this server reads: item stacks of Inventory, EnderItems and the
// equipment, and the player's attributes. It raises the file's DataVersion to
// the last of those fixes (tooltipDisplayVersion), so a later load does not run
// them over the upgraded data; the respawn and equipment steps keyed on the
// claim stay with the caller.
// vanilla: DataFixTypes.PLAYER.updateToCurrentVersion
func UpgradePlayerData(root nbt.Compound) {
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	if version == 0 || version >= tooltipDisplayVersion {
		return
	}
	if version < legacyDimensionIDVersion {
		fixLegacyDimensionID(root)
	}
	if version < attributeIdPrefixVersion {
		upgradeLegacyEntity(root, int32(version))
	}
	fixStoredComponents(root, int32(version))
	fixEntityTexts(root, int32(version))
	root["DataVersion"] = nbt.Int(tooltipDisplayVersion)
}

// legacyDimensionIDVersion is the DataVersion that turned a player's numeric
// Dimension into a name. vanilla: LegacyDimensionIdFix
const legacyDimensionIDVersion = 2537

// fixLegacyDimensionID replaces a numeric Dimension with its name: -1 the
// Nether, 1 the End, any other number the overworld; a string stays.
// vanilla: LegacyDimensionIdFix.fixPlayer
func fixLegacyDimensionID(root nbt.Compound) {
	v, ok := nbt.NumberValue(root["Dimension"])
	if !ok {
		return
	}
	switch int32(int64(v)) {
	case -1:
		root["Dimension"] = nbt.String("minecraft:the_nether")
	case 1:
		root["Dimension"] = nbt.String("minecraft:the_end")
	default:
		root["Dimension"] = nbt.String("minecraft:overworld")
	}
}

// fixEntityTexts strips attribute id prefixes and unflattens CustomName on an
// entity and its passengers.
// vanilla: AttributeIdPrefixFix, UnflattenTextComponentFix
func fixEntityTexts(e nbt.CompoundTag, version int32) {
	entries := e.Entries()
	if version < attributeIdPrefixVersion {
		attributes, _ := nbt.Get[nbt.List](entries, "attributes")
		for _, a := range attributes.Items {
			if m, ok := nbt.CompoundView(a); ok {
				if id, ok := m["id"].(nbt.String); ok {
					m["id"] = nbt.String(stripAttributePrefix(string(id)))
				}
			}
		}
	}
	if version < textComponentVersion {
		unflattenText(entries, "CustomName")
	}
	if passengers, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				fixEntityTexts(c, version)
			}
		}
	}
}

// The DataVersions that reshaped a mob's gear, each the version a file must be
// below to need its step. vanilla: DataFixers.addFixers
const (
	dropChancesFormatVersion  = 4293 // DropChancesFormatFix
	saddleEquipmentVersion    = 4300 // SaddleEquipmentSlotFix
	equipmentFormatVersion    = 4301 // EquipmentFormatFix
	defaultEquipmentDropOdds  = float32(0.085)
	saddleEquipmentDropChance = float32(2) // a saddle always drops
)

// upgradeEquipment brings an entity written at version, and its passengers,
// to the equipment and drop_chances compounds this server reads, so an older
// world's mobs keep their gear.
func upgradeEquipment(e nbt.CompoundTag, version int32) {
	if version < dropChancesFormatVersion {
		upgradeDropChances(e)
	}
	if version < saddleEquipmentVersion {
		upgradeSaddle(e)
	}
	moveEquipment(e)
	if passengers, ok := nbt.Get[nbt.List](e.Entries(), "Passengers"); ok {
		for _, p := range passengers.Items {
			if c, ok := p.(nbt.CompoundTag); ok {
				upgradeEquipment(c, version)
			}
		}
	}
}

// upgradeDropChances folds ArmorDropChances, HandDropChances and
// body_armor_drop_chance into drop_chances, keeping only odds other than the
// default. vanilla: DropChancesFormatFix
func upgradeDropChances(e nbt.CompoundTag) {
	chances := nbt.NewOrderedCompound(8)
	for _, field := range equipmentLists {
		if list, ok := nbt.Get[nbt.List](e.Entries(), field.chances); ok {
			for i, slot := range field.slots {
				if i >= len(list.Items) {
					break
				}
				chance, ok := nbt.FloatField(nbt.Compound{"v": list.Items[i]}, "v")
				if !ok {
					chance = defaultEquipmentDropOdds
				}
				if chance != defaultEquipmentDropOdds {
					chances.Set(slot, nbt.Float(chance))
				}
			}
		}
	}
	if body, ok := nbt.FloatField(e.Entries(), "body_armor_drop_chance"); ok && body != defaultEquipmentDropOdds {
		chances.Set("body", nbt.Float(body))
	}
	for _, field := range equipmentLists {
		e.Delete(field.chances)
	}
	e.Delete("body_armor_drop_chance")
	if len(chances.Entries()) > 0 {
		e.Set("drop_chances", chances)
	}
}

// upgradeSaddle moves a ridden animal's SaddleItem, or a pig's or strider's
// Saddle flag, into the saddle slot, which always drops.
// vanilla: SaddleEquipmentSlotFix
func upgradeSaddle(e nbt.CompoundTag) {
	id, _ := nbt.Get[nbt.String](e.Entries(), "id")
	name := string(id)
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name // vanilla: NamespacedSchema.ensureNamespaced
	}
	switch name {
	case "minecraft:horse", "minecraft:skeleton_horse", "minecraft:zombie_horse", "minecraft:donkey",
		"minecraft:mule", "minecraft:camel", "minecraft:llama", "minecraft:trader_llama":
		item, ok := e.Entries()["SaddleItem"]
		if !ok {
			return
		}
		e.Delete("SaddleItem")
		e.Set("saddle", item)
	case "minecraft:pig", "minecraft:strider":
		saddled := nbt.BoolField(e.Entries(), "Saddle")
		e.Delete("Saddle")
		if !saddled {
			return
		}
		e.Set("saddle", nbt.Compound{"id": nbt.String("minecraft:saddle"), "count": nbt.Int(1)})
	default:
		return
	}
	chances, ok := e.Entries()["drop_chances"].(nbt.CompoundTag)
	if !ok {
		chances = nbt.NewOrderedCompound(2)
		e.Set("drop_chances", chances)
	}
	chances.Set("saddle", nbt.Float(saddleEquipmentDropChance))
}

// equipmentLists are the old per-slot item and drop chance lists, with the
// slots their entries stand for in order.
var equipmentLists = [...]struct {
	items, chances string
	slots          []string
}{
	{"ArmorItems", "ArmorDropChances", []string{"feet", "legs", "chest", "head"}},
	{"HandItems", "HandDropChances", []string{"mainhand", "offhand"}},
}

// moveEquipment moves ArmorItems, HandItems, body_armor_item and saddle into
// one equipment compound, leaving out the id-less placeholders that stood for
// empty slots. vanilla: EquipmentFormatFix
func moveEquipment(e nbt.CompoundTag) {
	equipment := nbt.NewOrderedCompound(8)
	put := func(slot string, item nbt.Tag) {
		if c, ok := item.(nbt.CompoundTag); ok {
			if _, ok := c.Entries()["id"]; ok {
				equipment.Set(slot, c)
			}
		}
	}
	for _, field := range equipmentLists {
		if list, ok := nbt.Get[nbt.List](e.Entries(), field.items); ok {
			for i, slot := range field.slots {
				if i < len(list.Items) {
					put(slot, list.Items[i])
				}
			}
		}
		e.Delete(field.items)
	}
	put("body", e.Entries()["body_armor_item"])
	put("saddle", e.Entries()["saddle"])
	e.Delete("body_armor_item")
	e.Delete("saddle")
	if len(equipment.Entries()) > 0 {
		e.Set("equipment", equipment)
	}
}

// SaveChunkEntities replaces the chunk's whole entity list, one-to-one with the
// caller's in-memory list, so a mob that died or left is not kept alive on
// disk. An empty list removes the entry instead of writing an empty one, so a
// missing entry means no entities.
// vanilla: IOWorker.STORE_EMPTY
func (w *World) SaveChunkEntities(pos ChunkPos, entities []nbt.Tag) error {
	rx, rz := regionCoords(pos)
	rf, err := w.entityRegion(rx, rz)
	if err != nil {
		return err
	}
	defer w.releaseEntityRegion(rf)
	if len(entities) == 0 {
		return skipUnreadable(rf.removeChunk(pos), "chunk's entities", pos)
	}
	root := nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"Position":    nbt.IntArray{int32(pos.X), int32(pos.Z)},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: entities},
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		return fmt.Errorf("serialize entities of chunk (%d,%d): %w", pos.X, pos.Z, err)
	}
	err = rf.writeChunk(pos, data)
	return skipUnreadable(err, "chunk's entities", pos)
}

// entityRegion returns the open entity region file for (rx, rz), opening it on
// demand, in a cache apart from the chunk regions'; the Anvil reader and writer
// are shared.
func (w *World) entityRegion(rx, rz int32) (*regionFile, error) {
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	return w.entityRegions.get(rx, rz)
}

// releaseEntityRegion gives back a handle from entityRegion.
func (w *World) releaseEntityRegion(rf *regionFile) {
	w.regionMu.Lock()
	defer w.regionMu.Unlock()
	w.entityRegions.release(rf)
}

// moveChunkEntities hands the entities a chunk in the layout before 1.18 holds
// to the entity region, upgraded, unless the region already has an entry for
// the chunk: from then on they are read as any chunk's. moved is true when the
// chunk record still holds an entities list, which the caller must rewrite
// without it so the entity region stays the only record.
// vanilla: ChunkRenamesFix, SerializableChunkData.parse
func (w *World) moveChunkEntities(pos ChunkPos, root nbt.Compound) (moved bool, err error) {
	entities, ok := nbt.Get[nbt.List](root, "entities")
	if !ok || len(entities.Items) == 0 {
		return false, nil
	}
	if _, has, err := w.LoadChunkEntities(pos); err != nil || has {
		return err == nil, err
	}
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	upgradeEntities(entities, int32(version))
	return true, w.SaveChunkEntities(pos, entities.Items)
}
