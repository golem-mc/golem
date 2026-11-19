package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

func TestTradeDrawPublishesEmptyOffersWithoutATradeSet(t *testing.T) {
	for _, profession := range []string{"minecraft:none", "minecraft:nitwit"} {
		t.Run(profession, func(t *testing.T) {
			s := &Server{}
			m := entity.NewMob(entity.TypeVillager, 1, 0, 64, 0, 1)
			m.Villager.Profession, m.Villager.Level = profession, 1
			draw := s.prepareTradeDraw(nil, m)
			s.finishTradeDraw(draw)
			if m.Offers == nil {
				t.Fatal("finished draw left offers pending")
			}
			recipes, ok := nbt.Get[nbt.List](m.Offers, "Recipes")
			if !ok || recipes.Elem != nbt.TypeCompound || len(recipes.Items) != 0 {
				t.Fatalf("expected empty recipes, got %+v", m.Offers)
			}
		})
	}
}

func TestTradeDrawPreservesConcurrentOfferUpdates(t *testing.T) {
	s := &Server{}
	m := entity.NewMob(entity.TypeVillager, 1, 0, 64, 0, 1)
	m.Villager.Profession = "minecraft:librarian"
	m.Villager.Level = 1
	m.Villager.Type = "minecraft:plains"
	recipe := nbt.Compound{"uses": nbt.Int(0)}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{recipe}}}
	s.mobsMu.Lock()
	draw := s.prepareTradeDraw(nil, m)
	done := s.tradeDraws[m].done
	if done == nil {
		t.Fatal("draw did not reserve the merchant")
	}
	// A trade can complete while a destination search runs outside the lock.
	recipe["uses"] = nbt.Int(1)
	s.mobsMu.Unlock()
	s.finishTradeDraw(draw)
	recipes, _ := nbt.Get[nbt.List](m.Offers, "Recipes")
	if len(recipes.Items) != 3 || recipes.Items[0].(nbt.Compound)["uses"] != nbt.Int(1) {
		t.Fatalf("draw lost existing uses or new offers: %+v", recipes)
	}
	select {
	case <-done:
	default:
		t.Fatal("draw did not release the waiting holder")
	}
	if s.tradeDraws[m] != nil {
		t.Fatal("finished draw retained its reservation")
	}
}

func TestASecondDrawWaitsForTheFirstBeforeReleasingReaders(t *testing.T) {
	s := &Server{}
	m := entity.NewMob(entity.TypeVillager, 1, 0, 64, 0, 1)
	m.Villager.Profession, m.Villager.Type = "minecraft:librarian", "minecraft:plains"
	m.Villager.Level = 1
	s.mobsMu.Lock()
	first := s.prepareTradeDraw(nil, m)
	done := s.tradeDraws[m].done
	m.Villager.Level = 2
	second := s.prepareTradeDraw(nil, m)
	s.mobsMu.Unlock()
	s.finishTradeDraw(first)
	select {
	case <-done:
		t.Fatal("reader released before level-up draw")
	default:
	}
	s.finishTradeDraw(second)
	recipes, _ := nbt.Get[nbt.List](m.Offers, "Recipes")
	if len(recipes.Items) != 4 {
		t.Fatalf("offers from two levels: %d", len(recipes.Items))
	}
	select {
	case <-done:
	default:
		t.Fatal("reader still waiting")
	}
}
