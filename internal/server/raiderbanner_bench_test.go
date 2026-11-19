package server

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkRaiderBannerLoot runs the full loot pass, including captain claims,
// with 40 raid members and 1,000 ground items. Movement and clients are excluded;
// resetting the banner and equipment is timed on both revisions.
func BenchmarkRaiderBannerLoot(b *testing.B) {
	for _, replacement := range []bool{false, true} {
		name := "CaptainPresent"
		if replacement {
			name = "Replacement"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			dim := s.allDims()[0]
			r := dim.raids.CreateOrExtend(raid.Player{UUID: uuid.New(), Alive: true}, 0, raid.Pos{}, nil)
			banner := ominousBanner()
			for i := range 40 {
				m := entity.NewMob(entity.TypePillager, s.nextEntity(), float64(i)*4, 64, 0, int64(i+1))
				m.CanPickUpLoot, m.NoAI = true, true
				ent := &mobEntity{mob: m, w: w, uuid: uuid.New()}
				s.addMob(ent)
				ent.raider = &raiderRef{s: s, ent: ent, raid: r, wave: 1}
				m.SetBannerSearch(replacement, nil)
			}
			captain := dim.mobs.book[dim.mobs.herd[0]].raider
			if !replacement {
				r.SetLeader(1, captain)
				captain.ent.mob.Worn[3] = banner
			}
			// Keep the stone outside pickup reach so main cannot consume it.
			for i := range 999 {
				s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, float64(i%40)*4, 70, 0, 0, 0, 0, 0)
			}
			// The last raider can take the banner, so the other seekers scan first.
			s.spawnItem(w, banner, 39*4, 64, 0, 0, 0, 0, 0)
			it := s.items[len(s.items)-1]
			heir := dim.mobs.herd[39]
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				heir.MainHand, heir.Worn[3] = protocol.ItemStack{}, protocol.ItemStack{}
				heir.PatrolLeader = false
				it.item.Dead, it.item.Stack = false, banner
				if replacement {
					r.SetLeader(1, nil)
				}
				s.tickMobLoot()
			}
			b.StopTimer()
			for _, ground := range s.items[:len(s.items)-1] {
				if ground.item.Dead || ground.item.Stack.Count != 1 {
					b.Fatal("a raider consumed the out-of-reach stone")
				}
			}
			if !it.item.Dead || (!heir.MainHand.Equal(banner) && !heir.Worn[3].Equal(banner)) {
				b.Fatal("the reachable banner was not equipped")
			}
			if !replacement && r.Leader(1) != captain {
				b.Fatal("the living captain was replaced")
			}
			// Main lacks the ordinary-pickup captain claim; expose that difference
			// alongside timing rather than asserting a claim it cannot perform.
			claims := 0
			if replacement && heir.Worn[3].Equal(banner) && r.Leader(1) == dim.mobs.book[heir].raider {
				claims = 1
			}
			b.ReportMetric(float64(claims), "captain-claims/op")
		})
	}
}
