// The hooks in this file are the seam the black-box tests drive the server
// through (internal/server/servertest and internal/server/tests/...): they reach state no
// packet exposes. Production code never calls them; the linker drops them
// from the binary, since nothing outside the tests reaches them.

package server

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"math/rand"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestFlatLevelType is the level-type the black-box harness starts its
// servers on: not a real one, but the name the test generator hook answers
// to, so the package's own tests are never mistaken for a level-type a
// player could configure.
const TestFlatLevelType = "test:flat"

// InstallTestWorld makes TestFlatLevelType generate the stone flat world,
// built fresh per server so two tests share no state through it. The world
// keeps a level-seed the config names, for /seed; without one its seed is 0
// rather than a fresh roll, so a test's world is the same every run. The test
// harness calls it from its init; a production server never does, so the
// level-type means nothing there.
func InstallTestWorld() {
	testLevelType = TestFlatLevelType
	testNewGenerator = func(levelSeed string) world.Generator {
		g := world.NewFlatGenerator()
		if levelSeed != "" {
			g.SetSeed(world.ParseSeed(levelSeed))
		}
		return g
	}
}

// NewSeeded is New with the root seed of every random source the server
// rolls given, so a test's dice fall the same every run.
func NewSeeded(cfg *config.Config, seed int64, plugins ...plugin.Registration) (*Server, error) {
	return newServerWith(cfg, seed, nil, plugins...)
}

// SharedNetherSeed is the nether seed of the test servers on the flat test
// world: their nethers are one terrain, so they share its chunks.
const SharedNetherSeed = 0

// netherFixture is a key of netherFixtures.
type netherFixture struct {
	seed       int64
	structures bool
}

var netherFixtures sync.Map // netherFixture -> func() *world.GenerationFixture

// NewSeededSharingNether is NewSeeded with a nether of netherSeed whose chunks
// are copied from a process-wide fixture per nether seed, so each nether chunk
// is generated once for all the servers sharing it.
func NewSeededSharingNether(cfg *config.Config, seed, netherSeed int64, plugins ...plugin.Registration) (*Server, error) {
	return newServerWith(cfg, seed, func(_ int64, structures bool) (int64, *world.GenerationFixture) {
		key := netherFixture{seed: netherSeed, structures: structures}
		build, _ := netherFixtures.LoadOrStore(key, sync.OnceValue(func() *world.GenerationFixture {
			gen := world.NewNetherGenerator(key.seed)
			if !key.structures {
				gen.DisableStructures()
			}
			return world.NewGenerationFixture(gen)
		}))
		return key.seed, build.(func() *world.GenerationFixture)()
	}, plugins...)
}

// SetKeepAlive overrides the keep-alive timings of a server, so that tests do
// not have to wait out the intervals a real client is served with. It may be
// called at any time; a test of the exchange calls it once its player is in,
// so a slow join is not held to the shortened timings.
func (s *Server) SetKeepAlive(interval, timeout time.Duration) {
	s.keepAliveInterval.Store(int64(interval))
	s.keepAliveTimeout.Store(int64(timeout))
}

// SetKeepAliveTimeout overrides how long a keep-alive may go unanswered,
// leaving the ping interval as it is.
func (s *Server) SetKeepAliveTimeout(timeout time.Duration) {
	s.keepAliveTimeout.Store(int64(timeout))
}

// SetKeepAliveClock replaces the clock the keep-alive exchange and its round
// trip measurement run by. It has to be called before a player joins.
func (s *Server) SetKeepAliveClock(now func() time.Time) { s.keepAliveNow = now }

// SetSimulationDistance applies a simulation distance after New, which reads
// the config's.
func (s *Server) SetSimulationDistance(d int) { s.setSimulationDistance(d) }

// SetAutoSave overrides the auto-save interval, in whole ticks of at least
// one, or turns it off with 0. It may be called while the loop runs; the
// interval is counted in ticks from the last save.
func (s *Server) SetAutoSave(interval time.Duration) {
	ticks := int64(interval / tickInterval)
	if interval > 0 && ticks == 0 {
		ticks = 1
	}
	s.autoSaveTicks.Store(ticks)
}

// SetChunkWriteGate makes every dimension's chunk saves call gate before their
// write, so a test can hold an auto-save open; nil removes it.
func (s *Server) SetChunkWriteGate(gate func()) {
	for _, d := range s.allDims() {
		d.w.SetChunkWriteGate(gate)
	}
}

// SetAsyncAutoSave makes a held test clock's auto-save finish off the tick like
// a real server's, so a test can step ticks while one is writing.
func (s *Server) SetAsyncAutoSave() { s.asyncAutoSave.Store(true) }

// OnStopWaitsForAutoSave sets f, called each time the loop's stop is about to
// wait on an auto-save still writing; f must not block. A nil f removes it.
func (s *Server) OnStopWaitsForAutoSave(f func()) {
	s.bgSave.mu.Lock()
	defer s.bgSave.mu.Unlock()
	s.bgSave.parked = f
}

// SetEntityWriteFault makes every dimension's entity file writes fail with err
// while it is non-nil, so a test can see what a failed write leaves behind.
func (s *Server) SetEntityWriteFault(err error) {
	for _, d := range s.allDims() {
		d.w.SetEntityWriteFault(err)
	}
}

// WaitAutoSave returns once no auto-save is writing.
func (s *Server) WaitAutoSave() {
	s.savingMu.Lock()
	s.savingMu.Unlock() //nolint:staticcheck // the lock is the wait
}

// SetTimeSync overrides the time-sync interval, in whole ticks of at least
// one, or turns the sync off with 0. It has to be called before the main loop
// is started, which is to say before Listen (or, in a test, StartTick).
func (s *Server) SetTimeSync(interval time.Duration) {
	s.timeSyncTicks = uint64(interval / tickInterval)
	if interval > 0 && s.timeSyncTicks == 0 {
		s.timeSyncTicks = 1
	}
}

// Time returns the world's current age and time of day, in ticks. It is what
// tests watch to check that the day/night cycle is advancing and that a
// restart picks up the clock where the last server left it.
func (s *Server) Time() (worldAge, dayTime int64) {
	return s.world.Time()
}

// SetPaused pauses or resumes the overworld clock directly, without going
// through the /time command. Tests that check exact time values pause the clock
// in their setup so the tick loop does not advance the time between a command
// and the assertion.
func (s *Server) SetPaused(paused bool) { s.world.SetClockPaused(world.OverworldClock, paused) }

// IsPaused reports whether the overworld clock is paused, for testing /time
// pause and /time resume.
func (s *Server) IsPaused() bool { return s.world.Paused() }

// GetRate returns the overworld clock's current rate, for testing /time rate.
func (s *Server) GetRate() float32 { return s.world.Rate() }

// SetClockPaused pauses or resumes the named clock directly, so a test can
// hold every clock still while the tick loop runs.
func (s *Server) SetClockPaused(clock string, paused bool) {
	s.world.SetClockPaused(clock, paused)
}

// EndClockTime returns the_end clock's total ticks, for testing /time of
// the_end against the clock that does not ride the overworld's.
func (s *Server) EndClockTime() int64 {
	_, total, _ := s.world.ClockTime(world.TheEndClock)
	return total
}

// SetTickClock replaces the clock the main loop schedules its ticks by and
// the logger the loop's own lines go to, the backlog warning and a
// finished sprint's report. It has to be called before the main loop is
// started.
func (s *Server) SetTickClock(clock tickClock, log *slog.Logger) {
	s.tickClockMu.Lock()
	s.tickClock = clock
	s.tickLog = log
	s.tickClockMu.Unlock()
}

// SetTickFrozen freezes or unfreezes the game the way /tick freeze and
// /tick unfreeze do, but tells no one: neither the command's line to the
// operators nor the Ticking State packet, which the readers of a test that
// freezes around its join or its commands do not expect.
func (s *Server) SetTickFrozen(frozen bool) { s.tickRate.SetFrozen(frozen) }

// StartTick begins the main loop in a test, the way Listen does in a real
// server.
func (s *Server) StartTick() { s.startTick() }

// StopTick stops the main loop in a test, the way Shutdown does, so the
// goroutine does not outlive the test.
func (s *Server) StopTick() { s.stopTick() }

// DisablePlugins disables the server's plugins, the way Shutdown does, so
// anything they hold is released when a test server stops. Calling it again,
// or after Shutdown, is safe.
func (s *Server) DisablePlugins() { s.disablePlugins() }

// FlushSaves waits for the chunk saves queued in every dimension. Tests call
// it once they have stopped driving the world, before their world directory is
// removed, so the removal does not race a save still in flight on the I/O pool.
func (s *Server) FlushSaves() {
	for _, dim := range s.allDims() {
		dim.w.FlushSaves()
	}
}

// CloseWorlds closes every dimension's region files and gives the session
// lock back: the end of Shutdown without its saves or disconnects. Call it
// after FlushSaves. Calling it again, or after Shutdown, is safe.
func (s *Server) CloseWorlds() error {
	var errs []error
	for _, dim := range s.allDims() {
		errs = append(errs, dim.w.Close())
	}
	return errors.Join(append(errs, s.sessionLock.Release())...)
}

// LoadedChunks is how many chunks the world has generated and not unloaded
// again, which is what tests watch to see chunks come and go.
func (s *Server) LoadedChunks() int { return s.world.LoadedChunks() }

// ChunkDirtyAt reports whether the overworld chunk holding (x, z) is waiting
// for its next save, so a test can watch a container change and its mark land
// together.
func (s *Server) ChunkDirtyAt(x, z int32) bool {
	return s.world.ChunkDirty(world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))})
}

// RegionFileCounts is how many region file handles the overworld's world
// holds open, the chunks' and the entities' counted apart, which is what the
// cache tests watch to check each count stays bounded however many regions are
// visited.
func (s *Server) RegionFileCounts() (chunks, entities int) { return s.world.RegionFileCounts() }

// BlockAt returns the block state at the given world coordinates, or air if the
// chunk is not loaded. It is what tests check that a break or a placement
// reached the world, rather than only the wire.
func (s *Server) BlockAt(x, y, z int32) uint16 { return s.world.BlockAt(x, y, z) }

// SetBlock sets a block in the overworld without broadcasting the change, so
// tests can put water where a player is about to move — or the obsidian of a
// portal frame — without involving the placement code. It reports whether
// the chunk was loaded, the way World.SetBlock does (by whether any chunks
// were affected). BlockInDimension is the form for the other worlds.
func (s *Server) SetBlock(x, y, z int32, state uint16) bool {
	return s.world.SetBlock(x, y, z, state)
}

// PostGameEvent posts a game event in the overworld with no entity behind it.
func (s *Server) PostGameEvent(event string, x, y, z float64) {
	s.gameEvent(s.world, registry.GameEventByName(event), x, y, z, rules.GameEventContext{})
}

// WardenAnger reads the saved anger at one source UUID under the mob lock.
func (s *Server) WardenAnger(wardenID int32, source uuid.UUID) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent := s.mobByEntityIDLocked(s.world, wardenID)
	if ent == nil {
		return 0
	}
	tag := mobTag(ent)
	anger, _ := nbt.Get[nbt.Compound](tag, "anger")
	suspects, _ := nbt.Get[nbt.List](anger, "suspects")
	for _, item := range suspects.Items {
		if c, ok := item.(nbt.Compound); ok {
			if id, ok := nbt.UUIDField(c, "uuid"); ok && id == source {
				value, _ := nbt.IntField(c, "anger")
				return int(value)
			}
		}
	}
	return 0
}

// PostMobGameEvent posts a game event in the overworld with the mob of the
// given entity ID behind it, false if there is no such mob.
func (s *Server) PostMobGameEvent(event string, mobID int32, x, y, z float64) bool {
	id, ok := s.MobUUID(mobID)
	if !ok {
		return false
	}
	s.gameEvent(s.world, registry.GameEventByName(event), x, y, z, rules.GameEventContext{Mob: uuid.MustParse(id)})
	return true
}

// SetOverworldBiome labels the whole column of the overworld chunk holding
// the given block coordinates the named biome, so a test can stand a
// black-box server in a swamp or a plains without a generator of its own.
func (s *Server) SetOverworldBiome(x, z int32, biome string) {
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	c, _ := s.world.Acquire(pos)
	defer s.world.Release(pos)
	c.SetBiome(registry.MustID("minecraft:worldgen/biome", biome))
}

// OverworldBiomeAt is the biome the overworld names for the block at the given
// coordinates — the zoomed read the game's own consumers (spawn lists, mob
// variants) make — or the empty string where nothing can be read.
func (s *Server) OverworldBiomeAt(x, y, z int32) string {
	return s.world.BiomeAt(x, y, z)
}

// OverworldNoiseBiomeAt is the biome stored in the 4x4x4 cell the block falls
// in, or the empty string where that chunk is not loaded.
func (s *Server) OverworldNoiseBiomeAt(x, y, z int32) string {
	return s.world.NoiseBiome(x, y, z).Name()
}

// OverworldWoodsAt reports whether the overworld's tree pass plants the column
// as woods rather than open country.
func (s *Server) OverworldWoodsAt(x, z int32) bool {
	return s.world.OverworldWoodsAt(x, z)
}

// ScanOverworldSurfaces walks the generated overworld's columns inside the
// block square x0..x1 by z0..z1: per column, the blocks that are not air from
// the top bound downward, each handed to visit with its position, descending
// while visit answers true. It is what a generation test reads to compare the
// ground with the biome cell it sits in, stepping through the trees and
// plants a column carries to the terrain beneath them.
func (s *Server) ScanOverworldSurfaces(x0, z0, x1, z1, top int32, visit func(x, y, z int32, state uint16) bool) {
	for cx := world.ChunkOf(float64(x0)); cx <= world.ChunkOf(float64(x1)); cx++ {
		for cz := world.ChunkOf(float64(z0)); cz <= world.ChunkOf(float64(z1)); cz++ {
			c, _ := s.world.Acquire(world.ChunkPos{X: cx, Z: cz})
			for z := range int32(16) {
				wz := cz*16 + z
				if wz < z0 || wz > z1 {
					continue // outside the square: another test's column
				}
				for x := range int32(16) {
					wx := cx*16 + x
					if wx < x0 || wx > x1 {
						continue
					}
					for y := top; y > 0; y-- {
						if state := c.BlockAt(x, y, z); state != 0 && !visit(wx, y, wz, state) {
							break // the column's surface, named
						}
					}
				}
			}
			s.world.Release(world.ChunkPos{X: cx, Z: cz})
		}
	}
}

// LoadBlockAt acquires the chunk at the given world coordinates, reads the block
// there, and releases the chunk. It is what tests use to check whether a change
// reached disk: a chunk that was saved is loaded from its region file, and one
// that was not is regenerated. BlockAt alone is not enough, since it returns air
// for any chunk that is not loaded.
func (s *Server) LoadBlockAt(x, y, z int32) uint16 {
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	_, _ = s.world.Acquire(pos)
	defer s.world.Release(pos)
	return s.world.BlockAt(x, y, z)
}

// SetLoadedBlock sets a block in the overworld, loading the chunk it sits in
// first, so a test can shape the ground before any player has joined — the
// shaping a join's own placement then reads. It waits for the save its release
// queues, so the next Acquire reads the shaped ground. Other writers may still
// be running; their saves must not share this wait's counter.
func (s *Server) SetLoadedBlock(x, y, z int32, state uint16) {
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	s.world.Acquire(pos)
	s.world.SetBlock(x, y, z, state)
	var saves sync.WaitGroup
	s.world.ReleaseWait(pos, &saves)
	saves.Wait()
}

// ScanOverworldBlocks walks the generated overworld inside the block square
// x0..x1 by z0..z1, between the Y bounds given, and calls visit with the Y
// and the state of every block that is not air. It is how a test of world
// generation reads a whole area of terrain — a count of an ore per Y band,
// say — without a player whose view would first have to be walked over it:
// each chunk is acquired the way a join acquires it — generated at first
// touch — and given back before the next.
func (s *Server) ScanOverworldBlocks(x0, z0, x1, z1, y0, y1 int32, visit func(y int32, state uint16)) {
	scanColumns(s.world, x0, z0, x1, z1, y0, y1, func(_, y, _ int32, state uint16) { visit(y, state) })
}

// ScanOverworldBlocksAt is ScanOverworldBlocks handing the visit the block's
// x and z as well: a test that asks where a fluid cell stands has to look
// at its neighbours, which the y-only visit cannot name.
func (s *Server) ScanOverworldBlocksAt(x0, z0, x1, z1, y0, y1 int32, visit func(x, y, z int32, state uint16)) {
	scanColumns(s.world, x0, z0, x1, z1, y0, y1, visit)
}

// ScanLoadedOverworldBlocks walks the chunks a settled join holds, without
// generating terrain outside the area the tick and spawning passes can read.
func (s *Server) ScanLoadedOverworldBlocks(y0, y1 int32, visit func(x, y, z int32, state uint16)) {
	for _, pos := range s.world.LoadedChunkPositions() {
		scanColumns(s.world, pos.X*16, pos.Z*16, pos.X*16+15, pos.Z*16+15, y0, y1, visit)
	}
}

// ScanEndBlocks is ScanOverworldBlocks for the end dimension, with the visit
// handed the block's x and z as well, since a generation test of the end
// reads the pillar ring in one pass and has to know where what it scanned
// stands.
func (s *Server) ScanEndBlocks(x0, z0, x1, z1, y0, y1 int32, visit func(x, y, z int32, state uint16)) {
	scanColumns(s.dimensionByName(world.EndName).w, x0, z0, x1, z1, y0, y1, visit)
}

// scanColumns is the scan of a dimension's box. The physics tick writes the
// sections under the world's lock while the scan runs, so each chunk's column
// is read through BlocksIn rather than the chunk's sections.
func scanColumns(w *world.World, x0, z0, x1, z1, y0, y1 int32, visit func(x, y, z int32, state uint16)) {
	var buf []uint16
	for cx := world.ChunkOf(float64(x0)); cx <= world.ChunkOf(float64(x1)); cx++ {
		for cz := world.ChunkOf(float64(z0)); cz <= world.ChunkOf(float64(z1)); cz++ {
			w.Acquire(world.ChunkPos{X: cx, Z: cz})
			// Outside the square: another test's column.
			bx0, bx1 := max(x0, cx*16), min(x1, cx*16+15)
			bz0, bz1 := max(z0, cz*16), min(z1, cz*16+15)
			if bx0 <= bx1 && bz0 <= bz1 && y0 <= y1 {
				buf = w.BlocksIn(bx0, y0, bz0, bx1, y1, bz1, buf)
				ny, nz := int(y1-y0+1), int(bz1-bz0+1)
				for i, state := range buf {
					if state != 0 {
						visit(bx0+int32(i/(ny*nz)), y0+int32(i/nz%ny), bz0+int32(i%nz), state)
					}
				}
			}
			w.Release(world.ChunkPos{X: cx, Z: cz})
		}
	}
}

// RemoveBlockEntity takes the overworld block entity at the given position
// out of the world without changing the block, the way a break or an unload
// does, for a test that needs a lookup to find the entity and the world's
// own check to find it gone.
func (s *Server) RemoveBlockEntity(x, y, z int32) {
	s.world.RemoveBlockEntity(x, y, z)
}

// ClearLidRecheck drops a pending lid recheck at the overworld block, the
// state a race between two opens leaves behind: each counted the other's
// window and neither booked, so the block has viewers but no recheck.
func (s *Server) ClearLidRecheck(x, y, z int32) {
	s.lidsMu.Lock()
	delete(s.lids, lidRecheck{w: s.world, x: x, y: y, z: z})
	s.lidsMu.Unlock()
}

// SetOpsFile points the server's operator list at a file of its own, so that a
// test does not have to write ops.json into the working directory the package's
// tests share. It has to be called before the server takes any connection, which
// is when the list is first read.
func (s *Server) SetOpsFile(path string) { s.ops.list.setFile(path) }

// OpsFile is where the server's operator list is kept, for tests that inspect
// what /op and /deop wrote back.
func (s *Server) OpsFile() string { return s.ops.list.file() }

// SetBannedPlayersFile, SetBannedIPsFile and SetWhitelistFile point the ban
// and white-list files at paths of the test's own, so that a test does not
// write them into the working directory the package's tests share. They must
// be called before the server reads a list.
func (s *Server) SetBannedPlayersFile(path string) { s.bans.players.setFile(path) }

func (s *Server) SetBannedIPsFile(path string) { s.bans.ips.setFile(path) }

func (s *Server) SetWhitelistFile(path string) { s.whitelist.list.setFile(path) }

// SetUsercacheFile points the user cache at a path of the test's own, for the
// same reason. It must be called before the cache is first read.
func (s *Server) SetUsercacheFile(path string) { s.usercache.list.setFile(path) }

// SetSessionServer points the login check at a session server of the test's
// own. It must be called before a connection is served.
func (s *Server) SetSessionServer(url string) { s.sessionServer = url }

// SetProfileLookupServer points the name lookup of /whitelist, /op, /ban and
// /pardon at a server of the test's own. It must be called before a command runs.
func (s *Server) SetProfileLookupServer(url string) { s.profileLookupServer = url }

// BannedPlayersFile, BannedIPsFile and WhitelistFile report where the server
// keeps the lists, for tests that inspect what the commands wrote back.
func (s *Server) BannedPlayersFile() string { return s.bans.players.file() }

func (s *Server) BannedIPsFile() string { return s.bans.ips.file() }

func (s *Server) WhitelistFile() string { return s.whitelist.list.file() }

// UsercacheFile reports where the server keeps the user cache, for tests
// that inspect what a join wrote into it.
func (s *Server) UsercacheFile() string { return s.usercache.list.file() }

// SetConfigFile points where /whitelist on and off rewrite server.properties,
// so a test does not write it into the working directory the package's tests
// share. It must be called before the white-list is turned.
func (s *Server) SetConfigFile(path string) { s.configPath = path }

// ConfigFile reports where the server rewrites server.properties.
func (s *Server) ConfigFile() string { return s.configPath }

// Difficulty returns how hard the world plays, as the Change Difficulty packet
// numbers it: 0 peaceful, 1 easy, 2 normal, 3 hard. It is what tests check
// against instead of parsing the packet they were sent.
func (s *Server) Difficulty() uint8 { return s.world.Difficulty() }

// SetDifficulty sets how hard the world plays directly, without going through
// the /difficulty command, so that a test can arrange a difficulty without
// running a command as an operator.
func (s *Server) SetDifficulty(d uint8) { s.world.SetDifficulty(d) }

// GameRule reads one of the world's game rules as a boolean, which is what
// the tests that wire a rule into the server's behaviour check the wiring
// against.
func (s *Server) GameRule(id string) bool { return s.world.BoolRule(world.RuleIndex(id)) }

// GameRuleValue reads one of the world's game rules as an integer.
func (s *Server) GameRuleValue(id string) int32 { return s.world.IntRule(world.RuleIndex(id)) }

// SetGameRule puts one of the world's game rules directly, without going
// through the /gamerule command, so that a test can arrange a rule without
// an operator to run it.
func (s *Server) SetGameRule(id string, value bool) { s.world.SetBoolRule(world.RuleIndex(id), value) }

// SetGameRuleValue puts one of the world's game rules directly as an
// integer.
func (s *Server) SetGameRuleValue(id string, value int32) {
	s.world.SetIntRule(world.RuleIndex(id), value)
}

// Raining reports whether it is raining, for the tests that watch the sky
// turn over.
func (s *Server) Raining() bool { return s.world.Raining() }

// IsRaining reports the fading rain level past the gameplay threshold, which
// is what the rain rules read rather than the flag.
func (s *Server) IsRaining() bool { return s.world.IsRaining() }

// Thundering reports whether a thunderstorm rides on the rain.
func (s *Server) Thundering() bool { return s.world.Thundering() }

// TickBlockEntities runs one turn of the block entities - the furnaces
// smelting, their fuel burning - the way the main tick does once it has been
// wired to (see internal/server/blockentities.go). The container tests call it in a
// loop to run a smelt in no time at all; the tests that run the server's
// own tick loop get it there instead.
func (s *Server) TickBlockEntities() { s.tickBlockEntities() }

// OverworldWorld is the overworld, for the tests that drive its chunks and
// block entities directly.
func (s *Server) OverworldWorld() *world.World { return s.world }

// WorldAge is the overworld's age in ticks, the game time /schedule counts.
func (s *Server) WorldAge() int64 {
	age, _ := s.world.Time()
	return age
}

// RunTick runs one whole turn of the main loop by hand, the gating and every
// pass included, for the tests that stop the loop and drive the tick
// themselves.
func (s *Server) RunTick() { s.tick() }

// TickSurvival runs one turn of the survival pass over every player - the
// loading grace's countdown, the vitals' rhythms - the way the main tick's
// tickSurvival does. The loading tests call it in a loop to run the
// sixty-tick grace out in no time at all, the main loop having been stopped
// so that no tick of its own races the ones driven by hand.
func (s *Server) TickSurvival() { s.tickSurvival() }

// TickSleeping runs one turn of the sleeping pass - the night's skip check,
// the sleepers' counters and the dawn rule - the way the main tick's
// tickSleeping does, between the weather and the clock. The sleep tests call
// it in a loop to lie a hundred ticks out in no time at all, the main loop
// having been stopped so that no tick of its own races the ones driven by
// hand.
func (s *Server) TickSleeping() { s.tickSleeping() }

// playerClient finds the connection of the named player, for the test
// wrappers that look one player up by name.
func (s *Server) playerClient(name string) *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.player.Name == name {
			return c
		}
	}
	return nil
}

// DropConnection closes the named player's connection from the server's
// side, so the server's writes to it fail at once and anything it still
// queues goes out nowhere. Reports whether the player was online.
func (s *Server) DropConnection(name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	c.conn.Close()
	return true
}

// StopMailbox ends the named player's writer goroutine; packets already
// queued are never written. Reports whether the player was online.
func (s *Server) StopMailbox(name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	c.out.stop()
	return true
}

// Sleeping reports whether the named online player lies in a bed, which is
// what the sleep tests watch a night click begin and the wakes end.
func (s *Server) Sleeping(name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	return c.player.Sleep().Lying
}

// SleepTicks reports how many ticks the named online player's sleep has run
// for, the counter the night's skip counts, or -1 for a body lying in no
// bed.
func (s *Server) SleepTicks(name string) int {
	c := s.playerClient(name)
	if c == nil {
		return -1
	}
	sl := c.player.Sleep()
	if !sl.Lying {
		return -1
	}
	return sl.Ticks
}

// SleepTimer reports the named online player's raw sleep counter, the count a
// sleeper has run up or the residue a wake left climbing, or -1 for a player
// who is not online. Unlike SleepTicks it reads the counter of a body lying in
// no bed, which is the residue the post-wake count climbs.
func (s *Server) SleepTimer(name string) int {
	c := s.playerClient(name)
	if c == nil {
		return -1
	}
	return c.player.Sleep().Ticks
}

// TickWeather runs one weather tick, for the tests that stop the loop and
// count the fade's ticks themselves.
func (s *Server) TickWeather() { s.tickWeather() }

// SetWeatherState puts the weather where the sleep tests want it: whether it
// rains and thunders, and how long until each cycle next turns over, with
// clearWeatherTime holding a forced clear in place of a rain countdown. It is
// the world's own SetWeather, the tests' door to it.
func (s *Server) SetWeatherState(raining, thundering bool, rainTime, thunderTime, clearTime int32) {
	s.world.SetWeather(raining, thundering, rainTime, thunderTime, clearTime)
}

// InventoryAt reads what the block entity at the given position holds, for
// the container tests to check a chest's or a furnace's contents without
// opening a window for it. The stacks come back in the block entity's own
// numbering, which the window of the same container shares. Nothing came of
// the position when it is nil.
func (s *Server) InventoryAt(x, y, z int32) []protocol.ItemStack {
	be := s.world.BlockEntity(x, y, z)
	if be == nil {
		return nil
	}
	var stacks []protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		stacks = append(stacks, b.Slots...)
	})
	return stacks
}

// CampfireProgress reads the cooking ticks of the four slots of the campfire
// at the given position, zeros where there is none.
func (s *Server) CampfireProgress(x, y, z int32) [world.CampfireSlots]int32 {
	var progress [world.CampfireSlots]int32
	if be := s.world.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *world.BlockEntity) { progress = b.CampfireProgress })
	}
	return progress
}

// InventoryInDimension reads what the block entity at the given position of
// the named dimension holds, the dimension-taking form of InventoryAt.
func (s *Server) InventoryInDimension(dimension string, x, y, z int32) []protocol.ItemStack {
	be := s.dimensionByName(dimension).w.BlockEntity(x, y, z)
	if be == nil {
		return nil
	}
	var stacks []protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		stacks = append(stacks, b.Slots...)
	})
	return stacks
}

// BlockEntityKindAt reports the kind of the block entity at the position,
// false when none is there. The bell tests need one that keeps no slots and
// sends no data, which InventoryAt cannot tell from an absent entity.
func (s *Server) BlockEntityKindAt(x, y, z int32) (world.BlockEntityKind, bool) {
	be := s.world.BlockEntity(x, y, z)
	if be == nil {
		return world.KindNone, false
	}
	return be.Kind, true
}

// BlockEntityKindInDimension is BlockEntityKindAt's dimension-taking form,
// for the tests that stand a block entity somewhere the overworld's reader
// cannot reach.
func (s *Server) BlockEntityKindInDimension(dimension string, x, y, z int32) (world.BlockEntityKind, bool) {
	be := s.dimensionByName(dimension).w.BlockEntity(x, y, z)
	if be == nil {
		return world.KindNone, false
	}
	return be.Kind, true
}

// CrafterDisabledSlots reports which of a crafter's grid slots are disabled,
// for the container tests to check a toggle without parsing the window
// property. It reports false when there is no crafter there.
func (s *Server) CrafterDisabledSlots(x, y, z int32) ([]bool, bool) {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCrafter {
		return nil, false
	}
	var disabled []bool
	be.Use(func(b *world.BlockEntity) {
		disabled = append(disabled, b.DisabledSlots[:]...)
	})
	return disabled, true
}

// BlockEntityTagAt reads the compound the chunk holds for the block entity at
// the given position, live or carried, for the setblock tests to check the
// tag a command gave the block with. Nothing came of the position when the
// second return is false.
func (s *Server) BlockEntityTagAt(x, y, z int32) (nbt.Compound, bool) {
	tag, ok := s.world.BlockEntityData(x, y, z)
	if !ok {
		return nil, false
	}
	return nbt.PlainCopy(tag).(nbt.Compound), true
}

// SetBlockEntityTagAt gives the block at the position the block entity data
// given, as a command's data tag does.
func (s *Server) SetBlockEntityTagAt(x, y, z int32, kind string, tag nbt.Compound) {
	s.world.SetBlockEntityTag(x, y, z, kind, tag)
}

// SetBlockEntityTagInDimension is SetBlockEntityTagAt's dimension-taking form.
func (s *Server) SetBlockEntityTagInDimension(dimension string, x, y, z int32, kind string, tag nbt.Compound) {
	s.dimensionByName(dimension).w.SetBlockEntityTag(x, y, z, kind, tag)
}

// FurnaceAt reads a furnace's progress - how far into the smelt it is, what
// that smelt takes, how much of its fuel is burning and what that lot was -
// for the smelting tests to check without parsing the packets they were sent.
// It reports false when there is no furnace there.
func (s *Server) FurnaceAt(x, y, z int32) (spent, total, lit, litTotal int32, ok bool) {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || !world.IsCooking(be.Kind) {
		return 0, 0, 0, 0, false
	}
	be.Use(func(b *world.BlockEntity) {
		spent, total, lit, litTotal = b.CookingSpent, b.CookingTotal, b.LitRemaining, b.LitTotal
	})
	return spent, total, lit, litTotal, true
}

// SetCookingAt writes a cooking block's timer and total straight into its
// block entity, so a test can lay in a timer a click or a fuel burn would
// not leave - one above its total, say. It reports false when there is no
// cooking block there.
func (s *Server) SetCookingAt(x, y, z int32, spent, total int32) bool {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || !world.IsCooking(be.Kind) {
		return false
	}
	be.Use(func(b *world.BlockEntity) {
		b.CookingSpent, b.CookingTotal = spent, total
	})
	return true
}

// BrewingAt reads a brewing stand's own numbers — the ticks left of the
// brew under way and the fuel power left — for the brewing tests to check
// without parsing the window packets they were sent. It reports false when
// there is no brewing stand there.
func (s *Server) BrewingAt(x, y, z int32) (brewTime, fuel int, ok bool) {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindBrewingStand {
		return 0, 0, false
	}
	be.Use(func(b *world.BlockEntity) { brewTime, fuel = b.BrewTime, b.Fuel })
	return brewTime, fuel, true
}

// FurnaceRecipesUsed reads a furnace's smelting history - the recipe id to
// finished-smelt count the experience is paid out of - as a copy, for the
// furnace experience tests to check what the smelting counted without
// parsing the packets it was told on. It reports false when there is no
// furnace there.
func (s *Server) FurnaceRecipesUsed(x, y, z int32) (map[string]int, bool) {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || !world.IsCooking(be.Kind) {
		return nil, false
	}
	var used map[string]int
	be.Use(func(b *world.BlockEntity) {
		if b.RecipesUsed.Len() == 0 {
			return
		}
		used = make(map[string]int, b.RecipesUsed.Len())
		maps.Copy(used, b.RecipesUsed.Counts())
	})
	return used, true
}

// InventorySlot reports what the named online player holds in a slot of
// their own window, or an empty stack if no such player is online.
func (s *Server) InventorySlot(name string, slot int) protocol.ItemStack {
	c := s.playerClient(name)
	if c == nil {
		return protocol.ItemStack{}
	}
	var stack protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) { stack = inv.Slots[slot] })
	return stack
}

// GiveSlot puts a stack straight into a slot of the named online player's
// own window, the way /give would, so that a test can hand a survival player
// something without the creative inventory packet a survival client may not
// use.
func (s *Server) GiveSlot(name string, slot int, stack protocol.ItemStack) {
	if c := s.playerClient(name); c != nil {
		c.player.Inventory(func(inv *player.Inventory) { inv.Set(slot, stack) })
	}
}

// PlayerEquipmentForTest is the Set Equipment payload the server tells
// viewers about p, for a bare player no server holds.
func PlayerEquipmentForTest(p *player.Player) []byte { return playerEquipment(p) }

// GiveBody puts a stack in the named online player's body equipment slot.
func (s *Server) GiveBody(name string, stack protocol.ItemStack) {
	if c := s.playerClient(name); c != nil {
		c.player.Inventory(func(inv *player.Inventory) { inv.Body = stack })
	}
}

// GiveSaddle puts a stack in the named online player's saddle equipment slot.
func (s *Server) GiveSaddle(name string, stack protocol.ItemStack) {
	if c := s.playerClient(name); c != nil {
		c.player.Inventory(func(inv *player.Inventory) { inv.Saddle = stack })
	}
}

// Eating reports whether the named online player has a meal in progress,
// which is what the eating tests watch the Use Item packet start and the
// release-use action call off.
func (s *Server) Eating(name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	c.eatMu.Lock()
	defer c.eatMu.Unlock()
	return c.eat.ticks > 0
}

// SetFood sets the named online player's food level, so that a test can keep
// natural regeneration (which runs from a food level of 18) from healing a
// player in the middle of what it measures.
func (s *Server) SetFood(name string, food int32) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { v.Food = food })
	}
}

// SetSaturation sets the named online player's saturation, so exhaustion
// reaches the food level.
func (s *Server) SetSaturation(name string, saturation float64) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { v.Saturation = saturation })
	}
}

// SetHealth sets the named online player's health and clears the
// invulnerable window a landed blow leaves, so that a test can wake a
// sleeper with Hurt again the round after.
func (s *Server) SetHealth(name string, health float64) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) {
			v.Health = health
			v.InvulnerableTicks = 0
		})
	}
}

// Hurt lands a blow on the named online player the way a mob's attack
// would, so a test can wake a sleeper the vanilla damage does, and reports
// whether the blow landed.
func (s *Server) Hurt(name string, amount float64) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	landed, _ := c.hurt(amount, rules.DamageTypeMobAttack, &damageCause{killer: &rules.Killer{Name: protocol.Text{Translate: "entity.minecraft.zombie"}}})
	return landed
}

// HurtByEntity lands a melee blow of the entity with the given ID on the
// named online player and reports whether it landed.
func (s *Server) HurtByEntity(name string, amount float64, mobID int32) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	killer, _ := s.mobKillerByID(c.theWorld(), mobID)
	landed, _ := c.hurt(amount, rules.DamageTypeMobAttack, &damageCause{entityID: mobID, killer: killer})
	return landed
}

// HurtAs lands a blow of the given damage type, with no cause, on the named
// online player and reports whether it landed.
func (s *Server) HurtAs(name string, amount float64, typ any) bool {
	damageType := testDamageType(typ)
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	landed, _ := c.hurt(amount, damageType, nil)
	return landed
}

// OpenHurtWindow runs the named online player's invulnerability window with
// the given last hurt, as if a blow of that size had just landed.
func (s *Server) OpenHurtWindow(name string, lastHurt float64) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) {
			v.InvulnerableTicks = rules.HurtInvulnerableTicks
			v.LastHurt = lastHurt
		})
	}
}

// HurtFrom is Hurt from a named position, so a test can choose the side a
// mob's blow comes from, and reports whether it landed.
func (s *Server) HurtFrom(name string, amount, x, z float64) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	landed, _ := c.hurt(amount, rules.DamageTypeMobAttack, &damageCause{x: x, z: z, killer: &rules.Killer{Name: protocol.Text{Translate: "entity.minecraft.zombie"}}})
	return landed
}

// ShieldCooldown is the ticks left of the cooldown an axe's blow put the
// named online player's shield away for, or -1 when they are not online.
func (s *Server) ShieldCooldown(name string) int32 {
	c := s.playerClient(name)
	if c == nil {
		return -1
	}
	c.shieldMu.Lock()
	defer c.shieldMu.Unlock()
	return c.itemCooldownTicks(protocol.ItemStack{ItemID: int32(registry.ItemShield), Count: 1})
}

// PlayerHealth is the named online player's health in half-hearts, or -1
// when they are not online.
func (s *Server) PlayerHealth(name string) float64 {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().Health
	}
	return -1
}

// HoldPlayerUnloaded makes the named player's client count as still loading
// for the rest of the test, so every hurt on them is refused.
func (s *Server) HoldPlayerUnloaded(name string) {
	if c := s.playerClient(name); c != nil {
		c.loadTicks.Store(1 << 30)
	}
}

// PlayerVelocity is the named online player's server-side delta movement in
// blocks per tick, or zeros when they are not online.
func (s *Server) PlayerVelocity(name string) (x, y, z float64) {
	if c := s.playerClient(name); c != nil {
		v := c.player.Velocity()
		return v.X, v.Y, v.Z
	}
	return 0, 0, 0
}

// SetPlayerVelocity replaces the named online player's server-side delta
// movement, for a test that holds a knockback across mob ticks.
func (s *Server) SetPlayerVelocity(name string, x, y, z float64) {
	if c := s.playerClient(name); c != nil {
		c.player.SetVelocity(player.Velocity{X: x, Y: y, Z: z})
	}
}

// PlayerFallDistance is the fall the named online player carries, or -1 when
// they are not online.
func (s *Server) PlayerFallDistance(name string) float64 {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().FallDistance
	}
	return -1
}

// PlayerFallFlying reports whether the named online player is gliding.
func (s *Server) PlayerFallFlying(name string) bool {
	c := s.playerClient(name)
	return c != nil && c.fallFlying.Load()
}

// PlayerSprinting reports whether the named online player is sprinting.
func (s *Server) PlayerSprinting(name string) bool {
	c := s.playerClient(name)
	return c != nil && c.player.Appearance().Sprinting
}

// PlayerSpinAttack reports whether the named online player is in a riptide
// spin.
func (s *Server) PlayerSpinAttack(name string) bool {
	c := s.playerClient(name)
	return c != nil && c.player.Appearance().SpinAttack
}

// PlayerAbsorption is the named online player's absorption in half-hearts, or
// -1 when they are not online.
func (s *Server) PlayerAbsorption(name string) float64 {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().Absorption
	}
	return -1
}

// PlayerAir is the breath the named online player has left in ticks, or -1
// when they are not online.
func (s *Server) PlayerAir(name string) int {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().AirSupply
	}
	return -1
}

// IgnitePlayer lights the named online player for the given ticks the way a
// flaming arrow or a fire aspect blade would, so a test can watch the thaw
// that follows.
func (s *Server) IgnitePlayer(name string, ticks int32) {
	if c := s.playerClient(name); c != nil {
		c.igniteForTicks(ticks)
	}
}

// PlayerFrozen is the frozen ticks the named online player carries, or -1
// when they are not online.
func (s *Server) PlayerFrozen(name string) int {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().TicksFrozen
	}
	return -1
}

// SetPlayerFire puts a burn of the given ticks on the named online player, so
// a test can light a body without a fire block under it.
func (s *Server) SetPlayerFire(name string, ticks int) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { v.FireTicks = ticks })
	}
}

// SetPlayerFrozen leaves the named online player with the given frozen ticks.
func (s *Server) SetPlayerFrozen(name string, ticks int) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { v.TicksFrozen = ticks })
	}
}

// SetPlayerAir leaves the named online player with the breath given in ticks,
// so a test can stand a body at a chosen point of drowning without a pool.
func (s *Server) SetPlayerAir(name string, ticks int) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { v.AirSupply = ticks })
	}
}

// PlayerFire is the burn the named online player still carries in ticks, or
// -1 when they are not online.
func (s *Server) PlayerFire(name string) int {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().FireTicks
	}
	return -1
}

// PlayerPose is the pose the named player is in, standing for nobody.
func (s *Server) PlayerPose(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return int32(c.player.Appearance().Pose)
	}
	return protocol.PoseStanding
}

// PlayerFood is the named player's food bar, zero for nobody.
func (s *Server) PlayerFood(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().Food
	}
	return 0
}

// PlayerExhaustion is the named online player's exhaustion counter, which
// the client is never told: what is not yet a lost point of saturation.
func (s *Server) PlayerExhaustion(name string) float64 {
	if c := s.playerClient(name); c != nil {
		return c.player.Vitals().Exhaustion
	}
	return -1
}

// SetPlayerGameMode puts the named online player in the game mode, as
// /gamemode does.
func (s *Server) SetPlayerGameMode(name string, mode int32) {
	if c := s.playerClient(name); c != nil {
		c.SetGameMode(mode)
	}
}

// SetMobCanPickUpLoot lets the overworld mob with the given entity ID pick
// loot up, the flag a dispenser's equipment needs.
func (s *Server) SetMobCanPickUpLoot(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.CanPickUpLoot = true
		}
	}
}

// ForcePandaSneeze makes the overworld panda with the given entity ID start
// its sneeze goal at its next check.
func (s *Server) ForcePandaSneeze(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.ForcePandaSneeze()
		}
	}
}

// TameHorse makes the overworld horse with the given entity ID tame, the
// state a dispenser's saddle needs.
func (s *Server) TameHorse(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetMetadataHorseTame(true)
		}
	}
}

// MobPersistent reports whether the overworld mob with the given entity ID
// is kept from despawning.
func (s *Server) MobPersistent(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Persistence
		}
	}
	return false
}

// MobSulfurPrimed reports whether the overworld sulfur cube with the given
// entity ID has its fuse lit.
func (s *Server) MobSulfurPrimed(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.SulfurPrimed()
		}
	}
	return false
}

// MobEquipment is what the overworld mob with the given entity ID wears in
// the named slot (feet, legs, chest, head, body, saddle or antenna).
func (s *Server) MobEquipment(id int32, slot string) protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID != id {
			continue
		}
		switch slot {
		case "body":
			return m.Body
		case "saddle":
			return m.Horse.Saddle
		case "mainhand":
			return m.MainHand
		case "antenna":
			return m.Antenna
		}
		for i, name := range entity.ArmorSlotNames {
			if name == slot {
				return m.Worn[i]
			}
		}
	}
	return protocol.ItemStack{}
}

// SetMobEquipment puts a stack in the body, saddle, antenna or main hand slot of the overworld
// mob with the given entity ID.
func (s *Server) SetMobEquipment(id int32, slot string, stack protocol.ItemStack) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID != id {
			continue
		}
		switch slot {
		case "body":
			m.SetEquipment(6, stack)
		case "saddle":
			m.SetMetadataHorseSaddle(stack)
		case "mainhand":
			m.SetEquipment(protocol.EquipMainHand, stack)
		case "antenna":
			m.SetMetadataAntenna(stack)
		}
	}
}

// ArmorStandStack is what the stand with the given entity ID holds in a wire
// slot (entity.StandMainHand and the rest).
func (s *Server) ArmorStandStack(id int32, slot int) protocol.ItemStack {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.stand.ID == id {
			return ent.stand.StackOf(slot)
		}
	}
	return protocol.ItemStack{}
}

// SetArmorStandStack puts a stack into a wire slot of the armor stand with the
// given entity ID.
func (s *Server) SetArmorStandStack(id int32, slot int, stack protocol.ItemStack) {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.stand.ID == id {
			ent.stand.SetStack(slot, stack)
		}
	}
}

// PlayerGameMode is the named online player's game mode, as the packets
// number it, or -1 when they are not online.
func (s *Server) PlayerGameMode(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return c.player.GameMode()
	}
	return -1
}

// AdvancementObtained reports whether the named online player has the
// criterion of the advancement obtained.
func (s *Server) AdvancementObtained(name, id, criterion string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	progress, n := c.advancements.Load(), advancement.Current().Get(id)
	return progress != nil && n != nil && progress.Obtained(n, criterion)
}

// RecipeKnown reports whether the named online player's recipe book holds
// the recipe.
func (s *Server) RecipeKnown(name, recipe string) bool {
	c := s.playerClient(name)
	return c != nil && c.recipeBookState().Knows(recipe)
}

// Places and dimensions. The three dimensions have no commands to reach them
// by — vanilla has none either, the /execute run tp form included — so the
// tests reach them through these: the same enterDimension a nether portal's
// travel runs, and reads of the world arrived at.

// PlayerDimension returns the wire name of the dimension the named online
// player is in, or the empty string when they are not online.
func (s *Server) PlayerDimension(name string) string {
	c := s.playerClient(name)
	if c == nil {
		return ""
	}
	return c.dimension().name
}

// TravelByEndPortalFrom runs, on the caller, the named player's end portal
// travel as fired from a portal in the named dimension: the travel a loaded
// machine can run late, once the body has already crossed.
func (s *Server) TravelByEndPortalFrom(name, dimension string) {
	if c := s.playerClient(name); c != nil {
		c.travelByEndPortal(s.dimensionByName(dimension))
	}
}

// EndExitState reports what an end portal's crossing for the named player
// turns on - the credits seen, the credits showing, a crossing in flight -
// so a test whose body stayed in the end can say which held it.
func (s *Server) EndExitState(name string) (seenCredits, wonGame, traveling bool) {
	c := s.playerClient(name)
	if c == nil {
		return false, false, false
	}
	return c.player.SeenCredits(), c.wonGame.Load(), c.traveling.Load()
}

// SetSeenCredits sets the named online player's end-credits flag, for the
// tests that check a later exit from the end.
func (s *Server) SetSeenCredits(name string, seen bool) {
	if c := s.playerClient(name); c != nil {
		c.player.SetSeenCredits(seen)
	}
}

// PlayerPosition returns where the named online player stands, in the
// coordinates of the dimension they are in, or the zero position when they
// are not online.
func (s *Server) PlayerPosition(name string) player.Position {
	c := s.playerClient(name)
	if c == nil {
		return player.Position{}
	}
	return c.player.Position()
}

// MovePlayerToDimension carries the named online player to another dimension
// standing at the given block coordinates, by the same cross-dimension
// sequence a nether portal or an end portal runs: Respawn, position,
// abilities, health, time and the chunks of the world arrived at. It is how
// a test puts a player into the nether or the end without lighting anything,
// and how it sets up what a portal then has to find.
func (s *Server) MovePlayerToDimension(name, dimension string, x, y, z float64) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	to := s.dimensionByName(dimension)
	if to == c.dimension() {
		return
	}
	if err := c.enterDimension(to, command.Move{Pos: standingAt(world.SpawnPos{
		X: int32(math.Floor(x)),
		Y: int32(math.Floor(y)),
		Z: int32(math.Floor(z)),
	})}, respawnNone, false); err != nil {
		c.log.Warn("test dimension move failed", "error", err)
	}
}

// CrossApart stores two players into two other dimensions before either
// one's waypoints are forgotten, the interleaving of two simultaneous
// crossings, then forgets both against the dimension they left.
func (s *Server) CrossApart(a, aDim, b, bDim string) {
	ca, cb := s.playerClient(a), s.playerClient(b)
	if ca == nil || cb == nil {
		return
	}
	old := ca.theWorld()
	ca.dim.Store(s.dimensionByName(aDim))
	cb.dim.Store(s.dimensionByName(bDim))
	s.forgetWaypoints(ca, old)
	s.forgetWaypoints(cb, old)
}

// HoldMailbox keeps the named player's mailbox from writing until release,
// so a test can leave packets queued while something else happens. Release
// may be called more than once.
func (s *Server) HoldMailbox(name string) (release func()) {
	c := s.playerClient(name)
	if c == nil {
		return func() {}
	}
	c.out.writing.Lock()
	c.out.held.Store(true)
	var once sync.Once
	return func() {
		once.Do(func() {
			c.out.held.Store(false)
			c.out.writing.Unlock()
		})
	}
}

// BeforeWriterHold runs before the named player's next holdWriter acquisition.
// It reports whether the player is online.
func (s *Server) BeforeWriterHold(name string, before func()) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	c.out.holdSeam.Store(&before)
	return true
}

// WriterHeld returns a probe reporting whether the named player's mailbox
// writer is held, for tests that watch a hold being taken and given back.
// The probe outlives the player, so it keeps answering after their
// connection is gone.
func (s *Server) WriterHeld(name string) (held func() bool, ok bool) {
	c := s.playerClient(name)
	if c == nil {
		return nil, false
	}
	m := c.out
	return func() bool {
		if m.writing.TryLock() {
			m.writing.Unlock()
			return false
		}
		return true
	}, true
}

// ShowItemAcrossCrossing pairs the named player with an item the way the
// item tick does, under the item lock, then carries them to dimension and
// only then queues the pairing: a tick that lost the race to a crossing.
func (s *Server) ShowItemAcrossCrossing(name string, itemID int32, dimension string, x, y, z float64) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	var shows []outbound
	var to showTo
	s.itemsMu.Lock()
	for _, it := range s.items {
		if it.item.ID == itemID && c.theWorld() == it.w {
			body := itemTracker.body(it)
			body.mark(c)
			shows = itemPairing(it)
			to = showTo{c, it.w}
		}
	}
	s.itemsMu.Unlock()
	s.MovePlayerToDimension(name, dimension, x, y, z)
	for _, p := range shows {
		to.send(p.id, p.data)
	}
}

// RunExplosion runs a primed TNT's blast at the centre given, the way the
// tick loop's fuse-out does, for the tests that destroy a container by a
// blast.
func (s *Server) RunExplosion(x, y, z, radius float64) {
	s.runExplosion(blast{
		w:           s.world,
		x:           x,
		y:           y,
		z:           z,
		radius:      radius,
		directType:  "minecraft:tnt",
		damageType:  rules.DamageTypeExplosion,
		rng:         rand.New(rand.NewSource(1)),
		interaction: rules.TNTExplosionInteraction(s.world),
	})
}

// BlastAcrossCrossing decides and applies a blast at the given position of
// the overworld the way the tick does, then carries the named player to the
// dimension named and only then queues the blast's packets: a blast whose
// packets lost the race to their reader's crossing.
func (s *Server) BlastAcrossCrossing(name string, x, y, z, radius float64, dimension string, dx, dy, dz float64) {
	b := blast{
		w: s.world,
		x: x, y: y, z: z,
		radius:     radius,
		directType: "minecraft:tnt",
		damageType: rules.DamageTypeExplosion,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		// The interaction a primed TNT's blast would carry.
		interaction: rules.TNTExplosionInteraction(s.world),
	}
	out := s.applyBlast(b)
	s.MovePlayerToDimension(name, dimension, dx, dy, dz)
	s.sendBlast(b, out)
}

// StrikeAcrossCrossing lands a monster's blow on the named player the way the
// tick's landingBlow runs it — the hurt, then the knockback — carries them to
// the dimension named between the two, and queues the knockback only then: a
// push that lost the race to its victim's crossing, worked out of the old
// world's coordinates against the body that arrived.
func (s *Server) StrikeAcrossCrossing(name string, x, y, z, damage float64, dimension string, dx, dy, dz float64) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	w := c.theWorld()
	if landed, _ := c.hurt(damage, rules.DamageTypeMobAttack, &damageCause{x: x, y: y, z: z, killer: &rules.Killer{Name: protocol.Text{Translate: "entity.minecraft.zombie"}}}); !landed {
		return
	}
	s.MovePlayerToDimension(name, dimension, dx, dy, dz)
	c.knockbackFromPoint(w, x, y, z, syncVelocityNow)
}

// SetPortalCooldown sets how many ticks must pass before a nether portal
// takes the named player anywhere again, so a test can put the cooldown
// wherever it needs it rather than waiting it out.
func (s *Server) SetPortalCooldown(name string, ticks int32) {
	if c := s.playerClient(name); c != nil {
		c.portalCooldown.Store(ticks)
	}
}

// PortalCooldown reports the named player's remaining portal cooldown ticks.
func (s *Server) PortalCooldown(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return c.portalCooldown.Load()
	}
	return 0
}

// PortalTime reports how many ticks the named player has stood towards a
// nether crossing, for the tests that step the portal clock by hand and
// check a tick's count rather than racing the loop for it.
func (s *Server) PortalTime(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return c.portalIn.Load()
	}
	return 0
}

// BlockInDimension returns the block state at the given coordinates of the
// named dimension, or air if the chunk is not loaded there. It is the
// dimension-taking form of BlockAt.
func (s *Server) BlockInDimension(dimension string, x, y, z int32) uint16 {
	return s.dimensionByName(dimension).w.BlockAt(x, y, z)
}

// BiomeInDimension is the biome name at the given block of the named dimension.
func (s *Server) BiomeInDimension(dimension string, x, y, z int32) string {
	return s.dimensionByName(dimension).w.BiomeAt(x, y, z)
}

// RainingAtInDimension is the named dimension's RainingAt.
func (s *Server) RainingAtInDimension(dimension string, x, y, z int32) bool {
	return s.dimensionByName(dimension).w.RainingAt(x, y, z)
}

// SkyDarkenInDimension is the named dimension's SkyDarken.
func (s *Server) SkyDarkenInDimension(dimension string) int {
	return s.dimensionByName(dimension).w.SkyDarken()
}

// SetBlockInDimension sets a block in the named dimension without
// broadcasting the change, the dimension-taking form of SetBlock. It is how
// a test lays the obsidian of a frame somewhere no player is standing to
// place it, or the end portal block that has no item to place it with.
func (s *Server) SetBlockInDimension(dimension string, x, y, z int32, state uint16) bool {
	return s.dimensionByName(dimension).w.SetBlock(x, y, z, state)
}

// EnsureBlockEntityInDimension gives the block at a position of the named
// dimension the block entity of the given kind, the dimension-taking form of
// the world's EnsureBlockEntity. It is how a container-move test gives a
// container placed with SetBlockInDimension its empty slots.
func (s *Server) EnsureBlockEntityInDimension(dimension string, x, y, z int32, kind world.BlockEntityKind) bool {
	return s.dimensionByName(dimension).w.EnsureBlockEntity(x, y, z, kind) != nil
}

// LoadBlockInDimension acquires the chunk at the given coordinates of the
// named dimension, reads the block there, and releases the chunk. It is the
// dimension-taking form of LoadBlockAt, and what a test checks the
// destination of a portal against when nobody is standing there to keep it
// loaded.
func (s *Server) LoadBlockInDimension(dimension string, x, y, z int32) uint16 {
	w := s.dimensionByName(dimension).w
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	return w.BlockAt(x, y, z)
}

// ChunkLoadedInDimension reports whether the chunk at the given block
// coordinates of the named dimension is in memory.
func (s *Server) ChunkLoadedInDimension(dimension string, x, z int32) bool {
	return s.dimensionByName(dimension).w.ChunkLoadedAt(x, z)
}

// EndPortalState returns the block state ID the end portal travels by, so a
// test can place the block without the registry knowing the name of it.
func (s *Server) EndPortalState() uint16 { return rules.EndPortalState }

// Mobs. The black-box tests drive the animals the way they drive everything
// else — through a real connection — but a mob has to be standing somewhere
// before anything can be done to it, and no command summons one yet, so the
// tests put one in the world by hand through the same path a chunk's saved
// mobs take when it is loaded.

// SpawnMob puts a live mob of the given kind — "minecraft:cow", say — at the
// given position in the overworld, shows it to every player standing there,
// and returns the entity ID the packets address it by. The seed is the same
// chunk-seeded roll the natural generation uses, so a test that plants a
// sheep in a place grows the same sheep there however often it runs.
func (s *Server) SpawnMob(kind string, x, y, z float64) int32 {
	return s.spawnMobInDim(s.allDims()[0], entity.MobTypeOf(kind), x, y, z,
		entity.ChunkSeed(s.world.SeedValue(), entity.ChunkPos{
			X: world.ChunkOf(x), Z: world.ChunkOf(z),
		})^int64(math.Round(x))^int64(math.Round(z))<<32, nil)
}

// SpawnMobTagged is a /summon of the kind with the saved-mob tag at the given
// position in the overworld; it returns the entity ID, or 0 if refused.
func (s *Server) SpawnMobTagged(kind string, x, y, z float64, tag nbt.Compound) int32 {
	id, err := s.spawnSummonedMobInDim(s.allDims()[0], entity.MobTypeOf(kind), x, y, z, tag)
	if err != nil {
		return 0
	}
	return id
}

// SpawnMobNatural is SpawnMob as the natural spawn pass stands a mob up, the
// only spawn that gives a zombie horse its zombie rider.
func (s *Server) SpawnMobNatural(kind string, x, y, z float64) int32 {
	return s.spawnMobInDim(s.allDims()[0], entity.MobTypeOf(kind), x, y, z, 1,
		entity.NewNaturalSpawnGroup(entity.GoDice{Rand: rand.New(rand.NewSource(1))}))
}

// SpawnCaptain puts a live raider of the kind at the position in the
// overworld wearing the captain's banner as its wave's leader.
func (s *Server) SpawnCaptain(kind string, x, y, z float64) int32 {
	return s.spawnMobArriving(s.allDims()[0], entity.MobTypeOf(kind), x, y, z, 1, nil, false, "", true,
		func(e *mobEntity) { e.mob.WearOminousBanner(ominousBanner()) })
}

// SpawnMobSeeded is SpawnMob with the mob's random stream seed given, so a
// test can pin what the spawn's dice roll. An optional true uses NATURAL.
func (s *Server) SpawnMobSeeded(kind string, x, y, z float64, seed int64, natural ...bool) int32 {
	var group *entity.SpawnGroup
	if len(natural) > 0 && natural[0] {
		group = entity.NewNaturalSpawnGroup(entity.GoDice{Rand: rand.New(rand.NewSource(seed))})
	}
	return s.spawnMobInDim(s.allDims()[0], entity.MobTypeOf(kind), x, y, z, seed, group)
}

// SpawnMobInDimension is SpawnMob's dimension-taking form: the same live mob
// of the given kind at the given position of the named dimension, seeded
// from the position the same way, so a sheep planted in the nether grows the
// same sheep there however often the test runs.
func (s *Server) SpawnMobInDimension(kind, dimension string, x, y, z float64) int32 {
	return s.spawnMobInDim(s.dimensionByName(dimension), entity.MobTypeOf(kind), x, y, z,
		entity.ChunkSeed(s.dimensionByName(dimension).w.SeedValue(), entity.ChunkPos{
			X: world.ChunkOf(x), Z: world.ChunkOf(z),
		})^int64(math.Round(x))^int64(math.Round(z))<<32, nil)
}

// SetMobInLove puts the overworld mob with the given entity ID in love for
// ticks, with no player behind it.
func (s *Server) SetMobInLove(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil {
		ent.mob.InLove = ticks
	}
}

// ObserveGameEvents calls fn with each game event posted from now on, from
// whichever goroutine posts it.
func (s *Server) ObserveGameEvents(fn func(event string, x, y, z float64)) {
	s.ObserveGameEventContexts(func(event string, x, y, z float64, _ rules.GameEventContext) { fn(event, x, y, z) })
}

// ObserveGameEventContexts is ObserveGameEvents with each event's context:
// its source and the block state it affected.
func (s *Server) ObserveGameEventContexts(fn func(event string, x, y, z float64, ctx rules.GameEventContext)) {
	s.gameEventSeam.Store(&fn)
}

// SetOverworldBorder sets the overworld's border to a square of the given
// side around (x, z).
func (s *Server) SetOverworldBorder(x, z, size float64) {
	b := s.dimensionOfWorld(s.world).border
	b.SetCenter(x, z)
	b.SetSize(size)
}

// SetShulkerPeek sets the raw shell opening, 0 to 100, of the overworld
// shulker with the entity ID, as a Peek tag does.
func (s *Server) SetShulkerPeek(id int32, raw uint8) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil {
		ent.mob.SetShulkerPeek(raw)
	}
}

// ShulkerState reads the attach face and raw opening of an overworld shulker.
func (s *Server) ShulkerState(id int32) (face, peek uint8, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil && ent.mob.IsShulker() {
		return ent.mob.ShulkerFace(), ent.mob.ShulkerPeek(), true
	}
	return 0, 0, false
}

// MobBox is the box of the overworld mob with the entity ID.
func (s *Server) MobBox(id int32) (minX, minY, minZ, maxX, maxY, maxZ float64, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil {
		minX, minY, minZ, maxX, maxY, maxZ = ent.mob.BlastBox()
		return minX, minY, minZ, maxX, maxY, maxZ, true
	}
	return 0, 0, 0, 0, 0, 0, false
}

// DyeShulker gives the overworld shulker with the entity ID the DyeColor id
// given, as a summon's Color tag does.
func (s *Server) DyeShulker(id int32, color uint8) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil {
		ent.mob.SetShulkerColor(color)
	}
}

// MoveMob puts the overworld mob with the given entity ID at (x, z), its
// height kept: a test's way of having a mob walk off its chunk without
// waiting on its legs.
func (s *Server) MoveMob(id int32, x, z float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetPos(x, m.Y, z)
		}
	}
}

// SwimMob orders the overworld water-bound mob with the given entity ID to
// swim to (x, y, z) at the pace given; false when no path stands.
func (s *Server) SwimMob(id int32, x, y, z, pace float64) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent := s.mobByEntityIDLocked(s.world, id)
	return ent != nil && ent.mob.SwimTo(s.world, x, y, z, pace)
}

// PushMob adds the velocity to the overworld mob with the given entity ID, as
// a knockback would.
func (s *Server) PushMob(id int32, vx, vy, vz float64) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.Push(vx, vy, vz)
			return true
		}
	}
	return false
}

// SetEggTimer winds the grown chicken with the given entity ID's egg
// countdown down to the ticks given, the laying a test reads at the next
// tick's pass instead of the minutes a fresh countdown runs.
func (s *Server) SetEggTimer(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.EggTimer = ticks
			}
		}
	}
}

// SetScuteTimer winds the armadillo shedding countdown to the given ticks.
func (s *Server) SetScuteTimer(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.ScuteTimer = ticks
			}
		}
	}
}

// WindBobberBite puts the named online player's hook one water tick short
// of the bite — the lured wait spent, the approach at its last tick — so
// a test watches the bite the water lands without waiting the lured
// minutes out. It reports whether the player had a hook out at all.
func (s *Server) WindBobberBite(name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	s.bobbersMu.Lock()
	defer s.bobbersMu.Unlock()
	for _, it := range s.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			it.hook.TimeUntilLured = 0
			it.hook.TimeUntilHooked = 1
			return true
		}
	}
	return false
}

// BiteBobber puts the named online player's hook in the bite window with a
// loot roll seeded by seed, so a reel pays a catch the test can compute. It
// reports whether the player had a hook out at all.
func (s *Server) BiteBobber(name string, seed int64) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	s.bobbersMu.Lock()
	defer s.bobbersMu.Unlock()
	for _, it := range s.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			it.hook.Nibble = 1000
			it.hook.Rand = rand.New(rand.NewSource(seed))
			return true
		}
	}
	return false
}

// BobberLuckLure reports the named player's live hook's luck and lure
// ticks, the values the cast carried from the rod. It reports whether the
// player had a hook out at all.
func (s *Server) BobberLuckLure(name string) (luck, lureTicks int, ok bool) {
	c := s.playerClient(name)
	if c == nil {
		return 0, 0, false
	}
	s.bobbersMu.Lock()
	defer s.bobbersMu.Unlock()
	for _, it := range s.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			return it.hook.Luck, it.hook.LureTicks, true
		}
	}
	return 0, 0, false
}

// BobberOnGround reports whether the named player's live hook lies on the
// ground, for a test that has stopped the loop and steps it by hand. It
// reports whether the player had a hook out at all.
func (s *Server) BobberOnGround(name string) (onGround, ok bool) {
	c := s.playerClient(name)
	if c == nil {
		return false, false
	}
	s.bobbersMu.Lock()
	defer s.bobbersMu.Unlock()
	for _, it := range s.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			return it.hook.OnGround, true
		}
	}
	return false, false
}

// TickMobs runs the mob pass of the main loop n times — the unloading
// chunks' writes, the wakes and the advance — for a test that has stopped
// the loop.
// Entity writes finish before returning unless SetEntityWriteGate parks them.
func (s *Server) TickMobs(n int) {
	for range n {
		s.tickMobs()
	}
	if !s.entityWritesGated.Load() {
		for _, d := range s.allDims() {
			d.w.DrainSaves()
		}
	}
}

// TickKnots runs the leash-knot pass of the main loop n times — the
// survives poll and the showing — for a test that has stopped the loop.
func (s *Server) TickKnots(n int) {
	for range n {
		s.tickKnots()
	}
}

// KnotCount is how many leash knots stand, for the poll test that watches
// one survive to its interval and no further.
func (s *Server) KnotCount() int {
	s.knotsMu.Lock()
	defer s.knotsMu.Unlock()
	return len(s.knots)
}

// KnotAt is the entity ID of the leash knot on the overworld fence cell given, false
// for none.
func (s *Server) KnotAt(x, y, z int32) (int32, bool) {
	s.knotsMu.Lock()
	defer s.knotsMu.Unlock()
	if k := s.knotAtCellLocked(s.world, x, y, z); k != nil {
		return k.id, true
	}
	return 0, false
}

// TickClouds drives the given number of area effect cloud ticks by hand,
// the way TickMobs drives the mobs' own. With the main loop stopped, no
// tick runs a cloud beside the ones driven here; the pass a test counts
// the rhythm of is the one it makes.
func (s *Server) TickClouds(n int) {
	for range n {
		s.tickClouds()
	}
}

// KillMob kills the live mob with the entity ID given the way /kill does,
// leaving the body to lie for its twenty-tick death animation. It reports
// whether a live mob answered the ID.
func (s *Server) KillMob(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		if ent := s.mobByEntityIDLocked(dim.w, id); ent != nil {
			ent.mob.Kill()
			return true
		}
	}
	return false
}

// SetLightningOdds sets the denominator of a chunk's thunderstorm strike die;
// 1 strikes every ticking chunk every pass.
func (s *Server) SetLightningOdds(n int) { s.lightningOdds = n }

// SetSkeletonTrapOdds sets the chance a thunderstorm strike brings a
// skeleton trap; 0 is vanilla's share of the effective difficulty.
func (s *Server) SetSkeletonTrapOdds(p float64) { s.trapOdds = p }

// SpawnTrapHorse stands a skeleton trap horse in the overworld, as a strike
// would leave it, and returns its entity ID.
func (s *Server) SpawnTrapHorse(x, y, z float64) int32 {
	return s.spawnMobArriving(s.allDims()[0], entity.TypeSkeletonHorse, x, y, z, 0, nil, false, "", false,
		func(ent *mobEntity) { ent.mob.Horse.Trap = true })
}

// HorseTrap reads a horse's trap and tame flags.
func (s *Server) HorseTrap(id int32) (trap, tame, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent := s.mobByEntityIDLocked(s.world, id)
	if ent == nil {
		return false, false, false
	}
	return ent.mob.Horse.Trap, ent.mob.Horse.Tame, true
}

// StrikeLightning stands a lightning bolt at the point in the overworld, as a
// strike that found its target would.
func (s *Server) StrikeLightning(x, y, z float64) {
	s.spawnBolt(s.world, x, y, z, false, plugin.LightningCommand)
}

// BoltCount is how many lightning bolts stand in the worlds.
func (s *Server) BoltCount() int {
	s.boltMu.Lock()
	defer s.boltMu.Unlock()
	return len(s.bolts)
}

// ForceChunkInDimension force-loads the chunk of the named dimension, so its
// entities tick with no player there.
func (s *Server) ForceChunkInDimension(dimension string, cx, cz int32) {
	s.dimensionByName(dimension).w.SetChunkForced(world.ChunkPos{X: cx, Z: cz}, true)
}

// RollThunderInForcedChunk force-loads the overworld chunk and rolls n random
// tick passes over it alone, whoever is near.
func (s *Server) RollThunderInForcedChunk(cx, cz int32, n int) {
	s.world.SetChunkForced(world.ChunkPos{X: cx, Z: cz}, true)
	base, _ := s.world.Time()
	for i := 1; i <= n; i++ {
		s.randomTickChunks(s.dimensionOfWorld(s.world), base+int64(i), []world.ChunkPos{{X: cx, Z: cz}})
	}
}

// HurtMobByWorld lands a hurt of the damage type on the overworld mob with
// the given entity ID, as a world source would.
func (s *Server) HurtMobByWorld(id int32, amount float64, typ any) {
	damageType := testDamageType(typ)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.HurtByWorld(amount, damageType)
		}
	}
}

// HurtMobByThrownHarming lands a splash potion of harming's damage on the
// overworld mob with the given entity ID and reports whether it landed.
func (s *Server) HurtMobByThrownHarming(id int32, amount float64) (landed bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			landed, _ = m.HurtByInstantMagic(amount, m.X+1, m.Z, 0, false, false, true)
		}
	}
	return landed
}

// TickOminousItemSpawners runs the ominous item spawner pass of the main loop
// n times for a test that has stopped the loop.
func (s *Server) TickOminousItemSpawners(n int) {
	for range n {
		s.tickOminousItemSpawners()
	}
}

// OminousLootRolls counts the loot rolls ominous trial spawners have made.
func (s *Server) OminousLootRolls() int { return s.ominousRolls }

// TickBolts runs the lightning pass of the main loop n times for a test that
// has stopped the loop.
func (s *Server) TickBolts(n int) {
	for range n {
		s.tickBolts()
	}
}

// TickFallings runs the falling-block pass of the main loop n times — the
// landings, the drops and the movements — for a test that has stopped the
// loop.
func (s *Server) TickFallings(n int) {
	for range n {
		s.tickFallingBlocks()
	}
}

// FlushMobs writes every live mob of every dimension into its chunk's entity
// files at once, the whole-herd snapshot the auto-save and the shutdown
// take. It is what a test calls to be sure a mob is on disk before a server
// is shut down or a world is looked at, without waiting out the interval the
// real save runs on.
func (s *Server) FlushMobs() { s.saveMobs() }

// HoldChunk keeps the chunk containing the given block coordinates loaded,
// generating it when it has never been held, until the release it returns is
// called. It is how a test puts a mob somewhere no player is: the mob's tick
// runs only while its chunk is held and entity-ticks, and a chunk a player has walked away
// from is given up to its entity file within the tick.
// HoldChunkInDimension is the dimension-taking form.
func (s *Server) HoldChunk(x, z int32) (release func()) {
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	return s.world.HoldEntityTicking(pos)
}

// TouchChunk marks the chunk containing the given block coordinates dirty,
// so the save a shutdown runs writes the chunk though nothing in it
// changed — the way a test of a saved chunk gets the chunk into its region
// file at all.
func (s *Server) TouchChunk(x, z int32) {
	s.world.Touch(x, 0, z)
}

// HoldChunkInDimension keeps the named dimension's chunk containing the
// given block coordinates loaded until the release it returns is called,
// the dimension-taking form of HoldChunk.
func (s *Server) HoldChunkInDimension(dimension string, x, z int32) (release func()) {
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	return s.dimensionByName(dimension).w.HoldEntityTicking(pos)
}

// RelightAt recomputes the light in the box around a column, the way a
// block change's own path runs it on the worker pool. It is how a test that
// roofed a zombie in also darkens the sky light the daylight burn reads —
// SetBlock alone changes the block and leaves the light standing.
func (s *Server) RelightAt(x, z int32) { s.world.Relight(x, z) }

// RecordRelights runs record for every block-change batch with the number of
// relight boxes the batch booked and the changes those boxes cover: how a
// test pins the per-chunk billing without the light pool's timing.
func (s *Server) RecordRelights(record func(booked int, changes []world.BlockChange)) {
	s.relightSeam = record
}

// RelightsRun is how many relights the server has submitted, those skipped
// for an unmarked box not counted.
func (s *Server) RelightsRun() int64 { return s.relightsRun.Load() }

// ReconnectShapesAt runs the world's reconnect pass at one cell and tells
// everyone its writes, as one shape pass.
func (s *Server) ReconnectShapesAt(x, y, z int32) { s.reconnectShapes(s.world, x, y, z) }

// MobCount is how many mobs of every dimension are standing in the worlds,
// for the tests that check a chunk's animals came back or went away, which
// the wire alone cannot always be made to say in time.
func (s *Server) MobCount() int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	n := 0
	for _, dim := range s.allDims() {
		n += len(dim.mobs.herd)
	}
	return n
}

// TestMob is one mob standing in a world, as a test reads it: the kind it is
// and where its feet are.
type TestMob struct {
	Kind string
	// Celebrating is Raider.IS_CELEBRATING.
	Celebrating bool
	X, Y, Z     float64
	ID          int32
	Baby        bool
	// Sheared is the sheep's fleece flag, the one a grazing grows back
	// and a test watches for the bite.
	Sheared bool
	// Powered is the creeper's charged flag.
	Powered bool
	// Frozen is the body's ticks frozen, which a test watches the powder
	// snow climb and the save carry home.
	Frozen int
	// Age is vanilla's AgeableMob.age: negative while the body grows, the
	// positive breed cooldown an adult counts down, and InLove the love
	// mode's ticks left.
	Age          int
	InLove       int
	UUID         string
	Yaw, HeadYaw float32
	Pitch        float32
	NoAI         bool
	Name         string
	// Variant is a cow's, pig's or chicken's finalize-chosen variant
	// ("minecraft:cold"), "" for the kinds without one.
	Variant string
	// Mushroom is a mooshroom's variant, "red" or "brown".
	Mushroom string
	// Vehicle is the entity ID of the mob this one rides, 0 for a walking
	// body; ChickenJockey is the mark a jockey hen carries.
	Vehicle       int32
	ChickenJockey bool
	// Standing is a polar bear up on its hind legs.
	Standing bool
	// Resting is a bat hanging upside down.
	Resting bool
	// CreakingFrozen, CreakingActive and CreakingBound are a creaking's
	// synched flags: held still by a gaze, hunting, and tied to a heart.
	CreakingFrozen, CreakingActive, CreakingBound bool
	// HorseFlags is an equine's synched flag byte (protocol.HorseFlag*).
	HorseFlags byte
}

// Mobs lists every mob of every dimension, the kinds and the places, for the
// tests that check what a spawn pass stood up or where the world keeps its
// monsters, which the wire cannot be made to say in full — a client is shown
// only what its own render distance reaches.
func (s *Server) Mobs() []TestMob {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var mobs []TestMob
	for _, dim := range s.allDims() {
		h := dim.mobs
		for _, m := range h.herd {
			vehicle := int32(0)
			if m.Vehicle != nil {
				vehicle = m.Vehicle.ID
			}
			tm := TestMob{Kind: m.Type.Name(), UUID: h.book[m].uuid.String(), X: m.X, Y: m.Y, Z: m.Z, ID: m.ID, Baby: m.IsBaby(),
				Sheared: m.Sheared, Powered: m.Powered,
				Frozen: m.TicksFrozen,
				Age:    m.BabyAge, InLove: m.InLove,
				Yaw: m.Yaw, HeadYaw: m.HeadYaw, Pitch: m.Pitch, NoAI: m.NoAI,
				Variant: m.Variant, Mushroom: m.Mushroom,
				Vehicle: vehicle, ChickenJockey: m.ChickenJockey, Standing: m.Standing, Resting: m.Resting, Celebrating: m.Celebrating}
			if custom, _, ok := mobCustomName(h.book[m]); ok {
				tm.Name = custom.Text
			}
			if entity.IsEquine(m.Type) {
				tm.HorseFlags = m.HorseFlags(protocol.HorseFlagTame, protocol.HorseFlagBred, protocol.HorseFlagEating, protocol.HorseFlagStanding, protocol.HorseFlagMouthOpen)
			}
			if m.IsCreaking() {
				v := m.CreakingView()
				tm.CreakingFrozen, tm.CreakingActive, tm.CreakingBound = v.Frozen, v.Active, v.Bound
			}
			mobs = append(mobs, tm)
		}
	}
	return mobs
}

// GrowNaturalMobsForTest grows the overworld chunk's first-generation animals
// the way its first wake would, without standing them in the world, so a test
// can read each animal's finalize against a chunk it has edited by hand.
func (s *Server) GrowNaturalMobsForTest(pos world.ChunkPos) []TestMob {
	herd := generateNaturalMobs(s, s.world, pos)
	mobs := make([]TestMob, 0, len(herd))
	for _, e := range herd {
		mobs = append(mobs, TestMob{
			Kind: e.mob.Type.Name(), X: e.mob.X, Y: e.mob.Y, Z: e.mob.Z,
			Variant: e.mob.Variant,
		})
	}
	return mobs
}

// MobPassengerIDs is the entity IDs of the mobs riding the live mob the ID
// names, in mounting order, for the tests that check what a jockey linked;
// the boat seats have BoatPassengerIDs of their own.
func (s *Server) MobPassengerIDs(id int32) []int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.ID == id {
				return m.PassengerIDs()
			}
		}
	}
	return nil
}

// MobVehicleID is the entity ID of the live mob's vehicle, or 0 when unseated.
func (s *Server) MobVehicleID(id int32) int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.ID != id {
				continue
			}
			if m.Vehicle != nil {
				return m.Vehicle.ID
			}
			ent := dim.mobs.book[m]
			if ent.vehicle != nil {
				return ent.vehicle.boat.ID
			}
			if ent.cart != nil {
				return ent.cart.cart.ID
			}
		}
	}
	return 0
}

// LinkMobsForTest seats the live mob the second ID names on the live mob the
// first names, the link a jockey's finalize would make, so a test can stand a
// pair up without rolling the one-in-a-hundred dice.
func (s *Server) LinkMobsForTest(vehicleID, riderID int32) bool {
	s.mobsMu.Lock()
	var vehicle, rider *mobEntity
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			switch m.ID {
			case vehicleID:
				vehicle = dim.mobs.book[m]
			case riderID:
				rider = dim.mobs.book[m]
			}
		}
	}
	s.mobsMu.Unlock()
	if vehicle == nil || rider == nil {
		return false
	}
	s.linkMobPassengers(vehicle, rider)
	return true
}

// MobPairingIDsForTest is the play packet IDs of the pairing a viewer is sent
// for the live mob the ID names, in order, for the tests that check what a
// late viewer learns about a rider.
func (s *Server) MobPairingIDsForTest(id int32) []int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.ID != id {
				continue
			}
			packets := mobPairing(dim.mobs.book[m])
			ids := make([]int32, len(packets))
			for i, p := range packets {
				ids[i] = p.id
			}
			return ids
		}
	}
	return nil
}

// GrazeGroundAt runs the world's half of a sheep's bite against the
// overworld block at the given position, the way the MobAte event does, so a
// test can drive a bite deterministically rather than rolling the goal's
// dice.
func (s *Server) GrazeGroundAt(x, y, z int32) {
	s.mobGrazedGround(mobGraze{w: s.world, x: x, y: y, z: z})
}

// MobAngerTicks is the persistent anger the live mob with the given ID has
// left, in ticks; 0 for a calm mob or an unknown ID.
func (s *Server) MobAngerTicks(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				ticks, _, _ := m.Anger()
				return ticks
			}
		}
	}
	return 0
}

// MobBeeFlags is the flag byte (roll 2, stung 4, nectar 8) of the live bee
// with the given entity ID, false for a mob not in the herd.
func (s *Server) MobBeeFlags(id int32) (uint8, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.BeeFlags(), true
		}
	}
	return 0, false
}

// StoreHiveBee puts a bee with the body tag given into the hive at the
// position, as a bee that has just flown in, for the tests of what storing a
// bee sets off.
func (s *Server) StoreHiveBee(x, y, z int32, body nbt.Compound) {
	s.storeHiveBee(hiveStay{w: s.world, x: x, y: y, z: z, tag: body})
}

// MobBeeMemory is the hive, flower and countdowns of the live bee with the
// given entity ID, false for a mob not in the herd.
func (s *Server) MobBeeMemory(id int32) (entity.BeeMemory, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.BeeMemory(), true
		}
	}
	return entity.BeeMemory{}, false
}

// MobTargetID is the entity ID of the body the live mob with the given ID
// has set its mind on, or 0 for a mob hunting nobody, for the tests that
// watch one mob turn on another after a blow.
func (s *Server) MobTargetID(id int32) int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				return m.TargetID
			}
		}
	}
	return 0
}

// SetMobPatrolling marks the overworld mob with the given ID as a patrol
// member, as the patrol spawner leaves it.
func (s *Server) SetMobPatrolling(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.Patrolling = true
		}
	}
}

// MobAggressive reports Mob.isAggressive of the live mob with the given ID.
func (s *Server) MobAggressive(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				return m.Aggressive
			}
		}
	}
	return false
}

// MobNavSpeedFactor is the speed factor of the walk the live mob with the
// given ID, or the vehicle it rides, is following.
func (s *Server) MobNavSpeedFactor(id int32) float64 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				return m.NavSpeedFactor()
			}
		}
	}
	return 0
}

// ShootMobArrowAt flies a plain arrow owned by the live mob with the first
// entity ID from that mob's eyes at the live mob with the second, and returns
// the arrow's entity ID. It is how a test gives a monster's blow without
// waiting on a bow's dice: the shaft is the mob's own, so the hurt it lands
// names that mob as the attacker.
func (s *Server) ShootMobArrowAt(shooterID, targetID int32) int32 {
	s.mobsMu.Lock()
	var (
		shooter, victim *entity.Mob
		w               *world.World
		ownerUUID       uuid.UUID
	)
	for _, dim := range s.allDims() {
		for m, e := range dim.mobs.book {
			switch m.ID {
			case shooterID:
				shooter, w, ownerUUID = m, e.w, e.uuid
			case targetID:
				victim = m
			}
		}
	}
	if shooter == nil || victim == nil {
		s.mobsMu.Unlock()
		return 0
	}
	_, _, _, _, shooterTop, _ := shooter.BlastBox()
	ox, oy, oz := shooter.X, shooter.Y+(shooterTop-shooter.Y)*0.85, shooter.Z
	_, victimBase, _, _, victimTop, _ := victim.BlastBox()
	tx, ty, tz := victim.X, (victimBase+victimTop)/2, victim.Z
	s.mobsMu.Unlock()

	dx, dy, dz := tx-ox, ty-oy, tz-oz
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if distance == 0 {
		return 0
	}
	const speed = 3.0
	vx, vy, vz := dx/distance*speed, dy/distance*speed, dz/distance*speed
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(w, ox, oy, oz, vx, vy, vz, yaw, pitch, false,
		entity.ArrowBaseDamage, entity.PickupDisallowed, shooterID, ownerUUID,
		protocol.ItemStack{}, 0)
}

// ShootMobArrowFrom flies a plain arrow owned by the live mob with the given
// entity ID from the given point at the given velocity, the mob's own shaft
// where a test wants it against something that is not a body.
func (s *Server) ShootMobArrowFrom(shooterID int32, x, y, z, vx, vy, vz float64) int32 {
	s.mobsMu.Lock()
	var (
		w         *world.World
		ownerUUID uuid.UUID
		found     bool
	)
	for _, dim := range s.allDims() {
		for m, e := range dim.mobs.book {
			if m.ID == shooterID {
				w, ownerUUID, found = e.w, e.uuid, true
			}
		}
	}
	s.mobsMu.Unlock()
	if !found {
		return 0
	}
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(w, x, y, z, vx, vy, vz, yaw, pitch, false,
		entity.ArrowBaseDamage, entity.PickupDisallowed, shooterID, ownerUUID,
		protocol.ItemStack{}, 0)
}

// SetDayTime sets the overworld clock's time of day directly, without going
// through the /time command, so a test can arrange a midnight or a noon
// without an operator to run the command for it.
func (s *Server) SetDayTime(ticks int64) { s.world.SetClockTime(world.OverworldClock, ticks) }

// SetSpawning turns the natural spawn pass on or off. The harness turns it
// off, so that the tests that are not about the spawn see a world where
// nothing turns up unasked, the way they were written; a test of the spawn
// itself turns it back on, SpawnPass along with it.
func (s *Server) SetSpawning(on bool) { s.spawnDisabled.Store(!on) }

// SetMoveChecks turns the moved-too-quickly and moved-wrongly checks on or
// off. The harness turns them off, so the tests that are not about them can
// put a body anywhere with one move, the way they were written; a test of
// the checks turns them back on.
func (s *Server) SetMoveChecks(on bool) { s.moveChecksOff = !on }

// SetAllowFlight sets allow-flight. The harness turns it on, so a test that
// leaves a body in the air for long is not kicked; a test of the kick turns
// it off.
func (s *Server) SetAllowFlight(on bool) { s.allowFlight.Store(on) }

// BlockLightAt returns the block light level at the given coordinates, or 0
// where the chunk is not loaded, for the tests that check what light a spawn
// cell stands in.
func (s *Server) BlockLightAt(x, y, z int32) uint8 { return s.world.BlockLightAt(x, y, z) }

// BlockLightInDimension returns the stored block light in the named dimension.
func (s *Server) BlockLightInDimension(dimension string, x, y, z int32) uint8 {
	return s.dimensionByName(dimension).w.BlockLightAt(x, y, z)
}

// SkyLightAt returns the sky light level at the given coordinates, or 0 where
// the chunk is not loaded, for the tests that check what sky light a placed
// block passes.
func (s *Server) SkyLightAt(x, y, z int32) uint8 { return s.world.SkyLightAt(x, y, z) }

// SpawnPass runs one tick's natural spawn pass with the pass's own dice
// replaced by a random source seeded by the value given, which is what makes
// a pass reproducible: the same world, the same seed, the same spawns. A test
// that has stopped the main loop drives the passes itself, and gets the same
// world every run.
func (s *Server) SpawnPass(seed int64) {
	s.spawnDisabled.Store(false)
	s.spawnRng.Seed(seed)
	s.parkPatrols()
	// The monster tests run their passes at a game time that is not the
	// four-hundredth tick, so the creature pass stays out of their counts;
	// the tests of the creatures drive SpawnCreaturePass, which moves the
	// age onto the multiple the gate reads.
	s.world.SetWorldAge(399)
	s.tickNaturalSpawning()
}

// NextSpawnRandom returns the next draw after a manually driven spawn pass.
// The caller must hold or stop the tick loop.
func (s *Server) NextSpawnRandom() int64 { return s.spawnRng.Int63() }

// SpawnCreaturePass runs one tick of spawning at the four-hundredth game
// tick, the tick the persistent categories join the walk on: the world's age
// is set to a multiple of four hundred, so both passes run — the monsters'
// and, under its own cap and grass-and-light rule, the creatures' — with the
// dice of the seed given.
func (s *Server) SpawnCreaturePass(seed int64) {
	s.spawnDisabled.Store(false)
	s.spawnRng.Seed(seed)
	s.parkPatrols()
	s.world.SetWorldAge(400)
	s.tickNaturalSpawning()
}

// LootSeed replaces the loot tables' dice with a random source seeded by the
// value given, which is what makes a drop reproducible: the same breaks, the
// same seed, the same drops. The loot dice roll for every block break and
// every death after the seed is set, in the order the rolls happen, so a
// test that plants a seed before its first break sees the same chances land
// every run.
func (s *Server) LootSeed(seed int64) {
	s.lootRng.Seed(seed)
}

// DropSeed replaces the dice a container's contents spill with by one seeded
// with the value given, the loot seed's own deal: the same seed spills the
// same stacks to the same places, so a test of what a broken chest leaves
// behind sees one spill and not many.
func (s *Server) DropSeed(seed int64) {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	s.dropRng.SetSeed(seed)
}

// XpSeed is the named online player's enchantment seed, the number their
// file keeps under XpSeed and the table's offers are drawn from.
func (s *Server) XpSeed(name string) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	return c.xpSeed.Load()
}

// SetXpSeed plants the enchantment seed under the named online player, so
// that a test can put the table's draws where they are known: the same
// seed, the same shelves, the same offers, the same enchantment.
func (s *Server) SetXpSeed(name string, seed int32) {
	if c := s.playerClient(name); c != nil {
		c.xpSeed.Store(seed)
	}
}

// CountBookshelves counts the shelves that feed the table at the given
// position, the way opening its window would, so a test of the ring's
// geometry does not have to read the offers back out of the wire.
func (s *Server) CountBookshelves(x, y, z int32) int {
	return rules.CountBookshelves(s.world, x, y, z)
}

// SetExperienceLevels puts the named online player at the given level, the
// way /xp would without the command, so a test can stand a player under the
// table's charge with levels to pay from.
func (s *Server) SetExperienceLevels(name string, levels int32) {
	if c := s.playerClient(name); c != nil {
		c.player.UpdateVitals(func(v *player.Vitals) { rules.SetExperienceLevels(v, int(levels)) })
	}
}

// ExperienceLevels is the named online player's level, read back the way
// the wire tells it: the set-experience packet's own number.
func (s *Server) ExperienceLevels(name string) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	return int32(c.player.Vitals().ExperienceLevel)
}

// SpawnStuckArrow puts an arrow already in the ground at the given position,
// pickup-ruled as the named rule allows — "allowed", "disallowed" or
// "creative" — owned by nobody and moving not at all. It is how a test
// stands a stuck shaft where a shot would leave one without waiting on a
// skeleton's dice: the pickup rules the arrows carry are the thing under
// test, not the luck of where a missed arrow lands.
func (s *Server) SpawnStuckArrow(pickup string, x, y, z float64) int32 {
	return s.spawnStuckArrow(pickup, x, y, z, protocol.EntityTypeArrow)
}

// SpawnStuckSpectralArrow is SpawnStuckArrow for a spectral shaft, for the
// tests of the entity type it saves and reloads as.
func (s *Server) SpawnStuckSpectralArrow(pickup string, x, y, z float64) int32 {
	return s.spawnStuckArrow(pickup, x, y, z, protocol.EntityTypeSpectralArrow)
}

func (s *Server) spawnStuckArrow(pickup string, x, y, z float64, entityType int32) int32 {
	var rule entity.ArrowPickup
	switch pickup {
	case "allowed":
		rule = entity.PickupAllowed
	case "creative":
		rule = entity.PickupCreativeOnly
	default:
		rule = entity.PickupDisallowed
	}
	identity := uuid.New()
	id := s.nextEntity()
	it := &arrowEntity{
		arrow: entity.NewArrow(id, x, y, z, 0, 0, 0,
			false, entity.ArrowBaseDamage, rule, 0, s.entitySeed(id)),
		uuid:       identity,
		w:          s.world,
		entityType: entityType,
	}
	it.arrow.InGround = true
	// The block the shaft stands out of, what a shot's onHitBlock would
	// have left as lastState.
	if name, props, ok := arrowLastBlock(s.world, it.arrow); ok {
		if state, found := registry.FindState(name, registry.ParseStateProps(props)); found {
			it.arrow.LastState = state
		}
	}
	s.registerArrow(it)
	return it.arrow.ID
}

// ArrowTypeName is the entity type the selector's snapshot gives the arrow
// with the given entity ID, the name @e[type=...] matches against.
func (s *Server) ArrowTypeName(id int32) string {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			if e := s.arrowSnapshot(it, nil); e != nil {
				return e.EntityType()
			}
		}
	}
	return ""
}

// SpawnStuckTrident puts a thrown trident already in the ground at the
// given position, its blow dealt, its stack carrying the loyalty level
// and the wear of a spent throw, and owned by nobody — the shape the
// tests of what the entity files keep stand where a throw would leave
// one without waiting on a charge.
func (s *Server) SpawnStuckTrident(x, y, z float64, loyalty, wear int32) int32 {
	id := int32(registry.ItemTrident)
	loyaltyEnch, _ := registry.EnchantmentID("minecraft:loyalty")
	stack := protocol.ItemStack{
		ItemID:       id,
		Count:        1,
		Damage:       wear,
		Enchantments: map[int32]int32{loyaltyEnch: loyalty},
	}
	identity := uuid.New()
	eid := s.nextEntity()
	it := &arrowEntity{
		arrow:      entity.NewTrident(eid, x, y, z, 0, 0, 0, entity.PickupAllowed, 0, stack, s.entitySeed(eid)),
		uuid:       identity,
		w:          s.world,
		entityType: protocol.EntityTypeTrident,
	}
	it.arrow.InGround = true
	it.arrow.Foil = tridentFoil(&stack)
	it.arrow.RestoreDealtDamage(true)
	// The block the shaft stands out of, what a throw's onHitBlock would
	// have left as lastState.
	if name, props, ok := arrowLastBlock(s.world, it.arrow); ok {
		if state, found := registry.FindState(name, registry.ParseStateProps(props)); found {
			it.arrow.LastState = state
		}
	}
	s.registerArrow(it)
	return it.arrow.ID
}

// ShootArrow flies a plain, ownerless arrow from the given position at the
// given velocity, and returns its entity ID.
func (s *Server) ShootArrow(x, y, z, vx, vy, vz float64) int32 {
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(s.world, x, y, z, vx, vy, vz, yaw, pitch, false, entity.ArrowBaseDamage,
		entity.PickupDisallowed, 0, uuid.Nil, protocol.ItemStack{}, 0)
}

// SeedArrow replaces an arrow's random stream so its impact pitch can be checked.
func (s *Server) SeedArrow(id int32, seed int64) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			rolls := entity.NewRolls(seed)
			it.arrow.Rand, it.arrow.RandSrc = rolls.Rand, rolls.Src
		}
	}
}

// SeedCritArrow is SeedArrow for an arrow that rolls the critical damage bonus.
func (s *Server) SeedCritArrow(id int32, seed int64) {
	s.SeedArrow(id, seed)
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.Crit = true
		}
	}
}

// ShootPiercingArrow flies an ownerless arrow with the given pierce level
// and, when crossbow is set, the crossbow's hit sound.
func (s *Server) ShootPiercingArrow(x, y, z, vx, vy, vz float64, level int32, crossbow bool) int32 {
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	it := newArrowEntity(s, s.world, x, y, z, vx, vy, vz, yaw, pitch, false, entity.ArrowBaseDamage,
		entity.PickupDisallowed, 0, uuid.Nil, protocol.ItemStack{}, 0)
	it.arrow.PierceLevel = level
	if crossbow {
		it.arrow.HitSound = crossbowHitSound
	}
	s.registerArrow(it)
	return it.arrow.ID
}

// ShootArrowFromWeapon flies an ownerless arrow fired from the given weapon,
// for the tests of the enchantments a bow's shot carries on its hit.
func (s *Server) ShootArrowFromWeapon(x, y, z, vx, vy, vz float64, weapon protocol.ItemStack) int32 {
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(s.world, x, y, z, vx, vy, vz, yaw, pitch, false, entity.ArrowBaseDamage,
		entity.PickupDisallowed, 0, uuid.Nil, weapon, 0)
}

// ShootPlayerArrow flies a plain arrow owned by the named online player from
// the given position at the given velocity, and returns its entity ID. The
// shaft is the player's own, so the pvp rule can tell it from a mob's.
func (s *Server) ShootPlayerArrow(name string, x, y, z, vx, vy, vz float64) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(c.theWorld(), x, y, z, vx, vy, vz, yaw, pitch, false, entity.ArrowBaseDamage,
		entity.PickupDisallowed, c.player.EntityID, c.player.UUID, protocol.ItemStack{}, 0)
}

// ThrowPlayerWindCharge flies a wind charge owned by the named online player
// from the given position along the given look, and returns its entity ID.
func (s *Server) ThrowPlayerWindCharge(name string, x, y, z float64, yaw, pitch float32) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	return s.spawnThrown(entity.ThrownWindCharge, c.theWorld(), x, y, z, yaw, pitch, 0, 0, 0,
		c.player.EntityID, c.player.UUID, 1, protocol.ItemStack{ItemID: int32(registry.ItemWindCharge), Count: 1})
}

// SpawnPlayerFirework flies a plain rocket owned by the named online player
// from the given position, and returns its entity ID.
func (s *Server) SpawnPlayerFirework(name string, x, y, z float64) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	item := int32(registry.ItemFireworkRocket)
	it := s.spawnFirework(c.theWorld(), protocol.ItemStack{ItemID: item, Count: 1}, x, y, z,
		c.player.EntityID, c.player.UUID, 0)
	return it.rocket.ID
}

// ShootMobArrow flies a plain arrow owned by the live mob with the given
// entity ID from the given position at the given velocity, and returns its
// entity ID. The shaft is the mob's own, so the pvp rule leaves it to land.
func (s *Server) ShootMobArrow(ownerID int32, x, y, z, vx, vy, vz float64) int32 {
	s.mobsMu.Lock()
	var (
		w         *world.World
		ownerUUID uuid.UUID
	)
	for _, dim := range s.allDims() {
		for m, e := range dim.mobs.book {
			if m.ID == ownerID {
				w, ownerUUID = e.w, e.uuid
			}
		}
	}
	s.mobsMu.Unlock()
	if w == nil {
		return 0
	}
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnArrow(w, x, y, z, vx, vy, vz, yaw, pitch, false, entity.ArrowBaseDamage,
		entity.PickupDisallowed, ownerID, ownerUUID, protocol.ItemStack{}, 0)
}

// ShootTrident flies an ownerless thrown trident from the given position at
// the given velocity, and returns its entity ID.
func (s *Server) ShootTrident(x, y, z, vx, vy, vz float64) int32 {
	id := int32(registry.ItemTrident)
	yaw, pitch := entity.ArrowAngles(vx, vy, vz)
	return s.spawnTrident(s.world, x, y, z, vx, vy, vz, yaw, pitch,
		entity.PickupDisallowed, 0, uuid.Nil, protocol.ItemStack{ItemID: id, Count: 1})
}

// AgeArrow sets how many ticks the arrow with the given entity ID has lain
// in the ground, for the tests of its despawn.
func (s *Server) AgeArrow(id int32, life int) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.Life = life
		}
	}
}

// TipArrow gives the arrow with the given entity ID a poison tip.
func (s *Server) TipArrow(id int32) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.Effect = entity.ArrowEffect{Name: "minecraft:poison", Ticks: 100}
		}
	}
}

// IgniteArrow sets the arrow with the given entity ID burning for the given
// ticks, as a Flame bow's shot is.
func (s *Server) IgniteArrow(id int32, ticks int) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.FireTicks = ticks
		}
	}
}

// MoveArrow puts the arrow with the given entity ID at the given position in
// its own dimension, for the tests that stand a shaft in a portal, move it
// out and back, and watch the projectile cooldown decide the crossing.
func (s *Server) MoveArrow(id int32, x, y, z float64) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.SetPos(x, y, z)
		}
	}
}

// SetArrowPickup re-rules the arrow with the given entity ID, the names
// the SpawnStuckArrow ones, for the tests of a throw whose pickup changed
// after it left the hand.
func (s *Server) SetArrowPickup(id int32, pickup string) {
	var rule entity.ArrowPickup
	switch pickup {
	case "allowed":
		rule = entity.PickupAllowed
	case "creative":
		rule = entity.PickupCreativeOnly
	default:
		rule = entity.PickupDisallowed
	}
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			it.arrow.Pickup = rule
		}
	}
}

// ArrowAlive reports whether the arrow with the given entity ID is still in
// the world, for the tests that check a pickup did not take a skeleton's
// arrow away with it.
func (s *Server) ArrowAlive(id int32) bool {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id && !it.arrow.Dead {
			return true
		}
	}
	return false
}

// SpawnOrb lays an experience orb worth the given value at the given
// position, moving not at all, and returns the entity ID the packets address
// it by. It is how a test stands an orb where no source would leave one: the
// merge rule the orbs run on being the thing under test, not the luck of
// which entity IDs a death's drops happened to draw.
func (s *Server) SpawnOrb(x, y, z float64, value int) int32 {
	it := s.spawnOrb(s.world, x, y, z, 0, 0, 0, value)
	return it.orb.ID
}

// SpawnOrbMotion is SpawnOrb for an orb that already moves, so a test can
// watch what the ground under it does to its slide.
func (s *Server) SpawnOrbMotion(x, y, z float64, value int, vx, vy, vz float64) int32 {
	it := s.spawnOrb(s.world, x, y, z, vx, vy, vz, value)
	return it.orb.ID
}

// AwardExperienceAt runs an award of the total at a position, the
// ExperienceOrb.award path a kill or a death takes, so a test can aim the
// award's own merge where it wants without arranging the drops that would
// normally carry it there.
func (s *Server) AwardExperienceAt(x, y, z float64, total int) {
	s.spawnOrbs(s.world, x, y, z, total)
}

// SpawnTNT stands a primed TNT at the given position, burning for the fuse
// given — the full eighty where a test wants the vanilla default, anything
// shorter where it does not want to wait the four seconds out — moving not
// at all and owned by nobody, the way a summon's body is. It returns the
// entity ID the packets address it by. It is how a test stands a lit block
// where no fuse would have lit one: the explosion and the falling being the
// things under test, not the luck of who happened to be holding the steel.
func (s *Server) SpawnTNT(x, y, z float64, fuse int) int32 {
	it := s.spawnTNT(s.world, x, y, z, fuse, 0, "", uuid.Nil, false)
	if it == nil {
		return 0
	}
	return it.tnt.ID
}

// SpawnBodiesForTest is the spawn's body list for the overworld's players,
// for the tests that check which entities obstruct a spawn.
func (s *Server) SpawnBodiesForTest(players []entity.SpawnPlayer) []entity.SpawnBody {
	return s.spawnBodies(s.dimensionOfWorld(s.world), players)
}

// PlayerSpawnBodiesForTest is the spawn's body list for the overworld's
// connected players, as the spawn pass builds it.
func (s *Server) PlayerSpawnBodiesForTest() []entity.SpawnBody {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	w := s.world
	return s.spawnBodies(s.dimensionOfWorld(w), spawnPlayersOf(clients, w))
}

// SpawnTNTInDimension is SpawnTNT's dimension-taking form, for the tests
// that watch one world's bodies leave another world's placements alone.
func (s *Server) SpawnTNTInDimension(dimension string, x, y, z float64, fuse int) int32 {
	it := s.spawnTNT(s.dimensionByName(dimension).w, x, y, z, fuse, 0, "", uuid.Nil, false)
	if it == nil {
		return 0
	}
	return it.tnt.ID
}

// SpawnPlayerTNT stands a primed TNT the named online player lit — the blast
// is theirs, so its damage type and cause ride the owner — burning for the
// fuse given. It returns the entity ID the packets address it by.
func (s *Server) SpawnPlayerTNT(name string, x, y, z float64, fuse int) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	it := s.spawnTNT(s.world, x, y, z, fuse, c.player.EntityID, c.player.Name, c.player.UUID, false)
	if it == nil {
		return 0
	}
	return it.tnt.ID
}

// SpawnFallingBlock stands a falling block carrying the state given in the
// cell at (x, y, z), at rest and moving not at all, the way
// FallingBlockEntity.fall stands one up when its support goes. It returns
// the entity ID the packets address it by.
func (s *Server) SpawnFallingBlock(x, y, z int32, state uint16) int32 {
	it := s.spawnFallingBlock(s.world, world.FallingSpawn{X: x, Y: y, Z: z, State: state})
	if it == nil {
		return 0
	}
	return it.fb.ID
}

// SpawnBoat stands a boat of the named kind at the given position, facing
// no way in particular and driven by nobody, the way a laid one begins —
// the summons the Use Item wire path stands in for when the thing under
// test is the boat's own carrying, not the laying of it. It returns the
// entity ID the packets address it by, or zero where the kind is no boat.
func (s *Server) SpawnBoat(x, y, z float64, kind string) int32 {
	it := s.spawnBoat(s.world, kind, x, y, z, 0, customName{})
	if it == nil {
		return 0
	}
	return it.boat.ID
}

// SpawnBoatInDimension is SpawnBoat's dimension-taking form, for the tests
// that watch one world's blast leave another world's hulls alone.
func (s *Server) SpawnBoatInDimension(dimension string, x, y, z float64, kind string) int32 {
	it := s.spawnBoat(s.dimensionByName(dimension).w, kind, x, y, z, 0, customName{})
	if it == nil {
		return 0
	}
	return it.boat.ID
}

// BoatCount is how many boats stand in the worlds still, the ones a test
// laid and none a test has taken out again.
func (s *Server) BoatCount() int {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	n := 0
	for _, it := range s.boats {
		if !it.boat.Dead {
			n++
		}
	}
	return n
}

// BoatCountInDimension is BoatCount for the named dimension alone, for the
// tests that check a crossing carried one.
func (s *Server) BoatCountInDimension(dimension string) int {
	w := s.dimensionByName(dimension).w
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	n := 0
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == w {
			n++
		}
	}
	return n
}

// BoatYawsInDimension lists the yaws of the boats standing in the named
// dimension, for the tests that check a crossing turned one.
func (s *Server) BoatYawsInDimension(dimension string) []float32 {
	w := s.dimensionByName(dimension).w
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	var out []float32
	for _, it := range s.boats {
		if !it.boat.Dead && it.w == w {
			out = append(out, it.boat.Yaw)
		}
	}
	return out
}

// BoatIDs lists the entity IDs of the boats standing in the worlds still,
// for the tests that hand-drive the boat tick rather than read the spawn.
func (s *Server) BoatIDs() []int32 {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	ids := make([]int32, 0, len(s.boats))
	for _, it := range s.boats {
		if !it.boat.Dead {
			ids = append(ids, it.boat.ID)
		}
	}
	return ids
}

// TickBoats runs the main loop's boat pass n times over, exactly as the main
// loop's own pass runs it, the way TickEntities carries the other kinds: the
// main loop having been stopped, no tick of its own races the ones driven
// here.
func (s *Server) TickBoats(n int) {
	for range n {
		s.tickBoats()
	}
}

// BoatState is where the boat with the given entity ID stands, turns and
// whether it rests on the ground, for the tests that check a drive or a
// teleport moved it without reading the wire twice.
func (s *Server) BoatState(id int32) (x, y, z, yaw float64, onGround, ok bool) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			return it.boat.X, it.boat.Y, it.boat.Z, float64(it.boat.Yaw), it.boat.OnGround, true
		}
	}
	return 0, 0, 0, 0, false, false
}

// BoatVelocity is the velocity of the boat with the given entity ID.
func (s *Server) BoatVelocity(id int32) (vx, vy, vz float64, ok bool) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			return it.boat.VX, it.boat.VY, it.boat.VZ, true
		}
	}
	return 0, 0, 0, false
}

// BoatRider is the name of the player driving the boat with the given
// entity ID — the first player aboard — for the tests that check a mount
// or a dismount took.
func (s *Server) BoatRider(id int32) (string, bool) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			if c := it.driver(); c != nil {
				return c.player.Name, true
			}
			return "", false
		}
	}
	return "", false
}

// BoatPassengerIDs is who rides the boat with the given entity ID, their
// entity IDs in mounting order, for the tests that check the seat a mount
// or a push gave a body without reading the passengers packet twice.
func (s *Server) BoatPassengerIDs(id int32) []int32 {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			return it.passengerIDs()
		}
	}
	return nil
}

// MountMobOnBoat seats the overworld mob with the entity ID on the boat with
// the other, as /ride mount does, and reports whether it was seated.
func (s *Server) MountMobOnBoat(mobID, boatID int32) bool {
	var ent *mobEntity
	s.mobsMu.Lock()
	for mob, e := range s.herdOf(s.world).book {
		if mob.ID == mobID {
			ent = e
		}
	}
	s.mobsMu.Unlock()
	var boat *boatEntity
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.boat.ID == boatID && !it.boat.Dead {
			boat = it
		}
	}
	s.boatsMu.Unlock()
	return ent != nil && boat != nil && s.mountMobOnBoat(ent, boat)
}

// MountMobOnMinecart seats the overworld mob with the entity ID in the cart
// with the other, as /ride mount does, and reports whether it was seated.
func (s *Server) MountMobOnMinecart(mobID, cartID int32) bool {
	var ent *mobEntity
	s.mobsMu.Lock()
	for mob, e := range s.herdOf(s.world).book {
		if mob.ID == mobID {
			ent = e
		}
	}
	s.mobsMu.Unlock()
	var cart *minecartEntity
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.cart.ID == cartID && !it.cart.Dead {
			cart = it
		}
	}
	s.minecartsMu.Unlock()
	return ent != nil && cart != nil && s.mountMobOnMinecart(ent, cart, true)
}

// MinecartMobRidersInDimension lists, for each cart standing in the named
// dimension, the kinds of the live mobs of that dimension riding it.
func (s *Server) MinecartMobRidersInDimension(dimension string) [][]string {
	w := s.dimensionByName(dimension).w
	var aboard [][]*mobEntity
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if !it.cart.Dead && it.w == w {
			aboard = append(aboard, slices.Clone(it.mobs))
		}
	}
	s.minecartsMu.Unlock()
	return s.liveMobKinds(w, aboard)
}

// liveMobKinds is the kinds of the mobs of each list still alive in w.
func (s *Server) liveMobKinds(w *world.World, aboard [][]*mobEntity) [][]string {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	out := make([][]string, len(aboard))
	for i, mobs := range aboard {
		out[i] = []string{}
		for _, ent := range mobs {
			if ent.w == w && ent.mob.Alive() && !ent.mob.Discarded() {
				out[i] = append(out[i], ent.mob.Type.Name())
			}
		}
	}
	return out
}

// BoatMobRidersInDimension lists, for each boat standing in the named
// dimension, the kinds of the live mobs of that dimension riding it.
func (s *Server) BoatMobRidersInDimension(dimension string) [][]string {
	w := s.dimensionByName(dimension).w
	var aboard [][]*mobEntity
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.boat.Dead || it.w != w {
			continue
		}
		var mobs []*mobEntity
		for _, p := range it.passengers {
			if p.ent != nil {
				mobs = append(mobs, p.ent)
			}
		}
		aboard = append(aboard, mobs)
	}
	s.boatsMu.Unlock()
	return s.liveMobKinds(w, aboard)
}

// SpawnMinecart stands a plain cart at the given position, the way a laid
// one begins — the summons the Use Item On wire path stands in for when
// the thing under test is the cart's own ride, not the laying of it. It
// returns the entity ID the packets address it by.
func (s *Server) SpawnMinecart(x, y, z float64) int32 {
	it := s.spawnMinecart(s.world, entity.MinecartPlain, x, y, z)
	return it.cart.ID
}

// TickMinecarts runs the main loop's minecart pass n times over, exactly
// as the main loop's own pass runs it: the main loop having been stopped,
// no tick of its own races the ones driven here.
func (s *Server) TickMinecarts(n int) {
	for range n {
		s.tickMinecartTracking()
		s.tickMinecarts()
	}
}

// MinecartState is where the cart with the given entity ID stands and
// turns, for the tests that check the ride moved it without reading the
// wire twice.
func (s *Server) MinecartState(id int32) (x, y, z, yaw float64, ok bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.cart.X, it.cart.Y, it.cart.Z, float64(it.cart.Yaw), true
		}
	}
	return 0, 0, 0, 0, false
}

// MinecartVelocityY is the vertical velocity of the cart with the given
// entity ID.
func (s *Server) MinecartVelocityY(id int32) (vy float64, ok bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.cart.VY, true
		}
	}
	return 0, false
}

// MinecartDimension is the wire name of the dimension the cart with the
// given entity ID stands in, for the tests that watch a rider's cart cross
// with them.
func (s *Server) MinecartDimension(id int32) string {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return s.dimensionOfWorld(it.w).name
		}
	}
	return ""
}

// MinecartTags is the scoreboard tags the cart with the given entity ID
// carries, for the tests that check a selector's /tag landed on it.
func (s *Server) MinecartTags(id int32) []string {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.tags.Values()
		}
	}
	return nil
}

// MinecartKind is the entity type of the live cart with the given entity ID,
// empty for none.
func (s *Server) MinecartKind(id int32) string {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.kind.Name()
		}
	}
	return ""
}

// MinecartCargoAt reads what a container cart carries in one of its slots,
// for the tests that check a click through its window reached the cart.
func (s *Server) MinecartCargoAt(id int32, slot int) protocol.ItemStack {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead && slot >= 0 && slot < len(it.cargo.slots) {
			return it.cargo.slots[slot]
		}
	}
	return protocol.ItemStack{}
}

// StowMinecartCargo puts a stack straight into a container cart's slot, as a
// click through its window would leave it.
func (s *Server) StowMinecartCargo(id int32, slot int, stack protocol.ItemStack) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && slot >= 0 && slot < len(it.cargo.slots) {
			cartCargo{it: it}.Put(slot, stack)
			return
		}
	}
}

// MinecartSaveTag is the compound a cart is saved as, for the tests that
// check what its file would carry.
func (s *Server) MinecartSaveTag(id int32) (nbt.Compound, bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return minecartTag(it).Entries(), true
		}
	}
	return nil, false
}

// SeedMinecart replaces the dice of the cart with the given entity ID by a
// source seeded by seed, so the rolls of a TNT cart's blast and fuse are the
// test's to compute.
func (s *Server) SeedMinecart(id int32, seed int64) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id {
			it.rng = rand.New(rand.NewSource(seed))
		}
	}
}

// MinecartFuse is the fuse of the TNT cart with the given entity ID, -1 while
// unlit; ok is false for a cart that is gone.
func (s *Server) MinecartFuse(id int32) (fuse int32, ok bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.tnt.Fuse, true
		}
	}
	return 0, false
}

// MinecartLit is the synced lit flag of the furnace cart with the given
// entity ID; ok is false for a cart that is gone or no furnace cart.
func (s *Server) MinecartLit(id int32) (lit, ok bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead && it.cart.Furnace != nil {
			return it.cart.Furnace.Lit, true
		}
	}
	return false, false
}

// ObserveCartBlasts calls fn with the radius of every TNT cart blast.
func (s *Server) ObserveCartBlasts(fn func(radius float64)) { s.tntCartBlastSeam = fn }

// HurtMinecart lands a blow of the given damage on the cart, as a fire
// source's (or an ordinary one's) hurt, and reports whether it landed.
func (s *Server) HurtMinecart(id int32, damage float64, ignites bool) bool {
	it := s.minecartByEntityID(s.world, id)
	if it == nil {
		return false
	}
	return s.minecartHurtBy(it, s.world, damage, false, entity.TNTCartHit{Ignites: ignites})
}

// SetMinecartMotion sets the cart's velocity in blocks per tick.
func (s *Server) SetMinecartMotion(id int32, vx, vy, vz float64) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id {
			it.cart.VX, it.cart.VY, it.cart.VZ = vx, vy, vz
		}
	}
}

// SpawnMinecartOfKind stands a cart of the kind given at the position, the
// way SpawnMinecart does a plain one.
func (s *Server) SpawnMinecartOfKind(kind string, x, y, z float64) int32 {
	return s.spawnMinecart(s.world, kind, x, y, z).cart.ID
}

// TestMinecart is one minecart in a world, as a test reads it: its entity
// ID, its identity and where it stands.
type TestMinecart struct {
	ID      int32
	UUID    string
	X, Y, Z float64
	Yaw     float32
}

// MinecartsInDimension lists the minecarts of the named dimension that are
// still in the world, for the tests that check a crossing carried one.
func (s *Server) MinecartsInDimension(dimension string) []TestMinecart {
	w := s.dimensionByName(dimension).w
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	var out []TestMinecart
	for _, it := range s.minecarts {
		if it.cart.Dead || it.w != w {
			continue
		}
		out = append(out, TestMinecart{
			ID:   it.cart.ID,
			UUID: it.uuid.String(),
			X:    it.cart.X, Y: it.cart.Y, Z: it.cart.Z,
			Yaw: it.cart.Yaw,
		})
	}
	return out
}

// MinecartRider is the name of the player riding the cart with the given
// entity ID, for the tests that check a mount or a dismount took.
func (s *Server) MinecartRider(id int32) (string, bool) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			if len(it.passengers) > 0 {
				return it.passengers[0].player.Name, true
			}
			return "", false
		}
	}
	return "", false
}

// MinecartPassengerIDs is who rides the cart with the given entity ID, the
// plain cart's one seat at most, for the tests that check the boarding
// without reading the passengers packet twice.
func (s *Server) MinecartPassengerIDs(id int32) []int32 {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.ID == id && !it.cart.Dead {
			return it.passengerIDs()
		}
	}
	return nil
}

// BoatLeashHolder is the entity ID of the body holding the boat with the
// given entity ID, 0 for an unleashed one, and false for a boat not in the
// list.
func (s *Server) BoatLeashHolder(id int32) (holder int32, ok bool) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			return it.holder, true
		}
	}
	return 0, false
}

// BoatCargoAt reads what a chest boat carries in one of its slots, for the
// tests that check a click through its window reached the hull.
func (s *Server) BoatCargoAt(id int32, slot int) protocol.ItemStack {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			if slot >= 0 && slot < len(it.cargo) {
				return it.cargo[slot]
			}
			return protocol.ItemStack{}
		}
	}
	return protocol.ItemStack{}
}

// BoatSaveTag is the compound a boat is saved as, for the tests that check
// what its file would carry.
func (s *Server) BoatSaveTag(id int32) (nbt.Compound, bool) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && !it.boat.Dead {
			return boatTag(it).Entries(), true
		}
	}
	return nil, false
}

// StowBoatCargo puts a stack straight into a chest boat's slot, as a click
// through its window would leave it, so a test can stand cargo in a hull
// without a window's worth of clicking.
func (s *Server) StowBoatCargo(id int32, slot int, stack protocol.ItemStack) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id && slot >= 0 && slot < len(it.cargo) {
			boatCargo{it: it}.Put(slot, stack)
			return
		}
	}
}

// DamageBoat takes the boat with the given entity ID through the hurt a
// survival blow of the amount would put it through, for the tests that
// want a broken boat without the blows the damage runs to.
func (s *Server) DamageBoat(id int32, amount float64) {
	s.boatsMu.Lock()
	defer s.boatsMu.Unlock()
	for _, it := range s.boats {
		if it.boat.ID == id {
			it.boat.TakeDamage(amount, false)
			return
		}
	}
}

// MobUUID is the UUID the mob with the given entity ID walks around under,
// which is what a selector naming its target by UUID names it by, and what
// an Add Entity packet shows a client.
func (s *Server) MobUUID(id int32) (string, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob, ent := range s.herdOf(s.world).book {
		if mob.ID == id {
			return ent.uuid.String(), true
		}
	}
	return "", false
}

// ItemCount, ArrowCount, OrbCount, TNTCount, FallingCount and PaintingCount
// are how many of each of the saved kinds that are not mobs are standing in
// the worlds, for the tests that check an unload took its entities with it
// or that a reload brought exactly one back, which the wire alone cannot
// always be made to say in time.
func (s *Server) ItemCount() int {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	n := 0
	for _, it := range s.items {
		if !it.item.Dead {
			n++
		}
	}
	return n
}

func (s *Server) ArrowCount() int {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	n := 0
	for _, it := range s.arrows {
		if !it.arrow.Dead {
			n++
		}
	}
	return n
}

func (s *Server) OrbCount() int {
	s.orbsMu.Lock()
	defer s.orbsMu.Unlock()
	n := 0
	for _, it := range s.orbs {
		if !it.orb.Dead {
			n++
		}
	}
	return n
}

// TestOrb is one live experience orb: its motion and the experience it
// holds across its merged pickups.
type TestOrb struct {
	X, Y, Z    float64
	VX, VY, VZ float64
	XP         int
}

// OrbsInWorld lists the live orbs of the overworld.
func (s *Server) OrbsInWorld() []TestOrb {
	s.orbsMu.Lock()
	defer s.orbsMu.Unlock()
	var out []TestOrb
	for _, it := range s.orbs {
		if !it.orb.Dead {
			o := it.orb
			out = append(out, TestOrb{o.X, o.Y, o.Z, o.VX, o.VY, o.VZ, o.Value * o.Count})
		}
	}
	return out
}

func (s *Server) TNTCount() int {
	s.tntMu.Lock()
	defer s.tntMu.Unlock()
	n := 0
	for _, it := range s.tnts {
		if !it.tnt.Dead {
			n++
		}
	}
	return n
}

func (s *Server) FallingCount() int {
	s.fallingMu.Lock()
	defer s.fallingMu.Unlock()
	n := 0
	for _, it := range s.fallings {
		if !it.fb.Dead {
			n++
		}
	}
	return n
}

// FallingBlockPosition is the position of a live falling block with the given ID.
func (s *Server) FallingBlockPosition(id int32) (x, y, z float64, ok bool) {
	s.fallingMu.Lock()
	defer s.fallingMu.Unlock()
	for _, it := range s.fallings {
		if it.fb.ID == id && !it.fb.Dead {
			return it.fb.X, it.fb.Y, it.fb.Z, true
		}
	}
	return 0, 0, 0, false
}

func (s *Server) PaintingCount() int {
	s.paintingsMu.Lock()
	defer s.paintingsMu.Unlock()
	return len(s.paintings)
}

// PaintingBox is the bounding box of the painting with the entity ID given;
// ok is false when no painting has it.
func (s *Server) PaintingBox(id int32) (minX, minY, minZ, maxX, maxY, maxZ float64, ok bool) {
	s.paintingsMu.Lock()
	defer s.paintingsMu.Unlock()
	for _, p := range s.paintings {
		if p.id == id {
			return p.minX, p.minY, p.minZ, p.maxX, p.maxY, p.maxZ, true
		}
	}
	return 0, 0, 0, 0, 0, 0, false
}

// FrameCount is the item frames hung in the worlds, for the tests that
// follow a frame through its hanging and its end.
func (s *Server) FrameCount() int {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	return len(s.frames)
}

// TestTrackedBody is a body one of the spawn hooks below stood up: the
// entity ID the packets and the selector name it by, and the identity the
// kill feedback spells out.
type TestTrackedBody struct {
	ID   int32
	UUID uuid.UUID
}

// CloudCount is how many area effect clouds linger, for the tests that follow
// one to its end.
func (s *Server) CloudCount() int {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	return len(s.clouds)
}

// FireworkCount is how many rockets fly, for the tests that follow one to its
// end.
func (s *Server) FireworkCount() int {
	s.fireworksMu.Lock()
	defer s.fireworksMu.Unlock()
	return len(s.fireworks)
}

// SpawnCloudEntity stands an area effect cloud where the tests put it, with
// no effects of its own, carrying the poison contents a shatter would have.
func (s *Server) SpawnCloudEntity(x, y, z float64) TestTrackedBody {
	poison, _ := registry.PotionID("minecraft:poison")
	s.spawnCloud(s.world, x, y, z, nil, 0, protocol.PotionContents{Potion: poison}, rules.LingeringDurationScale, uuid.Nil)
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	if len(s.clouds) == 0 {
		return TestTrackedBody{}
	}
	it := s.clouds[len(s.clouds)-1]
	return TestTrackedBody{ID: it.cloud.ID, UUID: it.uuid}
}

// SpawnOwnerlessCloud stands an area effect cloud with the effects given and
// no owner, already past its wait, where the tests put it.
func (s *Server) SpawnOwnerlessCloud(x, y, z float64, specs []protocol.PotionEffectSpec) {
	effects := make([]effect.Instance, 0, len(specs))
	for _, spec := range specs {
		effects = append(effects, effect.FromWireSpec(spec))
	}
	s.spawnCloud(s.world, x, y, z, effects, 0, protocol.PotionContents{}, rules.LingeringDurationScale, uuid.Nil)
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	it := s.clouds[len(s.clouds)-1]
	it.cloud.WaitTime = 0
	it.cloud.Restore(0, 3, -1e-3)
}

// CloudState reads the age and radius of the cloud with the identity given.
func (s *Server) CloudState(id uuid.UUID) (age int, radius float32, ok bool) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.clouds {
		if it.uuid == id {
			return it.cloud.TickCount, it.cloud.Radius(), true
		}
	}
	return 0, 0, false
}

// TuneCloud sets the timing fields and the custom particle of the cloud with
// the identity given, for the tests that save a cloud off the defaults.
func (s *Server) TuneCloud(id uuid.UUID, duration, wait, reapplication, durationOnUse int, radiusOnUse float32, particle nbt.Tag) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.clouds {
		if it.uuid == id {
			c := it.cloud
			c.Duration, c.WaitTime, c.Reapplication = duration, wait, reapplication
			c.DurationOnUse, c.RadiusOnUse = durationOnUse, radiusOnUse
			it.customParticle = particle
		}
	}
}

// FireworkState reads the age and total life of the rocket with the identity
// given.
func (s *Server) FireworkState(id uuid.UUID) (life, lifetime int, ok bool) {
	s.fireworksMu.Lock()
	defer s.fireworksMu.Unlock()
	for _, it := range s.fireworks {
		if it.uuid == id {
			return it.rocket.Life, it.rocket.Lifetime, true
		}
	}
	return 0, 0, false
}

// SpawnEyeEntity flies an eye of ender from where the tests put it, with the
// motion given and no target.
func (s *Server) SpawnEyeEntity(x, y, z, vx, vy, vz float64) TestTrackedBody {
	stack := protocol.ItemStack{Count: 1}
	stack.ItemID = int32(registry.ItemEnderEye)
	e := entity.NewEyeOfEnder(s.nextEntity(), x, y, z)
	e.VX, e.VY, e.VZ = vx, vy, vz
	ent := &eyeEntity{eye: e, w: s.world, uuid: uuid.New(), stack: stack}
	s.registerEye(ent)
	return TestTrackedBody{ID: e.ID, UUID: ent.uuid}
}

// EyeCount is how many eyes of ender fly.
func (s *Server) EyeCount() int {
	s.eyesMu.Lock()
	defer s.eyesMu.Unlock()
	return len(s.eyes)
}

// SpawnFireworkEntity flies a rocket from where the tests put it, unowned and
// without a flight attached to a glider.
func (s *Server) SpawnFireworkEntity(x, y, z float64) TestTrackedBody {
	item := int32(registry.ItemFireworkRocket)
	it := s.spawnFirework(s.world, protocol.ItemStack{ItemID: item, Count: 1}, x, y, z, 0, uuid.Nil, 0)
	return TestTrackedBody{ID: it.rocket.ID, UUID: it.uuid}
}

// SpawnFireworkStackEntity flies an unowned rocket built from stack, whose
// explosions give its burst a hurt.
func (s *Server) SpawnFireworkStackEntity(stack protocol.ItemStack, x, y, z float64) TestTrackedBody {
	it := s.spawnFirework(s.world, stack, x, y, z, 0, uuid.Nil, 0)
	return TestTrackedBody{ID: it.rocket.ID, UUID: it.uuid}
}

// SpawnFireworkAtAngle flies an unowned rocket shot at an angle, which keeps
// the velocity given, for a life of lifetime ticks.
func (s *Server) SpawnFireworkAtAngle(x, y, z, vx, vy, vz float64, lifetime int) TestTrackedBody {
	item := int32(registry.ItemFireworkRocket)
	return s.SpawnFireworkStackAtAngle(protocol.ItemStack{ItemID: item, Count: 1}, x, y, z, vx, vy, vz, lifetime)
}

// SpawnFireworkStackAtAngle flies a crossbow rocket carrying stack's explosions.
func (s *Server) SpawnFireworkStackAtAngle(stack protocol.ItemStack, x, y, z, vx, vy, vz float64, lifetime int) TestTrackedBody {
	it := s.newFirework(s.world, stack, x, y, z, 0, uuid.Nil, 0)
	it.rocket.VX, it.rocket.VY, it.rocket.VZ = vx, vy, vz
	it.rocket.Restore(0, lifetime, true, true)
	s.registerFirework(it)
	return TestTrackedBody{ID: it.rocket.ID, UUID: it.uuid}
}

// SpawnBobberEntity floats a fishing hook where the tests put it, with no
// angler behind it.
func (s *Server) SpawnBobberEntity(x, y, z float64) TestTrackedBody {
	id := s.nextEntity()
	hook := entity.NewBobber(id, x, y, z, 0, 0, 0, s.entitySeed(id), 0, 0)
	it := newBobberEntity(hook, s.world, uuid.Nil)
	s.registerBobber(it)
	return TestTrackedBody{ID: hook.ID, UUID: it.uuid}
}

// ServerOnlyBodiesInDimension lists the live bodies of one kind standing in
// the named dimension: kind is "painting", "frame", "knot", "cloud",
// "firework" or "bobber".
func (s *Server) ServerOnlyBodiesInDimension(kind, dimension string) []TestTrackedBody {
	w := s.dimensionByName(dimension).w
	var out []TestTrackedBody
	switch kind {
	case "painting":
		s.paintingsMu.Lock()
		for _, p := range s.paintings {
			if p.w == w {
				out = append(out, TestTrackedBody{ID: p.id, UUID: p.uuid})
			}
		}
		s.paintingsMu.Unlock()
	case "frame":
		s.framesMu.Lock()
		for _, f := range s.frames {
			if f.w == w {
				out = append(out, TestTrackedBody{ID: f.id, UUID: f.uuid})
			}
		}
		s.framesMu.Unlock()
	case "knot":
		s.knotsMu.Lock()
		for _, k := range s.knots {
			if k.w == w {
				out = append(out, TestTrackedBody{ID: k.id, UUID: k.uuid})
			}
		}
		s.knotsMu.Unlock()
	case "cloud":
		s.arrowsMu.Lock()
		for _, it := range s.clouds {
			if it.w == w && !it.cloud.Dead {
				out = append(out, TestTrackedBody{ID: it.cloud.ID, UUID: it.uuid})
			}
		}
		s.arrowsMu.Unlock()
	case "firework":
		s.fireworksMu.Lock()
		for _, it := range s.fireworks {
			if it.w == w && !it.rocket.Dead {
				out = append(out, TestTrackedBody{ID: it.rocket.ID, UUID: it.uuid})
			}
		}
		s.fireworksMu.Unlock()
	case "bobber":
		s.bobbersMu.Lock()
		for _, it := range s.bobbers {
			if it.w == w && !it.hook.Dead {
				out = append(out, TestTrackedBody{ID: it.hook.ID, UUID: it.uuid})
			}
		}
		s.bobbersMu.Unlock()
	}
	return out
}

// SpawnKnotEntity ties a lead knot at the cell the tests put it.
func (s *Server) SpawnKnotEntity(x, y, z int32) TestTrackedBody {
	k, _, _ := s.tieKnot(s.world, x, y, z)
	return TestTrackedBody{ID: k.id, UUID: k.uuid}
}

// SpawnPaintingEntity hangs a painting of the variant given, facing south,
// without a wall behind it, so the tests can stand one up where they like.
func (s *Server) SpawnPaintingEntity(x, y, z, variant int32) TestTrackedBody {
	painting, _ := paintingCandidate(variant)
	p := newPainting(s.nextEntity(), uuid.New(), s.world, x, y, z, 3, variant, painting.Width, painting.Height)
	s.registerPainting(p)
	return TestTrackedBody{ID: p.id, UUID: p.uuid}
}

// SpawnFrameEntity hangs an item frame where the tests put it, facing south,
// without a wall behind it, so the tests can stand one up where they like.
func (s *Server) SpawnFrameEntity(x, y, z int32) TestTrackedBody {
	f := newFrame(s.nextEntity(), uuid.New(), s.world, x, y, z, 3, false)
	s.registerFrame(f)
	return TestTrackedBody{ID: f.id, UUID: f.uuid}
}

// SpawnFilledFrameEntity hangs an item frame holding the stack given, facing
// south, without a wall behind it.
func (s *Server) SpawnFilledFrameEntity(x, y, z int32, item protocol.ItemStack) TestTrackedBody {
	f := newFrame(s.nextEntity(), uuid.New(), s.world, x, y, z, 3, false)
	f.setItem(item)
	s.registerFrame(f)
	return TestTrackedBody{ID: f.id, UUID: f.uuid}
}

// SpawnGlowFrameEntity hangs a glow item frame where the tests put it, facing
// south, without a wall behind it, so the tests can stand one up where they
// like.
func (s *Server) SpawnGlowFrameEntity(x, y, z int32) TestTrackedBody {
	f := newFrame(s.nextEntity(), uuid.New(), s.world, x, y, z, 3, true)
	s.registerFrame(f)
	return TestTrackedBody{ID: f.id, UUID: f.uuid}
}

// ArmorStandCount is how many armor stands stand in the world, live ones
// only.
func (s *Server) ArmorStandCount() int {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	return len(s.armorStands)
}

// TickArmorStands runs the armor stand pass the main loop runs, n times
// over, with the main loop stopped, so a test can step a stand by hand the
// way TickEntities steps an item.
func (s *Server) TickArmorStands(n int) {
	for range n {
		s.tickArmorStands()
	}
}

// SpawnArmorStand stands a plain armor stand at the given position of the
// overworld and returns its entity ID.
func (s *Server) SpawnArmorStand(x, y, z float64) int32 {
	ent, err := s.summonArmorStand(s.world, x, y, z, nil, plugin.SpawnCommand)
	if err != nil {
		panic(err)
	}
	return ent.stand.ID
}

// SetArmorStandInvulnerable sets the Invulnerable tag of the armor stand with
// the given entity ID.
func (s *Server) SetArmorStandInvulnerable(id int32) {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.stand.ID == id {
			ent.stand.Invulnerable = true
		}
	}
}

// ArmorStandVelocity is the velocity of the armor stand with the given entity
// ID, false for a stand that is not standing.
func (s *Server) ArmorStandVelocity(id int32) (vx, vy, vz float64, ok bool) {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.stand.ID == id && !ent.stand.Dead {
			return ent.stand.VX, ent.stand.VY, ent.stand.VZ, true
		}
	}
	return 0, 0, 0, false
}

// TestArmorStand is one armor stand standing in a world, as a test reads it:
// its entity ID, its identity and where it stands.
type TestArmorStand struct {
	ID      int32
	UUID    string
	X, Y, Z float64
	Yaw     float32
	Health  float64
}

// ArmorStandsInDimension lists the armor stands of the named dimension that
// are still standing, for the tests that check a crossing carried one.
func (s *Server) ArmorStandsInDimension(dimension string) []TestArmorStand {
	w := s.dimensionByName(dimension).w
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	var out []TestArmorStand
	for _, ent := range s.armorStands {
		if ent.stand.Dead || ent.w != w {
			continue
		}
		out = append(out, TestArmorStand{
			ID:   ent.stand.ID,
			UUID: ent.uuid.String(),
			X:    ent.stand.X, Y: ent.stand.Y, Z: ent.stand.Z,
			Yaw: ent.stand.Yaw, Health: ent.stand.Health,
		})
	}
	return out
}

// MobFallDistance is the fall distance of the overworld mob with the given
// entity ID, false for a mob not in the herd.
func (s *Server) MobFallDistance(id int32) (float64, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.FallDistance, true
		}
	}
	return 0, false
}

// MobPos is where the mob with the given entity ID stands: its block
// coordinates in the overworld.
func (s *Server) MobPos(id int32) (x, y, z float64, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.X, mob.Y, mob.Z, true
		}
	}
	return 0, 0, 0, false
}

// MobVelocity is the velocity of the overworld mob with the given entity
// ID, false for a mob not in the herd.
func (s *Server) MobVelocity(id int32) (vx, vy, vz float64, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.VX, mob.VY, mob.VZ, true
		}
	}
	return 0, 0, 0, false
}

// IgniteMob sets the overworld mob with the given entity ID alight for
// ticks through the path lava and fire take, false for a mob not in the herd.
func (s *Server) IgniteMob(id int32, ticks int) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.IgniteForTicks(ticks)
			return true
		}
	}
	return false
}

// MobPace is the movement pace of the overworld mob with the given entity
// ID, 0 for a mob not in the herd.
func (s *Server) MobPace(id int32) float64 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.Pace()
		}
	}
	return 0
}

// MobOnGround reports the overworld mob's onGround flag, false for a mob
// not in the herd.
func (s *Server) MobOnGround(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.OnGround
		}
	}
	return false
}

// MobHealth is the health of the overworld mob with the given entity ID,
// false for a mob not in the herd.
func (s *Server) MobHealth(id int32) (float64, bool) {
	return s.MobHealthInDimension(world.DimensionName, id)
}

// CreeperPowered reports whether the overworld creeper with the given entity
// ID is charged.
func (s *Server) CreeperPowered(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.herdOf(s.world).herd {
		if m.ID == id {
			return m.Powered
		}
	}
	return false
}

// OfferGold has the overworld piglin with the given entity ID take a gold
// ingot as a player's offer would, and reports whether it did.
func (s *Server) OfferGold(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.AdmireGold(protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 1})
		}
	}
	return false
}

// SavedDragonFight writes the End's fight data and reads it back from disk.
func (s *Server) SavedDragonFight() nbt.Compound {
	return s.SavedDragonFightInDimension(world.EndName)
}

// SavedDragonFightInDimension writes the named dimension's fight data and reads
// it back from disk.
func (s *Server) SavedDragonFightInDimension(dimension string) nbt.Compound {
	dim := s.dimensionByName(dimension)
	s.saveDragonFight(dim)
	return dim.w.ReadDragonFight()
}

// DragonArenaSettled reports whether no dragon fight arena chunk is still
// generating, so the fight's next tick acts on a whole arena.
func (s *Server) DragonArenaSettled() bool {
	for _, dim := range s.allDims() {
		f := dim.dragonFight
		if f == nil {
			continue
		}
		f.mu.Lock()
		for _, a := range f.loading {
			if len(a.ch) == 0 {
				f.mu.Unlock()
				return false
			}
		}
		f.mu.Unlock()
	}
	return true
}

// MobHealthInDimension reads a mob's health in the named dimension.
func (s *Server) MobHealthInDimension(dimension string, id int32) (float64, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.dimensionByName(dimension).w).book {
		if mob.ID == id {
			return mob.Health, true
		}
	}
	return 0, false
}

// BindCreaking ties the creaking with the entity ID to a heart at the block,
// the bond a heart makes when it stands one up: the heart lists it as its
// protector when the block is a heart with a block entity. A block without
// one, or one that is no heart, leaves the creaking an orphan.
func (s *Server) BindCreaking(id int32, x, y, z int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob, ent := range s.herdOf(s.world).book {
		if mob.ID != id {
			continue
		}
		mob.CreakingBind(x, y, z)
		if be := creakingHeartAt(s.world, x, y, z); be != nil {
			be.UseLive(func(b *world.BlockEntity) { b.Creaking = ent.uuid })
		}
	}
}

// UnlinkCreakingHeart clears the creaking the heart at the block lists, the
// link a heart loses when its creaking was away past the grace period.
func (s *Server) UnlinkCreakingHeart(x, y, z int32) {
	if be := creakingHeartAt(s.world, x, y, z); be != nil {
		be.UseLive(func(b *world.BlockEntity) { b.Creaking = uuid.Nil })
	}
}

// TestFish is the fish's bucket mark, variant and schooling state.
type TestFish struct {
	FromBucket      bool
	SalmonSize      int
	TropicalVariant int32
	// PuffState is a pufferfish's puff state.
	PuffState  int
	Follower   bool
	SchoolSize int
}

// MobFishState is the fish state of the overworld mob with the given entity
// ID, false for a mob not in the herd.
func (s *Server) MobFishState(id int32) (TestFish, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return TestFish{mob.FromBucket, mob.SalmonSize, mob.TropicalVariant, mob.PuffState(), mob.Following(), mob.SchoolSize()}, true
		}
	}
	return TestFish{}, false
}

// SetMobHealth sets the health of the overworld mob with the given entity ID
// and reports whether it found it.
func (s *Server) SetMobHealth(id int32, health float64) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetMetadataHealth(health)
			return true
		}
	}
	return false
}

// MobEffectOf returns the duration and amplifier of the named effect on the
// mob with the given entity ID, if it carries it.
func (s *Server) MobEffectOf(id int32, name string) (duration, amplifier int32, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID != id {
			continue
		}
		for _, in := range mob.AllEffects() {
			if registry.MobEffectName(in.ID) == name {
				return in.Duration, in.Amplifier, true
			}
		}
	}
	return 0, 0, false
}

// MobLeashHolder is the entity ID of the body holding the overworld mob with
// the given entity ID, 0 for one unleashed, and false for a mob not in the
// herd.
func (s *Server) MobLeashHolder(id int32) (holder int32, ok bool) {
	return s.MobLeashHolderInDimension(world.DimensionName, id)
}

// MobLeashHolderInDimension is MobLeashHolder for the named dimension, for
// the tests that watch a lead a crossing carried into another world.
func (s *Server) MobLeashHolderInDimension(dimension string, id int32) (holder int32, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.dimensionByName(dimension).mobs.book {
		if mob.ID == id {
			return mob.LeashHolderID, true
		}
	}
	return 0, false
}

// OrbPos is where the orb with the given entity ID stands, its feet.
func (s *Server) OrbPos(id int32) (x, y, z float64, ok bool) {
	s.orbsMu.Lock()
	defer s.orbsMu.Unlock()
	for _, it := range s.orbs {
		if it.orb.ID == id && !it.orb.Dead {
			return it.orb.X, it.orb.Y, it.orb.Z, true
		}
	}
	return 0, 0, 0, false
}

// Entities across the dimensions. The portal tests read what a crossing did
// to the five non-player kinds on both sides of it, which the wire alone
// cannot always be made to say — nobody may be standing where a body
// arrives — so the reads go through these, one list per kind per dimension.

// TestItem is one item entity standing in a world, as a test reads it: the
// entity ID and the UUID it is addressed and re-identified by, where it
// lies, the stack it holds, the ticks left before it may be picked up, its
// age and health, and the one player it is reserved for (empty for
// anyone's).
type TestItem struct {
	ID          int32
	UUID        string
	X, Y, Z     float64
	VX, VY, VZ  float64
	Item        string
	Stack       protocol.ItemStack
	Count       int32
	PickupDelay int
	Age         int
	Health      int
	Owner       string
}

// SpawnItemEntity lays a stack of items on the ground at the given position
// in the overworld, moving not at all, and returns the entity ID the
// packets address it by. It is how a test stands a stack where no player's
// throw would land it: the crossing being the thing under test, not the
// luck of where a throw happened to settle.
func (s *Server) SpawnItemEntity(x, y, z float64, item string, count int) int32 {
	return s.SpawnItemEntityDelay(x, y, z, item, count, entity.BreakPickupDelay)
}

// SpawnItemEntityDelay is SpawnItemEntity with the pickup delay chosen, so a
// test can stand a stack a player may claim on the very tick it appears.
func (s *Server) SpawnItemEntityDelay(x, y, z float64, item string, count, pickupDelay int) int32 {
	id, ok := registry.ItemID(item)
	if !ok {
		return 0
	}
	it := s.spawnItem(s.world, protocol.ItemStack{ItemID: id, Count: int32(count)},
		x, y-entity.ItemHalfSize, z, 0, 0, 0, pickupDelay)
	return it.item.ID
}

// SpawnItemStack lays a built stack on the ground, past its pickup delay,
// and returns its entity ID.
func (s *Server) SpawnItemStack(x, y, z float64, stack protocol.ItemStack) int32 {
	return s.spawnItem(s.world, stack, x, y-entity.ItemHalfSize, z, 0, 0, 0, 0).item.ID
}

// SpawnOminousBannerItem lays a raid captain's banner on the ground at the
// position, past its pickup delay, and returns its entity ID.
func (s *Server) SpawnOminousBannerItem(x, y, z float64) int32 {
	return s.spawnItem(s.world, ominousBanner(), x, y-entity.ItemHalfSize, z, 0, 0, 0, 0).item.ID
}

// DiscardItem marks the item entity with the given ID dead.
func (s *Server) DiscardItem(id int32) {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			it.item.Dead = true
		}
	}
}

// SpawnOminousBannerStack drops a stack of count ominous banners at the position.
func (s *Server) SpawnOminousBannerStack(count int, x, y, z float64) int32 {
	b := ominousBanner()
	b.Count = int32(count)
	return s.spawnItem(s.world, b, x, y-entity.ItemHalfSize, z, 0, 0, 0, 0).item.ID
}

// OminousBanner is the stack a raid captain wears on its head.
func (s *Server) OminousBanner() protocol.ItemStack { return ominousBanner() }

// SetItemFire lights the item with the given entity ID for the given ticks.
func (s *Server) SetItemFire(id int32, ticks int) {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			it.item.SetFire(ticks)
		}
	}
}

// SetItemAge sets the age of the item with the given entity ID.
func (s *Server) SetItemAge(id int32, age int) {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			it.item.Age = age
		}
	}
}

// ItemFire is the burn ticks the item with the given entity ID has left.
func (s *Server) ItemFire(id int32) int {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			return it.item.FireTicks()
		}
	}
	return -1
}

// ArrowFire is the burn ticks the arrow with the given entity ID has left.
func (s *Server) ArrowFire(id int32) int {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.arrows {
		if it.arrow.ID == id {
			return it.arrow.FireTicks
		}
	}
	return -1
}

// SpawnItemEntityMotion is SpawnItemEntity for a stack that already moves, so
// a test can watch what the ground under it does to its slide.
func (s *Server) SpawnItemEntityMotion(x, y, z float64, item string, count int, vx, vy, vz float64) int32 {
	id, ok := registry.ItemID(item)
	if !ok {
		return 0
	}
	it := s.spawnItem(s.world, protocol.ItemStack{ItemID: id, Count: int32(count)},
		x, y-entity.ItemHalfSize, z, vx, vy, vz, entity.BreakPickupDelay)
	return it.item.ID
}

// ItemsInDimension lists the item entities of the named dimension that are
// still alive, the stacks they hold and the identities they carry.
func (s *Server) ItemsInDimension(dimension string) []TestItem {
	w := s.dimensionByName(dimension).w
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	var items []TestItem
	for _, it := range s.items {
		if it.item.Dead || it.w != w {
			continue
		}
		name, _ := registry.ItemName(it.item.Stack.ItemID)
		owner := ""
		if it.item.Target != nil {
			owner = it.item.Target.String()
		}
		items = append(items, TestItem{
			ID:   it.item.ID,
			UUID: it.uuid.String(),
			X:    it.item.X, Y: it.item.Y, Z: it.item.Z,
			VX: it.item.VX, VY: it.item.VY, VZ: it.item.VZ,
			Item:        name,
			Stack:       it.item.Stack,
			Count:       it.item.Stack.Count,
			PickupDelay: it.item.PickupDelay,
			Age:         it.item.Age,
			Health:      it.item.Health,
			Owner:       owner,
		})
	}
	return items
}

// SetItemFallDistance sets the fall an item entity has banked.
func (s *Server) SetItemFallDistance(id int32, d float64) {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			it.item.SetFallDistance(d)
		}
	}
}

// ItemFallDistance reads the fall an item entity has banked.
func (s *Server) ItemFallDistance(id int32) float64 {
	s.itemsMu.Lock()
	defer s.itemsMu.Unlock()
	for _, it := range s.items {
		if it.item.ID == id {
			return it.item.FallDistance()
		}
	}
	return -1
}

// TestTNT is one primed TNT standing in a world, as a test reads it: where
// it stands, how much fuse it has left to burn, and the entity ID of the
// body that primed it.
type TestTNT struct {
	ID      int32
	OwnerID int32
	X, Y, Z float64
	VY      float64
	Fuse    int
}

// TNTsInDimension lists the primed TNT of the named dimension that is still
// burning, with the fuse each body has left.
func (s *Server) TNTsInDimension(dimension string) []TestTNT {
	w := s.dimensionByName(dimension).w
	s.tntMu.Lock()
	defer s.tntMu.Unlock()
	var tnts []TestTNT
	for _, it := range s.tnts {
		if it.tnt.Dead || it.w != w {
			continue
		}
		tnts = append(tnts, TestTNT{
			ID:      it.tnt.ID,
			OwnerID: it.tnt.OwnerID,
			X:       it.tnt.X, Y: it.tnt.Y, Z: it.tnt.Z,
			VY:   it.tnt.VY,
			Fuse: it.tnt.Fuse,
		})
	}
	return tnts
}

// MobsInDimension lists the mobs of the named dimension, the kinds and the
// places, for the tests that check a crossing took a body across or left it
// where it was.
func (s *Server) MobsInDimension(dimension string) []TestMob {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var mobs []TestMob
	h := s.dimensionByName(dimension).mobs
	for _, m := range h.herd {
		mob := TestMob{Kind: m.Type.Name(), X: m.X, Y: m.Y, Z: m.Z, ID: m.ID, Variant: m.Variant, Baby: m.IsBaby(), Yaw: m.Yaw}
		if m.Vehicle != nil {
			mob.Vehicle = m.Vehicle.ID
		}
		if ent := h.book[m]; ent != nil {
			mob.UUID = ent.uuid.String()
		}
		mobs = append(mobs, mob)
	}
	return mobs
}

// TeleportAfter selects every entity of the type given the way a command's
// selector does, runs between, and then teleports each one selected into
// the named dimension at (x, y, z): the window a command leaves between
// choosing its targets and moving them, held open for as long as between
// takes.
func (s *Server) TeleportAfter(entityType, dimension string, x, y, z float64, between func()) {
	var selected []command.Entity
	for _, e := range (worldScope{srv: s}).Entities(command.EntityQuery{}) {
		if e.EntityType() == entityType {
			selected = append(selected, e)
		}
	}
	between()
	for _, e := range selected {
		e.Teleport(command.Move{Pos: player.Position{X: x, Y: y, Z: z}}, dimension, "test")
	}
}

// SelectedEntities resolves every entity of the type given the way a
// command's selector does, one snapshot apiece, and leaves the acting to the
// caller: a command's action runs after its selector has read the world, and
// this is the reading half alone.
func (s *Server) SelectedEntities(entityType string) []command.Entity {
	var selected []command.Entity
	for _, e := range (worldScope{srv: s}).Entities(command.EntityQuery{}) {
		if e.EntityType() == entityType {
			selected = append(selected, e)
		}
	}
	return selected
}

// CartCrossBetween holds open the window a cart's crossing leaves after it
// has copied its riders and before it carries each one over, running between
// there: the window a rider's own crossing from another goroutine can win,
// which nothing outside a hook holds open long enough to land in.
func (s *Server) CartCrossBetween(between func()) {
	s.minecartCrossSeam = between
}

// CartBoardBetween holds open the window a cart's crossing leaves after the
// twin is registered and before its riders are seated on it, running between
// there: the window the tick's departure sweep can land in.
func (s *Server) CartBoardBetween(between func()) {
	s.minecartBoardSeam = between
}

// RideOutOfCart puts the named player's body into the state a second crossing
// of their own leaves it in — travelling, their cart's seat given up — for a
// test that holds a cart's crossing open while the rider is carried by
// another.
func (s *Server) RideOutOfCart(name string) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	c.dismountMinecart(false)
	c.traveling.Store(true)
}

// PickupBetween holds open the window a pickup leaves between its claim on
// an item and the giving of its stack to the collector, running between
// there for the pickup whose item carries the entity ID given: the window a
// command's goroutine can win while the tick is between the two, which
// nothing outside a hook holds open long enough to land in.
func (s *Server) PickupBetween(itemID int32, between func()) {
	s.pickupSeam = func(claimed int32) {
		if claimed == itemID {
			between()
		}
	}
}

// BarsBetween holds open the window the named player's vitals tick leaves
// between reading their bars and comparing them, running between there the
// way a death landing from another connection would: the straddle a blow
// from outside the tick wins in production, which nothing outside a hook
// holds open long enough to land in.
func (s *Server) BarsBetween(name string, between func()) {
	s.vitalsSeam = func(who string) {
		if who == name {
			between()
		}
	}
}

// RefusalBetween holds open the window a refused hurt leaves between the
// refusal and the deflection the tick then applies, running between there the
// way a command from another connection would: the straddle such a
// command wins in production, which nothing outside a hook holds open long
// enough to land in. The seam is armed under the arrow lock, for the tick
// reads it whenever a refusal sits in the window.
func (s *Server) RefusalBetween(between func()) {
	s.arrowsMu.Lock()
	s.arrowRefusalSeam = between
	s.arrowsMu.Unlock()
}

// BlastSpillBetween runs between after a blast has read a destroyed
// container's slots and before it gives them up, the point at which a
// hopper's move must find the container already out of the world.
func (s *Server) BlastSpillBetween(between func(*world.BlockEntity)) {
	s.blastSpillSeam = between
}

// PlayerTrackBetween holds open the window the players' tracking pass leaves
// after its snapshot and before it settles, running between there the way a
// dimension change from another connection would: a pass whose snapshot
// predates the crossing but whose settle follows it.
func (s *Server) PlayerTrackBetween(between func()) {
	s.mu.Lock()
	s.playerTrackSeam = between
	s.mu.Unlock()
}

// PlayerSendBetween holds open the window the players' tracking pass leaves
// between deciding a pairing and sending it, running between there the way a
// step from another connection would: a spawn the mover's step then doubles,
// or one that leaves the body behind.
func (s *Server) PlayerSendBetween(between func()) {
	s.mu.Lock()
	s.playerSendSeam = between
	s.mu.Unlock()
}

// PlayerJoinBetween holds open the window a join leaves between spawning a
// body and finishing the newcomer's view of it, running between there the way
// a step from the body's own connection would: a step the join's record does
// not yet name, and nothing corrects later.
func (s *Server) PlayerJoinBetween(between func()) {
	s.mu.Lock()
	s.playerJoinSeam = between
	s.mu.Unlock()
}

// PlayerJoinBodiesBetween holds open the window a join leaves after its tab
// list and before it spawns the others' bodies, running between there the way
// a dimension arrival from another connection would: an arrival whose queued
// spawn the join must not double.
func (s *Server) PlayerJoinBodiesBetween(between func()) {
	s.mu.Lock()
	s.playerJoinBodiesSeam = between
	s.mu.Unlock()
}

// PlayerMoveBetween holds open the window a step leaves between setting the
// player's position and telling the holders, running between there the way
// the pairing pass would: a pass that reads the movement baseline while the
// position has moved on and would place the body a step ahead.
func (s *Server) PlayerMoveBetween(name string, between func()) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	c.tp.mu.Lock()
	c.moveSeam = between
	c.tp.mu.Unlock()
}

// ClickBetween runs between after a container click has looked the open
// window's block entity up and before it takes the entity's lock, the point
// at which a blast that has removed the container must be found.
func (s *Server) ClickBetween(between func(*world.BlockEntity)) {
	s.clickSeam = between

}

// BlockEntityTickBetween runs between with each block entity whose turn the
// block-entity pass is about to take, the point at which a break or an unload
// may have removed the entity since the pass read its list.
func (s *Server) BlockEntityTickBetween(between func(*world.BlockEntity)) {
	s.blockEntityTickSeam = between
}

// HoldChunkTakes holds open every paced chunk take between its writes to
// the client's chunk bookkeeping and its world work, the window a
// shutdown's stop can be walked into beside a take.
func (s *Server) HoldChunkTakes(between func()) {
	s.chunkTakeSeam.Store(&between)
}

// WatchChunkStops runs watch on every chunk-sender stop that has closed its
// stop channel and is about to wait for the sender to exit, the point a
// test walks a second stopper — a leaving client's own — in beside the
// wait.
func (s *Server) WatchChunkStops(watch func()) {
	s.chunkStopSeam = watch
}

// ChunkSendersStarted is how many chunk senders have been started. A sender
// whose client is gone ends itself on its first failed write, so a test
// counts starts rather than the senders still running.
func (s *Server) ChunkSendersStarted() int { return int(s.chunkSenders.Load()) }

// HoldChunkSenders keeps every chunk sender started from now on from
// sending or loading anything until the release it returns is called: a
// join then waits on its chunks for as long as the test likes.
func (s *Server) HoldChunkSenders() (release func()) {
	held := make(chan struct{})
	s.chunkSenderHold.Store(&held)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.chunkSenderHold.Store(nil)
			close(held)
		})
	}
}

// PlayerClientLoaded reports whether the named player's client counts as
// loaded: its Player Loaded came in, or the load timeout ran out.
func (s *Server) PlayerClientLoaded(name string) bool {
	c := s.playerClient(name)
	return c != nil && c.clientLoaded()
}

// HoldFirstChunk holds back the first chunk the server sends, after its
// batch has begun, until the release it returns is called. It has to be
// taken before the server's first connection is.
func (s *Server) HoldFirstChunk() (release func()) {
	held := make(chan struct{})
	s.chunkBatchSeam = func() { <-held }
	var once sync.Once
	return func() { once.Do(func() { close(held) }) }
}

// LeaveBetween runs between after a leaving player's own save has written
// their boat and pearls and before their removal from the player list, the
// window a save that still sees the player must not overwrite.
func (s *Server) LeaveBetween(between func()) {
	s.leaveSeam = between
}

// LeaveWaitBetween runs between once a leaving player is off the player list
// and has been announced, and before the departure waits out the saves of the
// chunks it gave up: the window a player arriving while those saves drain must
// not be told of the departure in.
func (s *Server) LeaveWaitBetween(between func()) {
	s.leaveWaitSeam = between
}

// Gone returns the channel closed once the named player's connection has
// finished leaving — saved, off the player list and announced. It is nil when
// the player is not online, so it has to be taken before the connection is
// dropped.
func (s *Server) Gone(name string) <-chan struct{} {
	if c := s.playerClient(name); c != nil {
		return c.gone
	}
	return nil
}

// SetDropUntouchedChunks sets world.SetDropUntouchedChunks on every
// dimension. The test harnesses turn it on before a test's setup runs; a test
// of what vanilla saves turns it back off there.
func (s *Server) SetDropUntouchedChunks(drop bool) {
	for _, dim := range s.allDims() {
		dim.w.SetDropUntouchedChunks(drop)
	}
}

// SetLevelSeed replaces the level dice (anvil wear, xp rolls) with a fresh
// source over the value given.
func (s *Server) SetLevelSeed(seed int64) {
	s.levelRng = newDice(seed)
}

// SetPhysicsSeed replaces the fire dice of every dimension's physics engine
// with a fresh source over the value given, the way SpawnPass seeds the
// spawn's dice: the same world, the same seed, the same burns. It has to be
// called before the ticks a test drives by hand, and only once the world's
// loaded chunks are settled: the passes roll per loaded chunk, so a chunk
// arriving between two runs of a seeded test moves every die after it.
// Callers wait with awaitChunksSettled.
func (s *Server) SetPhysicsSeed(seed int64) {
	for _, dim := range s.allDims() {
		dim.physics.SetSeed(seed)
	}
}

// TickEntities runs the entity passes the main loop runs after the physics,
// the items and the arrows and the throwables and the fishing hooks and the orbs and the primed
// bodies, n times over, exactly as the main loop's own passes run them. It
// is how a test carries what a dispenser birthed along: the main loop
// having been stopped, no tick of its own races the ones driven here.
func (s *Server) TickEntities(n int) {
	for range n {
		s.tickItems()
		s.tickArrows()
		s.tickThrowables()
		s.tickBobbers()
		s.tickEyes()
		s.tickOrbs()
		s.tickTNT()
	}
}

// TickRedstoneTouch runs the main loop's entity-inside pass the given number
// of times: the plates, buttons, wires and dripleaves the bodies overlap.
func (s *Server) TickRedstoneTouch(n int) {
	for range n {
		s.tickRedstoneTouch()
	}
}

// TransferDripstoneFluid runs the fluid transfer of the pointed dripstone at
// the cell in the named dimension with the roll given in place of the random
// tick's die, and tells out what it changed.
func (s *Server) TransferDripstoneFluid(dimension string, x, y, z int32, roll float32) {
	dim := s.dimensionByName(dimension)
	base, _ := s.world.Time()
	changes := dim.physics.TransferDripstoneFluid(base, x, y, z, roll)
	for _, e := range dim.physics.TakeRandomEffects() {
		s.tellEffect(dim.w, e)
	}
	s.physicsResults(dim, changes, nil, nil, nil)
}

// TickPhysics drives the given number of ticks of every dimension's physics
// engine by hand — the fluids, the falls and the fire coming due and their
// results told out, the portals revalidated, the falling blocks and the
// primed TNT stood up — exactly as the main loop's own physics pass runs
// them. It is how a test runs fire out in no time at all: the main loop
// having been stopped, no tick of its own races the ones driven here, and
// the world's clock advances one tick with each, the way the main loop's
// tickTime would carry it, so the schedules one call books the next call
// comes due on.
func (s *Server) TickPhysics(n int) {
	for i := 1; i <= n; i++ {
		s.world.TickTime()
		base, _ := s.world.Time()
		// The filter reads the forced set when it is built, so a dimension
		// whose chunks a test forced after the last real tick needs a fresh one.
		gate := s.newEntityGate()
		for _, dim := range s.allDims() {
			if dim.w.AnyChunkForced() {
				dim.physics.SetTickFilter(gate.blockFilter(dim.w))
			}
			changes, falls, primes, effects := dim.physics.Tick(base)
			for _, e := range effects {
				s.tellEffect(dim.w, e)
			}
			s.physicsResults(dim, changes, falls, primes, effects)
			for _, d := range dim.physics.TakeDispenses() {
				s.runDispense(dim.w, d)
			}
			for _, t := range dim.physics.TakeStructureTriggers() {
				s.runStructureTrigger(dim.w, t)
			}
		}
	}
}

// DriveTicksHere makes the calling goroutine the one plugin listeners fire on,
// for a test that has stopped the main loop and drives TickPhysics or
// TickRandomTicks itself.
func (s *Server) DriveTicksHere() {
	if s.plugins != nil {
		s.plugins.BindTick()
	}
}

// Dispense runs one dispense visit at the cell given, out of the face named,
// the visit a booked rising edge would have the server run without booking
// one. It is how a test pours, scoops and drops by hand.
func (s *Server) Dispense(x, y, z int32, face int32) {
	s.runDispense(s.dimensionByName(world.DimensionName).w,
		world.Dispense{X: x, Y: y, Z: z, Dir: int(face)})
}

// DispenseInDimension runs one dispense visit in the named dimension, the
// dimension-taking form of Dispense; it is how a test pours a water bucket in
// the nether.
func (s *Server) DispenseInDimension(dimension string, x, y, z int32, face int32) {
	s.runDispense(s.dimensionByName(dimension).w,
		world.Dispense{X: x, Y: y, Z: z, Dir: int(face)})
}

// SetDispenseActHook plants a func between the decision and the act of every
// dispense visit, standing in for the container click a connection runs
// there in the window the dropped lock opens.
func (s *Server) SetDispenseActHook(f func()) { s.dispenseActHook = f }

// SetDispenseDecideHook plants a func between the world lookup of a dispense
// visit's block entity and the lock the decision takes it under, standing in
// for a break or an unload that takes the entity out of the world there.
func (s *Server) SetDispenseDecideHook(f func()) { s.dispenseDecideHook = f }

// SetShelfSwapHook plants a func between a shelf swap readying its block
// entities and taking their locks, standing in for a break or an unload that
// takes one out of the world there.
func (s *Server) SetShelfSwapHook(f func()) { s.shelfSwapHook = f }

// SetPearlCarryHook plants a func at the start of a cross-dimension pearl
// carry, after the landing's tick check, standing in for a death in the
// window before the goroutine carries the owner.
func (s *Server) SetPearlCarryHook(f func()) { s.carryPearlHook = f }

// SetWearSeed replaces the wear dice of every connected player with a fresh
// source over the value given, the way SpawnPass seeds the spawn's dice and
// SetPhysicsSeed the physics': the rolls the unbreaking on a worn or held
// item makes become the test's to place, and the wear a dig or a hit costs
// reproducible.
func (s *Server) SetWearSeed(seed int64) {
	for _, c := range s.playerClients() {
		c.ownMu.Lock()
		c.ownRng = newDice(seed)
		c.ownMu.Unlock()
	}
}

// SetEnvironmentSeed replaces the environment dice the survival ticks roll
// with a fresh source over the value given, which makes the oxygen bonus's
// rolls — respiration's sparing of the breath — reproducible beside the
// fire ignition the dice also serve.
func (s *Server) SetEnvironmentSeed(seed int64) {
	s.envRng.Seed(seed)
}

// SetSoundSeed reseeds the sound dice with the value given, which makes the
// pitch of the next sound played — a container's lid among them — reproducible.
func (s *Server) SetSoundSeed(seed int64) {
	s.soundRng.Seed(seed)
}

// BowTicks reports the ticks of the item draw the named player has run so
// far — a bow's, or a trident's charge, the one session both run — which
// is what a test of a shot or a throw waits on: a bow's power is its
// draw's ticks', and a trident's release is nothing before its ten.
func (s *Server) BowTicks(name string) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	c.bowMu.Lock()
	defer c.bowMu.Unlock()
	return c.bow.ticks
}

// TicksRun reports how many main-loop ticks have finished.
func (s *Server) TicksRun() uint64 { return s.ticksRun.Load() }

// AdvanceTicksRun adds n finished ticks to the count the gateway cooldowns
// are measured in, for the tests that stop the loop.
func (s *Server) AdvanceTicksRun(n uint64) { s.ticksRun.Add(n) }

// SetEndGatewayExit gives the end gateway at (x, y, z) the exit its block
// entity data names, in a loaded chunk.
func (s *Server) SetEndGatewayExit(x, y, z, ex, ey, ez int32, exact bool) {
	tag := nbt.Compound{"exit_portal": nbt.IntArray{ex, ey, ez}}
	if exact {
		tag["ExactTeleport"] = nbt.Byte(1)
	}
	s.world.SetBlockEntityTag(x, y, z, "minecraft:end_gateway", tag)
}

// EndGatewayExitInDimension reads the exit the gateway at the cell of the
// named dimension names, and whether it has one.
func (s *Server) EndGatewayExitInDimension(dimension string, x, y, z int32) (ex, ey, ez int32, ok bool) {
	tag, _ := s.dimensionByName(dimension).w.BlockEntityTag(x, y, z)
	ex, ey, ez, _, ok = rules.EndGatewayExit(tag)
	return ex, ey, ez, ok
}

// SetEndGatewayAge gives the gateway at the cell the age its block entity
// data names.
func (s *Server) SetEndGatewayAge(x, y, z int32, age int64) {
	tag, _ := s.world.BlockEntityTag(x, y, z)
	tag = maps.Clone(tag)
	if tag == nil {
		tag = nbt.Compound{}
	}
	tag["Age"] = nbt.Long(age)
	s.world.SetBlockEntityTag(x, y, z, "minecraft:end_gateway", tag)
}

// TickPortals runs one turn of every player's portal clock, for the tests
// that stop the loop and step the survival wait by the tick rather than
// racing the loop's entry into the portal.
func (s *Server) TickPortals() { s.tickPortals() }

// AttackTicks reports how many ticks the named player's attack recovery has
// wound since its last reset, the count the strength scale reads.
func (s *Server) AttackTicks(name string) int32 {
	if c := s.playerClient(name); c != nil {
		return c.attackStrengthTicks.Load()
	}
	return -1
}

// ThrowThrown puts a thrown projectile of the kind given into the first
// dimension's world at the position given, thrown along the look given by
// the entity ID given, with the seed given making the flight's rolls
// reproducible: the spread of the throw, and an egg's chicks after it. It
// is the test hook's door — the real path a use takes is the one the
// throw-by-wire tests walk, and a test that wants to know what one seed
// hatches reaches here instead of rolling dice of its own.
func (s *Server) ThrowThrown(kind entity.ThrownKind, x, y, z float64, yaw, pitch float32, ownerID int32, seed int64) int32 {
	return s.ThrowThrownInDimension(world.DimensionName, kind, x, y, z, yaw, pitch, ownerID, seed)
}

// ThrowThrownInDimension is ThrowThrown into the named dimension's world.
func (s *Server) ThrowThrownInDimension(dimension string, kind entity.ThrownKind, x, y, z float64, yaw, pitch float32, ownerID int32, seed int64) int32 {
	item := "minecraft:snowball"
	if kind == entity.ThrownEgg {
		item = "minecraft:egg"
	}
	if kind == entity.ThrownEnderPearl {
		item = "minecraft:ender_pearl"
	}
	if kind == entity.ThrownExperienceBottle {
		item = "minecraft:experience_bottle"
	}
	if kind.IsPotion() {
		item = "minecraft:splash_potion"
	}
	if kind == entity.ThrownLingeringPotion {
		item = "minecraft:lingering_potion"
	}
	id, _ := registry.ItemID(item)
	return s.spawnThrown(kind, s.dimensionByName(dimension).w, x, y, z, yaw, pitch, 0, 0, 0, ownerID, uuid.Nil, seed,
		protocol.ItemStack{ItemID: id, Count: 1})
}

// ThrowMobPotion stands up the splash potion named (a minecraft:potion name)
// at the position given, falling straight down, owned by the mob with the
// entity ID given: the path a witch's own throw takes after its choice.
func (s *Server) ThrowMobPotion(potion string, x, y, z float64, ownerID int32) {
	s.spawnMobPotion(mobPotionThrow{
		w: s.allDims()[0].w, thrower: ownerID, potion: potion,
		x: x, y: y, z: z, vy: -0.3,
	})
}

// ThrowThrownBy is ThrowThrown for a body owned by the player identity given.
func (s *Server) ThrowThrownBy(kind entity.ThrownKind, x, y, z float64, owner uuid.UUID) int32 {
	id := int32(registry.ItemSnowball)
	return s.spawnThrown(kind, s.allDims()[0].w, x, y, z, 0, 0, 0, 0, 0, 0, owner, 7,
		protocol.ItemStack{ItemID: id, Count: 1})
}

// ThrowEnderPearlInDimension puts an ender pearl in the named dimension's
// world, thrown the way ThrowThrown throws one, owned by the identity
// given: an identity no online player holds stands in for an owner who has
// gone offline, so a test can watch what a crossing does for such a pearl
// without a thrower there.
func (s *Server) ThrowEnderPearlInDimension(dimension string, x, y, z float64, yaw, pitch float32, ownerID int32, owner uuid.UUID, seed int64) int32 {
	item := int32(registry.ItemEnderPearl)
	return s.spawnThrown(entity.ThrownEnderPearl, s.dimensionByName(dimension).w,
		x, y, z, yaw, pitch, 0, 0, 0, ownerID, owner, seed,
		protocol.ItemStack{ItemID: item, Count: 1})
}

// ThrowEnderPearlOf puts an ender pearl into the overworld, owned by the
// named online player, and returns its entity ID, or 0 when they are not
// online. The seed fixes the pearl's rolls, the endermite's 5% among them.
func (s *Server) ThrowEnderPearlOf(name string, x, y, z float64, yaw, pitch float32, seed int64) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	item := int32(registry.ItemEnderPearl)
	return s.spawnThrown(entity.ThrownEnderPearl, s.allDims()[0].w, x, y, z, yaw, pitch, 0, 0, 0,
		c.player.EntityID, c.player.UUID, seed, protocol.ItemStack{ItemID: item, Count: 1})
}

// ThrowPotion puts a thrown potion carrying the potion_contents of the
// potion given into the first dimension's world, the test hook ThrowThrown
// is with the stack a brewed potion rides in: the shatter a test of the
// splash or of the cloud reads needs the contents the flight carries.
func (s *Server) ThrowPotion(kind entity.ThrownKind, potion int32, x, y, z float64, yaw, pitch float32, ownerID int32, seed int64) int32 {
	item := "minecraft:splash_potion"
	if kind == entity.ThrownLingeringPotion {
		item = "minecraft:lingering_potion"
	}
	id, _ := registry.ItemID(item)
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(potion)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return s.spawnThrown(kind, s.allDims()[0].w, x, y, z, yaw, pitch, 0, 0, 0, ownerID, uuid.Nil, seed,
		protocol.ItemStack{ItemID: id, Count: 1, Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		}})
}

// ThrowEggItem throws the egg item named — the brown one or the blue one
// where a test of what they hatch asks — the way ThrowThrown throws the
// plain egg, the seed given making the flight's rolls reproducible.
func (s *Server) ThrowEggItem(item string, x, y, z float64, yaw, pitch float32, ownerID int32, seed int64) int32 {
	id, _ := registry.ItemID(item)
	return s.spawnThrown(entity.ThrownEgg, s.allDims()[0].w, x, y, z, yaw, pitch, 0, 0, 0, ownerID, uuid.Nil, seed,
		protocol.ItemStack{ItemID: id, Count: 1})
}

// SpawnPointOf is the personal spawn point the named player carries — the
// point a bed or a /spawnpoint set — with whether vanilla counts it as
// forced, or false twice when they have none and a death would return
// them to the world spawn.
func (s *Server) SpawnPointOf(name string) (world.SpawnPos, bool, bool) {
	c := s.playerClient(name)
	if c == nil {
		return world.SpawnPos{}, false, false
	}
	own := c.spawnPoint.Load()
	if own == nil {
		return world.SpawnPos{}, false, false
	}
	return own.pos, own.forced, true
}

// SpawnPointAnglesOf is the rotation stored beside the named player's spawn
// point, the angles a forced point comes back facing, or zero twice when
// they have none.
func (s *Server) SpawnPointAnglesOf(name string) (yaw, pitch float32) {
	c := s.playerClient(name)
	if c == nil {
		return 0, 0
	}
	own := c.spawnPoint.Load()
	if own == nil {
		return 0, 0
	}
	return own.yaw, own.pitch
}

// WorldSpawn is the world spawn as the world holds it, rotation included,
// which the tests of /setworldspawn's rotation half check against.
func (s *Server) WorldSpawn() world.SpawnPos { return s.world.Spawn() }

// SetWorldSpawn moves the world spawn within its dimension.
func (s *Server) SetWorldSpawn(pos world.SpawnPos) {
	s.world.SetSpawnIn(pos, s.world.SpawnDimension())
}

// WorldSpawnDimension is the dimension the world spawn is in.
func (s *Server) WorldSpawnDimension() string { return s.world.SpawnDimension() }

// EndExitDestination is the dimension and block the end portal sends a body
// to out of the end.
func (s *Server) EndExitDestination() (dimension string, spot world.SpawnPos) {
	to, spot := s.endPortalDestination(s.dimensionByName(world.EndName))
	return to.name, spot
}

// ConsoleSourceDimension is the dimension the console runs commands in.
func (s *Server) ConsoleSourceDimension() string { return s.consoleSource().in().name }

// ConsoleSourceAcrossSpawnMove builds a console source, then moves the world
// spawn to spot in dim, and returns the dimension and position the source
// runs at afterwards.
func (s *Server) ConsoleSourceAcrossSpawnMove(dim string, spot world.SpawnPos) (string, player.Position) {
	src := s.consoleSource()
	s.world.SetSpawnIn(spot, dim)
	return src.in().name, src.Position()
}

// GameLoopPermLevel is the permission level scheduled functions run at.
func (s *Server) GameLoopPermLevel() int { return s.gameLoopSource().PermLevel() }

// TickRandomTicks drives the given number of random tick passes by hand over
// the loaded chunks within randomTickTestReach of a player, in every
// dimension. It runs randomTickChunks, the same roll-and-apply production
// runs, over a narrower set of chunks: the dice are rolled per chunk, so a
// test that watches a cell beside its player would otherwise pay for the
// couple of hundred chunks around it on each of thousands of passes. With
// the main loop stopped, no pass races the ones driven here; the dice the
// rolls come from are the physics', which SetPhysicsSeed makes the test's to
// place. Optional chunk positions restrict the pass to the cells under test.
func (s *Server) TickRandomTicks(n int, only ...world.ChunkPos) {
	base, _ := s.world.Time()
	type pass struct {
		dim    *dimension
		chunks []world.ChunkPos
	}
	var passes []pass
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, dim := range s.allDims() {
		var chunks []world.ChunkPos
		for _, pos := range dim.w.LoadedChunkPositions() {
			if len(only) != 0 && !slices.Contains(only, pos) {
				continue
			}
			for _, p := range spawnPlayersOf(clients, dim.w) {
				if p.Spectator {
					continue
				}
				dx, dz := pos.X-world.ChunkOf(p.X), pos.Z-world.ChunkOf(p.Z)
				if max(dx, -dx) <= randomTickTestReach && max(dz, -dz) <= randomTickTestReach {
					chunks = append(chunks, pos)
					break
				}
			}
		}
		passes = append(passes, pass{dim, chunks})
	}
	for i := 1; i <= n; i++ {
		for _, ps := range passes {
			s.randomTickChunks(ps.dim, base+int64(i), ps.chunks)
		}
	}
}

// randomTickTestReach is how many chunks around a player TickRandomTicks
// rolls dice in: the tests put what they watch within a block or two of
// their player.
const randomTickTestReach = 1

// SetWorldAge sets the overworld's world age, the counter the creature pass's
// four-hundred-tick gate reads — every four hundredth tick the persistent
// categories join the monsters in the spawn walk. The tests that study the
// creatures reach it through SpawnCreaturePass; this is the raw dial.
func (s *Server) SetWorldAge(age int64) { s.world.SetWorldAge(age) }

// BlockNameAt names the block at the cell, empty where the chunk is not
// loaded or the state is one the registry does not name — what the tests
// read to ask what a spawn stood on.
func (s *Server) BlockNameAt(x, y, z int32) string {
	name, _, ok := registry.BlockStateName(s.world.BlockAt(x, y, z))
	if !ok {
		return ""
	}
	return name
}

// SetMobBaby makes the mob with the given entity ID a baby or grows it —
// a baby's age set to the minus twenty thousand four hundred the growing
// counts up from, a grown body's back to nought. It is how a test of what
// the wire says of a baby, or of what a baby drops, stands the state up
// without waiting out the two in-game days the age takes.
func (s *Server) SetMobBaby(id int32, baby bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetBaby(baby)
		}
	}
}

// SetMobSoundVariant overrides the sound variant of the mob with the given
// entity ID, so a test can pin the voice a spawn's finalize dice chose. It
// reports whether the mob was found.
func (s *Server) SetMobSoundVariant(id int32, variant string) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SoundVariant = variant
				return true
			}
		}
	}
	return false
}

// SetMobVariant overrides the variant of the mob with the given entity ID,
// so a test can pin the one a spawn's finalize dice chose. It reports whether
// the mob was found.
func (s *Server) SetMobVariant(id int32, variant string) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.Variant = variant
				return true
			}
		}
	}
	return false
}

// SetMobSilent sets the Silent tag on the mob with the given entity ID, the
// way a saved body carries it, so a test can check that no wrapper lets the
// voice of a silent body through. It reports whether the mob was found.
func (s *Server) SetMobSilent(id int32, silent bool) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.Silent = silent
				return true
			}
		}
	}
	return false
}

// SetMobSize sets a cube mob's size, the way a spawn's roll would, so a test
// can give a cube a voice whose volume scales its range past sixteen. It
// reports whether the mob was found.
func (s *Server) SetMobSize(id int32, size int) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SetSize(size, true)
				return true
			}
		}
	}
	return false
}

// SetMobNoAI freezes the mob with the given entity ID where it stands and
// reports whether it found it, so a test that uses something on a wandering
// animal is not racing its stride.
func (s *Server) SetMobNoAI(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SetMetadataNoAI(true)
				return true
			}
		}
	}
	return false
}

// SetMobOwner makes the mob with the given entity ID the tame pet of the
// player with the UUID given, standing, and reports whether it found it.
func (s *Server) SetMobOwner(id int32, owner uuid.UUID) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SetMetadataTamableTame(true)
				m.SetMetadataTamableOwner(owner)
				m.Tamable.OrderedToSit = false
				m.SetMetadataTamableSittingPose(false)
				return true
			}
		}
	}
	return false
}

// SetMobTrusting makes the ocelot with the given entity ID trust players and
// reports whether it found it.
func (s *Server) SetMobTrusting(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SetMetadataTrusting(true)
				return true
			}
		}
	}
	return false
}

// MobTemptRunning reports whether the mob with the given entity ID has its
// tempt goal running.
func (s *Server) MobTemptRunning(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				return m.TemptRunning()
			}
		}
	}
	return false
}

// SetMobInvulnerable marks the mob with the given entity ID as invulnerable —
// vanilla's Entity.invulnerable — and reports whether it found it, so a test
// can plant an invulnerable bystander without a command.
func (s *Server) SetMobInvulnerable(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.Invulnerable = true
				return true
			}
		}
	}
	return false
}

// SetMobClimbing marks the mob with the given entity ID as on a wall — the
// spider state LivingEntity.isPushable reads — and reports whether it found
// it, so a test can stand a climbing body in a hull's path without driving
// the body onto a wall.
func (s *Server) SetMobClimbing(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetMetadataClimbing(true)
			return true
		}
	}
	return false
}

// MobBoxBlocked reports whether the mob with the given entity ID's own box
// crosses a block — the read a test uses to check a body that grew up did
// not end inside one. A mob that has gone reads false.
func (s *Server) MobBoxBlocked(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				return m.BoxBlocked(dim.w)
			}
		}
	}
	return false
}

// MobIsBaby reports whether the mob with the given entity ID is a baby
// still — the age below nought, as IsBaby reads it — and false for a mob
// that has gone or is of a kind that is never a baby.
func (s *Server) MobIsBaby(id int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.IsBaby()
		}
	}
	return false
}

// SetMobReinforcementBase sets the spawn_reinforcements base of the zombie
// with the entity ID, so a test can make its call certain or impossible.
func (s *Server) SetMobReinforcementBase(id int32, v float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetReinforcementBase(v)
		}
	}
}

// MakeMobLeader turns the zombie with the entity ID into a full-health
// leader (80 health, a 0.6 bonus on its call chance, door breaking), so a
// test can follow the rolled traits through a save.
func (s *Server) MakeMobLeader(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			t := m.Traits()
			t.LeaderBonus, t.LeaderHealth = 0.6, 3
			m.SetTraits(t)
			m.SetMetadataHealth(m.MaxHealth)
			m.CanBreakDoors = true
		}
	}
}

// MobLeaderState is the health, maximum health and call chance of the
// overworld zombie with the entity ID, and whether it breaks doors.
func (s *Server) MobLeaderState(id int32) (health, maxHealth, chance float64, doors bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Health, m.MaxHealth, m.ReinforcementChance(), m.CanBreakDoors
		}
	}
	return 0, 0, 0, false
}

// SetMobKnockbackResistance sets the knockback resistance of the mob with
// the entity ID, so a test can read an exact push off a zombie.
func (s *Server) SetMobKnockbackResistance(id int32, v float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetKnockbackResistance(v)
		}
	}
}

// SetMobAir sets the breath a mob has left, for the tests that check what a
// part-spent breath saves and loads as without waiting out the countdown.
func (s *Server) SetMobAir(id int32, air int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetMetadataAirSupply(air)
		}
	}
}

// MobFireTicks is how many ticks the mob of the first dimension still burns,
// zero when it is not alight or not there.
func (s *Server) MobFireTicks(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.FireTicks
		}
	}
	return 0
}

// SetMobFire lights a mob of the first dimension for the given ticks, for
// the tests that check a water splash put a burning body out without waiting
// for a fire block to light it.
func (s *Server) SetMobFire(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetMetadataFireTicks(ticks)
		}
	}
}

// MobFire is the burn ticks of a mob of the first dimension, 0 when it is
// not there.
func (s *Server) MobFire(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.FireTicks
		}
	}
	return 0
}

// MobHeadItem is the registry ID of the item on an overworld mob's head slot,
// 0 for a bare head.
func (s *Server) MobHeadItem(id int32) int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Worn[3].ItemID
		}
	}
	return 0
}

// MobHeadStack is the stack on an overworld mob's head slot.
func (s *Server) MobHeadStack(id int32) protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Worn[3]
		}
	}
	return protocol.ItemStack{}
}

// SetMobWorn puts a stack on one armour slot of an overworld mob, index as
// entity.ArmorSlotNames, for the tests of what a mob's worn enchantments do.
func (s *Server) SetMobWorn(id int32, slot int, stack protocol.ItemStack) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetEquipment(protocol.EquipFeet+slot, stack)
		}
	}
}

// SetInventoryAt puts a stack into one slot of the block entity at the given
// position, the way a click into it would leave it, so the container-move
// tests can fill a hopper or a dispenser without walking the click packets.
// It reports whether there was a block entity with such a slot to fill.
func (s *Server) SetInventoryAt(x, y, z int32, slot int, stack protocol.ItemStack) bool {
	be := s.world.BlockEntity(x, y, z)
	if be == nil {
		return false
	}
	var ok bool
	be.Use(func(b *world.BlockEntity) {
		if slot >= 0 && slot < b.Len() {
			b.Put(slot, stack)
			ok = true
		}
	})
	return ok
}

// SetInventoryInDimension fills one slot of a block entity in the named
// dimension, the dimension-taking form of SetInventoryAt.
func (s *Server) SetInventoryInDimension(dimension string, x, y, z int32, slot int, stack protocol.ItemStack) bool {
	return s.SetInventoryAtInDimension(dimension, x, y, z, slot, stack)
}

// SetInventoryAtInDimension is SetInventoryAt's dimension-taking form: a
// stack into one slot of the block entity at a position of the named
// dimension, so a container-move test can fill a hopper in the nether or the
// end without a player standing there.
func (s *Server) SetInventoryAtInDimension(dimension string, x, y, z int32, slot int, stack protocol.ItemStack) bool {
	be := s.dimensionByName(dimension).w.BlockEntity(x, y, z)
	if be == nil {
		return false
	}
	var ok bool
	be.Use(func(b *world.BlockEntity) {
		if slot >= 0 && slot < b.Len() {
			b.Put(slot, stack)
			ok = true
		}
	})
	return ok
}

// PlaceContainer stands the named container block at a position and gives it
// its empty block entity, so a test can raise a container a blast took away.
// It reports whether the chunk was loaded.
func (s *Server) PlaceContainer(x, y, z int32, name string) bool {
	state, ok := registry.FindState(name, nil)
	if !ok {
		return false
	}
	kind, ok := containerEntityKindOf(registry.BlockOf(state))
	if !ok {
		return false
	}
	// The block may already stand as the one asked for, which SetBlock
	// reports as no change; the block entity is the thing a test needs.
	s.world.SetBlock(x, y, z, state)
	return s.world.EnsureBlockEntity(x, y, z, kind) != nil
}

// PlaceContainerInDimension stands the named container block at a position in
// the named dimension with its empty block entity, the dimension-taking form
// of PlaceContainer.
func (s *Server) PlaceContainerInDimension(dimension string, x, y, z int32, name string) bool {
	w := s.dimensionByName(dimension).w
	state, ok := registry.FindState(name, nil)
	if !ok {
		return false
	}
	kind, ok := containerEntityKindOf(registry.BlockOf(state))
	if !ok {
		return false
	}
	w.SetBlock(x, y, z, state)
	return w.EnsureBlockEntity(x, y, z, kind) != nil
}

// BlastAt applies one blast in the overworld at the given point and radius,
// returning once it is applied: a test's own blast, without a primed body
// and its fuse.
func (s *Server) BlastAt(x, y, z, radius float64) {
	s.runExplosion(blast{
		w: s.world,
		x: x, y: y, z: z,
		radius:     radius,
		directType: "minecraft:tnt",
		damageType: rules.DamageTypeExplosion,
		rng:        rand.New(rand.NewSource(1)),
		// A primed TNT's blast is the tnt decay rule's.
		interaction: rules.TNTExplosionInteraction(s.world),
	})
}

// BlastAtCause applies one blast in the overworld at the given point and
// radius whose indirect source is the named online player, so a test can
// fire a creative player's blast at an invulnerable body.
func (s *Server) BlastAtCause(x, y, z, radius float64, player string) {
	var cause uuid.UUID
	s.mu.RLock()
	for id, c := range s.clients {
		if c.player.Name == player {
			cause = id
		}
	}
	s.mu.RUnlock()
	s.runExplosion(blast{
		w: s.world,
		x: x, y: y, z: z,
		radius:     radius,
		directType: "minecraft:tnt",
		damageType: rules.DamageTypeExplosion,
		rng:        rand.New(rand.NewSource(1)),
		// A primed TNT's blast is the tnt decay rule's.
		interaction: rules.TNTExplosionInteraction(s.world),
		causeUUID:   cause,
	})
}

// BlastAtMob applies one mob blast in the overworld at the given point and
// radius with the interaction the game rules resolve, so a test can fire a
// blast the mob_griefing veto leaves blocklike entities out of.
func (s *Server) BlastAtMob(x, y, z, radius float64) {
	s.runExplosion(blast{
		w: s.world,
		x: x, y: y, z: z,
		radius:      radius,
		directType:  "minecraft:creeper",
		damageType:  rules.DamageTypeExplosion,
		rng:         rand.New(rand.NewSource(1)),
		interaction: rules.MobExplosionInteraction(s.world),
	})
}

// HasBlockEntity reports whether a block entity stands at a position.
func (s *Server) HasBlockEntity(x, y, z int32) bool {
	return s.world.BlockEntity(x, y, z) != nil
}

// MoveFromTo runs the locked move a hopper's turn makes of a container found
// before a blast, pulling one item from into the container at intoAt, and
// reports whether it ran: it runs only while from is still the world's, so a
// blast that has taken from out refuses it.
func (s *Server) MoveFromTo(from *world.BlockEntity, intoAt [3]int32) bool {
	into := s.world.BlockEntity(intoAt[0], intoAt[1], intoAt[2])
	if into == nil {
		return false
	}
	ran := false
	singleTarget{from}.use(s.world, containerMove{fn: func(src inventory.Block) {
		ran, _ = inventory.Suck(src, containerSlots(into, s.world))
	}})
	return ran
}

// HopperCooldownAt reads the move cooldown the hopper at the position
// carries, the ticks of quiet vanilla's setCooldown plants between two of
// its moves. The least int there is when no hopper stands at the position.
func (s *Server) HopperCooldownAt(x, y, z int32) int32 {
	be := s.world.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindHopper {
		return math.MinInt32
	}
	var cooldown int32
	be.Use(func(b *world.BlockEntity) { cooldown = b.Cooldown })
	return cooldown
}

// TestArrow is one arrow in flight, as a test reads it: where it stands and
// what it moves with.
type TestArrow struct {
	ID         int32
	X, Y, Z    float64
	VX, VY, VZ float64
	Burning    bool
	Weapon     protocol.ItemStack
	Pickup     string // "allowed", "creative" or "disallowed"
	// PickupStack is the item the arrow is saved with and hands back.
	PickupStack protocol.ItemStack
	PostHit     []string // names of the effects a landed hit leaves in the body
	// CrossbowHit and PierceLevel are the crossbow shot's hit sound and the
	// bodies it passes through.
	CrossbowHit bool
	PierceLevel int32
	// Foil is a trident's synced enchantment glint.
	Foil bool
}

// ArrowsInDimension lists the arrows of the named dimension still flying or
// stuck, with the velocity each was last told with.
func (s *Server) ArrowsInDimension(dimension string) []TestArrow {
	w := s.dimensionByName(dimension).w
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	var arrows []TestArrow
	for _, it := range s.arrows {
		if it.arrow.Dead || it.w != w {
			continue
		}
		arrows = append(arrows, TestArrow{
			ID: it.arrow.ID,
			X:  it.arrow.X, Y: it.arrow.Y, Z: it.arrow.Z,
			VX: it.arrow.VX, VY: it.arrow.VY, VZ: it.arrow.VZ,
			Burning:     it.arrow.FireTicks > 0,
			Weapon:      it.arrow.Weapon,
			Pickup:      [...]string{entity.PickupDisallowed: "disallowed", entity.PickupAllowed: "allowed", entity.PickupCreativeOnly: "creative"}[it.arrow.Pickup],
			PickupStack: it.arrow.PickupStack,
			PostHit:     postHitNames(it),
			CrossbowHit: it.arrow.HitSound == crossbowHitSound, PierceLevel: it.arrow.PierceLevel,
			Foil: it.arrow.Foil,
		})
	}
	return arrows
}

// TestThrown is one thrown projectile in flight in a world, as a test reads
// it: the identity it is addressed by, the throw it is, where it flies and
// how fast.
type TestThrown struct {
	ID      int32
	UUID    string
	X, Y, Z float64
	VY      float64
	Kind    string
}

// ThrownInDimension lists the thrown projectiles of the named dimension
// that are still flying, the throws they are and the identities they
// carry, for the tests that check a crossing took one across.
func (s *Server) ThrownInDimension(dimension string) []TestThrown {
	w := s.dimensionByName(dimension).w
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	var out []TestThrown
	for _, it := range s.thrown {
		if it.throwable.Dead || it.w != w {
			continue
		}
		out = append(out, TestThrown{
			ID:   it.throwable.ID,
			UUID: it.uuid.String(),
			X:    it.throwable.X, Y: it.throwable.Y, Z: it.throwable.Z,
			VY:   it.throwable.VY,
			Kind: registry.EntityTypes[it.throwable.Kind.EntityType()],
		})
	}
	return out
}

// GiveEffect gives the named online player a status effect, the way
// /effect give does: not ambient, the icon shown with the particles.
func (s *Server) GiveEffect(name, effectName string, duration, amplifier int32, particles bool) {
	if c := s.playerClient(name); c != nil {
		c.AddEffect(effect.New(registry.MustMobEffect(effectName), duration, amplifier, false, particles, particles))
	}
}

// PlayerEffect reads the named online player's instance of a status effect.
func (s *Server) PlayerEffect(name, effectName string) (duration, amplifier int32, ok bool) {
	c := s.playerClient(name)
	if c == nil {
		return 0, 0, false
	}
	in, ok := c.player.Effect(registry.MustMobEffect(effectName))
	return in.Duration, in.Amplifier, ok
}

// RecordTick records a tick of the main loop that ran from start to end, so
// a test with the loop stopped can lay down a tick history of its own.
func (s *Server) RecordTick(start, end time.Time) { s.metrics.Tick(start, end) }

// RecordedTicks is how many ticks the loop has recorded; the tick-end signal
// fires before the recording, so a test counting recorded ticks waits on this.
func (s *Server) RecordedTicks() int64 { return s.metrics.Count() }

// FlushBlockAcks runs the tick end's block-ack flush for every connected
// client alone, the one step of a tick a test stopped short needs to see an
// interaction's answer without the rest a running tick would drag along.
func (s *Server) FlushBlockAcks() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		c.flushBlockAck()
	}
}

// PortalCooldownTicks returns how many ticks a crossing's arrival chunk is
// held.
func (s *Server) PortalCooldownTicks() int { return portalCooldownTicks }

// SetTimeSinceRest sets the named online player's ticks since they last slept
// or died, the count the phantom spawner reads.
func (s *Server) SetTimeSinceRest(name string, ticks int) {
	if c := s.playerClient(name); c != nil {
		c.resetStat(player.Custom(registry.CustomStatTimeSinceRest))
		c.awardStat(player.Custom(registry.CustomStatTimeSinceRest), int32(ticks))
	}
}

// TimeSinceRest is the named online player's ticks since they last slept or
// died, or -1 for a player who is not online.
func (s *Server) TimeSinceRest(name string) int {
	if c := s.playerClient(name); c != nil {
		return int(c.statValue(player.Custom(registry.CustomStatTimeSinceRest)))
	}
	return -1
}

// PhantomPass runs one tick of every dimension's phantom spawner with its dice
// replaced by a random source seeded by the value given.
func (s *Server) PhantomPass(seed int64) {
	s.spawnDisabled.Store(false)
	s.spawnRng.Seed(seed)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.tickPhantomSpawners(clients)
}

// CatSpawnerPass runs one tick of the overworld's stray cat spawner with its
// dice replaced by a random source seeded by the value given and its
// countdown reset.
func (s *Server) CatSpawnerPass(seed int64) {
	s.spawnDisabled.Store(false)
	for _, dim := range s.allDims() {
		if dim.cats != nil {
			dim.cats = &entity.CatSpawner{}
		}
	}
	s.spawnRng.Seed(seed)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.tickCatSpawner(clients)
}

// TraderSpawnerPass runs one tick of the overworld's wandering trader spawner
// with its check and saved delay due, its chance at the percent given and its
// dice seeded by the value given.
func (s *Server) TraderSpawnerPass(seed int64, chance int32) {
	s.spawnDisabled.Store(false)
	for _, dim := range s.allDims() {
		if dim.traders != nil {
			dim.traders = &entity.WanderingTraderSpawner{}
		}
	}
	s.traderData.Update(func(st *world.TraderSpawnState) { st.SpawnDelay, st.SpawnChance = 1, chance })
	s.spawnRng.Seed(seed)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.tickTraderSpawner(clients)
}

// TraderSpawnState is the wandering trader spawner's saved delay and chance.
func (s *Server) TraderSpawnState() world.TraderSpawnState {
	var out world.TraderSpawnState
	s.traderData.Update(func(st *world.TraderSpawnState) { out = *st })
	return out
}

// SetMobHand puts a stack in the main hand of an overworld mob, for the tests
// of what a held item does on its own.
func (s *Server) SetMobHand(id int32, stack protocol.ItemStack) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetEquipment(protocol.EquipMainHand, stack)
		}
	}
}

// MobHand is the main-hand stack of an overworld mob.
func (s *Server) MobHand(id int32) protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.MainHand
		}
	}
	return protocol.ItemStack{}
}

// MobGear is the six equipment slots of an overworld mob in the order
// entity.LootSlotNames lists them, with the drop chance of each.
func (s *Server) MobGear(id int32) (stacks [6]protocol.ItemStack, chances [6]float32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			stacks = [6]protocol.ItemStack{m.MainHand, m.OffHand, m.Worn[0], m.Worn[1], m.Worn[2], m.Worn[3]}
			return stacks, m.DropChances
		}
	}
	return stacks, chances
}

// SetFreeSpace replaces the probe of the usable disk space under the world,
// so that a test can make the disk look full. It has to be called before the
// first chunk save.
func (s *Server) SetFreeSpace(probe func(dir string) (uint64, error)) {
	for _, d := range s.allDims() {
		d.w.SetFreeSpace(probe)
	}
}

// FixtureCount is how many displays, interactions and markers stand in the
// worlds.
func (s *Server) FixtureCount() int {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	return len(s.fixtures)
}

// FixtureSavedTag is the compound the first standing fixture saves, nil when
// none stands.
func (s *Server) FixtureSavedTag() nbt.Compound {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	if len(s.fixtures) == 0 {
		return nil
	}
	return fixtureTag(s.fixtures[0]).Entries()
}

// EndCrystalCount is how many end crystals stand in the worlds.
func (s *Server) EndCrystalCount() int {
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	return len(s.endCrystals)
}

// SpawnEndCrystal stands a crystal at the given position of the overworld
// and returns its entity ID.
func (s *Server) SpawnEndCrystal(x, y, z float64) int32 {
	ent := s.newEndCrystalEntity(s.world, x, y, z)
	s.registerEndCrystal(ent)
	return ent.crystal.ID
}

// SpawnInvulnerableEndCrystal is SpawnEndCrystal's crystal that refuses every
// hurt, as the dragon fight's caged ones do.
func (s *Server) SpawnInvulnerableEndCrystal(x, y, z float64) int32 {
	ent := s.newEndCrystalEntity(s.world, x, y, z)
	ent.crystal.Invulnerable = true
	s.registerEndCrystal(ent)
	return ent.crystal.ID
}

// SpawnEndCrystalInDimension is SpawnEndCrystal's dimension-taking form.
func (s *Server) SpawnEndCrystalInDimension(dimension string, x, y, z float64) int32 {
	ent := s.newEndCrystalEntity(s.dimensionByName(dimension).w, x, y, z)
	s.registerEndCrystal(ent)
	return ent.crystal.ID
}

// EndCrystalPosition is where the end crystal with the given entity ID stands.
func (s *Server) EndCrystalPosition(id int32) (x, y, z float64, ok bool) {
	s.endCrystalsMu.Lock()
	defer s.endCrystalsMu.Unlock()
	for _, ent := range s.endCrystals {
		if ent.crystal.ID == id && !ent.crystal.Dead {
			return ent.crystal.X, ent.crystal.Y, ent.crystal.Z, true
		}
	}
	return 0, 0, 0, false
}

// TickEndCrystals runs the crystals' tick once.
func (s *Server) TickEndCrystals() { s.tickEndCrystals() }

// SetEyeTargets installs the locator an eye of ender flies toward, in place
// of the world's structures.
func (s *Server) SetEyeTargets(t entity.EyeTargets) { s.eyeTargets = t }

// StrikePlayerWith lands the overworld mob's blow of the given raw damage on
// the named player through the pipeline a monster's swing runs, and reports
// whether the mob and the player were found.
func (s *Server) StrikePlayerWith(mobID int32, name string, damage float64) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	strike, ok := s.testStrike(mobID, c.player.EntityID, damage)
	if !ok {
		return false
	}
	s.landingBlow(strike, []*client{c})
	return true
}

// StrikeMobWith lands the overworld mob's blow of the given raw damage on
// the overworld mob with victimID.
func (s *Server) StrikeMobWith(mobID, victimID int32, damage float64) bool {
	strike, ok := s.testStrike(mobID, victimID, damage)
	if !ok {
		return false
	}
	s.landingBlow(strike, nil)
	return true
}

// RestrictMob sets the home of the overworld mob with the given entity ID to
// the block and radius.
func (s *Server) RestrictMob(id, x, y, z, radius int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.RestrictTo(x, y, z, radius)
			return true
		}
	}
	return false
}

// MobHome is the home of the overworld mob with the given entity ID: its
// block, its radius and whether one is set. ok is false for a mob not in
// the herd.
func (s *Server) MobHome(id int32) (x, y, z, radius int32, set bool, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			x, y, z, radius, set = m.HomeRestriction()
			return x, y, z, radius, set, true
		}
	}
	return 0, 0, 0, 0, false, false
}

// testStrike is the mobStrike of one blow from the overworld mob mobID.
func (s *Server) testStrike(mobID, victimID int32, damage float64) (mobStrike, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	h := s.herdOf(s.world)
	for m := range h.book {
		if m.ID == mobID {
			return mobStrike{
				w: s.world, mob: m, victimID: victimID, damage: damage,
				x: m.X, y: m.Y, z: m.Z,
				killer: s.mobKiller(h.book[m]),
				owner:  m.CreditOwner(),
			}, true
		}
	}
	return mobStrike{}, false
}

// SetLoginTimeout shortens the time a login may take before the client is
// disconnected with slow_login.
func (s *Server) SetLoginTimeout(d time.Duration) { s.loginTimeout = d }

// OpenMerchant opens a merchant window with the given offers in front of the
// named online player, as a villager's right-click would.
func (s *Server) OpenMerchant(name string, offers []inventory.Offer) {
	if c := s.playerClient(name); c != nil {
		_ = c.openMerchant(&merchant{
			title:      protocol.Text{Translate: "entity.minecraft.villager"},
			offers:     offers,
			level:      1,
			regular:    true,
			canRestock: true,
		})
	}
}

// MerchantState reads the open merchant window's offers' uses and the
// merchant's experience.
func (s *Server) MerchantState(name string) (uses []int32, xp int32) {
	c := s.playerClient(name)
	if c == nil {
		return nil, 0
	}
	window := c.openContainerWindow()
	if window == nil || window.trader == nil {
		return nil, 0
	}
	c.player.Inventory(func(*player.Inventory) {
		for _, o := range window.trader.offers {
			uses = append(uses, o.Uses)
		}
		xp = window.trader.xp
	})
	return uses, xp
}

// TickOpenWindow runs the named player's open-window validity check, as a
// tick does.
func (s *Server) TickOpenWindow(name string) {
	if c := s.playerClient(name); c != nil {
		c.tickOpenWindow()
	}
}

// DifficultyLocked reports whether the world's difficulty is pinned.
func (s *Server) DifficultyLocked() bool { return s.world.DifficultyLocked() }

// SpawnVillagerWithOffers stands a villager of the profession, at the
// level, holding the saved Offers compound, at the given overworld position.
// Its job site is a block in the end, which the villager never acts on, so
// the brain does not take the profession from it.
func (s *Server) SpawnVillagerWithOffers(profession string, level int, offers nbt.Compound, x, y, z float64) int32 {
	return s.spawnMobArriving(s.allDims()[0], entity.TypeVillager, x, y, z, 0, nil, false, "", true, func(ent *mobEntity) {
		ent.mob.SetMetadataVillagerProfession(profession)
		ent.mob.SetMetadataVillagerLevel(level)
		ent.mob.Offers = offers
		ent.mob.SetJobSite("minecraft:the_end", 0, 0, 0)
	})
}

// SpawnVillagerNoProfession stands a jobless adult villager in the overworld,
// for a test that lets it find a job site.
func (s *Server) SpawnVillagerNoProfession(x, y, z float64) int32 {
	return s.spawnMobArriving(s.allDims()[0], entity.TypeVillager, x, y, z, 0, nil, false, "", true, func(ent *mobEntity) {
		ent.mob.SetMetadataVillagerProfession("minecraft:none")
	})
}

// SpawnVillagerAtJobSite stands a level-one villager of the profession,
// holding the Offers compound, that has the overworld block at jx, jy, jz as
// its job site.
func (s *Server) SpawnVillagerAtJobSite(profession string, offers nbt.Compound, x, y, z float64, jx, jy, jz int32) int32 {
	return s.spawnMobArriving(s.allDims()[0], entity.TypeVillager, x, y, z, 0, nil, false, "", true, func(ent *mobEntity) {
		ent.mob.SetMetadataVillagerProfession(profession)
		ent.mob.SetMetadataVillagerLevel(1)
		ent.mob.Offers = offers
		ent.mob.SetJobSite(s.allDims()[0].name, jx, jy, jz)
	})
}

// VillagerSaved reads the Offers compound and trade experience the overworld
// villager with the entity ID would save.
func (s *Server) VillagerSaved(id int32) (offers nbt.Compound, xp int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.allDims()[0].w, id); ent != nil {
		return ent.mob.Offers, ent.mob.VillagerXp
	}
	return nil, 0
}

// VillagerLevel reads the level of the overworld villager with the entity
// ID, or 0 for none.
func (s *Server) VillagerLevel(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.allDims()[0].w, id); ent != nil {
		return ent.mob.Villager.Level
	}
	return 0
}

// LaunchFireball puts a small fireball, a large fireball or a wither skull
// into the first dimension's world flying along the direction given, owned by
// the entity ID given (0 for none).
func (s *Server) LaunchFireball(kind entity.ThrownKind, x, y, z, dx, dy, dz float64, ownerID int32, explosionPower int) int32 {
	return s.spawnFireball(kind, s.allDims()[0].w, x, y, z, dx, dy, dz, ownerID, uuid.Nil, explosionPower, 1)
}

// LaunchShulkerBullet puts a shulker bullet at (x, y, z) in the first
// dimension's world, homing on the mob with the entity ID given.
func (s *Server) LaunchShulkerBullet(x, y, z float64, targetMobID int32, seed int64) {
	s.mobsMu.Lock()
	var target entity.ArrowTarget
	found := false
	for m := range s.herdOf(s.world).book {
		if m.ID == targetMobID {
			target, found = entity.MobTarget(m.ID, mobEntityType(m.Type), m), true
		}
	}
	s.mobsMu.Unlock()
	if found {
		s.spawnShulkerBullet(shulkerShot{w: s.allDims()[0].w, x: x, y: y, z: z, target: target, axis: 1, seed: seed})
	}
}

// ChunksPending is how many chunks the connected players' senders still have
// to generate or acquire, plus one per connection that has booked no view
// yet. Zero means the loading around every player is done; chunks waiting
// for a client's acknowledgement to be sent are not counted.
func (s *Server) ChunksPending() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, c := range s.clients {
		n += int(c.chunkBacklog.Load())
		if !c.chunkBooked.Load() {
			n++
		}
	}
	return n
}

// ChunkBeat returns a channel closed after the next pass of any chunk
// sender, so a waiter re-reads ChunksPending on the event rather than on a
// timer.
func (s *Server) ChunkBeat() <-chan struct{} {
	ch := make(chan struct{})
	if s.chunkBeat.CompareAndSwap(nil, &ch) {
		return ch
	}
	return *s.chunkBeat.Load()
}

// noteChunkBeat wakes the waiters on ChunkBeat.
func (s *Server) noteChunkBeat() {
	if ch := s.chunkBeat.Swap(nil); ch != nil {
		close(*ch)
	}
}

// PayFurnaceSmelts pays the cooking block at (x, y, z) its stored smelting
// experience at the block's centre, the payout a result take runs after the
// slot click, so a test can place a save between the two.
func (s *Server) PayFurnaceSmelts(x, y, z int32) {
	if be := s.world.BlockEntity(x, y, z); be != nil {
		s.payFurnaceSmelts(be, s.world, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, nil)
	}
}

// SetSpawnClock replaces the clock the date-bound spawn rolls read, such as
// the Halloween pumpkin heads.
func (s *Server) SetSpawnClock(now func() time.Time) { s.spawnNow = now }

// SetQueryClock replaces the clock the query handshake tokens age by.
func (s *Server) SetQueryClock(now func() time.Time) {
	s.queryMu.Lock()
	defer s.queryMu.Unlock()
	s.queryChallenges.now = now
}

// StatValue reports one of the named online player's statistics, by its
// stat type and key as resource locations; zero for an unknown player.
func (s *Server) StatValue(name, statType, key string) int32 {
	c := s.playerClient(name)
	if c == nil {
		return 0
	}
	stats := c.stats.Load()
	if stats == nil {
		return 0
	}
	stat, ok := player.StatNamed(statType, key)
	if !ok {
		return 0
	}
	return stats.Value(stat)
}

// ScoreOf is the holder's score in the objective, false when it has none.
func (s *Server) ScoreOf(holder, objective string) (int32, bool) {
	score, ok := s.scoreboard.Score(holder, objective)
	return score.Value, ok
}

// EndermenMoveBlockTogether has the two overworld endermen take, or set
// down, the block at the cell in one pass, as two goals do in one tick, and
// returns the block state each carries afterwards. The goals have already
// changed their carried state: taking, both carry the cell's block; setting
// down, both have given up the state passed.
func (s *Server) EndermenMoveBlockTogether(a, b int32, x, y, z int32, took bool, placed uint16) (uint16, uint16) {
	s.mobsMu.Lock()
	var ma, mb *entity.Mob
	for m := range s.herdOf(s.world).book {
		switch m.ID {
		case a:
			ma = m
		case b:
			mb = m
		}
	}
	state, carrying := placed, uint16(0)
	if took {
		state = s.world.BlockAt(x, y, z)
		carrying = state
	}
	ma.SetCarriedBlock(carrying)
	mb.SetCarriedBlock(carrying)
	s.mobsMu.Unlock()
	for _, m := range []*entity.Mob{ma, mb} {
		s.mobMovedBlock(mobBlockMove{mob: m, w: s.world, x: x, y: y, z: z, state: state, took: took})
	}
	return ma.CarriedBlock(), mb.CarriedBlock()
}

// postHitNames lists the effects a landed hit of the arrow leaves, in order.
func postHitNames(it *arrowEntity) []string {
	var names []string
	for _, in := range arrowPostEffects(it) {
		names = append(names, registry.MobEffectName(in.ID))
	}
	return names
}

// TestTamable is a tameable mob's tame, owner and sit state as a test reads
// it.
type TestTamable struct {
	Tame         bool
	Owner        string
	OrderedToSit bool
	SittingPose  bool
	MaxHealth    float64
}

// MobTamable reads the tame state of the overworld mob with the given entity
// ID.
func (s *Server) MobTamable(id int32) (TestTamable, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			t := mob.Tamable
			return TestTamable{Tame: t.Tame, Owner: t.Owner.String(), OrderedToSit: t.OrderedToSit,
				SittingPose: t.SittingPose, MaxHealth: mob.MaxHealth}, true
		}
	}
	return TestTamable{}, false
}

// TestWolf is a wolf's collar, variant and baby state as a test reads it.
type TestWolf struct {
	Collar, Variant, SoundVariant string
	Baby                          bool
	Health                        float64
}

// WolfState reads the overworld wolf with the given entity ID.
func (s *Server) WolfState(id int32) (TestWolf, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return TestWolf{Collar: mob.Collar, Variant: mob.Variant, SoundVariant: mob.SoundVariant,
				Baby: mob.IsBaby(), Health: mob.Health}, true
		}
	}
	return TestWolf{}, false
}

// CatBeds is a cat's bed and seat state as a test reads it.
type CatBeds struct {
	Lying, Relaxing, SittingPose bool
	Collar                       string
}

// CatState reads the overworld cat with the given entity ID.
func (s *Server) CatState(id int32) (CatBeds, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return CatBeds{Lying: mob.Lying, Relaxing: mob.RelaxStateOne,
				SittingPose: mob.Tamable.SittingPose, Collar: mob.Collar}, true
		}
	}
	return CatBeds{}, false
}

// SetWolfVariant pins the variant of the overworld wolf with the given entity
// ID, and its collar colour when one is named.
func (s *Server) SetWolfVariant(id int32, variant, collar string) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.Variant = variant
			if collar != "" {
				mob.SetMetadataCollar(collar)
			}
			return true
		}
	}
	return false
}

// TestFox is the state only a fox keeps.
type TestFox struct {
	// Variant is Fox.Variant's id: 0 red, 1 snow.
	Variant  int32
	Sleeping bool
	// Flags is the flag byte the client is sent.
	Flags uint8
	// Mouth is the item ID in the mouth, 0 when empty.
	Mouth int32
	// X, Y, Z is where the fox stands.
	X, Y, Z float64
}

// MobFox reads the fox state of the overworld mob with the given entity ID.
func (s *Server) MobFox(id int32) (TestFox, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id && mob.Type == entity.TypeFox {
			st := TestFox{Variant: mob.Fox.Variant, Sleeping: mob.Fox.Sleeping, Flags: mob.FoxFlags(), X: mob.X, Y: mob.Y, Z: mob.Z}
			if !mob.MainHand.Empty() {
				st.Mouth = mob.MainHand.ItemID
			}
			return st, true
		}
	}
	return TestFox{}, false
}

// SetFoxMouth puts one of the item in the mouth of the overworld fox with
// the given entity ID.
func (s *Server) SetFoxMouth(id int32, item string) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: registry.MustItem(item), Count: 1})
		}
	}
}

// FaceplantFox sets the faceplant flag on the overworld fox with the given
// entity ID.
func (s *Server) FaceplantFox(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetMetadataFoxFaceplanted(true)
		}
	}
}

// TrustFox makes the named online player trusted by the overworld fox with
// the given entity ID.
func (s *Server) TrustFox(id int32, name string) {
	c := s.playerClient(name)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id && c != nil {
			mob.FoxTrust(c.player.UUID)
		}
	}
}

// FoxTrusts reports whether the overworld fox with the given entity ID
// trusts the named online player.
func (s *Server) FoxTrusts(id int32, name string) bool {
	c := s.playerClient(name)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id && c != nil {
			return slices.Contains(mob.Fox.Trusted, c.player.UUID)
		}
	}
	return false
}

// FoxTrustCount is how many players the overworld fox with the entity ID trusts.
func (s *Server) FoxTrustCount(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return len(mob.Fox.Trusted)
		}
	}
	return -1
}

// SetMobInLoveBy is SetMobInLove with the named player as the love cause.
func (s *Server) SetMobInLoveBy(id int32, ticks int, name string) {
	c := s.playerClient(name)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil && c != nil {
		ent.mob.InLove = ticks
		ent.mob.LoveCause = c.player.UUID
	}
}

// TameMobFor makes the named online player the owner of the overworld wolf
// with the given entity ID, standing, as a taming that left it up would.
func (s *Server) TameMobFor(id int32, name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetMetadataTamableTame(true)
			mob.SetMetadataTamableOwner(c.player.UUID)
			mob.MaxHealth = 40 // vanilla: Wolf.applyTamingSideEffects
			return true
		}
	}
	return false
}

// TellBlockChanges tells the overworld's players the cells the given changes
// name, as the main loop's flush of a tick that changed them would; with the
// loop stopped it flushes at once. The states in the changes are ignored: the
// world's current ones go out.
func (s *Server) TellBlockChanges(cells ...[3]int32) {
	changes := make([]world.BlockChange, len(cells))
	for i, c := range cells {
		changes[i] = world.BlockChange{X: c[0], Y: c[1], Z: c[2]}
	}
	s.tellBlockChanges(s.world, changes)
}

// SetBlockAndTell writes a block in the overworld through the engine's
// flag-11 write: queued for the tick's flush, with its light and wake.
func (s *Server) SetBlockAndTell(x, y, z int32, state uint16) {
	s.setBlockAndTell(s.world, x, y, z, state)
}

// WaitRelights returns once every relight booked so far has finished and its
// light is queued for the next flush (or sent, with the loop stopped).
func (s *Server) WaitRelights() { s.relightWG.Wait() }

// WaitGatewayMoves returns once every gateway move booked so far has finished.
func (s *Server) WaitGatewayMoves() { s.gatewayWG.Wait() }

// SetBeforeAckHook runs f where a block interaction's acknowledgement is
// about to be sent, standing in for a handler's change landing after the
// tick's flush.
func (s *Server) SetBeforeAckHook(f func()) { s.beforeAckHook = f }

// SeedInfested reseeds the dice the infested effect rolls on players with.
func (s *Server) SeedInfested(seed int64) { s.infestRng = newDice(seed) }

// SeedMobInfested reseeds the dice the infested effect rolls on the
// overworld mob with the given entity ID.
func (s *Server) SeedMobInfested(id int32, seed int64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SeedInfested(seed)
		}
	}
}

// SeedMob reseeds the random source of the overworld mob with the given
// entity ID, so a test can pin the outcome of its per-tick rolls.
func (s *Server) SeedMob(id int32, seed int64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.Rand = rand.New(rand.NewSource(seed))
		}
	}
}

// PortalTripsDecided is how many player nether portal trips have ended,
// crossed or refused.
func (s *Server) PortalTripsDecided() int64 { return s.portalTripsDecided.Load() }

// SetMobNamed gives the overworld mob a custom name, visible or not.
func (s *Server) SetMobNamed(id int32, name string, visible bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m, ent := range s.herdOf(s.world).book {
		if m.ID == id {
			ent.name = protocol.TextTag(protocol.Text{Text: name})
			ent.nameVisible = visible
		}
	}
}

// SetMobColoredName gives the overworld mob a custom name drawn in a colour,
// which saves as a compound rather than a plain string.
func (s *Server) SetMobColoredName(id int32, name, color string) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m, ent := range s.herdOf(s.world).book {
		if m.ID == id {
			ent.name = protocol.TextTag(protocol.Text{Text: name, Color: color})
		}
	}
}

// SetMobLeashHolder ties the overworld mob to the entity with the given ID.
func (s *Server) SetMobLeashHolder(id, holder int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.LeashHolderID = holder
		}
	}
}

// MobPortalCooldown is the portal cooldown, in ticks, of the overworld mob.
func (s *Server) MobPortalCooldown(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m, ent := range s.herdOf(s.world).book {
		if m.ID == id {
			return int(ent.portal.Cooldown)
		}
	}
	return -1
}

// RandomTickDripstone runs the random tick of the pointed dripstone at the
// cell in the named dimension on dice seeded with seed, and tells out what it
// changed.
func (s *Server) RandomTickDripstone(dimension string, seed int64, x, y, z int32) {
	dim := s.dimensionByName(dimension)
	base, _ := s.world.Time()
	changes := dim.physics.RandomTickDripstone(base, seed, x, y, z)
	for _, e := range dim.physics.TakeRandomEffects() {
		s.tellEffect(dim.w, e)
	}
	s.physicsResults(dim, changes, nil, nil, nil)
}

// SetGoatRamCooldown sets the overworld goat's ram cooldown in ticks, the
// brain memory a new goat starts with a 600 to 6000 draw of.
func (s *Server) SetGoatRamCooldown(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetGoatRamCooldown(ticks)
		}
	}
}

// GiveMobEffect puts the named effect on the overworld mob with the given
// entity ID, for the tests that read what an effect changes in its doing.
func (s *Server) GiveMobEffect(id int32, name string, duration, amplifier int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.AddEffect(effect.Instance{ID: registry.MustMobEffect(name), Duration: duration, Amplifier: amplifier})
		}
	}
}

// GoatHornsLeft reports whether the overworld goat still has its left and
// right horn.
func (s *Server) GoatHornsLeft(id int32) (left, right bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return !m.Goat.NoLeftHorn, !m.Goat.NoRightHorn
		}
	}
	return false, false
}

// SetGoatLongJumpCooldown sets the overworld goat's long jump cooldown in
// ticks, the brain memory a new goat starts with a 600 to 1200 draw of.
func (s *Server) SetGoatLongJumpCooldown(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetGoatLongJumpCooldown(ticks)
		}
	}
}

// SetFrogLongJumpCooldown sets the overworld frog's long jump cooldown in ticks.
func (s *Server) SetFrogLongJumpCooldown(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetFrogLongJumpCooldown(ticks)
		}
	}
}

// GoatLongJumpCooldown reports the overworld goat's long jump cooldown in
// ticks.
func (s *Server) GoatLongJumpCooldown(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.LongJumpCooldown()
		}
	}
	return -1
}

// GoatRamCooldown reports the overworld goat's ram cooldown in ticks.
func (s *Server) GoatRamCooldown(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.RamCooldown()
		}
	}
	return -1
}

// wolfOf finds the overworld wolf with the given entity ID; the mob lock is
// held by the caller.
func (s *Server) wolfOf(id int32) *entity.Mob {
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob
		}
	}
	return nil
}

// SetWolfArmor puts the named armor, worn by the damage given, in the body
// slot of the overworld wolf with the given entity ID; an empty name clears it.
func (s *Server) SetWolfArmor(id int32, item string, damage int32) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := s.wolfOf(id)
	if m == nil {
		return false
	}
	m.SetEquipment(6, protocol.ItemStack{})
	if item != "" {
		itemID, ok := registry.ItemID(item)
		if !ok {
			return false
		}
		m.SetEquipment(6, protocol.ItemStack{ItemID: itemID, Count: 1, Damage: damage})
	}
	return true
}

// WolfArmor reads the body slot of the overworld wolf with the given entity
// ID: the item name ("" for none) and the damage it carries.
func (s *Server) WolfArmor(id int32) (item string, damage int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if m := s.wolfOf(id); m != nil && !m.Body.Empty() {
		item, _ = registry.ItemName(m.Body.ItemID)
		return item, m.Body.Damage
	}
	return "", 0
}

// SetWolfStance sets the sit order and the sitting pose of the overworld
// wolf, as the sit goal would after a few ticks.
func (s *Server) SetWolfStance(id int32, sitting bool) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := s.wolfOf(id)
	if m == nil {
		return false
	}
	m.Tamable.OrderedToSit = sitting
	m.SetMetadataTamableSittingPose(sitting)
	return true
}

// SetMobSitting sets the sitting pose of the mob with the given entity ID, as
// a sit goal would after a few ticks, and reports whether it found it.
func (s *Server) SetMobSitting(id int32, sitting bool) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			if m.ID == id {
				m.SetMetadataTamableSittingPose(sitting)
				return true
			}
		}
	}
	return false
}

// SetWolfSoundVariant pins the sound variant of the overworld wolf.
func (s *Server) SetWolfSoundVariant(id int32, variant string) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := s.wolfOf(id)
	if m == nil {
		return false
	}
	m.SoundVariant = variant
	return true
}

// AngerMob starts the persistent anger of the overworld wolf, against nobody
// in particular, with the ticks given.
func (s *Server) AngerMob(id int32, ticks int) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := s.wolfOf(id)
	if m == nil {
		return false
	}
	m.SetAnger(ticks, uuid.Nil, false)
	return true
}

// WolfVoice is the sound events the overworld wolf would speak for its idle
// moment, a hurt and its death.
func (s *Server) WolfVoice(id int32) (ambient, hurt, death string) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := s.wolfOf(id)
	if m == nil {
		return "", "", ""
	}
	voice, _, _ := m.AmbientVoice()
	ambient = voice.Name()
	return ambient, registry.Sound(mobSound(m, true).ID + 1).Name(), registry.Sound(mobSound(m, false).ID + 1).Name()
}

// MobIDsOfKind is the entity IDs of the live mobs of the kind, oldest first.
func (s *Server) MobIDsOfKind(kind string) []int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var ids []int32
	for m := range s.herdOf(s.world).book {
		if m.Type.Name() == kind {
			ids = append(ids, m.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// MobCrossbow reports whether the overworld mob with the given entity ID is
// charging its crossbow and whether one is loaded, false for a mob not in
// the herd.
func (s *Server) MobCrossbow(id int32) (charging, loaded, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			return mob.ChargingCrossbow, mob.CrossbowLoaded(), true
		}
	}
	return false, false, false
}

// MobRavagerTicks reads the overworld ravager's attack, stun and roar
// countdowns, false for a mob not in the herd.
func (s *Server) MobRavagerTicks(id int32) (attack, stun, roar int, ok bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			attack, stun, roar = mob.RavagerTicks()
			return attack, stun, roar, true
		}
	}
	return 0, 0, 0, false
}

// SetMobRavagerTicks sets the overworld ravager's attack, stun and roar
// countdowns.
func (s *Server) SetMobRavagerTicks(id int32, attack, stun, roar int) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetRavagerTicks(attack, stun, roar)
			return true
		}
	}
	return false
}

// ThrownTagOf is the compound a thrown projectile with the entity ID saves,
// plus its entity-ID owner and velocity.
func (s *Server) ThrownTagOf(id int32) (tag nbt.Compound, ownerID int32, vx, vy, vz float64, ok bool) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.throwable.ID == id && !it.throwable.Dead {
			t := it.throwable
			return thrownTag(it).Entries(), t.OwnerID, t.VX, t.VY, t.VZ, true
		}
	}
	return nil, 0, 0, 0, 0, false
}

// TestRaid is one raid as a test reads it.
type TestRaid struct {
	X, Y, Z       int
	Started       bool
	Active        bool
	Groups        int
	OmenLevel     int
	Status        string
	Alive         int
	Progress      float32
	BarPlayers    int
	BarTitleKind  int
	BarRemaining  int
	BarWire       uuid.UUID
	LeaderWaveOne bool
}

// Raids lists the overworld's raids.
func (s *Server) Raids() []TestRaid {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	var out []TestRaid
	for _, r := range s.allDims()[0].raids.All() {
		c := r.Center()
		title := r.BarTitle()
		out = append(out, TestRaid{
			X: c.X, Y: c.Y, Z: c.Z, Started: r.Started(), Active: r.Active(), Groups: r.GroupsSpawned(),
			OmenLevel: r.OmenLevel(), Status: r.Status().String(), Alive: r.Alive(), Progress: r.Progress(),
			BarPlayers: len(r.BarPlayers()), BarTitleKind: int(title.Kind), BarRemaining: title.Remaining,
			BarWire: r.BarWire(), LeaderWaveOne: r.Leader(1) != nil,
		})
	}
	return out
}

// VillageNear reports whether the block is within one section of a village
// point of interest a villager holds a ticket on.
func (s *Server) VillageNear(dimension string, x, y, z int32) bool {
	return s.dimensionByName(dimension).isVillage(int(x), int(y), int(z))
}

// ClaimedPOIsAround counts the overworld's claimed village points of interest
// within r blocks of the block.
func (s *Server) ClaimedPOIsAround(x, y, z int32, r int) int {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	return len(s.dimensionByName("minecraft:overworld").villages.OccupiedAround(int(x), int(y), int(z), r))
}

// RaidsIn counts the raids of the dimension.
func (s *Server) RaidsIn(dimension string) int {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	return len(s.dimensionByName(dimension).raids.All())
}

// SaveRaids writes the overworld's raids.dat now.
func (s *Server) SaveRaids() { s.saveRaids() }

// ReloadRaids drops the overworld's raids and reads raids.dat again.
func (s *Server) ReloadRaids() {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	for _, d := range s.allDims() {
		s.loadDimensionRaids(d)
	}
}

// MobSaveTag is the entity compound the overworld mob with the ID is saved as.
func (s *Server) MobSaveTag(id int32) (nbt.Compound, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil {
		return mobTag(ent), true
	}
	return nil, false
}

// SpawnMobFromTag stands the overworld mob a saved compound describes at the
// position, as a chunk load does, with a fresh UUID.
func (s *Server) SpawnMobFromTag(tag nbt.Compound, x, y, z float64) int32 {
	tag = maps.Clone(tag)
	delete(tag, "UUID")
	id, err := s.spawnSummonedMobFacing(s.allDims()[0], entity.MobTypeOf(string(tag["id"].(nbt.String))), x, y, z, tag, nil)
	if err != nil {
		return 0
	}
	return id
}

// MobRaid reports whether an overworld mob is counted by a raid, and the wave.
func (s *Server) MobRaid(id int32) (inRaid bool, wave int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m, ent := range s.herdOf(s.world).book {
		if m.ID == id && ent.raider != nil {
			return ent.raider.raid != nil, ent.raider.wave
		}
	}
	return false, 0
}

// ShulkerBulletState reads the entity it homes on, the steps left in its leg,
// its creep direction (-1 for none) and its delta to the target off the
// bullet with the identity given.
func (s *Server) ShulkerBulletState(id uuid.UUID) (targetID int32, steps int, dir int8, delta [3]float64, ok bool) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.uuid == id && it.throwable.Bullet != nil {
			steps, dir, delta[0], delta[1], delta[2] = it.throwable.BulletSave()
			return it.throwable.BulletTargetID(), steps, dir, delta, true
		}
	}
	return 0, 0, 0, delta, false
}

// TuneShulkerBullet sets the steps left in the leg, the direction and the
// delta to the target of the bullet with the identity given, for the tests
// that save a bullet mid-leg.
func (s *Server) TuneShulkerBullet(id uuid.UUID, steps int, dir int8, delta [3]float64) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.uuid == id && it.throwable.Bullet != nil {
			it.throwable.RestoreBullet(it.throwable.BulletTargetID(), steps, dir, delta[0], delta[1], delta[2])
		}
	}
}

// TuneFireballAcceleration sets how much of its heading the fireball with
// the identity given adds to its velocity each tick.
func (s *Server) TuneFireballAcceleration(id uuid.UUID, power float64) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.uuid == id {
			it.throwable.AccelerationPower = power
		}
	}
}

// MakeSkullDangerous charges the wither skull with the identity given.
func (s *Server) MakeSkullDangerous(id uuid.UUID) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.uuid == id {
			it.throwable.Dangerous = true
		}
	}
}

// TestVex is a vex's own state for the tests that read what the wire does not
// say.
type TestVex struct {
	Charging  bool
	Limited   bool
	LifeTicks int
	OwnerID   int32
	BoundSet  bool
	BoundX    int32
	BoundY    int32
	BoundZ    int32
}

// MobVexState reads the vex with the ID.
func (s *Server) MobVexState(id int32) (TestVex, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID != id || m.Type != entity.TypeVex {
			continue
		}
		v := TestVex{Charging: m.VexCharging(), OwnerID: m.VexOwnerID()}
		v.LifeTicks, v.Limited = m.VexLife()
		v.BoundX, v.BoundY, v.BoundZ, v.BoundSet = m.VexBound()
		return v, true
	}
	return TestVex{}, false
}

// SetMobVexLife gives the vex a limited life of the ticks given.
func (s *Server) SetMobVexLife(id int32, ticks int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id && m.Type == entity.TypeVex {
			m.SetVexLife(ticks)
		}
	}
}

// SetMobVexBound sets the block the vex wanders around.
func (s *Server) SetMobVexBound(id int32, x, y, z int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id && m.Type == entity.TypeVex {
			m.SetVexBound(x, y, z)
		}
	}
}

// MobOffHand is the off-hand stack of an overworld mob.
func (s *Server) MobOffHand(id int32) protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.OffHand
		}
	}
	return protocol.ItemStack{}
}

// MobInventory is the filled inventory slots of an overworld piglin or villager.
func (s *Server) MobInventory(id int32) []protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.CarriedInventory()
		}
	}
	return nil
}

// MobMainHand is the main-hand stack of an overworld mob.
func (s *Server) MobMainHand(id int32) protocol.ItemStack {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.MainHand
		}
	}
	return protocol.ItemStack{}
}

// MobSpell is the spell byte of the spellcaster with the ID.
func (s *Server) MobSpell(id int32) uint8 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Spell()
		}
	}
	return 0
}

// MobColour is the wool colour of the sheep with the ID.
func (s *Server) MobColour(id int32) string {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			return m.Colour
		}
	}
	return ""
}

// SetMobColour dyes the sheep with the ID.
func (s *Server) SetMobColour(id int32, colour string) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetMetadataColour(colour)
		}
	}
}

// FangsCount is the number of evoker fangs standing in the worlds.
func (s *Server) FangsCount() int {
	s.fangs.mu.Lock()
	defer s.fangs.mu.Unlock()
	return len(s.fangs.list)
}

// SetMobVexOwner makes the evoker with ownerID the summoner of the vex, by
// entity ID and by the identity a save keeps.
func (s *Server) SetMobVexOwner(id, ownerID int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	book := s.herdOf(s.world).book
	var owner uuid.UUID
	for m, ent := range book {
		if m.ID == ownerID {
			owner = ent.uuid
			break
		}
	}
	for m := range book {
		if m.ID == id && m.Type == entity.TypeVex {
			m.SetVexOwner(ownerID)
			m.SetVexOwnerUUID(owner)
		}
	}
}

// SpawnFangs stands overworld fangs that bite at once, owned by the entity
// ownerID names (0 for none).
func (s *Server) SpawnFangs(x, y, z float64, ownerID int32) {
	s.spawnFangs(fangsCast{w: s.world, owner: ownerID, x: x, y: y, z: z, warmup: 0})
}

// RunWindBurst bursts a wind charge at the centre given, owned by nobody.
func (s *Server) RunWindBurst(x, y, z float64) {
	s.burstWindCharge(windBurst{w: s.world, x: x, y: y, z: z, rng: rand.New(rand.NewSource(1))})
}

// RunBreezeWindBurst bursts a breeze's wind charge at the centre given.
func (s *Server) RunBreezeWindBurst(x, y, z float64) {
	s.burstWindCharge(windBurst{w: s.world, x: x, y: y, z: z, rng: rand.New(rand.NewSource(1)), breeze: true})
}

// RiddenMobIDs is the entity IDs of the live mobs carrying a player.
func (s *Server) RiddenMobIDs() []int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var ids []int32
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if !m.Discarded() && len(m.PassengerIDs()) > 0 {
				ids = append(ids, m.ID)
			}
		}
	}
	return ids
}

// SetMobRideTarget gives the baby piglin the ID names the baby hoglin with
// the target ID as its ride target, in place of the ten-to-forty-second roll.
func (s *Server) SetMobRideTarget(id, target int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetRideTarget(target)
		}
	}
}

// MakeSkullDangerousByID charges the wither skull with the entity ID given.
func (s *Server) MakeSkullDangerousByID(id int32) {
	s.arrowsMu.Lock()
	defer s.arrowsMu.Unlock()
	for _, it := range s.thrown {
		if it.throwable.ID == id {
			it.throwable.Dangerous = true
		}
	}
}

// PutOnTeam makes the named member (a player's name, a mob's UUID) join the
// team, adding the team first.
func (s *Server) PutOnTeam(team, member string) {
	s.scoreboard.AddTeam(team, nbt.String(team))
	s.scoreboard.JoinTeam(team, []string{member})
}

// TeamOf names the team the member (a player's name, a mob's UUID) is on.
func (s *Server) TeamOf(member string) (string, bool) { return s.scoreboard.PlayerTeam(member) }

// SetChatKeyProvider injects service trust roots before any clients connect.
func (s *Server) SetChatKeyProvider(provider chatsecurity.KeyProvider) { s.chatKeys = provider }

// VillagerActivity reads an overworld villager's activity, absent from metadata.
func (s *Server) VillagerActivity(id int32) string {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.allDims()[0].mobs.herd {
		if m.ID == id {
			return m.VillagerActivity()
		}
	}
	return ""
}

// SetMobTargetPlayer makes the overworld mob with the given entity ID target
// the named online player, as a target goal's pick would.
func (s *Server) SetMobTargetPlayer(id int32, name string) bool {
	c := s.playerClient(name)
	if c == nil {
		return false
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetMetadataTargetID(c.player.EntityID)
			return true
		}
	}
	return false
}

// HurtMobByMob lands a projectile hurt of the type on the overworld mob
// victim, credited to the overworld mob shooter, as the shooter's skull would.
func (s *Server) HurtMobByMob(victim, shooter int32, amount float64, typ any) {
	damageType := testDamageType(typ)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == victim {
			m.HurtByProjectile(amount, damageType, 0, 0, shooter, false, false)
		}
	}
}

// PlaceMob puts the overworld mob with the given entity ID at the point and
// stills it, for a test that holds a flier where it stands.
func (s *Server) PlaceMob(id int32, x, y, z float64) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.SetPos(x, y, z)
			m.VX, m.VY, m.VZ = 0, 0, 0
		}
	}
}

// SetDragonPhase forces an overworld dragon phase at the named player.
func (s *Server) SetDragonPhase(id int32, phase int, name string) bool {
	var target int32
	if c := s.playerClient(name); c != nil {
		target = c.player.EntityID
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil && ent.mob.Type == entity.TypeEnderDragon {
		ent.mob.SetDragonPhase(phase, target)
		return true
	}
	return false
}

// MobDragonPhase returns an overworld dragon's phase, or -1 if no such dragon exists.
func (s *Server) MobDragonPhase(id int32) int {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	if ent := s.mobByEntityIDLocked(s.world, id); ent != nil && ent.mob.Type == entity.TypeEnderDragon {
		return ent.mob.DragonPhase()
	}
	return -1
}

// PatrolSpawnerPass runs a due overworld patrol attempt with seeded dice.
func (s *Server) PatrolSpawnerPass(seed int64) {
	s.spawnDisabled.Store(false)
	for _, dim := range s.allDims() {
		if dim.patrols != nil {
			dim.patrols = &entity.PatrolSpawner{}
		}
	}
	s.spawnRng.Seed(seed)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.tickPatrolSpawner(clients)
}

// parkPatrols pushes every patrol countdown out of reach, so a seeded spawn
// pass counts only the creatures it is about.
func (s *Server) parkPatrols() {
	for _, dim := range s.allDims() {
		if dim.patrols != nil {
			dim.patrols = entity.NewPatrolSpawner(1 << 30)
		}
	}
}

// ForceGolemFlowerOffer forces the next eligible offer roll of the named golem.
func (s *Server) ForceGolemFlowerOffer(id int32) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == id {
			m.ForceGolemFlowerOffer()
			return
		}
	}
}

// ForceVillageSiege forces tonight's siege; the caller holds the tick loop.
func (s *Server) ForceVillageSiege(seed int64) {
	s.spawnRng.Seed(seed)
	for _, dim := range s.allDims() {
		if dim.siege != nil {
			dim.siege.ForceTonight()
		}
	}
}

// VillageSiegePass runs only the siege spawner on a held server.
func (s *Server) VillageSiegePass() {
	disabled := s.spawnDisabled.Swap(false)
	defer s.spawnDisabled.Store(disabled)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.tickVillageSiege(clients)
}

// ContainerOpeners is how many players and copper golems hold the container
// at the cell open.
func (s *Server) ContainerOpeners(x, y, z int32) int {
	return s.containerOpenerCount(s.world, x, y, z)
}

// SetParrotVariant sets the variant of the overworld parrot with the given
// entity ID.
func (s *Server) SetParrotVariant(id int32, variant int) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for mob := range s.herdOf(s.world).book {
		if mob.ID == id {
			mob.SetParrotVariant(variant)
		}
	}
}

// ShoulderParrots is the variant of the parrot on each of the named player's
// shoulders, -1 for a free one.
func (s *Server) ShoulderParrots(name string) (left, right int32) {
	c := s.playerClient(name)
	if c == nil {
		return -1, -1
	}
	sh := c.player.Shoulders()
	left, right = -1, -1
	if v, ok := shoulderParrotVariant(sh.Left); ok {
		left = v
	}
	if v, ok := shoulderParrotVariant(sh.Right); ok {
		right = v
	}
	return left, right
}

// RandomTickChunkPositions is the overworld chunks the next random tick pass
// would run over.
func (s *Server) RandomTickChunkPositions() []world.ChunkPos {
	return s.randomTickSelection(s.allDims()[0])
}

// ManagementHandler is the management server's HTTP handler, for a test to
// serve without Listen; the management server must be enabled.
func (s *Server) ManagementHandler() http.Handler { return s.mgmt.Handler() }

// ManagementHeartbeat sends one management heartbeat now.
func (s *Server) ManagementHeartbeat() { s.mgmt.Heartbeat() }

// ManagementAddr waits until Listen has bound the management port and
// returns its host:port, or "" if the server stops first.
func (s *Server) ManagementAddr() string {
	select {
	case <-s.mgmtBound:
		return s.mgmtAddr
	case <-s.stopReq:
		return ""
	}
}

// SetEntityWriteGate parks every dimension's entity writes before disk I/O.
func (s *Server) SetEntityWriteGate(gate func()) {
	s.entityWritesGated.Store(gate != nil)
	for _, d := range s.allDims() {
		d.w.SetEntityWriteGate(gate)
	}
}

// SetSpawnProtectionRadius sets the live spawn-protection radius for a test.
func (s *Server) SetSpawnProtectionRadius(radius int) {
	s.spawnProtection.Store(int32(radius))
	s.Config.SpawnProtection = radius
}

// MobWitherHeads is the target entity IDs of the three heads of the live
// wither with the given ID, 0 for a head with none.
func (s *Server) MobWitherHeads(id int32) [3]int32 {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range dim.mobs.book {
			if m.ID == id {
				return m.WitherHeads()
			}
		}
	}
	return [3]int32{}
}

func testDamageType(value any) registry.DamageType {
	if d, ok := value.(registry.DamageType); ok {
		return d
	}
	d, _ := registry.DamageTypeByName(value.(string))
	return d
}

// FirePluginEventForTest exercises events whose gameplay hooks are not installed yet.
func (s *Server) FirePluginEventForTest(event plugin.Event) {
	if s.plugins != nil {
		s.plugins.Fire(event)
	}
}

// SetBrainMemoryMapOrder pins native memory-map packing for currently loaded mobs.
func (s *Server) SetBrainMemoryMapOrder(hashes map[string]uint32, salt uint32) {
	order := entity.NewMemoryMapOrder(hashes, salt)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			m.SetMemoryMapOrder(order)
		}
	}
}

// SetDropChancesOrder pins native equipment-map traversal for currently loaded mobs.
func (s *Server) SetDropChancesOrder(hashes [8]uint32) {
	order := entity.NewDropChancesOrder(hashes)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		for m := range s.herdOf(dim.w).book {
			m.SetDropChancesOrder(order)
		}
	}
}

// BodyUUID is the UUID of the boat, minecart or primed TNT with the entity ID
// given, or the nil UUID.
func (s *Server) BodyUUID(id int32) uuid.UUID {
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.boat.ID == id {
			s.boatsMu.Unlock()
			return it.uuid
		}
	}
	s.boatsMu.Unlock()
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.cart.ID == id {
			s.minecartsMu.Unlock()
			return it.uuid
		}
	}
	s.minecartsMu.Unlock()
	s.tntMu.Lock()
	defer s.tntMu.Unlock()
	for _, it := range s.tnts {
		if it.tnt.ID == id {
			return it.uuid
		}
	}
	return uuid.Nil
}

// TrackedBodyVY reads the vertical velocity of the "bobber", "firework", "eye"
// or "mannequin" with the entity ID given.
func (s *Server) TrackedBodyVY(kind string, id int32) (vy float64, ok bool) {
	switch kind {
	case "bobber":
		s.bobbersMu.Lock()
		defer s.bobbersMu.Unlock()
		for _, it := range s.bobbers {
			if it.hook.ID == id {
				return it.hook.VY, true
			}
		}
	case "firework":
		s.fireworksMu.Lock()
		defer s.fireworksMu.Unlock()
		for _, it := range s.fireworks {
			if it.rocket.ID == id {
				return it.rocket.VY, true
			}
		}
	case "eye":
		s.eyesMu.Lock()
		defer s.eyesMu.Unlock()
		for _, it := range s.eyes {
			if it.eye.ID == id {
				return it.eye.VY, true
			}
		}
	case "mannequin":
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		for _, it := range s.mannequins {
			if it.mannequin.ID == id {
				return it.mannequin.VY, true
			}
		}
	}
	return 0, false
}

// PlaceBobberForTest moves the named online player's live hook to the position
// given at rest with fall banked, and returns its entity ID.
func (s *Server) PlaceBobberForTest(name string, x, y, z, fall float64) (int32, bool) {
	c := s.playerClient(name)
	if c == nil {
		return 0, false
	}
	s.bobbersMu.Lock()
	defer s.bobbersMu.Unlock()
	for _, it := range s.bobbers {
		if it.owner == c.player.UUID && !it.hook.Dead {
			h := it.hook
			h.SetPos(x, y, z)
			h.VX = 0
			h.VY = 0
			h.VZ = 0
			h.SetFallDistance(fall)
			return h.ID, true
		}
	}
	return 0, false
}

// SetMannequinForTest sets the health and banked fall of the mannequin with
// the entity ID given.
func (s *Server) SetMannequinForTest(id int32, health, fall float64) {
	s.mannequinsMu.Lock()
	defer s.mannequinsMu.Unlock()
	for _, it := range s.mannequins {
		if it.mannequin.ID == id {
			it.mannequin.Health, it.mannequin.FallDistance = health, fall
		}
	}
}

// BodyFallDistance reads the fall banked by the "bobber" or "mannequin" with
// the entity ID given.
func (s *Server) BodyFallDistance(kind string, id int32) (float64, bool) {
	switch kind {
	case "bobber":
		s.bobbersMu.Lock()
		defer s.bobbersMu.Unlock()
		for _, it := range s.bobbers {
			if it.hook.ID == id {
				return it.hook.FallDistance(), true
			}
		}
	case "mannequin":
		s.mannequinsMu.Lock()
		defer s.mannequinsMu.Unlock()
		for _, it := range s.mannequins {
			if it.mannequin.ID == id {
				return it.mannequin.FallDistance, true
			}
		}
	}
	return 0, false
}

// SpawnMannequinForTest stands a mannequin at the position given and returns
// its entity ID.
func (s *Server) SpawnMannequinForTest(x, y, z float64) int32 {
	ent := &mannequinEntity{mannequin: entity.NewMannequin(s.nextEntity(), x, y, z), w: s.world, uuid: uuid.New()}
	s.registerMannequin(ent)
	return ent.mannequin.ID
}

// FixtureIDForTest returns the entity ID of the first live fixture of kind
// (entity.FixtureInteraction, ...), or 0.
func (s *Server) FixtureIDForTest(kind string) int32 {
	s.fixturesMu.Lock()
	defer s.fixturesMu.Unlock()
	for _, ent := range s.fixtures {
		if ent.kind == kind && !ent.dead {
			return ent.id
		}
	}
	return 0
}

// SetBodyFallDistance seeds the banked fall of a tracked body.
func (s *Server) SetBodyFallDistance(kind string, id int32, fall float64) bool {
	_, ok := s.bodyFallForTest(kind, id, &fall)
	return ok
}

// TrackedFallDistance reads the banked fall of a tracked body.
func (s *Server) TrackedFallDistance(kind string, id int32) (float64, bool) {
	return s.bodyFallForTest(kind, id, nil)
}

func (s *Server) bodyFallForTest(kind string, id int32, set *float64) (float64, bool) {
	switch kind {
	case "boat":
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		for _, it := range s.boats {
			if it.boat.ID == id {
				if set != nil {
					it.boat.SetFallDistance(*set)
				}
				return it.boat.FallDistance(), true
			}
		}
	case "minecart":
		s.minecartsMu.Lock()
		defer s.minecartsMu.Unlock()
		for _, it := range s.minecarts {
			if it.cart.ID == id {
				if set != nil {
					it.cart.SetFallDistance(*set)
				}
				return it.cart.FallDistance(), true
			}
		}
	case "orb":
		s.orbsMu.Lock()
		defer s.orbsMu.Unlock()
		for _, it := range s.orbs {
			if it.orb.ID == id {
				if set != nil {
					it.orb.SetFallDistance(*set)
				}
				return it.orb.FallDistance(), true
			}
		}
	case "tnt":
		s.tntMu.Lock()
		defer s.tntMu.Unlock()
		for _, it := range s.tnts {
			if it.tnt.ID == id {
				if set != nil {
					it.tnt.SetFallDistance(*set)
				}
				return it.tnt.FallDistance(), true
			}
		}
	case "falling_block":
		s.fallingMu.Lock()
		defer s.fallingMu.Unlock()
		for _, it := range s.fallings {
			if it.fb.ID == id {
				if set != nil {
					it.fb.FallDistance = *set
				}
				return it.fb.FallDistance, true
			}
		}
	case "armor_stand":
		s.armorStandsMu.Lock()
		defer s.armorStandsMu.Unlock()
		for _, it := range s.armorStands {
			if it.stand.ID == id {
				if set != nil {
					it.stand.FallDistance = *set
				}
				return it.stand.FallDistance, true
			}
		}
	}
	return 0, false
}

// TieKnotInDimension ties a leash knot at the cell in the named dimension.
func (s *Server) TieKnotInDimension(dimension string, x, y, z int32) {
	s.tieKnot(s.dimensionByName(dimension).w, x, y, z)
}

// PutBlockEntityStack puts stack in a slot of the overworld block entity
// at x, y, z, creating the entity of the given kind.
func (s *Server) PutBlockEntityStack(kind world.BlockEntityKind, x, y, z int32, slot int, stack protocol.ItemStack) bool {
	be := s.world.EnsureBlockEntity(x, y, z, kind)
	if be == nil {
		return false
	}
	be.Use(func(b *world.BlockEntity) { b.Put(slot, stack) })
	return true
}

// SetBrewingTime sets the ticks left of the brew under way in the stand at x, y, z.
func (s *Server) SetBrewingTime(x, y, z int32, ticks int) {
	if be := s.world.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *world.BlockEntity) { b.BrewTime = ticks })
	}
}

// BlockEntityStack reads a slot of the overworld block entity at x, y, z.
func (s *Server) BlockEntityStack(x, y, z int32, slot int) (stack protocol.ItemStack) {
	if be := s.world.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *world.BlockEntity) { stack = b.Slots[slot] })
	}
	return stack
}

// BlockExplosion runs the blast of a bed or anchor at x, y, z.
func (s *Server) BlockExplosion(x, y, z int32, state uint16) {
	s.runExplosion(blast{
		w: s.world, x: float64(x) + 0.5, y: float64(y) + 0.5, z: float64(z) + 0.5,
		radius: 5, damageType: rules.DamageTypeBadRespawnPoint, rng: rand.New(rand.NewSource(1)),
		interaction: rules.BlockExplosionInteraction(s.world),
		source:      &blastSourceBlock{x: x, y: y, z: z, state: state},
	})
}

// PrimeTNTAt primes the TNT of the overworld cell at x, y, z as a redstone
// signal would.
func (s *Server) PrimeTNTAt(x, y, z int32) bool {
	return s.primeTNT(s.world, world.TNTPrime{X: x, Y: y, Z: z})
}

// DouseCauldronAt lowers the cauldron at x, y, z as a burning body would.
func (s *Server) DouseCauldronAt(x, y, z int32) { s.cauldronDoused(s.world, x, y, z) }

// SetSpawnTuning sets the overworld's natural spawn limit and tick rate of a
// category, as a plugin does through plugin.World.
func (s *Server) SetSpawnTuning(c registry.MobCategory, limit, ticks int) {
	w := pluginWorld{srv: s, dim: s.allDims()[0], host: s.pluginAdapter}
	w.SetSpawnLimit(c, limit)
	w.SetTicksPerSpawn(c, ticks)
}

// HurtMobByPlayerProjectile lands a projectile hurt of the type on the
// overworld mob victim, credited to the named online player, as their
// trident would.
func (s *Server) HurtMobByPlayerProjectile(victim int32, name string, amount float64, typ any) {
	c := s.playerClient(name)
	if c == nil {
		return
	}
	damageType := testDamageType(typ)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(s.world).book {
		if m.ID == victim {
			m.HurtByProjectile(amount, damageType, 0, 0, c.player.EntityID, false, false)
		}
	}
}

// ChunkIndexDrift checks chunk membership and UUID lookup after the loop stops.
func (s *Server) ChunkIndexDrift() error {
	for _, k := range observerTrackers() {
		if d, ok := k.(interface{ chunkDrift(*Server) error }); ok {
			if err := d.chunkDrift(s); err != nil {
				return err
			}
		}
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		h := dim.mobs
		if h.chunks.Len() != len(h.book) {
			return fmt.Errorf("%s mobs: %d live, %d filed", dim.name, len(h.book), h.chunks.Len())
		}
		for m, ent := range h.book {
			if !h.chunks.Holds(&m.Filing, entity.ChunkAt(m.X, m.Z)) {
				return fmt.Errorf("%s mob %d not filed under its chunk", dim.name, m.ID)
			}
			if got, ok := h.chunks.ByUUID(ent.uuid); !ok || got != m {
				return fmt.Errorf("%s mob %d not filed under its UUID", dim.name, m.ID)
			}
		}
	}
	return nil
}
