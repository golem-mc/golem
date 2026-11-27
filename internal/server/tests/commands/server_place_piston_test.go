package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// pistonPairTemplate is two sticky up-facing pistons at x=0,1 with slime on
// top and a redstone block beside each, all one placement.
func pistonPairTemplate() nbt.Compound {
	palette := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		paletteEntry("minecraft:sticky_piston", map[string]string{"facing": "up", "extended": "false"}),
		paletteEntry("minecraft:slime_block", nil),
		paletteEntry("minecraft:redstone_block", nil),
	}}
	blocks := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		templateBlock(0, 0, 0, 0, nil),
		templateBlock(2, 0, 0, 1, nil),
		templateBlock(0, 1, 0, 0, nil),
		templateBlock(2, 1, 0, 1, nil),
		templateBlock(1, 0, 1, 0, nil),
		templateBlock(1, 1, 1, 0, nil),
	}}
	return nbt.Compound{
		"size": ints(2, 2, 2), "palette": palette, "blocks": blocks, "DataVersion": nbt.Int(4440),
	}
}

// TestPlaceTemplateFiresThePistonBesideTheFirstCellFirst checks that a
// placement's neighbour updates run after every cell is placed, in cell
// order, so the first cell's update reaches the second piston before the
// second cell's reaches the first: the east piston's event is queued first
// and the slime it pushes carries the west piston away.
// vanilla: StructureTemplate.placeInWorld
func TestPlaceTemplateFiresThePistonBesideTheFirstCellFirst(t *testing.T) {
	t.Parallel()
	srv, c := startPlaceWorldWith(t, map[string]nbt.Compound{"pair": pistonPairTemplate()})
	c.ChatCommand("place template test:pair 10 70 10")
	extended := func(x int32) string {
		name, props := servertest.StateProps(srv.BlockAt(x, 70, 10))
		if name != "minecraft:sticky_piston" {
			return name
		}
		return props["extended"]
	}
	expectPlaced(t, c, "commands.place.template.success", "test:pair", 10, 70, 10)
	servertest.UntilTicks(t, srv, 40, "the east piston extending", func() bool { return extended(11) == "true" })
	if got := extended(11); got != "true" {
		t.Errorf("east piston is %q, want extended", got)
	}
	if got := extended(10); got != "minecraft:air" {
		t.Errorf("west piston is %q, want air: the east piston fired first and carried it off", got)
	}
	c.Disconnect()
}
