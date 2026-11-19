package entity

import (
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ReputationEvent is a deed a villager hears about.
// vanilla: ReputationEventType
type ReputationEvent uint8

// The reputation events a villager reacts to.
const (
	ReputationTrade ReputationEvent = iota
	ReputationVillagerHurt
	ReputationVillagerKilled
	ReputationZombieVillagerCured
)

// Gossip timings and reach. vanilla: Villager.gossip, Villager.maybeDecayGossip,
// TradeWithVillager
const (
	gossipDayTicks      = 24000
	gossipCooldown      = 1200
	gossipChatTicks     = 60 // the Behavior default duration
	gossipTransferCount = 10
	gossipTalkDist2     = 5.0 // vanilla: TradeWithVillager.tick
)

// villagerGossip is a villager's opinions and the deeds it has yet to weigh.
type villagerGossip struct {
	gossips   Gossips
	lastDecay int64 // LastGossipDecay; 0 = not started
	// lastGossip is not saved. vanilla: Villager.lastGossipTime
	lastGossip int64
	// hurtBy is the entity ID of an attacker not yet reported, 0 for none.
	hurtBy int32
}

// PlayerReputation is the villager's reputation of the player.
// vanilla: Villager.getPlayerReputation
func (m *Mob) PlayerReputation(id uuid.UUID) int32 { return m.vg.gossips.Reputation(id) }

// OnReputationEvent is Villager.onReputationEventFrom.
func (m *Mob) OnReputationEvent(ev ReputationEvent, source uuid.UUID) {
	if m.Type != TypeVillager {
		return
	}
	if m.vg.gossips == nil {
		m.vg.gossips = Gossips{}
	}
	switch ev {
	case ReputationZombieVillagerCured:
		m.vg.gossips.add(source, GossipMajorPositive, 20)
		m.vg.gossips.add(source, GossipMinorPositive, 25)
	case ReputationTrade:
		m.vg.gossips.add(source, GossipTrading, 2)
	case ReputationVillagerHurt:
		m.vg.gossips.add(source, GossipMinorNegative, 25)
	case ReputationVillagerKilled:
		m.vg.gossips.add(source, GossipMajorNegative, 25)
	}
}

// setRetaliate records the attacker; a villager also queues it for its
// reputation. vanilla: Villager.setLastHurtByMob
func (m *Mob) setRetaliate(id int32) {
	m.retaliate = id
	m.retaliateStamp = m.Age
	if id != 0 {
		m.lastAttackerID, m.lastAttackerAge = id, m.Age
	}
	if id != 0 && m.Type == TypeVillager {
		m.vg.hurtBy = id
	}
	if id != 0 && m.Type == TypePiglin {
		m.barter.hurtBy = id
	}
}

// villagerGossipStep reports the pending attacker and decays the gossip once
// a day.
// vanilla: Villager.setLastHurtByMob, Villager.maybeDecayGossip
func (m *Mob) villagerGossipStep(t *tickView) {
	vg := &m.vg
	if m.Unhappy > 0 {
		m.SetMetadataUnhappy(m.Unhappy - 1)
	}
	if vg.hurtBy != 0 {
		for i := range t.players {
			if p := &t.players[i]; p.EntityID == vg.hurtBy {
				m.OnReputationEvent(ReputationVillagerHurt, p.UUID)
				t.events = append(t.events, MobEvent{Mob: m, Kind: MobStatus, Status: protocol.EntityStatusVillagerAngry})
				break
			}
		}
		vg.hurtBy = 0
	}
	now := m.GameTime
	if vg.lastDecay == 0 {
		vg.lastDecay = now
		return
	}
	if now < vg.lastDecay+gossipDayTicks {
		return
	}
	vg.gossips.decay()
	vg.lastDecay = now
}

// tellWitnesses gives each villager that sees this one die by a player's hand
// a major negative.
// vanilla: Villager.tellWitnessesThatIWasMurdered
func (m *Mob) tellWitnesses(w World, players []Player, others []*Mob) {
	var killer *Player
	for i := range players {
		if players[i].EntityID == m.blowAttacker {
			killer = &players[i]
			break
		}
	}
	if killer == nil {
		return
	}
	view := tickView{w: w}
	for _, o := range others {
		if o != m && o.Type == TypeVillager && o.Alive() && m.sees(&view, o) {
			o.OnReputationEvent(ReputationVillagerKilled, killer.UUID)
		}
	}
}

// gossipWith is Villager.gossip: both sides must be out of their cooldown. It
// reports whether gossip passed.
func (m *Mob) gossipWith(o *Mob) bool {
	now := m.GameTime
	if cooling(now, m.vg.lastGossip) || cooling(now, o.vg.lastGossip) {
		return false
	}
	if m.vg.gossips == nil {
		m.vg.gossips = Gossips{}
	}
	m.vg.gossips.transferFrom(o.vg.gossips, m.Rand.Intn, gossipTransferCount)
	m.vg.lastGossip, o.vg.lastGossip = now, now
	return true
}

func cooling(now, last int64) bool { return now >= last && now < last+gossipCooldown }

// villagerChatGoal walks to a villager in view and trades gossip and items
// with it.
// vanilla: InteractWith (idle), SocializeAtBell (meet), TradeWithVillager
type villagerChatGoal struct {
	m      *Mob
	target *Mob
	left   int
	act    villagerActivity
	recalc int
	pace   float64
	stop   float64
}

func (g *villagerChatGoal) Priority() int           { return priorityPoiStrol }
func (g *villagerChatGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *villagerChatGoal) RequiresEveryTick() bool { return true }

func (g *villagerChatGoal) CanStart(t *tickView) bool {
	m := g.m
	switch m.vb.activity {
	case actIdle:
		if m.Rand.Intn(4) != 0 {
			return false
		}
		g.target = m.nearestSeen(t, 64, func(o *Mob) bool { return o.Type == TypeVillager })
		g.pace, g.stop = 0.5, 2
	case actMeet:
		mp := m.vb.meetingPoint
		if m.Rand.Intn(100) != 0 || !m.memHere(mp) || !m.nearCenter(mp.at(), 4) {
			return false
		}
		g.target = m.nearestSeen(t, 32, func(o *Mob) bool { return o.Type == TypeVillager })
		g.pace, g.stop = 0.3, 1
	default:
		return false
	}
	g.act = m.vb.activity
	return g.target != nil
}

func (g *villagerChatGoal) Start(t *tickView) {
	g.left, g.recalc = gossipChatTicks, 0
	g.m.seekTo(t.w, g.target.X, g.target.Y, g.target.Z, g.pace, int32(g.stop))
}

func (g *villagerChatGoal) ShouldContinue(t *tickView) bool {
	tg := g.target
	return g.left > 0 && tg != nil && tg.Alive() && !tg.gone && g.m.vb.activity == g.act && g.m.sees(t, tg)
}

func (g *villagerChatGoal) Tick(t *tickView) {
	g.left--
	g.m.lookAtMob(g.target)
	if g.recalc--; g.recalc <= 0 {
		g.recalc = reducedTickDelay(followReplan)
		g.m.seekTo(t.w, g.target.X, g.target.Y, g.target.Z, 0.5, 2)
	}
	if Dist2(g.m.X, g.m.Y, g.m.Z, g.target.X, g.target.Y, g.target.Z) <= gossipTalkDist2 {
		if g.m.gossipWith(g.target) {
			g.m.spawnGolemIfNeeded(t, golemGossipVillagers)
		}
		g.m.shareWith(t, g.target)
	}
}

func (g *villagerChatGoal) Stop(*tickView) {
	g.target = nil
	g.m.stopSeeking()
}

// saveGossip writes Gossips and LastGossipDecay.
// vanilla: Villager.addAdditionalSaveData, GossipContainer.CODEC
func (m *Mob) saveGossip(out nbt.CompoundTag) {
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, e := range m.vg.gossips.entries() {
		entry := nbt.NewOrderedCompound(3)
		entry.Set("Target", nbt.UUIDToIntArray(e.target))
		entry.Set("Type", nbt.String(gossipTypes[e.typ].id))
		entry.Set("Value", nbt.Int(e.value))
		list.Items = append(list.Items, entry)
	}
	out.Set("Gossips", list)
}

// loadGossip reads them back, skipping an entry of an unknown type or with a
// value of 0 or less.
// vanilla: Villager.readAdditionalSaveData
func (m *Mob) loadGossip(c nbt.Compound) {
	vg := &m.vg
	vg.gossips = Gossips{}
	vg.lastDecay = 0
	if v, ok := nbt.LongValue(c["LastGossipDecay"]); ok {
		vg.lastDecay = v
	}
	list, _ := nbt.Get[nbt.List](c, "Gossips")
	for _, item := range list.Items {
		e, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		target, ok := nbt.UUIDField(e, "Target")
		name, _ := nbt.Get[nbt.String](e, "Type")
		value, _ := nbt.IntValue(e["Value"])
		typ := slices.IndexFunc(gossipTypes[:], func(t gossipTypeInfo) bool {
			return t.id == string(name)
		})
		if !ok || typ < 0 || value <= 0 {
			continue
		}
		vals := vg.gossips[target]
		vals[typ] = value
		vg.gossips[target] = vals
	}
}
