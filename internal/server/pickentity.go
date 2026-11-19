package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// handlePickItemFromEntity handles middle-click on an entity, the picked
// item going through the picked block's hotbar path. The include-data flag
// prints a player's or mannequin's profile for a creative game master.
// vanilla: ServerGamePacketListenerImpl.handlePickItemFromEntity
func (c *client) handlePickItemFromEntity(r *protocol.Reader) error {
	target := r.ReadVarInt()
	includeData := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("pick_item_from_entity", err)
	}
	if pick, ok := c.entityPickResult(target); ok {
		if err := c.pickBlock(pick); err != nil {
			return err
		}
	}
	if includeData && c.canUseGameMasterBlocks() {
		if name, profile, ok := c.pickedAvatar(target); ok {
			return command.PrintProfile(c, name, profile)
		}
	}
	return nil
}

// pickedAvatar is the name and profile of the player or mannequin with this
// ID in reach. vanilla: FetchProfileCommand.printForAvatar
func (c *client) pickedAvatar(target int32) (protocol.Text, nbt.Compound, bool) {
	s := c.srv
	inReach := c.pickReach()
	if other := s.clientByEntityID(target); other != nil && other.theWorld() == c.theWorld() {
		minX, minY, minZ, maxX, maxY, maxZ := other.Box()
		if !inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return protocol.Text{}, nil, false
		}
		return other.DisplayName(), command.ProfileTag(other.player.Profile), true
	}
	s.mannequinsMu.Lock()
	var found *mannequinEntity
	var minX, minY, minZ, maxX, maxY, maxZ float64
	for _, ent := range s.mannequins {
		if ent.mannequin.ID == target && ent.w == c.theWorld() {
			found = ent
			minX, minY, minZ, maxX, maxY, maxZ = ent.mannequin.BlastBox()
			break
		}
	}
	s.mannequinsMu.Unlock()
	if found == nil || !inReach(minX, minY, minZ, maxX, maxY, maxZ) {
		return protocol.Text{}, nil, false
	}
	snap := s.mannequinSnapshot(found, nil)
	if snap == nil {
		return protocol.Text{}, nil, false
	}
	return snap.DisplayName(), mannequinProfileTag(s, found), true
}

// pickReach says whether a box is within the player's entity interaction range.
func (c *client) pickReach() func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	pos := c.player.Position()
	ex, ey, ez := pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z
	rng := c.entityRange()
	return func(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
		return rules.WithinEntityInteractionRange(rng, ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ)
	}
}

// entityPickResult is Entity.getPickResult for the entity with this ID in
// the player's world; false for none in reach, no item, or an unsimulated
// kind. vanilla: Entity.getPickResult
func (c *client) entityPickResult(target int32) (protocol.ItemStack, bool) {
	s := c.srv
	w := c.theWorld()
	inReach := c.pickReach()
	var item string
	var held protocol.ItemStack
	var custom *protocol.Text
	if it := s.boatByEntityID(w, target); it != nil {
		s.boatsMu.Lock()
		minX, minY, minZ, maxX, maxY, maxZ := it.boat.Box()
		s.boatsMu.Unlock()
		if !inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return protocol.ItemStack{}, false
		}
		item = it.kind.Name()
	} else if it := s.minecartByEntityID(w, target); it != nil {
		s.minecartsMu.Lock()
		minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
		s.minecartsMu.Unlock()
		if !inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return protocol.ItemStack{}, false
		}
		item = it.kind.Name()
	} else if f := s.frameByEntityID(w, target); f != nil {
		s.framesMu.Lock()
		if !inReach(f.minX, f.minY, f.minZ, f.maxX, f.maxY, f.maxZ) {
			s.framesMu.Unlock()
			return protocol.ItemStack{}, false
		}
		// A filled frame picks as a copy of what it holds.
		// vanilla: ItemFrame.getPickResult
		held = f.item
		custom = customNameText(f.custom.tag())
		item = "minecraft:item_frame"
		if f.glow {
			item = "minecraft:glow_item_frame"
		}
		s.framesMu.Unlock()
	} else {
		item = c.pickNameOfOther(target, inReach)
	}
	if !held.Empty() {
		held.Count = 1
		return held, true
	}
	id, ok := registry.ItemID(item)
	if !ok {
		return protocol.ItemStack{}, false
	}
	return protocol.ItemStack{ItemID: id, Count: 1, CustomName: custom}, true
}

// pickNameOfOther is the item a painting, end crystal, leash knot, cushion,
// armor stand or mob picks as, "" for none in reach; a mob picks its spawn egg,
// or nothing without one.
// vanilla: Painting.getPickResult, EndCrystal.getPickResult,
// LeashFenceKnotEntity.getPickResult, Cushion.getPickResult, ArmorStand.getPickResult,
// Mob.getPickResult
func (c *client) pickNameOfOther(target int32, inReach func(minX, minY, minZ, maxX, maxY, maxZ float64) bool) string {
	s := c.srv
	w := c.theWorld()
	s.paintingsMu.Lock()
	if p := s.paintingByIDLocked(w, target); p != nil {
		ok := inReach(p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ)
		s.paintingsMu.Unlock()
		if ok {
			return "minecraft:painting"
		}
		return ""
	}
	s.paintingsMu.Unlock()
	s.endCrystalsMu.Lock()
	if ent := s.endCrystalByIDLocked(w, target); ent != nil {
		minX, minY, minZ, maxX, maxY, maxZ := ent.crystal.BlastBox()
		s.endCrystalsMu.Unlock()
		if inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return "minecraft:end_crystal"
		}
		return ""
	}
	s.endCrystalsMu.Unlock()
	if k, ok := s.knotTellByEntityID(w, target); ok {
		minX, minY, minZ, maxX, maxY, maxZ := k.box()
		if inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return "minecraft:lead"
		}
		return ""
	}
	if ent := s.fixtureByEntityID(w, target, entity.FixtureCushion); ent != nil {
		s.fixturesMu.Lock()
		minX, minY, minZ, maxX, maxY, maxZ := entity.FixtureBox(ent.kind, ent.tag, ent.x, ent.y, ent.z)
		color := entity.CushionColor(ent.tag)
		s.fixturesMu.Unlock()
		if name, ok := registry.ItemName(int32(registry.ItemWhiteCushion) + color); ok && inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return name
		}
		return ""
	}
	s.armorStandsMu.Lock()
	if st := s.armorStandByIDLocked(w, target); st != nil {
		minX, minY, minZ, maxX, maxY, maxZ := st.stand.BlastBox()
		s.armorStandsMu.Unlock()
		if inReach(minX, minY, minZ, maxX, maxY, maxZ) {
			return "minecraft:armor_stand"
		}
		return ""
	}
	s.armorStandsMu.Unlock()
	s.mobsMu.Lock()
	ent := s.mobByEntityIDLocked(w, target)
	var minX, minY, minZ, maxX, maxY, maxZ float64
	var kind string
	if ent != nil {
		minX, minY, minZ, maxX, maxY, maxZ = ent.mob.BlastBox()
		kind = ent.mob.Type.Name()
	}
	s.mobsMu.Unlock()
	if kind == "" || !inReach(minX, minY, minZ, maxX, maxY, maxZ) {
		return ""
	}
	egg := kind + "_spawn_egg"
	if item, known := registry.ItemByName(egg); !known {
		return ""
	} else if _, ok := registry.SpawnEggEntity(item); !ok {
		return ""
	}
	return egg
}
