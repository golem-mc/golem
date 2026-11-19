package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/world"
)

// The server's half of a raider taking a dropped captain's banner: who may
// look for one and which banners lie about, then the take through the loot
// pass's claim. The walk is entity.obtainBannerGoal's.
// vanilla: Raider.ObtainRaidLeaderBannerGoal, Raider.pickUpItem

// bannerSeeker is a raider with its raid and wave read under the mob lock.
type bannerSeeker struct {
	ref     *raiderRef
	raid    *raid.Raid
	wave    int
	wants   bool
	restore bool
}

// tickRaiderBanners tells each raider in a raid whether it may take a
// captain's banner and where the banners lie. A raider outside a raid is told
// to look for none.
// vanilla: Raider.ObtainRaidLeaderBannerGoal.canUse, cannotPickUpBanner
func (s *Server) tickRaiderBanners() {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	var seekers []bannerSeeker
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if !entity.IsIllagerType(m.Type) {
				continue
			}
			if ent := dim.mobs.record(m); ent != nil && ent.raider != nil {
				seekers = append(seekers, bannerSeeker{ref: ent.raider, raid: ent.raider.raid, wave: ent.raider.wave,
					restore: !ent.dying && ent.raider.raid == nil && ent.raider.savedRaid > 0})
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	if len(seekers) == 0 {
		return
	}
	banner := ominousBanner()
	anyWants := false
	for i := range seekers {
		sk := &seekers[i]
		// Restore before ordinary pickup can put a captain's banner in the hand.
		if sk.restore {
			sk.raid = s.restoreRaider(s.dimensionOfWorld(sk.ref.ent.w), sk.ref)
		}
		if sk.raid == nil || !sk.raid.Active() || sk.raid.Over() {
			continue
		}
		// The leader's liveness takes the mob lock, so it is read with none held.
		if leader := sk.raid.Leader(sk.wave); leader != nil && leader.Alive() {
			continue
		}
		s.lockTick(&s.mobsMu)
		sk.wants = !sk.ref.ent.dying && !sk.ref.ent.mob.Worn[3].Equal(banner)
		s.unlockTick(&s.mobsMu)
		anyWants = anyWants || sk.wants
	}
	var lying map[*world.World][]entity.BannerItem
	if anyWants {
		lying = map[*world.World][]entity.BannerItem{}
		s.lockTick(&s.itemsMu)
		for _, it := range s.items {
			if !it.item.Dead && it.item.PickupDue() && it.item.Stack.Equal(banner) {
				lying[it.w] = append(lying[it.w], entity.BannerItem{ID: it.item.ID, X: it.item.X, Y: it.item.Y, Z: it.item.Z})
			}
		}
		s.unlockTick(&s.itemsMu)
	}
	s.lockTick(&s.mobsMu)
	for _, sk := range seekers {
		sk.ref.ent.mob.SetBannerSearch(sk.wants, lying[sk.ref.ent.w])
	}
	s.unlockTick(&s.mobsMu)
}

// bannerTakes turns the banners the dimension's raiders reached into takes of
// the head slot, one per wave. The mob lock is held.
// vanilla: Raider.pickUpItem
func (s *Server) bannerTakes(dim *dimension, ground []mobLootItem, mobGriefing bool) []mobLootTake {
	var takes []mobLootTake
	taken := map[*raid.Raid]map[int]bool{}
	var banners []int
	filtered := false
	for _, m := range dim.mobs.herd {
		id, ok := m.BannerGrab()
		ent := dim.mobs.record(m)
		if !m.Alive() || ent == nil || ent.raider == nil || ent.raider.raid == nil {
			continue
		}
		nearby := mobGriefing && m.CaptainBannerPickupEnabled()
		if !ok && !nearby {
			continue
		}
		ref := ent.raider
		if taken[ref.raid][ref.wave] {
			continue
		}
		if !filtered {
			banner := ominousBanner()
			for i, g := range ground {
				if g.loot.Stack.Equal(banner) {
					banners = append(banners, i)
				}
			}
			filtered = true
		}
		for _, i := range banners {
			g := ground[i]
			goal := ok && g.it.item.ID == id
			if !goal && (!nearby || !m.CanPickUpCaptainBanner(g.loot)) {
				continue
			}
			if taken[ref.raid] == nil {
				taken[ref.raid] = map[int]bool{}
			}
			taken[ref.raid][ref.wave] = true
			takes = append(takes, mobLootTake{
				w: dim.w, it: g.it, stack: g.loot.Stack, leads: ref, mobUUID: ent.uuid,
				take: entity.LootTake{Mob: m, Item: i, Slot: "head", Count: 1, Banner: true},
			})
			break
		}
	}
	return takes
}

// raiderTookBanner makes the raider the captain of its wave, unless another
// took the post since.
// vanilla: Raider.pickUpItem, Raid.setLeader
func (s *Server) raiderTookBanner(ref *raiderRef) {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	s.mobsMu.Lock()
	r, wave := ref.raid, ref.wave
	s.mobsMu.Unlock()
	if r != nil && r.Leader(wave) == nil {
		r.SetLeader(wave, ref)
		s.mobsMu.Lock()
		ref.ent.mob.PatrolLeader = true
		s.mobsMu.Unlock()
	}
}
