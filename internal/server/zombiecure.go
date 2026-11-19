package server

import (
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

// vanilla: ZombieVillager.mobInteract, startConverting
func (c *client) cureZombieVillager(ent *mobEntity, hand int32) error {
	s := c.srv
	s.mobsMu.Lock()
	changes, started := ent.mob.CureZombie(c.player.UUID, ent.w.Difficulty())
	tell := mobEffectTellOf(ent, changes)
	s.mobsMu.Unlock()
	if !started {
		c.useSwing = swingNone
		return nil
	}
	c.useSwing = swingServer
	c.spendHeld(hand)
	s.tellSplashMob(ent.w, tell)
	s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent,
		entityEventPayload(ent.mob.ID, protocol.EntityStatusZombieVillagerCure))
	return nil
}

// zombieVillagerCured applies ZombieVillager.finishConversion after the mob lock.
func (s *Server) zombieVillagerCured(cv mobConversion, m *entity.Mob) {
	old := cv.old.mob
	ent := convertedEntity(cv.old, m, cv.w, false)
	drops := m.CuredFrom(old)
	// CONVERSION finalizes the villager type without the zombie attribute roll.
	m.FinalizeSpawn(m.SpawnDice(), cv.w.Biome(rules.BlockOf(old.X), rules.BlockOf(old.Y), rules.BlockOf(old.Z)), nil, rules.NewLocalDifficulty(cv.w.Difficulty(), s.effectiveDifficultyAt(cv.w, old.X, old.Z)))
	if c := s.clientByUUID(old.CurePlayer()); c != nil && c.theWorld() == cv.w {
		m.OnReputationEvent(entity.ReputationZombieVillagerCured, c.player.UUID)
		if progress := c.advancements.Load(); progress != nil {
			progress.CuredZombieVillager(time.Now())
		}
	}
	for _, stack := range drops {
		s.spawnItemAtFeet(cv.w, stack, old.X, old.Y, old.Z)
	}
	s.addRiddenConversion(cv.old, ent)
	if !old.Silent {
		s.broadcastLevelEvent(cv.w, protocol.WorldEventZombieVillagerCured, rules.BlockOf(old.X), rules.BlockOf(old.Y), rules.BlockOf(old.Z), 0)
	}
}

// villagerInfected spawns the zombie villager a zombie's kill made.
// vanilla: Zombie.killedEntity
func (s *Server) villagerInfected(cv mobConversion, m *entity.Mob) {
	old := cv.old.mob
	m.InfectedFrom(old, rules.SpecialMultiplier(s.effectiveDifficultyAt(cv.w, old.X, old.Z)))
	s.addRiddenConversion(cv.old, convertedEntity(cv.old, m, cv.w, false))
	if !cv.infectionSilent {
		s.broadcastLevelEvent(cv.w, protocol.WorldEventZombieVillagerInfected, cv.infectionX, cv.infectionY, cv.infectionZ, 0)
	}
}
