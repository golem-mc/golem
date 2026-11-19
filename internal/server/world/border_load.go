package world

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// borderFile is vanilla's world border file under a dimension's storage folder.
// vanilla: WorldBorder.TYPE
const borderFile = "data/minecraft/world_border.dat"

// LoadBorder returns the saved border, or the default if nothing valid is
// saved. Pre-26.1 worlds kept the overworld's border in level.dat's Border*
// fields, the fallback there. A missing field or out-of-range centre fails
// vanilla's codec.
// vanilla: WorldBorder.Settings.CODEC, WorldBorder.applySettings, LegacyWorldBorderFix
func (w *World) LoadBorder() *Border {
	data, err := readSavedData(w.storage, borderFile)
	switch {
	case err == nil:
		if b, ok := borderFromCompound(data); ok {
			w.noteBorderSaved(b)
			return b
		}
		slog.Warn("world_border.dat is not a valid border, using the default", "dimension", w.dimName)
	case !errors.Is(err, errNoSavedData):
		slog.Warn("could not read world_border.dat, using the default", "dimension", w.dimName, "error", err)
	case w.dimName == DimensionName && w.level != nil:
		if b, ok := legacyBorder(w.level.raw); ok {
			return b
		}
	}
	b := NewBorder()
	w.noteBorderSaved(b)
	return b
}

// borderSnapshot is every field world_border.dat holds.
type borderSnapshot struct {
	BorderState
	damage, safeZone float64
}

func snapshotBorder(b *Border) borderSnapshot {
	damage, safe := b.DamageSettings()
	st := b.State()
	st.AbsoluteMax = 0 // from server.properties, not saved
	return borderSnapshot{BorderState: st, damage: damage, safeZone: safe}
}

// borderSaved is the border as last loaded or written, so an unchanged border
// is not rewritten.
type borderSaved struct {
	mu   sync.Mutex
	last borderSnapshot
	set  bool
}

func (w *World) noteBorderSaved(b *Border) {
	w.border.mu.Lock()
	w.border.last, w.border.set = snapshotBorder(b), true
	w.border.mu.Unlock()
}

// SaveBorder writes world_border.dat when the border changed since the last
// load or save; a lerp is saved with its remaining ticks.
// vanilla: WorldBorder.Settings.CODEC
func (w *World) SaveBorder(b *Border) {
	snap := snapshotBorder(b)
	w.border.mu.Lock()
	defer w.border.mu.Unlock()
	if w.border.set && w.border.last == snap {
		return
	}
	data := nbt.Compound{
		"center_x": nbt.Double(snap.CenterX), "center_z": nbt.Double(snap.CenterZ),
		"size": nbt.Double(snap.Size), "lerp_time": nbt.Long(snap.LerpTicks),
		"lerp_target": nbt.Double(snap.Target), "damage_per_block": nbt.Double(snap.damage),
		"safe_zone": nbt.Double(snap.safeZone), "warning_blocks": nbt.Int(snap.WarningBlocks),
		"warning_time": nbt.Int(snap.WarningTime),
	}
	if err := writeSavedData(w.storage, borderFile, data); err != nil {
		slog.Error("could not save world_border.dat", "dimension", w.dimName, "error", err)
		return
	}
	w.border.last, w.border.set = snap, true
}

func borderFromCompound(data nbt.Compound) (*Border, bool) {
	var d [5]nbt.Double
	for i, name := range [...]string{"center_x", "center_z", "size", "damage_per_block", "safe_zone"} {
		v, ok := nbt.Get[nbt.Double](data, name)
		if !ok {
			return nil, false
		}
		d[i] = v
	}
	target, ok1 := nbt.Get[nbt.Double](data, "lerp_target")
	ticks, ok2 := nbt.Get[nbt.Long](data, "lerp_time")
	blocks, ok3 := nbt.Get[nbt.Int](data, "warning_blocks")
	warn, ok4 := nbt.Get[nbt.Int](data, "warning_time")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, false
	}
	if cx, cz := float64(d[0]), float64(d[1]); cx < -BorderMaxCenter || cx > BorderMaxCenter ||
		cz < -BorderMaxCenter || cz > BorderMaxCenter {
		return nil, false
	}
	b := NewBorder()
	b.SetCenter(float64(d[0]), float64(d[1]))
	if ticks > 0 {
		b.LerpSize(float64(d[2]), float64(target), int64(ticks))
	} else {
		b.SetSize(float64(d[2]))
	}
	b.SetDamagePerBlock(float64(d[3]))
	b.SetSafeZone(float64(d[4]))
	b.SetWarningBlocks(int32(blocks))
	b.SetWarningTime(int32(warn))
	return b, true
}

// legacyBorder reads a pre-26.1 level.dat's Border* fields, false if none.
// Missing ones take LegacyWorldBorderFix's defaults, whose warning time is 15,
// not a new border's 300.
// vanilla: LegacyWorldBorderFix
func legacyBorder(raw nbt.Compound) (*Border, bool) {
	b := NewBorder()
	b.SetWarningTime(15)
	found := false
	double := func(name string, set func(float64)) {
		if v, ok := nbt.Get[nbt.Double](raw, name); ok {
			set(float64(v))
			found = true
		}
	}
	integer := func(name string, set func(int32)) {
		if v, ok := nbt.Get[nbt.Int](raw, name); ok {
			set(int32(v))
			found = true
		}
	}
	var cx, cz float64
	double("BorderCenterX", func(v float64) { cx = v })
	double("BorderCenterZ", func(v float64) { cz = v })
	b.SetCenter(cx, cz)
	double("BorderSize", b.SetSize)
	integer("BorderWarningBlocks", b.SetWarningBlocks)
	integer("BorderWarningTime", b.SetWarningTime)
	return b, found
}
