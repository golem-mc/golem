package inventory

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	_ "github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkComposterInput measures the hopper's compostable acceptance and
// single-item transfer into a fresh composter input view.
func BenchmarkComposterInput(b *testing.B) {
	for _, tc := range []struct {
		name    string
		item    registry.Item
		accepts bool
	}{
		{"DefaultApple", registry.ItemApple, true},
		{"PlainStick", registry.ItemStick, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
			var moves ComposterMoves
			dest := moves.Input()
			stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
			b.ReportAllocs()
			for b.Loop() {
				moves = ComposterMoves{}
				hopper.slots[0] = stack
				moved, _ := Eject(hopper, dest, protocol.FaceTop)
				if moved != tc.accepts {
					b.Fatal("unexpected acceptance")
				}
			}
		})
	}
}

// BenchmarkComposterInsertion includes the provider evaluation after the hopper
// hands its item to the composter; the input-view benchmark ends at that handoff.
func BenchmarkComposterInsertion(b *testing.B) {
	for _, tc := range []struct {
		name string
		item registry.Item
	}{
		{"Apple", registry.ItemApple},
		{"WheatSeeds", registry.ItemWheatSeeds},
	} {
		for _, level := range []int{0, 3} {
			b.Run(fmt.Sprintf("%s/Level%d", tc.name, level), func(b *testing.B) {
				hopper := &testBlock{kind: KindHopper, slots: make([]protocol.ItemStack, 5)}
				var moves ComposterMoves
				dest := moves.Input()
				stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
				state, _ := registry.WithInt(registry.BlockComposter.DefaultState(), registry.PropLevel, level)
				ctx := rules.LootContext{}
				rng := rand.New(rand.NewSource(1))
				b.ReportAllocs()
				for b.Loop() {
					moves = ComposterMoves{}
					hopper.slots[0] = stack
					moved, _ := Eject(hopper, dest, protocol.FaceTop)
					if !moved || moves.Received == nil {
						b.Fatal("compostable refused")
					}
					_, inserted, used := rules.ComposterInsert(state, *moves.Received, &ctx, rng)
					if !inserted || !used {
						b.Fatal("compostable not inserted")
					}
				}
			})
		}
	}
}
