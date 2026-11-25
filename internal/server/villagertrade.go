package server

import (
	"maps"
	"math/rand"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type villagerOfferUpdater struct{}

func (villagerOfferUpdater) UpdateOfferState(offers nbt.Compound, update func(uses, maxUses, demand int32) (int32, int32)) nbt.Compound {
	return inventory.UpdateOfferState(offers, update)
}

// villagerTradeRangeExtra is the buffer a trade window keeps beyond the
// entity interaction range.
// vanilla: AbstractVillager.stillValid
const villagerTradeRangeExtra = 4.0

// useOnVillager opens a villager's or wandering trader's trades, or makes a
// tradeless villager shake its head. Only the main hand talks, and only the
// main hand shakes a grown villager.
// vanilla: Villager.mobInteract, Villager.startTrading,
// WanderingTrader.mobInteract
func (c *client) useOnVillager(ent *mobEntity, hand int32) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	for pending := s.tradeDraws[m]; pending != nil; pending = s.tradeDraws[m] {
		s.mobsMu.Unlock()
		<-pending.done
		s.mobsMu.Lock()
	}
	// vanilla: Villager.mobInteract refuses a sleeper
	if m.Dead || m.Trading || m.Sleeping {
		s.mobsMu.Unlock()
		return false, nil
	}
	trader := m.Type == entity.TypeWanderingTrader
	if trader {
		c.useSwing = swingServer
	}
	baby := m.IsBaby()
	if trader && baby {
		s.mobsMu.Unlock()
		return false, nil
	}
	var offers []inventory.Offer
	var saved []int
	if !baby {
		if m.Offers == nil {
			// vanilla: AbstractVillager.getOffers draws them on first use; a
			// baby returns from mobInteract before reaching it.
			draw := s.prepareTradeDraw(ent.w, m)
			s.mobsMu.Unlock()
			s.finishTradeDraw(draw)
			s.mobsMu.Lock()
			if m.Dead || m.Trading || m.Sleeping {
				s.mobsMu.Unlock()
				return false, nil
			}
		}
		offers, saved = inventory.OffersFromTag(m.Offers)
	}
	if !trader {
		hero, heroed := c.player.Effect(heroOfTheVillage)
		priceOffers(offers, int(m.PlayerReputation(c.player.UUID)), hero, heroed)
	}
	level, xp := int32(m.Villager.Level), m.VillagerXp
	title := protocol.Text{Translate: "entity.minecraft.villager." + strings.TrimPrefix(m.Villager.Profession, "minecraft:")}
	if trader {
		// vanilla: WanderingTrader.mobInteract opens at level 1;
		// AbstractVillager.getVillagerXp is 0
		level, xp = 1, 0
		title = protocol.Text{Translate: "entity.minecraft.wandering_trader"}
	}
	x, y, z := m.X, m.Y, m.Z
	if len(offers) > 0 {
		m.Trading = true
		m.TradingPlayer = c.player.UUID
	}
	after := s.bodyInteraction(ent)
	s.mobsMu.Unlock()
	// A click that opens the trades or shakes the head both consume it.
	// vanilla: ServerGamePacketListenerImpl.handleInteract
	c.interactedWithEntityAdvancements(after, c.heldItem(hand))

	if !baby && hand == protocol.HandMain {
		c.awardStat(player.Custom(registry.CustomStatTalkedToVillager), 1)
	}
	if len(offers) == 0 {
		if !baby {
			c.useSwing = swingNone
		}
		if !trader && (baby || hand == protocol.HandMain) {
			// vanilla: Villager.setUnhappy
			s.mobsMu.Lock()
			was := mobMetadataStateOf(m)
			m.SetMetadataUnhappy(40)
			metadata := mobMetadataDeltaPayload(ent, was)
			watchers := trackedWatchers(ent.tracked)
			silent, pitch := m.Silent, m.VoicePitch()
			s.mobsMu.Unlock()
			s.sendEntityMetadata(ent.w, watchers, metadata)
			s.broadcastVoiceID(ent.w, m.Type, silent, x, y, z, registry.SoundEntityVillagerNo, m.SoundVolume(), pitch)
		}
		return true, nil
	}

	if text, _, ok := mobCustomName(ent); ok {
		title = text
	}
	var mm *merchant
	mm = &merchant{
		title:      title,
		ent:        ent,
		offers:     offers,
		level:      level,
		xp:         xp,
		regular:    !trader,
		canRestock: !trader,
		persist: func(now, traded []inventory.Offer) {
			if levelled, reputation := s.villagerTraded(ent, c.player.UUID, now, saved, traded); levelled {
				c.repriceOffers(mm, reputation)
			}
		},
		tradeUpdated: func(valid bool) {
			s.mobsMu.Lock()
			event, ok := m.TradeUpdatedVoice(valid)
			silent, vol, pitch := m.Silent, m.SoundVolume(), m.VoicePitch()
			tx, ty, tz := m.X, m.Y, m.Z
			s.mobsMu.Unlock()
			if ok {
				s.broadcastVoiceID(ent.w, m.Type, silent, tx, ty, tz, event, vol, pitch)
			}
		},
		stillValid: func(w *world.World, rng, ex, ey, ez float64) bool {
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			// vanilla: AbstractVillager.stillValid (getTradingPlayer() == player)
			if m.Dead || ent.w != w || !m.Trading || m.TradingPlayer != c.player.UUID {
				return false
			}
			// vanilla: Villager.customServerAiStep stops trading once the profession is gone
			if !trader && m.Villager.Profession == "minecraft:none" {
				return false
			}
			minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
			return rules.WithinEntityInteractionRangeBuffered(rng,
				villagerTradeRangeExtra, ex, ey, ez, minX, minY, minZ, maxX, maxY, maxZ)
		},
		refresh: func() ([]inventory.Offer, int32, int32) {
			s.mobsMu.Lock()
			defer s.mobsMu.Unlock()
			fresh, _ := inventory.OffersFromTag(m.Offers)
			return fresh, int32(m.Villager.Level), m.VillagerXp
		},
		release: func() {
			s.mobsMu.Lock()
			m.Trading = false
			ent.resendOffers = nil
			if !trader {
				m.Offers = clearSpecialPrices(m.Offers)
			}
			s.mobsMu.Unlock()
		},
	}
	s.mobsMu.Lock()
	ent.resendOffers = func() { c.resendOffers(mm) }
	s.mobsMu.Unlock()
	err := c.openMerchant(mm)
	if err != nil {
		s.mobsMu.Lock()
		m.Trading = false
		ent.resendOffers = nil
		s.mobsMu.Unlock()
	}
	return true, err
}

// heroOfTheVillage is the effect that discounts a villager's trades.
const heroOfTheVillage = registry.MobEffectHeroOfTheVillage

// clearSpecialPrices zeroes the special price of every saved offer.
// vanilla: Villager.resetSpecialPrices
func clearSpecialPrices(tag nbt.Compound) nbt.Compound {
	if tag == nil {
		return nil
	}
	offers, saved := inventory.OffersFromTag(tag)
	for i := range offers {
		offers[i].SpecialPrice = 0
	}
	return inventory.WriteOfferState(tag, offers, saved)
}

// villagerTraded writes the window's offer state back to the villager and
// rewards each trade: its experience, an orb half a block up, and the happy
// particles. A trade that levels the villager up draws the new level's trades
// before it returns; levelled reports that, with the buyer's reputation
// before this trade's gossip, which the refreshed window's prices use.
// vanilla: AbstractVillager.notifyTrade, Villager.rewardTradeXp,
// Villager.increaseMerchantCareer, Villager.updateTrades,
// Villager.customServerAiStep (entity event 14)
func (s *Server) villagerTraded(ent *mobEntity, buyer uuid.UUID, now []inventory.Offer, saved []int, traded []inventory.Offer) (levelled bool, reputation int) {
	s.mobsMu.Lock()
	m := ent.mob
	if m.Offers != nil {
		m.Offers = inventory.WriteOfferState(m.Offers, now, saved)
	}
	var orbs []int
	var regen []effect.Change
	var draws []mobTradeDraw
	if len(traded) > 0 {
		m.TradeNotified()
		if m.Type == entity.TypeVillager {
			// vanilla: Villager.customServerAiStep applies the TRADE gossip on
			// the next tick, after updateSpecialPrices read the reputation.
			reputation = int(m.PlayerReputation(buyer))
			m.OnReputationEvent(entity.ReputationTrade, buyer)
		}
	}
	for _, o := range traded {
		orb, up, changes := m.RewardTrade(o.XP, o.RewardExp)
		if orb > 0 {
			orbs = append(orbs, orb)
		}
		regen = append(regen, changes...)
		if up {
			draws = append(draws, s.prepareTradeDraw(ent.w, m))
		}
	}
	var regenTell splashMobTell
	if len(regen) > 0 {
		regenTell = mobEffectTellOf(ent, regen)
	}
	w, id, x, y, z, trader := ent.w, m.ID, m.X, m.Y, m.Z, m.Type == entity.TypeWanderingTrader
	s.mobsMu.Unlock()

	for _, draw := range draws {
		s.finishTradeDraw(draw)
	}
	s.tellSplashMob(w, regenTell)
	for _, value := range orbs {
		// Added whole (addFreshEntity), so it does not merge.
		vx, vy, vz := s.orbPopVector()
		s.spawnOrb(w, x, y+0.5, z, vx, vy, vz, value)
	}
	if trader {
		return false, 0
	}
	s.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(id, protocol.EntityStatusVillagerHappy))
	return len(draws) > 0, reputation
}

// tradeDrawState keeps first-use readers waiting until all queued levels are drawn.
type tradeDrawState struct {
	done    chan struct{}
	tail    chan struct{}
	pending int
}

// mobTradeDraw owns a trade draw's inputs while destination chunks are loaded.
type mobTradeDraw struct {
	w                  *world.World
	mob                *entity.Mob
	id                 uuid.UUID
	sets               []string
	variant            string
	x, y, z            int32
	rng                *rand.Rand
	previous, finished chan struct{}
}

// prepareTradeDraw snapshots inputs under the mob lock.
func (s *Server) prepareTradeDraw(w *world.World, m *entity.Mob) mobTradeDraw {
	sets := []string{strings.TrimPrefix(m.Villager.Profession, "minecraft:") + "/level_" + strconv.Itoa(m.Villager.Level)}
	if m.Type == entity.TypeWanderingTrader {
		sets = entity.WanderingTraderTradeSets[:]
	}
	if s.tradeDraws == nil {
		s.tradeDraws = map[*entity.Mob]*tradeDrawState{}
	}
	if s.tradeDraws[m] == nil {
		s.tradeDraws[m] = &tradeDrawState{done: make(chan struct{})}
	}
	state := s.tradeDraws[m]
	state.pending++
	previous := state.tail
	state.tail = make(chan struct{})
	var id uuid.UUID
	if w != nil {
		if ent := s.herdOf(w).book[m]; ent != nil {
			id = ent.uuid
		}
	}
	return mobTradeDraw{w: w, mob: m, id: id, sets: sets, variant: m.Villager.Type,
		x: rules.BlockOf(m.X), y: rules.BlockOf(m.Y), z: rules.BlockOf(m.Z),
		previous: previous, finished: state.tail,
		// The draw must not share the mob's RNG after releasing its lock.
		rng: entity.NewDice(m.Rand.Int63())}
}

// finishTradeDraw searches and renders maps without the mob lock, then publishes offers.
// vanilla: Villager.updateTrades, WanderingTrader.updateTrades
func (s *Server) finishTradeDraw(draw mobTradeDraw) {
	// A first-use search can still be running when a tick queues the next level.
	if draw.previous != nil {
		<-draw.previous
	}
	var tag nbt.Compound
	s.tradeRollMu.Lock()
	for _, name := range draw.sets {
		if set, ok := registry.FindTradeSet(name); ok {
			tag = inventory.DrawOffers(tag, set, draw.variant, draw.rng, s.mapLootContext(draw.w, draw.x, draw.y, draw.z))
		}
	}
	s.tradeRollMu.Unlock()
	tag = s.askAcquiredTrades(draw, tag)
	s.mobsMu.Lock()
	// vanilla: AbstractVillager.getOffers publishes a list even without a trade set.
	if draw.mob.Offers == nil {
		draw.mob.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound}}
	}
	// Trades can update the existing recipes while a level-up draw searches.
	// Append to the live offers so those uses, demand and discounts survive.
	if tag != nil {
		old, _ := nbt.Get[nbt.List](draw.mob.Offers, "Recipes")
		added, _ := nbt.Get[nbt.List](tag, "Recipes")
		out := maps.Clone(draw.mob.Offers)
		if out == nil {
			out = nbt.Compound{}
		}
		out["Recipes"] = nbt.List{Elem: nbt.TypeCompound, Items: slices.Concat(old.Items, added.Items)}
		draw.mob.Offers = out
	}
	close(draw.finished)
	state := s.tradeDraws[draw.mob]
	state.pending--
	if state.pending == 0 {
		close(state.done)
		delete(s.tradeDraws, draw.mob)
	}
	s.mobsMu.Unlock()
}
