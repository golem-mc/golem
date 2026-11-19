package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"log/slog"
	"sync"
)

// saveGroup counts the auto-saves still writing on their own goroutines.
type saveGroup struct {
	mu      sync.Mutex
	running int
	idle    *sync.Cond
	// parked is a test hook called, with mu held, each time Wait is about to
	// sleep on a running save; mu is released only by the sleep itself, so a save
	// ending after the call is guaranteed to wake the waiter.
	parked func()
}

// Go runs f as a save Wait waits for.
func (g *saveGroup) Go(f func()) {
	g.mu.Lock()
	g.running++
	g.mu.Unlock()
	go func() {
		defer g.done()
		f()
	}()
}

func (g *saveGroup) done() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running--; g.running == 0 && g.idle != nil {
		g.idle.Broadcast()
	}
}

// Wait returns once no save is running.
func (g *saveGroup) Wait() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.idle == nil {
		g.idle = sync.NewCond(&g.mu)
	}
	for g.running > 0 {
		if g.parked != nil {
			g.parked()
		}
		g.idle.Wait()
	}
}

// tickAutoSave writes online players and every dimension's dirty chunks
// when the interval elapses, so a crash loses at most one interval. The
// interval is in ticks, 0 for off (chunks then save only on shutdown); a
// lagging server saves at the same tick counts.
//
// On the wall clock the tick only reads what the save needs from live state;
// encoding the chunks and writing the files, which took the tick whole seconds
// on a world with a few hundred changed chunks, finishes on another goroutine.
// The save counts as running until then, and the next interval waits for it.
// A stepped test clock saves inline unless the test asks otherwise, so a tick
// count is still a save count.
// vanilla: MinecraftServer.tickServer, ticksUntilAutosave
func (s *Server) tickAutoSave() {
	ticks := s.autoSaveTicks.Load()
	// ticksRun counts the finished ticks, so this one is the next.
	if ticks <= 0 || int64(s.ticksRun.Load()-s.lastSaveTick)+1 < ticks {
		return
	}
	if !s.savingMu.TryLock() {
		return // the last save is still writing, or /save-off holds it; due again next tick
	}
	s.lastSaveTick = s.ticksRun.Load() + 1
	flush := s.autoSaveSnapshot()
	if s.hasTickClock() && !s.asyncAutoSave.Load() {
		flush()
		s.saveServerData()
		s.savingMu.Unlock()
		return
	}
	// Read under savingMu, which /save-off takes to write savingOff.
	s.saveServerData()
	s.bgSave.Go(func() {
		defer s.savingMu.Unlock()
		flush()
	})
}

// hasTickClock reports whether a test steps the main loop's clock.
func (s *Server) hasTickClock() bool {
	s.tickClockMu.RLock()
	defer s.tickClockMu.RUnlock()
	return s.tickClock != nil
}

// autoSave writes the players, levels and server-wide saved data.
// vanilla: MinecraftServer.autoSave
func (s *Server) autoSave() {
	// Held across the whole save so /save-off returns only after an in-flight
	// auto-save; vanilla runs both on the server thread.
	s.savingMu.Lock()
	defer s.savingMu.Unlock()
	s.autoSaveSnapshot()()
	s.saveServerData()
}

// autoSaveSnapshot reads the state an auto-save writes, with savingMu held, and
// returns the rest of the save: the player, entity and chunk files. The stores
// take their own locks, so the returned function runs beside the tick.
func (s *Server) autoSaveSnapshot() (flush func()) {
	// vanilla's auto-save says nothing at INFO; only the debug pair marks it.
	slog.Debug("Autosave started")
	s.mgmt.SaveStarted()
	// Players and entities first, so their snapshot matches the blocks.
	// /save-off gates only the levels (chunks, entities, border); players and
	// server-wide data still save.
	// vanilla: MinecraftServer.saveAllChunks, ServerLevel.save(noSave)
	players := s.snapshotOnlinePlayers()
	var writes []mobWrite
	off := s.savingOff
	if !off {
		writes = s.snapshotMobs()
	}
	dims := s.allDims()
	if !off {
		s.fireWorldSaves(dims)
	}
	return func() {
		s.writePlayers(players)
		if !off {
			s.writeMobs(writes)
			for _, dim := range dims {
				dim.w.Save()
			}
		}
		slog.Debug("Autosave finished")
		s.mgmt.SaveCompleted()
	}
}

// saveServerData writes what an auto-save keeps beside the chunks.
func (s *Server) saveServerData() {
	if !s.savingOff {
		// world_border.dat is in the level data storage, which ServerLevel.save
		// skips under /save-off. vanilla: ServerLevel.save(noSave)
		s.saveBorders()
		s.saveRaids()
		s.saveDragonFight()
	}
	s.storages.Save()
	s.entityData.Save()
	s.maps.Save()
	s.randomSequences.Save()
	s.stopwatches.Save()
	s.traderData.Save()
	s.scheduled.Save()
	s.bossBars.Save()
	s.saveScoreboard()
}

// saveBorders writes each dimension's world border when it has changed.
func (s *Server) saveBorders() {
	for _, d := range s.allDims() {
		d.w.SaveBorder(d.border)
	}
}

// saveOnlinePlayers writes every connected player's file without touching
// their open container, grid or cursor. vanilla: PlayerList.saveAll
func (s *Server) saveOnlinePlayers() {
	s.writePlayers(s.snapshotOnlinePlayers())
}

// snapshotOnlinePlayers reads what saveOnlinePlayers writes. A leaving player
// is skipped: their own save wrote their boat and pearls, which a snapshot now
// would lack.
func (s *Server) snapshotOnlinePlayers() []playerWrite {
	s.mu.RLock()
	connected := s.clientsLocked()
	s.mu.RUnlock()
	writes := make([]playerWrite, 0, len(connected))
	for _, c := range connected {
		c.saveMu.Lock()
		if c.leaving.Load() {
			c.saveMu.Unlock()
			continue
		}
		if w, ok := c.snapshotPlayerData(c.savedVehicle(false), s.savedPearlTags(c.player.UUID)); ok {
			writes = append(writes, w)
		}
		c.saveMu.Unlock()
	}
	return writes
}

// writePlayers writes the players' files, logging each failure.
func (s *Server) writePlayers(writes []playerWrite) {
	for _, w := range writes {
		if err := s.players.write(w); err != nil {
			slog.Error("could not save player data", "player", w.id, "error", err)
		}
	}
}

// SaveAll writes every player, mob and chunk for /save-all. flush is
// vanilla's immediate flush, which this synchronous save always does; it
// only enables the per-dimension chunk-store report. It returns whether the
// save succeeded. vanilla: MinecraftServer.saveEverything
func (s *Server) SaveAll(flush bool) bool {
	// Waits for an auto-save still writing, so two saves never write the level
	// files at once.
	s.savingMu.Lock()
	defer s.savingMu.Unlock()
	s.mgmt.SaveStarted()
	defer s.mgmt.SaveCompleted()
	s.saveOnlinePlayers()
	s.saveMobs()
	s.fireWorldSaves(s.allDims())
	for _, dim := range s.allDims() {
		dim.w.Save()
	}
	s.saveBorders()
	s.saveRaids()
	s.saveDragonFight()
	s.storages.Save()
	s.entityData.Save()
	s.maps.Save()
	s.randomSequences.Save()
	s.stopwatches.Save()
	s.traderData.Save()
	s.scheduled.Save()
	s.bossBars.Save()
	s.saveScoreboard()
	if flush {
		// Vanilla's legacy per-dimension chunk storage folder names.
		// vanilla: ChunkMap.save, MinecraftServer.saveAllChunks
		for _, name := range []string{"world", "DIM-1", "DIM1"} {
			slog.Info("ThreadedAnvilChunkStorage (" + name + "): All chunks are saved")
		}
		slog.Info("ThreadedAnvilChunkStorage: All dimensions are saved")
	}
	return true
}

// SetSaving turns auto-save on or off for /save-on and /save-off, returning
// whether it changed (else the commands say "already").
// vanilla: SaveOffCommand, SaveOnCommand
func (s *Server) SetSaving(on bool) bool {
	s.savingMu.Lock()
	defer s.savingMu.Unlock()
	changed := s.savingOff == on
	s.savingOff = !on
	return changed
}

// sendLowDiskSpaceWarning logs the warning and shows the admins' clients a
// toast. vanilla: DedicatedServer.sendLowDiskSpaceWarning
func (s *Server) sendLowDiskSpaceWarning() {
	slog.Warn("Low disk space! Might not be able to save the world.")
	for _, c := range s.playerClients() {
		if s.ops.Level(c.player.UUID) >= command.PermLevelAdmins {
			c.send(protocol.IDPlayLowDiskSpaceWarning, nil)
		}
	}
}
