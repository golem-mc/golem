package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// breezeDeflect is a breeze turning a projectile back, its sound played
// from the breeze after the locks.
type breezeDeflect struct {
	w        *world.World
	id       int32
	category int32
	x, y, z  float64
}

// deflectedBy is the sound a projectile's deflection off the target queues:
// a breeze's, none for any other deflects_projectiles body.
// vanilla: Breeze.PROJECTILE_DEFLECTION, Entity.deflection
func deflectedBy(w *world.World, m *entity.Mob) (breezeDeflect, bool) {
	if m == nil || m.Type != entity.TypeBreeze {
		return breezeDeflect{}, false
	}
	return breezeDeflect{w: w, id: m.ID, category: mobSoundCategory(m.Type), x: m.X, y: m.Y, z: m.Z}, true
}

// appendDeflectBy queues the deflect sound of the herd's mob with the id.
func appendDeflectBy(out []breezeDeflect, w *world.World, herd []*entity.Mob, id int32) []breezeDeflect {
	for _, m := range herd {
		if m.ID == id {
			if d, ok := deflectedBy(w, m); ok {
				out = append(out, d)
			}
			break
		}
	}
	return out
}

// playBreezeDeflect plays the deflect sound from the breeze, volume and
// pitch 1.
func (s *Server) playBreezeDeflect(d breezeDeflect) {
	s.broadcastEntitySoundID(d.w, nil, d.id, registry.SoundEntityBreezeDeflect, d.category, d.x, d.y, d.z, 1, 1)
}

// projectilePowerPayload builds Projectile Power: the body's VarInt ID and
// its acceleration power as a double.
func projectilePowerPayload(entityID int32, power float64) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutF64(power)
	return b.Bytes()
}

// hurtingMotion is a hurting projectile's Set Entity Motion bundled with its
// Projectile Power, so the client's prediction takes both on one tick.
// vanilla: ServerEntity.sendChanges
func hurtingMotion(t *entity.Thrown) []outbound {
	return bundle([]outbound{
		{id: protocol.IDPlaySetEntityMotion, data: entityVelocityPayload(t.ID, t.VX, t.VY, t.VZ)},
		{id: protocol.IDPlayProjectilePower, data: projectilePowerPayload(t.ID, t.ProjectilePower())},
	})
}
