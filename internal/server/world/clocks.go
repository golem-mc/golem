package world

import (
	"errors"
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The wire names of the minecraft:world_clock clocks /time of targets; each
// shares its dimension's name.
const (
	OverworldClock = "minecraft:overworld"
	TheEndClock    = "minecraft:the_end"
)

// clockState is one clock's running state: an unbounded total, its rate,
// paused, and the accumulated fraction of a tick for rates below one.
// vanilla: ServerClockManager.ClockInstance
type clockState struct {
	total      int64
	rate       float32
	paused     bool
	fractional float32
}

// clock is a minecraft:world_clock entry and its running state.
type clock struct {
	name  string
	state clockState
}

// worldClocksFile is world/data/minecraft/world_clocks.dat: one compound per
// clock under its wire name, holding total, rate and paused. A level.dat
// carries no clock.
const worldClocksFile = "data/minecraft/world_clocks.dat"

// defaultClocks is every registry clock in ID order (Update Time's order) at
// fresh-world defaults: total 0, rate 1, running.
func defaultClocks() []clock {
	r, _ := registry.Get("minecraft:world_clock")
	clocks := make([]clock, 0, len(r.Entries))
	for _, name := range r.Entries {
		clocks = append(clocks, clock{name: name, state: clockState{rate: 1}})
	}
	return clocks
}

// loadClocks seeds the clocks from world_clocks.dat; a clock the file omits
// keeps defaults, except the overworld's clock in a world without the file,
// which seeds from level.dat's older DayTime, ClockRate and ClockPaused so
// upgrading does not reset the day.
func loadClocks(dir string, level *LevelData) []clock {
	clocks := defaultClocks()
	overworldFromFile := false
	data, err := readSavedData(dir, worldClocksFile)
	if err == nil {
		for i := range clocks {
			entry, ok := nbt.Get[nbt.Compound](data, clocks[i].name)
			if !ok {
				continue
			}
			readClockEntry(entry, &clocks[i].state)
			if clocks[i].name == OverworldClock {
				overworldFromFile = true
			}
		}
	} else if !errors.Is(err, errNoSavedData) {
		slog.Warn("could not read world_clocks.dat, using the defaults", "error", err)
	}
	if !overworldFromFile {
		if c := clockByName(clocks, OverworldClock); c != nil {
			c.state = clockState{total: level.DayTime, rate: level.Rate, paused: level.Paused}
		}
	}
	return clocks
}

// ReloadClocks rebuilds the clocks from world_clocks.dat once the data packs'
// world_clock entries are in the registry, so a pack clock gets state too. It
// runs before the first tick: nothing has moved the clocks yet.
// vanilla: ServerClockManager.init
func (w *World) ReloadClocks() {
	if !w.levelDat {
		return
	}
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.clocks = loadClocks(w.dir, w.level)
	w.envSeq.Add(1)
}

// readClockEntry reads one clock's world_clocks.dat entry into state.
func readClockEntry(entry nbt.Compound, state *clockState) {
	if total, ok := nbt.Get[nbt.Long](entry, "total_ticks"); ok {
		state.total = int64(total)
	}
	if rate, ok := nbt.Get[nbt.Float](entry, "rate"); ok {
		state.rate = float32(rate)
	}
	if paused, ok := nbt.Get[nbt.Byte](entry, "paused"); ok {
		state.paused = paused != 0
	}
}

// saveClocks writes every clock to world_clocks.dat so either server reads the
// same clock, building the compound under the setters' lock.
func (w *World) saveClocks() error {
	w.timeMu.Lock()
	data := w.clocksCompound()
	w.timeMu.Unlock()
	return writeSavedData(w.dir, worldClocksFile, data)
}

// clocksCompound is the world_clocks.dat compound; timeMu must be held.
func (w *World) clocksCompound() nbt.Compound {
	data := make(nbt.Compound, len(w.clocks))
	for _, c := range w.clocks {
		data[c.name] = nbt.Compound{
			"total_ticks": nbt.Long(c.state.total),
			"rate":        nbt.Float(c.state.rate),
			"paused":      nbt.Byte(boolByte(c.state.paused)),
		}
	}
	return data
}

// clockByName is the named clock of a list, or nil.
func clockByName(clocks []clock, name string) *clock {
	for i := range clocks {
		if clocks[i].name == name {
			return &clocks[i]
		}
	}
	return nil
}

// clock is the named clock's state, nil for a name the registry does not
// number. The caller holds timeMu.
func (w *World) clock(name string) *clockState {
	if c := clockByName(w.clocks, name); c != nil {
		return &c.state
	}
	return nil
}

// Clocks names every clock in registry ID order, Update Time's order.
func (w *World) Clocks() []string {
	r, _ := registry.Get("minecraft:world_clock")
	return slices.Clone(r.Entries)
}

// TickTime advances WorldAge by one and every running clock by its rate (totals
// unbounded; the client wraps at a day). advance_time gates the clocks only:
// the age is ServerLevel.tickTime's counter, which runs on regardless.
// vanilla: ServerClockManager.tick
func (w *World) TickTime() {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	w.setWorldAge(w.level.WorldAge + 1)
	if !w.ruleOf(RuleAdvanceTime) {
		return
	}
	moved := false
	for i := range w.clocks {
		c := &w.clocks[i].state
		if c.paused || c.rate <= 0 {
			continue
		}
		// A rate below one advances the clock once the fraction reaches a whole
		// tick.
		c.fractional += c.rate
		advance := int64(c.fractional)
		c.fractional -= float32(advance)
		if advance != 0 {
			c.total += advance
			moved = true
		}
	}
	if moved {
		w.envSeq.Add(1)
	}
}

// ClockTime reports the world's age and the named clock's total; ok is false
// for an unregistered name, which the command's argument refuses first.
func (w *World) ClockTime(clock string) (worldAge, total int64, ok bool) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if c := w.clock(clock); c != nil {
		return w.level.WorldAge, c.total, true
	}
	return 0, 0, false
}

// SetClockTime sets the clock's total, clamped at 0 (/time set); the client
// takes the time of day as the total modulo the timeline's period. It reports
// false, changing nothing, for a clock already at that total or unregistered.
// vanilla: TimeCommand.setTotalTicks
func (w *World) SetClockTime(clock string, ticks int64) bool {
	if ticks < 0 {
		ticks = 0
	}
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	c := w.clock(clock)
	if c == nil || c.total == ticks {
		return false
	}
	c.total = ticks
	c.fractional = 0
	w.envSeq.Add(1)
	return true
}

// AddClockTime adds to the clock's total, clamped at 0 (/time add).
// vanilla: ServerClockManager.addTicks
func (w *World) AddClockTime(clock string, ticks int64) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if c := w.clock(clock); c != nil {
		if next := max(c.total+ticks, 0); next != c.total {
			c.total = next
			w.envSeq.Add(1)
		}
	}
}

// MoveClockToMarker moves the clock forward to the marker's next occurrence
// over period, never backward (/time set <marker>). It reports false, the
// clock left alone, when the clock already stands on the marker (vanilla's
// NOT_MOVED) or is unregistered. The fraction a slow rate carried is dropped,
// since a jump sets the clock absolutely.
// vanilla: ServerClockManager.moveToTimeMarker, ClockTimeMarker.resolveTimeToMoveTo
func (w *World) MoveClockToMarker(clock string, ticks, period int64) bool {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	c := w.clock(clock)
	if c == nil || c.total%period == ticks {
		return false
	}
	// durationToNext is always positive, so the total only grows.
	c.total += durationToNext(period, c.total%period, ticks)
	c.fractional = 0
	w.envSeq.Add(1)
	return true
}

// durationToNext is the ticks from the total's position in the period to the
// marker: the plain distance if ahead, else wrapped by a period, which is also
// a clock on the marker's wait for the next.
// vanilla: ClockTimeMarker.durationToNext
func durationToNext(period, from, to int64) int64 {
	d := to - from
	if d <= 0 {
		d += period
	}
	return d
}

// SetClockPaused pauses or resumes the clock, keeping its rate for the resume
// (/time pause and resume, against the overworld or /time of's clock).
// It reports false for a clock already paused or running as asked, or
// unregistered.
// vanilla: TimeCommand.setPaused
func (w *World) SetClockPaused(clock string, paused bool) bool {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	c := w.clock(clock)
	if c == nil || c.paused == paused {
		return false
	}
	c.paused = paused
	w.envSeq.Add(1)
	return true
}

// SetClockRate sets the clock ticks advanced per server tick (/time rate). It
// reports false for a clock already at that rate, or unregistered.
// vanilla: TimeCommand.setRate
func (w *World) SetClockRate(clock string, rate float32) bool {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	c := w.clock(clock)
	if c == nil || c.rate == rate {
		return false
	}
	c.rate = rate
	c.fractional = 0
	w.envSeq.Add(1)
	return true
}

// ClockView is one clock as an Update Time packet's entry carries it.
type ClockView struct {
	Name     string
	Total    int64
	Fraction float32
	// Rate is clock ticks per client tick: 0 while paused or advance_time holds
	// the clocks, so the client's own ticking stops too.
	Rate float32
}

// ClockViews reads the age and the named clocks under one lock, in the given
// order, for Update Time; unregistered names are skipped.
func (w *World) ClockViews(clocks ...string) (worldAge int64, views []ClockView) {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	advance := w.ruleOf(RuleAdvanceTime)
	for _, name := range clocks {
		c := w.clock(name)
		if c == nil {
			continue
		}
		rate := c.rate
		if c.paused || !advance {
			rate = 0
		}
		views = append(views, ClockView{
			Name:     name,
			Total:    c.total,
			Fraction: c.fractional,
			Rate:     rate,
		})
	}
	return w.level.WorldAge, views
}

// Rate returns the overworld clock's current rate, what /time rate sets.
func (w *World) Rate() float32 {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.clock(OverworldClock).rate
}

// Paused reports whether the overworld clock is paused.
func (w *World) Paused() bool {
	w = w.timeWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.clock(OverworldClock).paused
}

// VillageSiegeTime reports whether the overworld clock is on the siege roll marker.
// vanilla: ServerClockManager.isAtTimeMarker, timeline/day.json
func (w *World) VillageSiegeTime() bool {
	_, total, ok := w.ClockTime(OverworldClock)
	return ok && total%24000 == 18000
}
