package scoreboard

// Samples are the values of a player that the read-only criteria follow.
type Samples struct {
	Health, Food, Air, Armor, XP, Level int32
}

// Sampler remembers one player's last written Samples so only changes reach
// the board; the zero value makes the first Apply write every criteria.
// vanilla: ServerPlayer.lastRecordedHealthAndAbsorption and its siblings
type Sampler struct {
	last    Samples
	written bool
}

// Reset forgets what was written, as a new ServerPlayer starts with.
func (s *Sampler) Reset() { *s = Sampler{} }

// Apply writes each changed sample into its criteria's objectives. holder is
// called only for a write, so an unchanged tick builds nothing.
// vanilla: ServerPlayer.doTick, updateScoreForCriteria
func (s *Sampler) Apply(b *Board, holder func() Holder, now Samples) {
	set := func(criteria string, last *int32, value int32) {
		if s.written && *last == value {
			return
		}
		*last = value
		b.UpdateCriteria(criteria, holder(), func(int32) int32 { return value })
	}
	set("health", &s.last.Health, now.Health)
	set("food", &s.last.Food, now.Food)
	set("air", &s.last.Air, now.Air)
	set("armor", &s.last.Armor, now.Armor)
	set("xp", &s.last.XP, now.XP)
	set("level", &s.last.Level, now.Level)
	s.written = true
}
