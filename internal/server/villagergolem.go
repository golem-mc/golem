package server

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Villager.spawnGolemIfNeeded
const golemAgreeBox = 10

// mobGolemRequest keeps the asker so earlier effects can cancel its request.
type mobGolemRequest struct {
	mobAct
	mob *entity.Mob
}

// spawnVillageGolem tries ten spots within eight blocks of the villager and
// puts an iron golem on the first that fits; the villagers around it then
// remember a golem.
// vanilla: SpawnUtil.trySpawnMob, IronGolem.checkSpawnObstruction
func (s *Server) spawnVillageGolem(at mobGolemRequest) {
	s.mobsMu.Lock()
	wants := at.mob.WantsGolem()
	s.mobsMu.Unlock()
	// An earlier request in this tick may already have spawned a golem.
	if !wants {
		return
	}
	w := at.w
	x, y, z, ok := entity.FindVillageGolemSpot(w, at.x, at.y, at.z, rand.Intn)
	if !ok {
		return
	}
	var voice registry.Sound
	var volume, pitch float32
	dim := s.dimensionOfWorld(w)
	s.spawnMobArriving(dim, entity.TypeIronGolem, float64(x)+0.5, float64(y), float64(z)+0.5, 0, nil, false, "", false,
		func(ent *mobEntity) { voice, volume, pitch = ent.mob.AmbientVoice() })
	s.broadcastVoiceID(w, entity.TypeIronGolem, false, float64(x)+0.5, float64(y), float64(z)+0.5, voice, volume, pitch)
	s.golemSeenAround(dim, at)
}

// golemSeenAround sets the golem memory of every villager near the asker.
// vanilla: Villager.spawnGolemIfNeeded
func (s *Server) golemSeenAround(dim *dimension, at mobGolemRequest) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range dim.mobs.herd {
		if m.Type == entity.TypeVillager && m.NearBox(at.mob, golemAgreeBox) {
			m.GolemDetected(m.GameTime)
		}
	}
}
