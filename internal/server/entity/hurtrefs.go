package entity

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/nbt"
)

// hurtRef is an attacker kept by identity beside the entity ID the live state
// uses. id is 0 while the UUID is read from a save and not yet matched to a
// loaded entity.
// vanilla: EntityReference
type hurtRef struct {
	id   int32
	uuid uuid.UUID
	ok   bool
}

// note remembers the attacker's identity when it is known.
func (r *hurtRef) note(id int32, who uuid.UUID) {
	if who != uuid.Nil {
		*r = hurtRef{id: id, uuid: who, ok: true}
	}
}

// saveHurtBy writes the player credit and the last attacker.
// vanilla: LivingEntity.addAdditionalSaveData
func (m *Mob) saveHurtBy(tag nbt.CompoundTag) {
	if m.playerMemoryTicks > 0 && m.credit.ok && (m.credit.id == m.playerCreditID || m.credit.id == 0) {
		tag.Set("last_hurt_by_player", nbt.UUIDToIntArray(m.credit.uuid))
		tag.Set("last_hurt_by_player_memory_time", nbt.Int(int32(m.playerMemoryTicks)))
	}
	if m.grudge.ok && m.Age-m.lastAttackerAge <= 100 && (m.grudge.id == m.lastAttackerID || m.grudge.id == 0) {
		tag.Set("last_hurt_by_mob", nbt.UUIDToIntArray(m.grudge.uuid))
		tag.Set("ticks_since_last_hurt_by_mob", nbt.Int(int32(m.Age-m.lastAttackerAge)))
	}
}

// loadHurtBy reads what saveHurtBy wrote; the entity IDs follow in
// learnHurtRefs once the attackers are loaded.
// vanilla: LivingEntity.readAdditionalSaveData
func (m *Mob) loadHurtBy(compound nbt.Compound) {
	if who, ok := nbt.UUIDField(compound, "last_hurt_by_player"); ok {
		ticks, _ := nbt.IntField(compound, "last_hurt_by_player_memory_time")
		m.credit = hurtRef{uuid: who, ok: true}
		m.playerCreditID, m.playerMemoryTicks = 0, int(ticks)
		m.hurtByPlayer = ticks > 0
	}
	if who, ok := nbt.UUIDField(compound, "last_hurt_by_mob"); ok {
		ticks, _ := nbt.IntField(compound, "ticks_since_last_hurt_by_mob")
		m.grudge = hurtRef{uuid: who, ok: true}
		m.lastAttackerID, m.retaliate = 0, 0
		m.lastAttackerAge = m.Age - int(ticks)
		m.retaliateStamp = m.lastAttackerAge
	}
}

// learnHurtRefs pairs the attackers' entity IDs with their identities from the
// bodies around: a saved one finds its ID, a live one gives its UUID.
func (m *Mob) learnHurtRefs(players []Player, others []*Mob) {
	if m.credit.ok && m.credit.id == 0 {
		for i := range players {
			if players[i].UUID == m.credit.uuid {
				m.credit.id, m.playerCreditID = players[i].EntityID, players[i].EntityID
				break
			}
		}
	} else if m.playerMemoryTicks > 0 && m.playerCreditID != 0 && m.credit.id != m.playerCreditID {
		for i := range players {
			if players[i].EntityID == m.playerCreditID {
				m.credit.note(m.playerCreditID, players[i].UUID)
				break
			}
		}
	}
	if m.grudge.ok && m.grudge.id == 0 {
		for i := range players {
			if players[i].UUID == m.grudge.uuid {
				m.grudge.id, m.lastAttackerID, m.retaliate = players[i].EntityID, players[i].EntityID, players[i].EntityID
				return
			}
		}
		for _, o := range others {
			if o.UUID == m.grudge.uuid {
				m.grudge.id, m.lastAttackerID, m.retaliate = o.ID, o.ID, o.ID
				return
			}
		}
	} else if id := m.lastAttackerID; id != 0 && m.grudge.id != id {
		for i := range players {
			if players[i].EntityID == id {
				m.grudge.note(id, players[i].UUID)
				return
			}
		}
	}
}
