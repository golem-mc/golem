package server

import (
	"math"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// spawnOminousItemSpawner spawns an ominous item spawner holding stack. It
// counts one tick short, since an entity added during the block entity pass
// ticks from the next tick. vanilla: OminousItemSpawner.create
func (s *Server) spawnOminousItemSpawner(w *world.World, stack protocol.ItemStack, x, y, z float64, afterTicks int) {
	tag := nbt.Compound{"spawn_item_after_ticks": nbt.Long(afterTicks)}
	if !stack.Empty() {
		tag["item"] = registry.StackTag(stack)
	}
	ent := fixtureOfTag(s, w, summonTag(tag, entity.FixtureOminousItemSpawner, x, y, z))
	if ent == nil {
		return
	}
	ent.age = -1
	s.registerFixture(ent)
}

// tickOminousItemSpawners ticks every spawner: the warning sound, then the
// drop. vanilla: OminousItemSpawner.tickServer
func (s *Server) tickOminousItemSpawners() {
	s.lockTick(&s.fixturesMu)
	var due []*fixtureEntity
	for _, ent := range s.fixtures {
		if ent.dead || ent.kind != entity.FixtureOminousItemSpawner {
			continue
		}
		due = append(due, ent)
	}
	s.unlockTick(&s.fixturesMu)
	for _, ent := range due {
		ent.age++
		after, _ := nbt.LongValue(ent.tag["spawn_item_after_ticks"])
		if int64(ent.age) == after-entity.OminousItemWarning {
			s.broadcastSoundID(ent.w, registry.SoundBlockTrialSpawnerAboutToSpawnItem, protocol.SoundCategoryNeutral,
				math.Floor(ent.x)+0.5, math.Floor(ent.y)+0.5, math.Floor(ent.z)+0.5, 1, 1)
		}
		if int64(ent.age) >= after {
			s.dropOminousItem(ent)
			s.killFixture(ent)
		}
	}
}

// dropOminousItem releases the item: a projectile item fires straight down,
// anything else drops with no pickup delay.
// vanilla: OminousItemSpawner.spawnItem, spawnProjectile
func (s *Server) dropOminousItem(ent *fixtureEntity) {
	item, _ := nbt.Get[nbt.Compound](ent.tag, "item")
	stack, ok := registry.StoredStack(item)
	if !ok || stack.Empty() {
		return
	}
	switch registry.Item(stack.ItemID) {
	case registry.ItemArrow, registry.ItemTippedArrow, registry.ItemSpectralArrow:
		s.shootArrow(ent.w, stack, ent.x, ent.y, ent.z, 0, -1, 0)
	case registry.ItemEgg, registry.ItemBrownEgg, registry.ItemBlueEgg, registry.ItemSnowball,
		registry.ItemSplashPotion, registry.ItemLingeringPotion, registry.ItemExperienceBottle:
		s.shootThrown(ent.w, stack, ent.x, ent.y, ent.z, 0, -1, 0)
	case registry.ItemFireCharge:
		s.shootCharge(ent, entity.ThrownSmallFireball, stack, levelEventFireChargeDispense)
	case registry.ItemWindCharge:
		s.shootCharge(ent, entity.ThrownWindCharge, stack, levelEventWindChargeDispense)
	default:
		vx, vy, vz := entity.PopVelocity(s.popRng)
		s.spawnItem(ent.w, stack, ent.x, ent.y, ent.z, vx, vy, vz, 0)
	}
	s.gameEvent(ent.w, rules.GameEventEntityPlace, ent.x, ent.y, ent.z, rules.GameEventContext{})
	s.broadcastLevelEvent(ent.w, entity.TrialEventSpawnItem, int32(math.Floor(ent.x)), int32(math.Floor(ent.y)), int32(math.Floor(ent.z)), 1)
}

// shootCharge plays the charge's dispense event, then fires it down.
// vanilla: OminousItemSpawner.spawnProjectile, FireChargeItem.createDispenseConfig, WindChargeItem.createDispenseConfig
func (s *Server) shootCharge(ent *fixtureEntity, kind entity.ThrownKind, stack protocol.ItemStack, dispenseEvent int32) {
	s.broadcastLevelEvent(ent.w, dispenseEvent, int32(math.Floor(ent.x)), int32(math.Floor(ent.y)), int32(math.Floor(ent.z)), 0)
	s.launchCharge(kind, ent.w, ent.x, ent.y, ent.z, 0, -1, 0, stack)
}

// launchCharge fires a charge along the aim at the power and uncertainty
// both charge items configure. vanilla: Projectile.shoot
func (s *Server) launchCharge(kind entity.ThrownKind, w *world.World, x, y, z, aimX, aimY, aimZ float64, stack protocol.ItemStack) {
	seed := s.levelRng.Int63()
	rolls := entity.NewRolls(seed)
	vx, vy, vz := entity.ShootVelocity(aimX, aimY, aimZ, ominousChargePower, ominousChargeInaccuracy, rolls.Rand)
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	s.standThrown(kind, w, x, y, z, vx, vy, vz, yaw, pitch, 0, uuid.Nil, seed, rolls, stack)
}

// The dispense events the charge items override.
const (
	levelEventFireChargeDispense = 1018
	levelEventWindChargeDispense = 1051
)

// The power and uncertainty both charge items configure.
const (
	ominousChargePower      = 1.0
	ominousChargeInaccuracy = 6.6666665
)
