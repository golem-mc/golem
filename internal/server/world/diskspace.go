package world

// lowDiskSpaceBytes is the usable space under which a failed chunk save is
// reported as a full disk.
// vanilla: LevelStorageSource.LevelStorageAccess.checkForLowDiskSpace (0x4000000)
const lowDiskSpaceBytes = 64 << 20

// SetSaveFailureHook sets what runs after a chunk fails to save and the
// world's disk is low on space. It belongs to construction time.
func (w *World) SetSaveFailureHook(hook func()) { w.onLowDiskSpace = hook }

// SetFreeSpace replaces the usable-space probe, which is given the world's
// directory; it belongs to construction time.
func (w *World) SetFreeSpace(probe func(dir string) (uint64, error)) { w.freeSpace = probe }

// reportSaveFailure runs the low-disk-space hook when the disk is under the
// threshold. A probe that fails counts as plenty of space.
// vanilla: MinecraftServer.warnOnLowDiskSpace
func (w *World) reportSaveFailure() {
	if w.onLowDiskSpace == nil {
		return
	}
	probe := w.freeSpace
	if probe == nil {
		probe = usableSpace
	}
	if free, err := probe(w.dir); err == nil && free < lowDiskSpaceBytes {
		w.onLowDiskSpace()
	}
}
