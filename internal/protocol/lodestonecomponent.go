package protocol

import "slices"

// The minecraft:lodestone_tracker component is one of the unmodelled ones
// this server keeps as the bytes it arrived in, but a compass used on a
// lodestone becomes one, so its value is decoded and written here.

// LodestoneTarget is the global place a compass points at: the dimension
// its lodestone stands in, and the block.
type LodestoneTarget struct {
	Dimension string
	X, Y, Z   int32
}

// LodestoneTracker is what one stack's minecraft:lodestone_tracker says:
// the place its compass points at — nil for the spinning of no target — and
// whether the game keeps the point tied to the lodestone that set it.
type LodestoneTracker struct {
	Target  *LodestoneTarget
	Tracked bool
}

// LodestoneTrackerOf returns what the tracker a stack carries says,
// reporting false for a stack that carries none — absent, the component is
// the item's own default, the compass that points at the world spawn.
func (s ItemStack) LodestoneTrackerOf() (LodestoneTracker, bool) {
	for _, raw := range s.Raw {
		if raw.Type != DataComponentLodestoneTracker {
			continue
		}
		return lodestoneTrackerValue(NewReader(raw.Data))
	}
	return LodestoneTracker{}, false
}

// SetLodestoneTracker lays the minecraft:lodestone_tracker component on the
// stack, storing what it says as the value its codec carries.
func (s *ItemStack) SetLodestoneTracker(tracker LodestoneTracker) {
	var value Buffer
	value.PutBool(tracker.Target != nil)
	if tracker.Target != nil {
		value.PutString(tracker.Target.Dimension)
		value.PutPosition(tracker.Target.X, tracker.Target.Y, tracker.Target.Z)
	}
	value.PutBool(tracker.Tracked)
	// A copied stack - the clone a creative middle-click makes - shares the
	// Raw backing array; the write below must not reach the stack it came
	// from.
	s.Raw = slices.Clone(s.Raw)
	s.putRaw(RawComponent{Type: DataComponentLodestoneTracker, Data: value.Bytes()})
}

// readLodestoneTracker reads one minecraft:lodestone_tracker value, the
// shape the raw-component reader checks a stack's arrival against.
// vanilla: LodestoneTracker.STREAM_CODEC, GlobalPos.STREAM_CODEC
func readLodestoneTracker(r *Reader) bool {
	_, ok := lodestoneTrackerValue(r)
	return ok
}

// lodestoneTrackerValue decodes one tracker's value, answering whether it
// is whole: the target's present flag, then its dimension as an identifier
// and its block, then the tracked flag.
func lodestoneTrackerValue(r *Reader) (LodestoneTracker, bool) {
	var tracker LodestoneTracker
	if !r.ReadBool() {
		tracker.Tracked = r.ReadBool()
		return tracker, r.Err() == nil
	}
	dimension := r.ReadString(32767)
	if r.Err() != nil || !ValidIdentifier(dimension) {
		return LodestoneTracker{}, false
	}
	x, y, z := r.ReadPosition()
	if r.Err() != nil {
		return LodestoneTracker{}, false
	}
	tracker.Target = &LodestoneTarget{Dimension: dimension, X: x, Y: y, Z: z}
	tracker.Tracked = r.ReadBool()
	return tracker, r.Err() == nil
}
