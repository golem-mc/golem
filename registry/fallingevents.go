package registry

// fallingEvent is one block's entry of a falling-event table.
type fallingEvent struct {
	event int32
	ok    bool
}

var (
	fallingLandEvent   = fallingEventTable(FallingLandEvent)
	fallingBrokenEvent = fallingEventTable(FallingBrokenEvent)
)

func fallingEventTable(byName map[string]int32) *[BlockCount]fallingEvent {
	t := new([BlockCount]fallingEvent)
	for b := range Block(BlockCount) {
		event, ok := byName[blockInfo[b].name]
		t[b] = fallingEvent{event, ok}
	}
	return t
}

// FallingLandEvent is the level event a falling block of this kind plays
// where it places itself, and false for a block that plays none; only the
// anvil family has one.
func (b Block) FallingLandEvent() (int32, bool) {
	if b >= BlockCount {
		return 0, false
	}
	return fallingLandEvent[b].event, fallingLandEvent[b].ok
}

// FallingBrokenEvent is the level event a falling block of this kind plays
// where it gives its place up as an item, and false for a block that plays
// none.
func (b Block) FallingBrokenEvent() (int32, bool) {
	if b >= BlockCount {
		return 0, false
	}
	return fallingBrokenEvent[b].event, fallingBrokenEvent[b].ok
}
