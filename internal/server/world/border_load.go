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
// saved. Pre-26.1 worlds kept the border in the overworld's level.dat Border*
// fields, the fallback for the three vanilla dimensions. A missing field or out-of-range centre fails
// vanilla's codec.
// vanilla: WorldBorder.Settings.CODEC, WorldBorder.applySettings, LegacyWorldBorderFix
func (w *World) LoadBorder(overworld *World) *Border {
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
	case overworld != nil && overworld.level != nil:
		scale := 1.0
		if w.dimName == NetherName {
			scale = 8
		}
		if w.dimName == DimensionName || w.dimName == NetherName || w.dimName == EndName {
			if b, ok := legacyBorder(overworld.level.raw, scale); ok {
				return b
			}
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

// legacyBorder reads a pre-26.1 level.dat's Border* fields, false if none. A
// missing field takes LegacyWorldBorderFix's default, whose warning time is
// 15, not a new border's 300; the Nether's centre is divided by 8.
// vanilla: LegacyWorldBorderFix, LevelDatToSavedDataFileFix.extractWorldBorderToFiles
func legacyBorder(raw nbt.Compound, centerScale float64) (*Border, bool) {
	found := false
	for _, k := range [...]string{"BorderCenterX", "BorderCenterZ", "BorderSize", "BorderSizeLerpTime",
		"BorderSizeLerpTarget", "BorderSafeZone", "BorderDamagePerBlock", "BorderWarningBlocks", "BorderWarningTime"} {
		if _, ok := raw[k]; ok {
			found = true
		}
	}
	if !found {
		return nil, false
	}
	size := nbt.DoubleFieldOr(raw, "BorderSize", 5.9999968e7)
	target := nbt.DoubleFieldOr(raw, "BorderSizeLerpTarget", 0)
	var ticks int64
	if v, ok := nbt.LongValue(raw["BorderSizeLerpTime"]); ok {
		ticks = v
	} else if f := nbt.DoubleFieldOr(raw, "BorderSizeLerpTime", 0); f > 0 {
		ticks = int64(f)
	}
	b := NewBorder()
	b.SetCenter(nbt.DoubleFieldOr(raw, "BorderCenterX", 0)/centerScale, nbt.DoubleFieldOr(raw, "BorderCenterZ", 0)/centerScale)
	if ticks > 0 {
		b.LerpSize(size, target, ticks)
	} else {
		b.SetSize(size)
	}
	b.SetDamagePerBlock(nbt.DoubleFieldOr(raw, "BorderDamagePerBlock", 0.2))
	b.SetSafeZone(nbt.DoubleFieldOr(raw, "BorderSafeZone", 5))
	blocks, _ := nbt.IntField(raw, "BorderWarningBlocks")
	if _, ok := raw["BorderWarningBlocks"]; !ok {
		blocks = 5
	}
	warn, _ := nbt.IntField(raw, "BorderWarningTime")
	if _, ok := raw["BorderWarningTime"]; !ok {
		warn = 15
	}
	b.SetWarningBlocks(blocks)
	b.SetWarningTime(warn)
	return b, true
}
