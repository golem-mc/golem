package registry

import "testing"

func TestItemEquippableMatchesMap(t *testing.T) {
	t.Parallel()
	for i := range Item(ItemCount) {
		got, ok := i.Equippable()
		want, wantOK := Equippable[i.Name()]
		if got != want || ok != wantOK {
			t.Fatalf("%s: Equippable = %+v, %v; want %+v, %v", i.Name(), got, ok, want, wantOK)
		}
	}
	for _, i := range []Item{-1, -1 << 31, ItemCount, 1<<31 - 1} {
		if got, ok := i.Equippable(); ok || got != (EquippableItem{}) {
			t.Errorf("%d: Equippable = %+v, %v; want zero, false", i, got, ok)
		}
	}
}
