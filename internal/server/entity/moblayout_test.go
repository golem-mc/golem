package entity

import (
	"testing"
	"unsafe"
)

func TestSpawnCapFieldsShareTheMobHead(t *testing.T) {
	var m Mob
	for name, off := range map[string]uintptr{
		"Type":          unsafe.Offsetof(m.Type),
		"Persistence":   unsafe.Offsetof(m.Persistence),
		"RidesVehicle":  unsafe.Offsetof(m.RidesVehicle),
		"InRaid":        unsafe.Offsetof(m.InRaid),
		"LeashHolderID": unsafe.Offsetof(m.LeashHolderID),
	} {
		if off >= 256 {
			t.Errorf("%s at offset %d: spawnCountsAll reads it for every mob, keep it in the first 256 bytes", name, off)
		}
	}
	if off := unsafe.Offsetof(m.LivingEffects); off != 240 {
		t.Errorf("LivingEffects at offset %d, want 240: the hot fields after it moved", off)
	}
}
