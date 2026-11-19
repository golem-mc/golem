package blocklogic

import "github.com/golem-mc/golem/registry"

// Fire and SoulFire are the default states used by the shared fire rules.
var (
	Fire     = registry.MustState("minecraft:fire", nil)
	SoulFire = registry.MustState("minecraft:soul_fire", nil)
)

// FireHasFuel reports whether any of the six neighbors can burn.
func FireHasFuel(w BlockReader, x, y, z int32) bool {
	for _, d := range [6][3]int32{
		{0, -1, 0}, {0, 1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1},
	} {
		if FireCanBurn(w.BlockAt(x+d[0], y+d[1], z+d[2])) {
			return true
		}
	}
	return false
}

// FireSurvivesAt tests floor support and neighbor fuel above the void.
// supportY is the floor at or below which sturdy support cannot count.
func FireSurvivesAt(w BlockReader, x, y, z, voidY, supportY int32) bool {
	if y <= voidY {
		return false
	}
	return FireSupported(w.BlockAt(x, y-1, z), y, supportY) || FireHasFuel(w, x, y, z)
}

// FireCanBurn reports a state with nonzero ignite odds.
func FireCanBurn(state uint16) bool { return registry.FireIgniteOdds(state) > 0 }

// SoulFireBase reports the base blocks whose flames become soul fire.
func SoulFireBase(state uint16) bool { return registry.BlockOf(state).IsSoulFireBase() }

// FireStateAt is the state a fire placed at the cell takes: soul fire over a
// soul-fire base, else plain fire over fuel or a sturdy top face, otherwise
// leaning on each fuel neighbour (north, south, west, east and up). supportY
// is the floor at or below which the sturdy face cannot count.
// vanilla: BaseFireBlock.getState
func FireStateAt(w BlockReader, x, y, z, supportY int32, age int) uint16 {
	below := w.BlockAt(x, y-1, z)
	if SoulFireBase(below) {
		return SoulFire
	}
	if FireCanBurn(below) || FireSupported(below, y, supportY) {
		return fireAge(Fire, age)
	}
	state := Fire
	for _, n := range [5]struct {
		prop       registry.Prop
		dx, dy, dz int32
	}{
		{registry.PropNorth, 0, 0, -1}, {registry.PropSouth, 0, 0, 1}, {registry.PropWest, -1, 0, 0},
		{registry.PropEast, 1, 0, 0}, {registry.PropUp, 0, 1, 0},
	} {
		if FireCanBurn(w.BlockAt(x+n.dx, y+n.dy, z+n.dz)) {
			state, _ = registry.WithBool(state, n.prop, true)
		}
	}
	return fireAge(state, age)
}

// FireSupported reports a full top face below a cell above supportY.
func FireSupported(below uint16, y, supportY int32) bool {
	return y > supportY && registry.FaceSturdy(below, 1, registry.SupportFull)
}

func fireAge(state uint16, age int) uint16 {
	if aged, ok := registry.WithInt(state, registry.PropAge, age); ok {
		return aged
	}
	return Fire
}
