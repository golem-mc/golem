package server

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestCollectItemsToPushSnapshotsTheDestination pins that the item collector
// reads the teleport destination under itemsMu rather than when the teleport
// runs: moving the item between the two must not move where it lands. An
// unlocked read there is the race the tick goroutine's own writes would trip.
func TestCollectItemsToPushSnapshotsTheDestination(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: newMobHerd(w)}})
	it := &itemEntity{item: &entity.Item{ID: 1}, w: w}
	it.item.X, it.item.Y, it.item.Z = 5.5, 63.5, 0.5
	s.items = []*itemEntity{it}
	ch := world.BlockChange{X: 5, Y: 63, Z: 0, State: pushStoneState(t), PushUp: 0.0625}

	moves := s.collectItemsToPush(w, blockShove(ch))
	if len(moves) != 1 {
		t.Fatalf("the crossed item offered %d teleports, want 1", len(moves))
	}
	s.itemsMu.Lock()
	it.item.Y = 100
	s.itemsMu.Unlock()
	moves[0]()

	// The destination teleportItem takes is the box's feet, so the centre
	// ends half an item above the position read under the lock.
	const want = 63.5 - entity.ItemHalfSize + 0.0625 + entity.ItemHalfSize
	if it.item.Y != want {
		t.Errorf("the pushed item stands at y %v, want %v", it.item.Y, want)
	}
}

// TestCollectMobsToPushSnapshotsTheDestination is the same for the mob
// collector, whose destination is read under mobsMu before the teleport runs
// without it.
func TestCollectMobsToPushSnapshotsTheDestination(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: newMobHerd(w)}})
	h := s.herdOf(w)
	m := &entity.Mob{ID: 7, X: 5.5, Y: 63.5, Z: 0.5}
	ent := &mobEntity{mob: m, w: w}
	h.herd = entity.Mobs{m}
	h.remember(ent)
	ch := world.BlockChange{X: 5, Y: 63, Z: 0, State: pushStoneState(t), PushUp: 0.0625}

	moves := s.collectMobsToPush(w, blockShove(ch))
	if len(moves) != 1 {
		t.Fatalf("the crossed mob offered %d teleports, want 1", len(moves))
	}
	s.mobsMu.Lock()
	m.Y = 100
	s.mobsMu.Unlock()
	moves[0]()

	if want := 63.5 + 0.0625; m.Y != want {
		t.Errorf("the pushed mob stands at y %v, want %v", m.Y, want)
	}
}

// pushStoneState is the full stone block a push test's body overlaps.
func pushStoneState(t *testing.T) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	return state
}

// TestCollectArmorStandsToPushSnapshotsTheDestination is the stand's
// share of the same: the stand's teleport destination is read under
// armorStandsMu before the move runs without it, a marker having none to
// give and a live one rising by the change's rise.
func TestCollectArmorStandsToPushSnapshotsTheDestination(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: newMobHerd(w)}})
	ent := &armorStandEntity{
		stand: entity.NewArmorStand(9, 5.5, 63.5, 0.5),
		w:     w,
	}
	s.armorStands = append(s.armorStands, ent)
	marker := &armorStandEntity{
		stand: entity.NewArmorStand(10, 5.5, 63.5, 2.5),
		w:     w,
	}
	marker.stand.Marker = true
	s.armorStands = append(s.armorStands, marker)
	ch := world.BlockChange{X: 5, Y: 63, Z: 0, State: pushStoneState(t), PushUp: 0.0625}

	moves := s.collectArmorStandsToPush(w, blockShove(ch))
	if len(moves) != 1 {
		t.Fatalf("the change offered %d teleports, want the live stand's one", len(moves))
	}
	s.armorStandsMu.Lock()
	ent.stand.Y = 100
	s.armorStandsMu.Unlock()
	moves[0]()

	if want := 63.5 + 0.0625; ent.stand.Y != want {
		t.Errorf("the pushed stand stands at y %v, want %v", ent.stand.Y, want)
	}
}

// TestPushCollectorsReadTheSliceUnderTheLock drives a PushUp change through
// every push collector while another goroutine rebuilds each kind's slice
// under its lock, the store tickArmorStands and kin make. A collector that
// evaluates its slice argument before taking the lock reads the header
// outside it and races; the race detector is the assertion.
func TestPushCollectorsReadTheSliceUnderTheLock(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := withDims(&Server{}, []*dimension{{w: w, mobs: newMobHerd(w)}})
	ch := world.BlockChange{X: 5, Y: 63, Z: 0, State: pushStoneState(t), PushUp: 0.0625}

	item := &itemEntity{item: &entity.Item{ID: 1, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	orb := &orbEntity{orb: &entity.Orb{ID: 2, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	arrow := &arrowEntity{arrow: &entity.Arrow{ID: 3, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	tnt := &tntEntity{tnt: &entity.TNT{ID: 4, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	fall := &fallingEntity{fb: &entity.FallingBlock{ID: 5, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	boat := &boatEntity{boat: &entity.Boat{ID: 6, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	stand := &armorStandEntity{stand: entity.NewArmorStand(7, 5.5, 63.5, 0.5), w: w}
	cart := &minecartEntity{cart: &entity.Minecart{ID: 8, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	ball := &thrownEntity{throwable: &entity.Thrown{ID: 9, X: 5.5, Y: 63.5, Z: 0.5}, w: w}
	s.items = []*itemEntity{item}
	s.orbs = []*orbEntity{orb}
	s.arrows = []*arrowEntity{arrow}
	s.tnts = []*tntEntity{tnt}
	s.fallings = []*fallingEntity{fall}
	s.boats = []*boatEntity{boat}
	s.armorStands = []*armorStandEntity{stand}
	s.minecarts = []*minecartEntity{cart}
	s.thrown = []*thrownEntity{ball}

	const turns = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range turns {
			rebuildKind(&s.itemsMu, &s.items, item)
			rebuildKind(&s.orbsMu, &s.orbs, orb)
			rebuildKind(&s.arrowsMu, &s.arrows, arrow)
			rebuildKind(&s.tntMu, &s.tnts, tnt)
			rebuildKind(&s.fallingMu, &s.fallings, fall)
			rebuildKind(&s.boatsMu, &s.boats, boat)
			rebuildKind(&s.armorStandsMu, &s.armorStands, stand)
			rebuildKind(&s.minecartsMu, &s.minecarts, cart)
			rebuildKind(&s.arrowsMu, &s.thrown, ball)
		}
	}()
	go func() {
		defer wg.Done()
		for range turns {
			s.pushEntitiesUp(w, ch)
		}
	}()
	wg.Wait()
}

// rebuildKind replaces *list with just keep, under mu: the store a kind's
// tick pass makes to the slice its lock guards.
func rebuildKind[T any](mu *sync.Mutex, list *[]*T, keep *T) {
	mu.Lock()
	*list = append((*list)[:0], keep)
	mu.Unlock()
}

func TestShellShoveSnapshotsCartsAndThrowsWithoutStoppingThem(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	s := &Server{}
	cart := &minecartEntity{cart: &entity.Minecart{X: 5, Y: 63, Z: 2, VX: 0.2, VY: 0.1, VZ: -0.3}, w: w}
	ball := &thrownEntity{throwable: &entity.Thrown{Kind: entity.ThrownSnowball, X: 5, Y: 63, Z: 2, VX: 0.2, VY: 0.1, VZ: -0.3}, w: w}
	s.minecarts = []*minecartEntity{cart}
	s.thrown = []*thrownEntity{ball}
	sh := bodyShove{dx: 0.25, overlaps: func(_, _, _, _, _, _ float64) bool { return true }}
	moves := append(s.collectMinecartsToPush(w, sh), s.collectThrownToPush(w, sh)...)
	if len(moves) != 2 {
		t.Fatalf("got %d moves, want 2", len(moves))
	}
	cart.cart.X, ball.throwable.X = 100, 100
	for _, move := range moves {
		move()
	}
	if cart.cart.X != 5.25 || ball.throwable.X != 5.25 {
		t.Errorf("destinations not snapshotted: cart=%v ball=%v", cart.cart.X, ball.throwable.X)
	}
	for _, v := range [][3]float64{{cart.cart.VX, cart.cart.VY, cart.cart.VZ}, {ball.throwable.VX, ball.throwable.VY, ball.throwable.VZ}} {
		if v != [3]float64{0.2, 0.1, -0.3} {
			t.Errorf("shove changed velocity: %v", v)
		}
	}
}
