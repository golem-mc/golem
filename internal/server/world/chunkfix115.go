package world

import (
	"encoding/binary"
	"math/bits"
	"strconv"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Data versions of the 1.15 to 1.16 fixes a Level chunk gets that a 1.16
// chunk is already past.
const (
	furnaceRecipesVersion = 2501 // vanilla: DataFixers.addFixers (FurnaceRecipeFix)
	wallPropertyVersion   = 2503 // vanilla: DataFixers.addFixers (WallPropertyFix)
	fungusRenameVersion   = 2508 // vanilla: DataFixers.addFixers (Renamed fungi to fungus)
	blockEntityUUIDFixed  = 2514 // vanilla: DataFixers.addFixers (BlockEntityUUIDFix)
	bitStorageAlignFixed  = 2527 // vanilla: DataFixers.addFixers (BitStorageAlignFix)
	soulFireRenameVersion = 2528 // vanilla: DataFixers.addFixers (Rename soul fire torch and lantern)
	redstoneWireVersion   = 2531 // vanilla: DataFixers.addFixers (RedstoneWireConnectionsFix)
)

// wallBlocks are the walls of 1.15, whose sides were "true" or "false".
// vanilla: WallPropertyFix.WALL_BLOCKS
var wallBlocks = map[string]bool{
	"minecraft:andesite_wall": true, "minecraft:brick_wall": true, "minecraft:cobblestone_wall": true,
	"minecraft:diorite_wall": true, "minecraft:end_stone_brick_wall": true, "minecraft:granite_wall": true,
	"minecraft:mossy_cobblestone_wall": true, "minecraft:mossy_stone_brick_wall": true,
	"minecraft:nether_brick_wall": true, "minecraft:prismarine_wall": true, "minecraft:red_nether_brick_wall": true,
	"minecraft:red_sandstone_wall": true, "minecraft:sandstone_wall": true, "minecraft:stone_brick_wall": true,
}

var sideProps = [...]string{"east", "west", "north", "south"}

// fixWallSides turns a wall's boolean sides into "low" and "none".
// vanilla: WallPropertyFix
func fixWallSides(props nbt.Compound, version int32) {
	if version >= wallPropertyVersion {
		return
	}
	for _, side := range sideProps {
		if v, ok := props[side].(nbt.String); ok {
			props[side] = nbt.String(map[bool]string{true: "low", false: "none"}[v == "true"])
		}
	}
}

// fixRedstoneSides gives a redstone wire with no neighbour on an axis the
// "side" connection 1.16 shows for it.
// vanilla: RedstoneWireConnectionsFix
func fixRedstoneSides(props nbt.Compound, version int32) {
	if version >= redstoneWireVersion {
		return
	}
	connected := func(side string) bool {
		v, _ := props[side].(nbt.String)
		return v != "" && v != "none"
	}
	eastWest := connected("east") || connected("west")
	northSouth := connected("north") || connected("south")
	for _, side := range sideProps {
		other := northSouth
		if side == "north" || side == "south" {
			other = eastWest
		}
		if !connected(side) && !other {
			props[side] = nbt.String("side")
		} else if _, ok := props[side]; !ok {
			props[side] = nbt.String("none")
		}
	}
}

// realignBlockStates rewrites block state data a version before 1.16 packed
// across long boundaries into the one-per-long layout.
// vanilla: BitStorageAlignFix.addPadding
func realignBlockStates(data nbt.LongArray, paletteSize int) nbt.LongArray {
	width := max(4, bits.Len(uint(paletteSize-1)))
	if len(data) == 0 || 64%width == 0 {
		return data
	}
	mask := uint64(1)<<width - 1
	indices := make([]uint16, blocksPerSection)
	for i := range indices {
		pos := i * width
		word, off := pos>>6, pos&63
		if word >= len(data) {
			break
		}
		v := uint64(data[word]) >> off
		if off+width > 64 && word+1 < len(data) {
			v |= uint64(data[word+1]) << (64 - off)
		}
		indices[i] = uint16(v & mask)
	}
	return packLongs(indices, width)
}

// fixFurnaceRecipes folds a furnace's RecipeLocation and RecipeAmount pairs
// into the RecipesUsed compound.
// vanilla: FurnaceRecipeFix.updateFurnaceContents
func fixFurnaceRecipes(tag nbt.Compound) {
	n, _ := nbt.IntValue(tag["RecipesUsedSize"])
	delete(tag, "RecipesUsedSize")
	used := nbt.Compound{}
	for i := range n {
		location, amount := "RecipeLocation"+strconv.Itoa(int(i)), "RecipeAmount"+strconv.Itoa(int(i))
		count, _ := nbt.IntValue(tag[amount])
		if name, ok := tag[location].(nbt.String); ok && count > 0 {
			used[string(name)] = nbt.Int(count)
		}
		delete(tag, location)
		delete(tag, amount)
	}
	tag["RecipesUsed"] = used
}

// uuidInts is a UUID as the four ints the 1.16 formats store.
func uuidInts(most, least uint64) nbt.IntArray {
	return nbt.IntArray{int32(most >> 32), int32(most), int32(least >> 32), int32(least)}
}

// fixBlockEntityUUIDs moves a conduit's target and a skull's owner to the
// int array UUID format; a skull's Owner becomes SkullOwner.
// vanilla: BlockEntityUUIDFix
func fixBlockEntityUUIDs(id string, tag nbt.Compound) {
	switch id {
	case "minecraft:conduit":
		target, ok := nbt.CompoundView(tag["Target"])
		if !ok {
			return
		}
		most, _ := nbt.Get[nbt.Long](target, "M")
		least, _ := nbt.Get[nbt.Long](target, "L")
		if most == 0 || least == 0 {
			return
		}
		delete(tag, "Target")
		tag["target_uuid"] = uuidInts(uint64(most), uint64(least))
	case "minecraft:skull":
		owner, ok := nbt.CompoundView(tag["Owner"])
		if !ok {
			return
		}
		if s, ok := owner["Id"].(nbt.String); ok {
			if u, err := uuid.Parse(string(s)); err == nil {
				delete(owner, "Id")
				owner["Id"] = uuidInts(binary.BigEndian.Uint64(u[:8]), binary.BigEndian.Uint64(u[8:]))
			}
		}
		delete(tag, "Owner")
		tag["SkullOwner"] = owner
	}
}
