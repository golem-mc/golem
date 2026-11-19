package plugin

import (
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

// UUID identifies a player or an entity.
type UUID = uuid.UUID

// Key is a namespaced identifier such as minecraft:diamond_sword or
// warps:home. A plugin's own keys use its name as the namespace.
type Key struct{ Namespace, Path string }

// MinecraftKey answers the key path in the minecraft namespace.
func MinecraftKey(path string) Key { return Key{"minecraft", path} }

// ParseKey reads "namespace:path", or "path" in the minecraft namespace.
func ParseKey(s string) (Key, error) {
	ns, path, ok := strings.Cut(s, ":")
	if !ok {
		ns, path = "minecraft", s
	}
	if !validKeyPart(ns, false) || !validKeyPart(path, true) {
		return Key{}, fmt.Errorf("plugin: %q is not a valid key", s)
	}
	return Key{ns, path}, nil
}

// validKeyPart reports whether s uses only the characters vanilla's
// ResourceLocation allows; a path may also hold '/'.
func validKeyPart(s string, path bool) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		case c == '/' && path:
		default:
			return false
		}
	}
	return true
}

func (k Key) String() string { return k.Namespace + ":" + k.Path }

// Vec3 is a position or a velocity in blocks (per tick for a velocity).
type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) Add(o Vec3) Vec3           { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3           { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Scale(f float64) Vec3      { return Vec3{v.X * f, v.Y * f, v.Z * f} }
func (v Vec3) Len() float64              { return math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z) }
func (v Vec3) DistanceSq(o Vec3) float64 { d := v.Sub(o); return d.X*d.X + d.Y*d.Y + d.Z*d.Z }

// Block answers the block position holding v.
func (v Vec3) Block() BlockPos {
	return BlockPos{int(math.Floor(v.X)), int(math.Floor(v.Y)), int(math.Floor(v.Z))}
}

// BlockPos is a block's position.
type BlockPos struct{ X, Y, Z int }

// Offset answers the neighbouring position on face f, n blocks away.
func (p BlockPos) Offset(f BlockFace, n int) BlockPos {
	d := f.Direction()
	return BlockPos{p.X + d.X*n, p.Y + d.Y*n, p.Z + d.Z*n}
}

// Center answers the middle of the block.
func (p BlockPos) Center() Vec3 { return Vec3{float64(p.X) + .5, float64(p.Y) + .5, float64(p.Z) + .5} }

// Chunk answers the chunk holding p.
func (p BlockPos) Chunk() ChunkPos { return ChunkPos{p.X >> 4, p.Z >> 4} }

// ChunkPos is a chunk's position, in chunks.
type ChunkPos struct{ X, Z int }

// Location is a position in a world with a facing. Yaw and Pitch are in
// degrees, vanilla's convention: yaw 0 faces south, pitch -90 straight up.
type Location struct {
	World      World
	Pos        Vec3
	Yaw, Pitch float32
}

// Direction answers the unit vector the location faces.
func (l Location) Direction() Vec3 {
	yaw, pitch := float64(l.Yaw)*math.Pi/180, float64(l.Pitch)*math.Pi/180
	return Vec3{-math.Sin(yaw) * math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw) * math.Cos(pitch)}
}

// BlockFace is one of a block's six faces.
type BlockFace uint8

// The faces, in vanilla's Direction order.
const (
	Down BlockFace = iota
	Up
	North
	South
	West
	East
)

// Direction answers the unit offset of the face.
func (f BlockFace) Direction() BlockPos {
	return [...]BlockPos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}[f]
}

// Opposite answers the face on the other side.
func (f BlockFace) Opposite() BlockFace { return f ^ 1 }

// Box is an axis-aligned box from Min to Max.
type Box struct{ Min, Max Vec3 }

// Contains reports whether p is inside the box.
func (b Box) Contains(p Vec3) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y && p.Z >= b.Min.Z && p.Z <= b.Max.Z
}

// Hand is the hand an action uses.
type Hand uint8

const (
	MainHand Hand = iota
	OffHand
)

// EquipmentSlot is a slot an entity wears or holds an item in.
type EquipmentSlot uint8

// The slots, in vanilla's EquipmentSlot order.
const (
	SlotMainHand EquipmentSlot = iota
	SlotOffHand
	SlotFeet
	SlotLegs
	SlotChest
	SlotHead
	SlotBody   // a horse's armour, a wolf's armour, a llama's carpet
	SlotSaddle // the saddle of anything that takes one
)
