package world

// Sleeping reports whether the entity's tick slot currently does no work.
func (b *BlockEntity) Sleeping() bool { return b.ticker.Load()&1 != 0 }

// Wake restores the ticker without changing its position in the world's list.
// It may run under the world lock, without the entity lock.
func (b *BlockEntity) Wake() {
	switch b.Kind {
	case KindFurnace, KindBlastFurnace, KindSmoker, KindBrewingStand, KindCampfire, KindCrafter, KindHopper:
	default:
		return
	}
	for {
		v := b.ticker.Load()
		if b.ticker.CompareAndSwap(v, (v+2)&^1) {
			return
		}
	}
}

// Tick runs a live, awake entity's ticker under its lock. fn returns whether
// nothing can change until a wake. Use and UseLive conservatively wake because
// their callbacks may write fields; slot setters and block writes wake too.
func (b *BlockEntity) Tick(fn func(*BlockEntity) bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.ticker.Load()
	if v&1 != 0 || b.removed.Load() {
		return
	}
	if fn(b) {
		// A block write can wake while fn holds mu. Never overwrite that wake
		// with a sleep based on the state fn read before the write.
		if b.ticker.CompareAndSwap(v, v|1) && b.Kind == KindHopper {
			// A receiver reconstructs its last no-op turn from registration order.
			b.hopperSlept = true
		}
	}
}
