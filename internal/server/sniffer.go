package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// snifferDug queues a dig's gift at the head block, then plays the drop.
// vanilla: Sniffer.dropSeed
func (t *mobDimTick) snifferDug(fx *mobEffects, ev entity.MobEvent) {
	if stacks := ev.Mob.RollSnifferDigging(t.s.lootLevelAt(t.w, ev.Mob.X, ev.Mob.Y, ev.Mob.Z)); len(stacks) > 0 {
		fx.deaths = append(fx.deaths, mobDrops{w: t.w, stacks: stacks, x: ev.X, y: ev.Y, z: ev.Z})
	}
	fx.voiceID(t.w, ev.Mob, registry.SoundEntitySnifferDropSeed, 1, 1)
}

// laySnifferEgg is a breeding's result: the egg item at the mother, after the
// parents' hearts and experience, with the plop.
// vanilla: Sniffer.spawnChildFromBreeding
func (s *Server) laySnifferEgg(birth mobBirth) {
	s.mobsMu.Lock()
	m := birth.mother
	pitch := (m.Rand.Float32()-m.Rand.Float32())*0.2 + 0.5
	id, silent := m.ID, m.Silent
	s.mobsMu.Unlock()
	if !silent {
		s.broadcastEntitySoundID(birth.w, nil, id, registry.SoundBlockSnifferEggPlop, mobSoundCategory(m.Type), birth.x, birth.y, birth.z, 1, pitch)
	}
	s.spawnItemAtFeet(birth.w, protocol.ItemStack{ItemID: int32(registry.ItemSnifferEgg), Count: 1}, birth.x, birth.y, birth.z)
}

// hatchSniffer stands the egg's baby at the block's centre.
// vanilla: SnifferEggBlock.tick
func (s *Server) hatchSniffer(dim *dimension, h world.SnifferHatch) {
	s.spawnMobArriving(dim, entity.TypeSniffer, float64(h.X)+0.5, float64(h.Y)+0.5, float64(h.Z)+0.5, 0, nil, true, "", false, func(ent *mobEntity) {
		ent.mob.Yaw, ent.mob.HeadYaw = h.Yaw, h.Yaw
	})
}

// StartSnifferSearch sends the sniffer walking to a block to dig there.
func (s *Server) StartSnifferSearch(id int32, x, y, z int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.StartSnifferSearch(x, y, z)
			}
		}
	}
}
