package registry

import "testing"

// TestBlockNameTablesMatchMaps holds that the typed tables read the same
// answers as the generated name-keyed maps.
func TestBlockNameTablesMatchMaps(t *testing.T) {
	t.Parallel()
	for name, typ := range BlockTypes {
		b, ok := BlockByName(name)
		if !ok || b.Type().Name() != typ {
			t.Fatalf("%s: Type = %q, want %q", name, b.Type().Name(), typ)
		}
	}
	for b := range Block(BlockCount) {
		if got := b.NoCollision(); got != NoCollision[b.Name()] {
			t.Fatalf("%s: NoCollision = %v", b.Name(), got)
		}
		concrete, ok := b.Concrete()
		want, wantOK := ConcreteOf[b.Name()]
		if ok != wantOK || ok && concrete.Name() != want {
			t.Fatalf("%s: Concrete = %s, %v; want %s, %v", b.Name(), concrete.Name(), ok, want, wantOK)
		}
		less, ok := b.Scraped()
		want, wantOK = CopperScraped[b.Name()]
		if ok != wantOK || ok && less.Name() != want {
			t.Fatalf("%s: Scraped = %s, %v; want %s, %v", b.Name(), less.Name(), ok, want, wantOK)
		}
	}
}
