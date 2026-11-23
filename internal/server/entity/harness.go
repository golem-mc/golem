package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// HarnessDropHeight is the sheared harness's drop height above the feet, the
// average of the happy ghast's four passenger points.
// vanilla: Mob.shearItem, EntityTypes.HAPPY_GHAST passengerAttachments
const HarnessDropHeight = 4.0

// IsHarness reports whether the stack is one of the sixteen harnesses.
func IsHarness(stack protocol.ItemStack) bool {
	return !stack.Empty() && registry.Item(stack.ItemID).InTag(registry.ItemTagIDHarnesses)
}

// WearsHarness reports whether the mob's body slot holds a harness.
func (m *Mob) WearsHarness() bool { return IsHarness(m.Body) }

// canTakeHarness is whether a harness may go in the bare body slot of a
// living grown happy ghast.
// vanilla: HappyGhast.canUseSlot, Equippable.canBeEquippedBy (can_equip_harness)
func (m *Mob) canTakeHarness() bool {
	return m.Type == TypeHappyGhast && m.Alive() && !m.IsBaby() && m.Body.Empty()
}

// EquipHarness fits one of the held harness to a grown happy ghast whose
// body slot is bare, and reports whether it did.
// vanilla: Equippable.equipOnTarget
func (m *Mob) EquipHarness(stack protocol.ItemStack) bool {
	if !IsHarness(stack) || !m.canTakeHarness() {
		return false
	}
	stack.Count = 1
	m.SetEquipment(6, stack)
	m.BodyDropChance = guaranteedDropChance
	return true
}

// The happy ghast's still timeout: the ticks it holds still and takes no
// steering after a passenger leaves, counted down only once the body has
// lived past the grace.
// vanilla: HappyGhast.MAX_STILL_TIMEOUT, STILL_TIMEOUT_ON_LOAD_GRACE_PERIOD
const (
	happyGhastStillTicks = 10
	happyGhastStillGrace = 60
)

// StaysStill reports the happy ghast's still timeout, the STAYS_STILL flag.
// vanilla: HappyGhast.isOnStillTimeout
func (m *Mob) StaysStill() bool { return m.stillTimeout > 0 }

// PlayerBoarded is HappyGhast.addPassenger for a player just seated:
// the ghast stops holding still. It names the goggles-down sound when the
// body was empty before, SoundNone otherwise or for another kind.
// vanilla: HappyGhast.addPassenger
func (m *Mob) PlayerBoarded() registry.Sound {
	if m.Type != TypeHappyGhast {
		return registry.SoundNone
	}
	// A player on top holds it still again on the next tick.
	// vanilla: HappyGhast.addPassenger
	m.SetMetadataStillTimeout(0)
	if len(m.PassengerIDs()) == 1 {
		return registry.SoundEntityHappyGhastHarnessGogglesDown
	}
	return registry.SoundNone
}

// PlayerLeft is HappyGhast.removePassenger for a player just unseated: the
// ghast holds still for a while. It names the goggles-up sound when nobody
// rides any more, SoundNone otherwise or for another kind.
// vanilla: HappyGhast.removePassenger
func (m *Mob) PlayerLeft() registry.Sound {
	if m.Type != TypeHappyGhast {
		return registry.SoundNone
	}
	m.SetMetadataStillTimeout(happyGhastStillTicks)
	if len(m.PassengerIDs()) == 0 {
		return registry.SoundEntityHappyGhastHarnessGogglesUp
	}
	return registry.SoundNone
}

// happyGhastStillStep counts the still timeout down, then holds the ghast
// still while a player stands on top of it.
// vanilla: HappyGhast.tick
func (m *Mob) happyGhastStillStep(players []Player) {
	if m.stillTimeout > 0 && m.Age > happyGhastStillGrace {
		m.SetMetadataStillTimeout(m.stillTimeout - 1)
	}
	if m.playerAboveGhast(players) {
		m.SetMetadataStillTimeout(happyGhastStillTicks)
	}
}

// playerAboveGhast reports whether a player's feet are in the detection box
// above the ghast's back, min faces inclusive and max faces exclusive. A
// player riding another vehicle is tested by their own position; vanilla
// tests the vehicle's.
// vanilla: HappyGhast.scanPlayerAboveGhast
func (m *Mob) playerAboveGhast(players []Player) bool {
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	boxMinX, boxMinY, boxMinZ := minX-1, maxY-1e-5, minZ-1
	boxMaxX, boxMaxY, boxMaxZ := maxX+1, maxY+(maxY-minY)/2, maxZ+1
	for _, p := range players {
		if p.Spectator || p.Dead || p.OnHappyGhast {
			continue
		}
		if p.X >= boxMinX && p.X < boxMaxX && p.Y >= boxMinY && p.Y < boxMaxY && p.Z >= boxMinZ && p.Z < boxMaxZ {
			return true
		}
	}
	return false
}

// happyGhastHealStep heals one health every 600 ticks, every 20 in clouds,
// rain or snow.
// vanilla: HappyGhast.continuousHeal
func (m *Mob) happyGhastHealStep(w World) {
	// Both periods divide by 20, so the world reads wait for a multiple of it.
	if m.Age%20 != 0 || !m.Alive() || m.Health >= m.healthCap() {
		return
	}
	c := m.cell()
	period := 600
	if w.InClouds(m.X, m.Y, m.Z, m.BbHeight()) || w.PrecipitatesAt(c.X, c.Y, c.Z) {
		period = 20
	}
	if m.Age%period == 0 {
		m.heal(1)
	}
}

// happyGhastStep is the happy ghast's own per-tick work.
func (m *Mob) happyGhastStep(w World, players []Player) {
	m.happyGhastStillStep(players)
	m.happyGhastHealStep(w)
}
