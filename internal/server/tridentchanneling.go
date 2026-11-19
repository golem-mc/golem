package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// tridentChannelHit snapshots a hit's position under the body locks. Hurts
// settled after the locks mark it landed before the channeling phase.
type tridentChannelHit struct {
	arrow                    *arrowEntity
	x, y, z                  float64
	victim                   int32
	block                    registry.Block
	blockHit, silent, landed bool
	category                 int32
}

func (fx *arrowEffects) landChanneling(it *arrowEntity) {
	if it.arrow.Channeling <= 0 {
		return
	}
	for i := range fx.channelHits {
		if fx.channelHits[i].arrow == it && !fx.channelHits[i].blockHit {
			fx.channelHits[i].landed = true
		}
	}
}

// applyTridentChanneling runs after all hurts, before the tridents deflect.
// vanilla: SummonEntityEffect.apply, PlaySoundEffect.apply
func (s *Server) applyTridentChanneling(hit tridentChannelHit) {
	if !hit.landed {
		return
	}
	w := hit.arrow.w
	ctx := &rules.EnchContext{
		Thundering: w.IsThundering(),
		CanSeeSky:  w.SkyLightAt(rules.BlockOf(hit.x), rules.BlockOf(hit.y), rules.BlockOf(hit.z)) == 15,
		Victim:     hit.victim, DirectAttacker: protocol.EntityTypeTrident, Block: hit.block,
	}
	if hit.blockHit {
		body := enchantTarget{uuid: hit.arrow.uuid, x: hit.x, y: hit.y, z: hit.z, width: .5, height: .5,
			entityID: hit.arrow.arrow.ID, causeID: hit.arrow.arrow.OwnerID}
		for _, e := range rules.HitBlockEffects(hit.arrow.arrow.Weapon, ctx) {
			s.applyPackEffects(w, e.Effect, e.Level, body)
		}
	}
	for _, e := range rules.TridentHitEffects(hit.arrow.arrow.Weapon, ctx, hit.blockHit) {
		if e.SummonEntity == "minecraft:lightning_bolt" {
			bolt := s.spawnBolt(w, hit.x, hit.y, hit.z, false, plugin.LightningTrident)
			if owner := s.entityClient(hit.arrow.arrow.OwnerID); bolt != nil && owner != nil {
				s.boltMu.Lock()
				bolt.cause = owner.player.UUID
				s.boltMu.Unlock()
			}
		}
		if sound := e.PlaySound; sound != nil && !hit.silent {
			s.playPending(pendingSound{w: w, sound: sound.Sound, category: hit.category, x: hit.x, y: hit.y, z: hit.z, volume: sound.Volume, pitch: sound.PitchWith(s.levelRng)})
		}
	}
}

// channelBlockHit keeps a boundary hit inside the hit block for location predicates.
// vanilla: BlockPos.clampLocationWithin
func channelBlockHit(it *arrowEntity, hit entity.BlockHit) tridentChannelHit {
	clamp := func(v float64, cell int32) float64 {
		return max(float64(float32(cell)+float32(1e-5)), min(v, float64(cell)+1-float64(float32(1e-5))))
	}
	return tridentChannelHit{arrow: it, x: clamp(hit.HitX, hit.X), y: clamp(hit.HitY, hit.Y), z: clamp(hit.HitZ, hit.Z),
		victim: protocol.EntityTypeTrident, block: registry.BlockOf(hit.State), blockHit: true, landed: true, category: protocol.SoundCategoryNeutral}
}
