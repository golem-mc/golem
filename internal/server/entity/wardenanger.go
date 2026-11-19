package entity

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// MobIdentities resolves the identities owned by the simulation's caller.
type MobIdentities interface{ MobUUID(*Mob) uuid.UUID }

type wardenSuspect struct {
	uuid   uuid.UUID
	id     int32
	anger  int
	player bool
	mob    *Mob
}

// WardenClientAnger is Warden.CLIENT_ANGER_LEVEL, synchronized once a second.
func (m *Mob) WardenClientAnger() int { return m.warden.clientAnger }

// wardenSuspect resolves id and validates it; a mob suspect carries its UUID.
func (m *Mob) wardenSuspect(t *tickView, id int32) (wardenSuspect, Player, bool) {
	for _, p := range t.players {
		if p.EntityID == id {
			s, ok := m.wardenCheck(t, p, p.width(), nil)
			return s, p, ok
		}
	}
	o := t.mobByID(id)
	if o == nil {
		return wardenSuspect{}, Player{}, false
	}
	s, p, ok := m.wardenCheckMob(t, o)
	if ok && m.WardenIdentities != nil {
		s.uuid, p.UUID = m.WardenIdentities.MobUUID(o), m.WardenIdentities.MobUUID(o)
	}
	return s, p, ok
}

// wardenCheckMob validates a resolved mob; the UUID is left unset.
func (m *Mob) wardenCheckMob(t *tickView, o *Mob) (wardenSuspect, Player, bool) {
	if o == m || !o.Alive() || o.Type == TypeWarden {
		return wardenSuspect{}, Player{}, false
	}
	p := o.asTarget()
	s, ok := m.wardenCheck(t, p, o.width(), o)
	return s, p, ok
}

// wardenCheck is vanilla's Warden.canTargetEntity for a resolved body.
func (m *Mob) wardenCheck(t *tickView, p Player, width float64, o *Mob) (wardenSuspect, bool) {
	if !p.Huntable || p.Dead || p.Creative || p.Spectator || m.Team != "" && m.Team == p.Team {
		return wardenSuspect{}, false
	}
	hw := width / 2
	if !t.w.InBorderBox(p.X-hw, p.Z-hw, p.X+hw, p.Z+hw) {
		return wardenSuspect{}, false
	}
	return wardenSuspect{id: p.EntityID, uuid: p.UUID, player: o == nil, mob: o}, true
}

// vanilla: AngerManagement.Sorter, sortAndUpdateHighestAnger
func (m *Mob) wardenSortAnger() {
	slices.SortStableFunc(m.warden.anger, func(a, b wardenSuspect) int {
		if (a.anger >= 80) != (b.anger >= 80) {
			if a.anger >= 80 {
				return -1
			}
			return 1
		}
		if a.player != b.player {
			if a.player {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.anger, a.anger)
	})
}

func (m *Mob) wardenActiveAnger() int {
	highest := 0
	for _, s := range m.warden.anger {
		if s.id == 0 {
			continue
		}
		if m.TargetID != 0 {
			if s.id == m.TargetID {
				return s.anger
			}
			continue
		}
		highest = max(highest, s.anger)
	}
	return highest
}

func (m *Mob) wardenAngerSound() registry.Sound {
	switch anger := m.wardenActiveAnger(); {
	case anger >= 80:
		return registry.SoundEntityWardenAngry
	case anger >= 40:
		return registry.SoundEntityWardenAgitated
	default:
		return registry.SoundEntityWardenAmbient
	}
}

// vanilla: Warden.increaseAngerAt
func (m *Mob) wardenIncreaseAnger(t *tickView, id int32, amount int, sound bool) {
	s, p, ok := m.wardenSuspect(t, id)
	if m.NoAI || !ok {
		return
	}
	state := &m.warden
	if state.digCooldown > 0 {
		state.digCooldown = wardenDigCooldown
	}
	found := false
	for i := range state.anger {
		old := &state.anger[i]
		if old.id == id || old.id == 0 && old.uuid == p.UUID {
			s.anger = min(150, old.anger+amount)
			// Vanilla adds serialized anger after capping the new live increment.
			if old.id == 0 {
				s.anger = old.anger + min(150, amount)
			}
			*old, found = s, true
			break
		}
	}
	if !found {
		s.anger = min(150, amount)
		state.anger = append(state.anger, s)
	}
	m.wardenSortAnger()
	// An angry player displaces a mob target; an existing player target stays.
	if s.player && s.anger >= 80 {
		if target, _, valid := m.wardenSuspect(t, m.TargetID); !valid || !target.player {
			m.SetMetadataTargetID(0)
		}
	}
	if sound && state.pose != protocol.PoseRoaring {
		sound := registry.SoundEntityWardenListening
		if m.wardenActiveAnger() >= 40 {
			sound = registry.SoundEntityWardenListeningAngry
		}
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: sound, Volume: 10, Pitch: 1})
	}
}

type wardenHurt struct {
	id     int32
	direct bool
}

// noteWardenHurt queues every attempted entity-caused hurt, including a refused blow.
// Identity and targetability are resolved from the next tick's snapshots.
// vanilla: Warden.hurtServer
func (m *Mob) noteWardenHurt(id int32, direct bool) {
	if m.Type == TypeWarden && !m.NoAI && !m.wardenUnderground() && id != 0 {
		m.warden.hurt = append(m.warden.hurt, wardenHurt{id, direct})
	}
}

// vanilla: AngerManagement.tick, convertFromUuids; Warden.customServerAiStep
func (m *Mob) wardenAngerTick(t *tickView) {
	state := &m.warden
	if m.Age%20 != 0 {
		return
	}
	state.conversionDelay--
	convert := state.conversionDelay <= 0
	if convert {
		state.conversionDelay = 2
	}
	kept := state.anger[:0]
	for _, s := range state.anger {
		if s.id == 0 && convert {
			for _, p := range t.players {
				if p.UUID == s.uuid {
					s.id, s.player = p.EntityID, true
					break
				}
			}
			if s.id == 0 && m.WardenIdentities != nil {
				for _, o := range t.others {
					if m.WardenIdentities.MobUUID(o) == s.uuid {
						s.id, s.mob = o.ID, o
						break
					}
				}
			}
		}
		if s.id != 0 {
			resolved, _, valid := m.wardenSuspect(t, s.id)
			if !valid {
				present := t.mobByID(s.id) != nil
				for _, p := range t.players {
					present = present || p.EntityID == s.id
				}
				if present || s.mob != nil && !s.mob.Alive() {
					continue
				}
				// Unloaded bodies retain anger by UUID; killed bodies do not.
				s.id, s.mob = 0, nil
			} else {
				resolved.anger = s.anger
				s = resolved
			}
		}
		s.anger--
		if s.anger > 0 {
			kept = append(kept, s)
		}
	}
	state.anger = kept
	m.wardenSortAnger()
	m.SetMetadataWardenClientAnger(m.wardenActiveAnger())
}

// vanilla: AngerManagement.codec, SUSPECT_ANGER_PAIR
func (m *Mob) wardenAngerFields(out nbt.CompoundTag, load bool) {
	tag := out.Entries()
	s := &m.warden
	if load {
		s.anger = nil
		s.hurt = nil
		m.SetMetadataWardenClientAnger(0)
		s.conversionDelay = m.Rand.Intn(3)
		anger, _ := nbt.Get[nbt.Compound](tag, "anger")
		list, _ := nbt.Get[nbt.List](anger, "suspects")
		for _, item := range list.Items {
			c, ok := nbt.CompoundView(item)
			if !ok {
				s.anger = nil
				return
			}
			id, validUUID := nbt.UUIDField(c, "uuid")
			value, validInt := nbt.IntValue(c["anger"])
			if !validUUID || !validInt || value < 0 {
				s.anger = nil
				return
			}
			s.anger = append(s.anger, wardenSuspect{uuid: id, anger: int(value)})
		}
		return
	}
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, suspect := range s.anger {
		if suspect.uuid != uuid.Nil {
			entry := nbt.NewOrderedCompound(2)
			entry.Set("uuid", nbt.UUIDToIntArray(suspect.uuid))
			entry.Set("anger", nbt.Int(suspect.anger))
			list.Items = append(list.Items, entry)
		}
	}
	anger := nbt.NewOrderedCompound(1)
	if len(list.Items) > 0 {
		anger.Set("suspects", list)
	}
	out.Set("anger", anger)
}
