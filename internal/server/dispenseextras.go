package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// dispenseArmorStand deliberately skips the player's placement collision check.
// vanilla: DispenseItemBehavior.bootStrap (ARMOR_STAND), Direction.toYRot
func (s *Server) dispenseArmorStand(w *world.World, stack protocol.ItemStack, d world.Dispense) {
	x, y, z := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	ent := s.newArmorStandEntity(w, float64(x)+0.5, float64(y), float64(z)+0.5)
	ent.stand.Yaw = inventory.DispenseArmorStandYaw(int32(d.Dir))
	s.applyArmorStandItemData(ent, stack)
	ent.spawnReason = spawnReasonCode(plugin.SpawnDispenser)
	s.registerArmorStand(ent)
	s.bodySpawned(ent.uuid, plugin.SpawnDispenser)
	s.dispenseDefaultSound(w, d)
	s.dispenseAnimation(w, d)
}

// dispenseChest uses the mount's chest slot without the hand-use equip sound.
// vanilla: DispenseItemBehavior.bootStrap (CHEST), AbstractChestedHorse.getSlot(499)
func (s *Server) dispenseChest(w *world.World, d world.Dispense) bool {
	x, y, z := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	box := entity.NewAABB(float64(x), float64(y), float64(z), float64(x+1), float64(y+1), float64(z+1))
	equipped := false
	s.mobsMu.Lock()
	for _, m := range s.herdOf(w).herd {
		if box.Crosses(entity.NewAABB(m.BlastBox())) && m.EquipChest() {
			equipped = true
			break
		}
	}
	s.mobsMu.Unlock()
	if equipped {
		s.dispenseDefaultSound(w, d)
		s.dispenseAnimation(w, d)
	}
	return equipped
}

// dispenseWaterBottle emits its effects at the dispenser, rather than the mud.
// vanilla: DispenseItemBehavior.bootStrap (POTION)
func (s *Server) dispenseWaterBottle(w *world.World, visit dispenseVisit, d world.Dispense, dice *rules.JavaRandom) dispenseAct {
	x, y, z := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	contents, ok := visit.taken.PotionContentsOf()
	if !ok || !rules.PotionIsWater(contents) || !registry.BlockOf(w.BlockAt(x, y, z)).InTag(registry.TagIDConvertibleToMud) {
		s.dispenseDefaultDrop(w, visit, d, dice)
		// The nested default behaviour and the outer potion behaviour each play feedback.
		s.dispenseDefaultSound(w, d)
		s.dispenseAnimation(w, d)
		return dispenseAct{}
	}
	for range 5 {
		s.sendParticles(w, protocol.ParticleSplash, float64(d.X)+s.levelRng.Float64(), float64(d.Y)+1, float64(d.Z)+s.levelRng.Float64(), 0, 0, 0, 1, 1)
	}
	s.broadcastSoundID(w, registry.SoundItemBottleEmpty, protocol.SoundCategoryBlock, float64(d.X)+0.5, float64(d.Y)+0.5, float64(d.Z)+0.5, 1, 1)
	s.gameEvent(w, registry.GameEventFluidPlace, float64(d.X)+0.5, float64(d.Y)+0.5, float64(d.Z)+0.5, rules.GameEventContext{})
	s.setBlockAndTell(w, x, y, z, registry.BlockMud.DefaultState())
	return s.dispenseSwap(w, d, registry.ItemGlassBottle)
}

// vanilla: DispenseItemBehavior.bootStrap (BRUSH)
func (s *Server) dispenseBrush(w *world.World, visit dispenseVisit, d world.Dispense) dispenseAct {
	x, y, z := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	for _, ent := range s.mobsIn(w, float64(x), float64(y), float64(z), float64(x+1), float64(y+1), float64(z+1)) {
		if ent.mob.Type == entity.TypeArmadillo && s.brushArmadillo(ent, visit.taken, rules.LootEntity{}) {
			s.dispenseDefaultSound(w, d)
			s.dispenseAnimation(w, d)
			worn, _ := rules.Wear(visit.taken, rules.ProcessDurabilityChange(visit.taken, 16, s.levelRng))
			return dispenseAct{back: worn}
		}
	}
	s.broadcastLevelEvent(w, protocol.WorldEventDispenseFail, d.X, d.Y, d.Z, 0)
	s.dispenseAnimation(w, d)
	return dispenseAct{back: visit.taken}
}
