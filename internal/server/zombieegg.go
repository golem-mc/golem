package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// eggStomped queues the effects of a zombie's stomp on the egg at ev's cell.
// vanilla: RemoveBlockGoal.tick, Zombie.ZombieAttackTurtleEggGoal
func (fx *mobEffects) eggStomped(w *world.World, ev entity.MobEvent) {
	x, y, z := int32(ev.X), int32(ev.Y), int32(ev.Z)
	near := func(data []byte) {
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlayLevelParticles, data: data,
			near: true, x: ev.X + 0.5, y: ev.Y, z: ev.Z + 0.5, radius: particleRange})
	}
	if ev.Kind == entity.MobStompedEgg {
		fx.stompedEggs = append(fx.stompedEggs, turtleEgg{w: w, x: x, y: y, z: z})
		const poof = protocol.ParticlePoof
		near(particleDataPayload(poof, nil, false, ev.X+0.5, ev.Y, ev.Z+0.5, 0.02, 0.02, 0.02, 0.15, 20))
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlaySoundEffect,
			data: soundEffectIDAt(registry.SoundEntityTurtleEggBreak, protocol.SoundCategoryBlock, ev.X+0.5, ev.Y+0.5, ev.Z+0.5, 0.7, ev.Pitch),
			near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: soundRange(0.7)})
		return
	}
	if ev.Amount > 0 {
		const id = protocol.ParticleItem
		var data protocol.Buffer
		data.PutSlot(protocol.ItemStack{ItemID: int32(registry.ItemEgg), Count: 1})
		r := ev.Mob.Rand
		near(particleDataPayload(id, data.Bytes(), false, ev.X+0.5, ev.Y+0.7, ev.Z+0.5,
			(r.Float32()-0.5)*0.08, (r.Float32()-0.5)*0.08, (r.Float32()-0.5)*0.08, 0.15, 3))
	}
	if ev.Pitch > 0 {
		fx.sends = append(fx.sends, mobSend{w: w, id: protocol.IDPlaySoundEffect,
			data: soundEffectIDAt(registry.SoundEntityZombieDestroyEgg, protocol.SoundCategoryHostile, ev.X+0.5, ev.Y+0.5, ev.Z+0.5, 0.5, ev.Pitch),
			near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: soundRange(0.5)})
	}
}

// removeStompedEgg removes the egg block without effects of its own.
// vanilla: Level.removeBlock
func (s *Server) removeStompedEgg(egg turtleEgg) {
	s.setBlockAndTell(egg.w, egg.x, egg.y, egg.z, registry.BlockAir.DefaultState())
}
