package server

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// ticksPerSecond is the rate the vanilla server's main loop runs at: 20 game
// ticks a second, one every 50 milliseconds. Everything the server does on a
// rhythm happens on this clock, the auto-save and the rate limit counting its
// ticks.
const (
	ticksPerSecond = 20

	// playerLatencyTicks is how many ticks apart the tab list latencies are
	// broadcast: vanilla counts to 600 and fires on the tick after.
	// vanilla: PlayerList.tick
	playerLatencyTicks = 601
	tickInterval       = time.Second / ticksPerSecond

	// defaultTimeSyncTicks is how many ticks apart the world's time is
	// broadcast. Vanilla sends the world's age alone every 20 ticks, the
	// clock entries of the join and of a /time command being where the
	// client's own clocks come from.
	// vanilla: MinecraftServer.tickChildren (tickCount % 20)
	defaultTimeSyncTicks = 20
)

// startTick runs the main loop in the background. The loop is what drives
// anything that recurs while the server is up; the accept loop that starts it
// only accepts. It is a no-op while the loop already runs.
func (s *Server) startTick() {
	if s.tickDone != nil {
		return // already running: a second loop would tick the world twice
	}
	s.tickDone = make(chan struct{})
	s.tickExited = make(chan struct{})
	s.backstopMu.Lock()
	s.tickRunning = true
	s.backstopMu.Unlock()
	s.lastSaveTick = s.ticksRun.Load()
	s.lastTimeSync = s.ticksRun.Load()
	ended := make(chan struct{})
	s.tickEnded.Store(&ended)
	go s.tickLoop(s.tickDone, s.tickExited)

	if ticks := s.autoSaveTicks.Load(); ticks > 0 {
		slog.Debug("auto-save started", "ticks", ticks)
	} else {
		slog.Debug("auto-save is off")
	}
}

// stopTick stops the main loop and waits for a tick in progress to finish
// and for the chunk saves the releases around it queued and an auto-save still
// writing, so nothing the tick wrote is still running once it returns: the
// shutdown's own saves and the closing of the worlds follow it, and a test that
// stops the loop may look at or take away the level directory the moment it is
// back. It is a no-op if the loop was never started, which is the case for a
// server that never got to Listen.
func (s *Server) stopTick() {
	if s.tickDone == nil {
		return
	}
	close(s.tickDone)
	<-s.tickExited
	s.tickDone = nil
	s.tickExited = nil
	s.bgSave.Wait()
	for _, d := range s.allDims() {
		d.w.DrainSaves()
	}
	// The stopping loop takes the keep-alive exchange with it, so the idle
	// deadline backing it stands down too. A read already waiting keeps the
	// deadline it was given, so each one is cleared in place.
	s.backstopMu.Lock()
	defer s.backstopMu.Unlock()
	s.tickRunning = false
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.keepAlive == nil {
			continue
		}
		_ = c.conn.SetReadDeadline(time.Time{})
	}
}

// armReadDeadline sets the idle deadline of the client's next play read:
// two keep-alive timeouts while the main loop runs, none while it is
// stopped. The keep-alive exchange is what decides whether a connection is
// alive; the deadline is a backstop for a client that stops reading and
// writing altogether. A stopped loop - a test driving the world by hand -
// sends nothing to answer, so a quiet client then is not a dead one.
func (s *Server) armReadDeadline(c *client) error {
	s.backstopMu.RLock()
	defer s.backstopMu.RUnlock()
	var deadline time.Time
	if s.tickRunning {
		deadline = time.Now().Add(2 * time.Duration(s.keepAliveTimeout.Load()))
	}
	return c.conn.SetReadDeadline(deadline)
}

// overloadThreshold is how far behind the loop may fall before it drops the
// backlog; overloadWarningInterval is the least time between two drops.
// Both widen with the pacing: a tick's worth twenty times over for the
// threshold, a hundred for the interval.
// vanilla: MinecraftServer.OVERLOADED_THRESHOLD_NANOS and
// OVERLOADED_WARNING_INTERVAL_NANOS, plus 20 and 100 ticks
func overloadThreshold(nanosPerTick int64) time.Duration {
	return time.Second + 20*time.Duration(nanosPerTick)
}

func overloadWarningInterval(nanosPerTick int64) time.Duration {
	return 10*time.Second + 100*time.Duration(nanosPerTick)
}

// tickClock is the time the main loop schedules its ticks by.
type tickClock interface {
	Now() time.Time
	// Wait blocks until the clock reads t, or returns false once done is
	// closed. It returns at once for a t already past.
	Wait(t time.Time, done <-chan struct{}) bool
}

// wallClock is the tickClock of a real server.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func (wallClock) Wait(t time.Time, done <-chan struct{}) bool {
	d := time.Until(t)
	if d <= 0 {
		select {
		case <-done:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

// loopClock is the clock the main loop schedules its ticks by. The keep-alive
// exchange measures its intervals on it, so a clock a test steps by ticks
// steps them too; on a real server it is the wall clock.
func (s *Server) loopClock() tickClock {
	s.tickClockMu.RLock()
	defer s.tickClockMu.RUnlock()
	if s.tickClock == nil {
		return wallClock{}
	}
	return s.tickClock
}

// tickLoop runs a tick a pacing over, the spacing the tick rate carries: a
// claimed sprint tick runs the moment the last one ended, every other one
// waits the rate out. Late ticks run back to back until the loop has
// caught up; only a backlog past the overload threshold is dropped. done is
// passed in so stopTick clearing the field does not race the wait.
// vanilla: MinecraftServer.runServer, waitUntilNextTick
func (s *Server) tickLoop(done, exited chan struct{}) {
	defer close(exited)
	defer s.nextTickNanos.Store(0)
	clock := s.loopClock()
	s.tickClockMu.RLock()
	log := s.tickLog
	s.tickClockMu.RUnlock()
	if log == nil {
		log = slog.Default()
	}
	// Unlike vanilla, the first tick is one interval out, so what a caller
	// does just after starting the loop happens before any tick.
	next := clock.Now().Add(tickInterval)
	if !clock.Wait(next, done) {
		return
	}
	// Zero: never warned, and Sub from it saturates past the interval.
	var lastWarning time.Time
	for {
		// A sprinting loop claims each iteration's tick first: a claimed
		// sprint tick runs with no wait and holds the backlog warning off,
		// and the iteration that claims the sprint's last tick finishes
		// the sprint and falls through to the plain pacing.
		// vanilla: MinecraftServer.runServer
		sprintTick := false
		if s.tickRate.Sprinting() {
			sprint, finished := s.tickRate.ClaimSprintTick(time.Now().UnixNano())
			if finished != nil {
				s.sprintReported(finished)
			}
			if sprint {
				sprintTick = true
				next = clock.Now()
				lastWarning = next
			}
		}
		if !sprintTick {
			nanosPerTick := s.tickRate.NanosPerTick()
			behind := clock.Now().Sub(next)
			if behind > overloadThreshold(nanosPerTick) && next.Sub(lastWarning) >= overloadWarningInterval(nanosPerTick) {
				ticks := int64(behind / time.Duration(nanosPerTick))
				log.Warn(fmt.Sprintf("Can't keep up! Is the server overloaded? Running %dms or %d ticks behind",
					behind.Milliseconds(), ticks))
				next = next.Add(time.Duration(ticks) * time.Duration(nanosPerTick))
				lastWarning = next
			}
			next = next.Add(time.Duration(nanosPerTick))
		}
		s.nextTickNanos.Store(next.UnixNano())
		// The sample send is outside the measured window: vanilla's
		// tickTimesNanos covers tickServer alone.
		now := time.Now()
		s.sendTickSamples(now)
		start := time.Now()
		if !s.runTick() {
			return
		}
		s.metrics.Tick(start, time.Now())
		if !s.waitForTick(clock, next, done) {
			return
		}
		if sprintTick {
			s.tickRate.EndSprintTick(time.Now().UnixNano())
		}
	}
}

// waitForTick is clock.Wait for the next tick, which on a real clock runs
// the plugin calls connections queue in the meantime: a connection that
// fires an event (a move, a chat line) waits for its listeners, and
// waiting for the next tick's start would hold every packet behind it -
// keep-alives, chunk acknowledgements, commands - by up to a tick.
// vanilla: MinecraftServer.waitUntilNextTick
func (s *Server) waitForTick(clock tickClock, next time.Time, done <-chan struct{}) bool {
	if _, real := clock.(wallClock); !real || s.plugins == nil {
		return clock.Wait(next, done)
	}
	queued := s.plugins.CallsQueued()
	for {
		d := time.Until(next)
		if d <= 0 {
			return clock.Wait(next, done)
		}
		timer := time.NewTimer(d)
		select {
		case <-timer.C:
			return true
		case <-done:
			timer.Stop()
			return false
		case <-queued:
			timer.Stop()
			s.plugins.RunCallsBetweenTicks()
		}
	}
}

// runTick runs one tick, turning a panic into a stop request so the shutdown
// still saves the world and disconnects the players; false means the tick
// panicked and the loop must end.
// vanilla: MinecraftServer.runServer (catch Throwable, then stopServer)
func (s *Server) runTick() (ok bool) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		slog.Error("Encountered an unexpected exception", "panic", p, "stack", string(debug.Stack()))
		s.crashed.Store(true)
		s.releaseTickLocks()
		// The panic skipped the tick's end, which turns mailbox wakes back on.
		s.deferWakes.Store(false)
		s.RequestStop()
		ok = false
	}()
	s.tick()
	return true
}

// lockTick takes an entity kind's lock for the tick goroutine, recording it
// so a tick panic, which skips the unlockTick, does not leave it held for the
// shutdown and the leaving clients to block on.
func (s *Server) lockTick(mu *sync.Mutex) {
	mu.Lock()
	s.tickLocks = append(s.tickLocks, mu)
	if fault := s.tickLockFault.Load(); fault != nil {
		(*fault)(mu)
	}
}

// unlockTick releases a lock lockTick took.
func (s *Server) unlockTick(mu *sync.Mutex) {
	for i, v := range slices.Backward(s.tickLocks) {
		if v == mu {
			s.tickLocks = slices.Delete(s.tickLocks, i, i+1)
			break
		}
	}
	mu.Unlock()
}

// lockedDo runs fn holding mu and releases it even when fn panics. A
// section other goroutines run too takes it rather than lockTick, whose
// record only the tick goroutine may touch.
func lockedDo(mu *sync.Mutex, fn func()) {
	mu.Lock()
	defer mu.Unlock()
	fn()
}

// releaseTickLocks releases, innermost first, the locks a panicked tick
// still held.
func (s *Server) releaseTickLocks() {
	for _, v := range slices.Backward(s.tickLocks) {
		v.Unlock()
	}
	s.tickLocks = s.tickLocks[:0]
}

// Crashed reports whether a tick panicked and stopped the main loop, or the
// watchdog found a tick overrunning max-tick-time.
func (s *Server) Crashed() bool { return s.crashed.Load() }

// tick runs one turn of the main loop. The ticking state moves first: a
// frozen game runs the steps it has left and holds the world's passes —
// the weather cycle, the clocks, the block and fluid ticks, the block
// entities and the entities — while the players keep ticking, an item, orb
// or arrow one of them touches is still picked up, every tracked entity's
// tracking keeps settling, and the sleep checks run. The falling blocks
// step ahead of the physics engine — the bodies falling in
// the entity pass, the falls a support's going spawns in the block ticks
// after — so a body's first fall is the tick after the one it was stood up
// in. The sleeping runs after the weather the night's passing may take down
// and before the clock the waking jump moves.
// vanilla: MinecraftServer.tickServer, tickChildren, ServerLevel.tick
func (s *Server) tick() {
	var pluginTickStart time.Time
	if s.plugins != nil {
		if s.plugins.Has(pluginruntime.ServerTickEnd) {
			pluginTickStart = time.Now()
		}
		s.plugins.StartTick()
		s.fireChunkEvents()
	}
	if fault := s.tickFault.Load(); fault != nil {
		(*fault)()
	}
	if paused, first := s.emptyPause.Tick(s.PlayerCount(), s.tickRate.Sprinting()); paused {
		if first {
			slog.Info(fmt.Sprintf("Server empty for %d seconds, pausing", s.emptyPause.Seconds()))
			s.autoSave()
		}
		// Connections run on their own goroutines, so skipping the
		// passes is all vanilla's tickConnection-only turn leaves.
		// vanilla: MinecraftServer.tickServer
		s.endPluginTick(pluginTickStart)
		s.signalTickEnd()
		return
	}
	// vanilla: MinecraftServer.tickChildren (suspendFlushing/resumeFlushing)
	s.deferWakes.Store(true)
	s.tickRate.Tick()
	runs := s.tickRate.Running()
	if runs {
		s.tickFunctions()
		s.tickBorders()
		s.tickWeather()
	}
	s.tickSleeping()
	s.tickMinecartTracking()
	if runs {
		s.tickTime()
		s.setItemStepDeferral(true)
		s.tickFallingBlocks()
		s.setItemStepDeferral(false)
		s.tickPhysics()
		s.setItemStepDeferral(true)
		s.tickBolts()
		s.setItemStepDeferral(false)
		s.tickBlockEntities()
		s.tickBrushes()
	}
	// Ahead of the players' tick, whose end acknowledges the blocks these
	// updates tell.
	// vanilla: ServerChunkCache.tick (broadcastChangedChunks)
	s.flushAllBlockChanges()
	s.tickPortals()
	s.tickPortalHolds()
	s.tickAutoSave()
	s.tickPlayerLatency()
	s.tickTimeSync()
	if runs {
		// Arrivals cannot join the entity pass already in progress.
		// vanilla: EntityTickList.forEach
		s.setItemStepDeferral(true)
	}
	s.tickSurvival()
	s.tickDeadBodies()
	s.tickPlayerTracking()
	s.tickWaypoints()
	// The hulls and the carts step even frozen while a player rides them.
	s.tickBoats()
	s.tickMinecarts()
	if runs {
		s.tickDragonCrystals()
		s.tickMobs()
		// Living bodies step before the projectiles that add effects to them.
		s.tickMannequins()
		s.tickItems()
		s.tickPaintings()
		s.tickFrames()
		s.tickKnots()
		s.tickArrows()
		s.tickThrowables()
		s.tickBobbers()
		s.tickClouds()
		s.tickFangs()
		s.tickOrbs()
		s.tickTNT()
		s.tickFireworks()
		s.tickNaturalSpawning()
		s.tickRaids()
		s.tickArmorStands()
		s.tickEndCrystals()
		s.tickDragonFight()
		s.tickOminousItemSpawners()
		s.tickCushions()
		s.tickEyes()
		s.finishSulfurLaunches()
		// After every move, so a press reads where each entity stands now.
		// vanilla: Entity.applyEffectsFromBlocks
		s.tickRedstoneTouch()
	} else {
		// The tracking and the touches the players still run while the
		// entities' own ticks are held. One read of where everyone stands
		// serves every kind.
		watchers := s.trackingWatchers()
		for _, watcher := range watchers {
			// A single world step can leave a shove awaiting player travel.
			// vanilla: TickRateManager.isEntityFrozen exempts players.
			if c := watcher.c; c.velocityDirty {
				c.stepVelocity(c.theWorld(), c.player.Position().OnGround)
			}
		}
		s.tickMobTracking(watchers)
		itemTracker.frozenPass(s, watchers)
		orbTracker.frozenPass(s, watchers)
		paintingTracker.frozenPass(s, watchers)
		arrowTracker.frozenPass(s, watchers)
		thrownTracker.frozenPass(s, watchers)
		bobberTracker.frozenPass(s, watchers)
		tntTracker.frozenPass(s, watchers)
		fallingTracker.frozenPass(s, watchers)
		boatTracker.frozenPass(s, watchers)
		cloudTracker.frozenPass(s, watchers)
		s.tickArmorStandTracking(watchers)
		endCrystalTracker.frozenPass(s, watchers)
		mannequinTracker.frozenPass(s, watchers)
		eyeTracker.frozenPass(s, watchers)
		s.tickItemPickups()
		s.tickOrbPickups()
		s.tickArrowPickups()
	}
	// After the players.
	// vanilla: MinecraftServer.tickChildren (gameTests)
	if runs {
		s.tickGameTests()
	}
	// Stationary effects still track command moves while their lifetimes are frozen.
	s.tickEffectTracking()
	s.tickFixtureTracking()
	s.flushDestroyed()
	s.flushPortalNotes()
	// After the entity passes and timed when it runs, so a ping is
	// stamped just before the flush that sends it: one stamped at the
	// tick's start would count a slow tick as the client's latency.
	// vanilla: MinecraftServer.tickChildren (tickConnection after the levels)
	s.tickKeepAlives(s.loopClock().Now())
	s.ticksRun.Add(1)
	s.wakeMailboxes()
	s.endPluginTick(pluginTickStart)
	s.signalTickEnd()
}

// signalTickEnd wakes whoever waits for the loop's tick to end.
func (s *Server) signalTickEnd() {
	next := make(chan struct{})
	if ended := s.tickEnded.Swap(&next); ended != nil {
		close(*ended)
	}
}

// awaitTickEnds waits for n ticks of the main loop to finish, or for limit to
// pass, whichever comes first; with no loop running it returns at once. Two
// ends are a whole tick from the call: the first may close a tick that was
// already past the step the caller wants.
func (s *Server) awaitTickEnds(n int, limit time.Duration) {
	timeout := time.NewTimer(limit)
	defer timeout.Stop()
	for range n {
		s.backstopMu.RLock()
		running := s.tickRunning
		s.backstopMu.RUnlock()
		ended := s.tickEnded.Load()
		if !running || ended == nil {
			return
		}
		select {
		case <-*ended:
		case <-timeout.C:
			return
		}
	}
}

// trackingWatcher is one player as a tracking pass reads them: the client,
// where it stands and how far it sees, read before the pass takes any
// entity lock.
type trackingWatcher struct {
	c           *client
	x, z        float64
	chunkRadius int32
}

// trackingWatchersLocked lists every player's place; s.mu is read-locked by
// the caller.
func (s *Server) trackingWatchersLocked() []trackingWatcher {
	watchers := make([]trackingWatcher, 0, len(s.clients))
	for _, c := range s.clients {
		if c.player == nil {
			continue
		}
		pos := c.player.Position()
		watchers = append(watchers, trackingWatcher{
			c: c, x: pos.X, z: pos.Z, chunkRadius: c.chunkRadius(),
		})
	}
	return watchers
}

// trackingWatchers reads where every player stands, taking the player list's
// lock: a position belongs to that lock.
func (s *Server) trackingWatchers() []trackingWatcher {
	s.mu.RLock()
	watchers := s.trackingWatchersLocked()
	s.mu.RUnlock()
	return watchers
}

// sendShows sends the pairing and removal packets a tracking pass gathered.
func sendShows(shows []outbound, showTos []showTo) {
	for i, show := range shows {
		showTos[i].c.sendOutboundIn(showTos[i].w, show)
	}
}

// tickSurvival runs the survival rhythm of every player in the world: the
// regenerating and starving and void-damaging that happens to a body on the
// tick clock, whether or not anyone is doing anything to it.
func (s *Server) tickSurvival() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	s.crammers.built, s.crammers.clients = false, clients

	for _, c := range clients {
		if c.keepAlive == nil {
			continue
		}
		// The inventory's criteria come first, where vanilla's own tick
		// opens with the menu broadcasts whose diff fires them; the recipes
		// they unlock are rewards of this tick's flushAdvancements.
		if c.equipmentDue.Swap(false) {
			c.broadcastEquipment()
		}
		c.tickInventoryAdvancements()
		c.tickAttackStrength()
		c.tickVitals()
		c.tickStats()
		c.tickAttributes()
		c.tickScoreCriteria()
		c.tickOpenWindow()
		c.tickLodestoneCompasses()
		c.tickMaps()
		// The pose follows the body's own news of the tick — the
		// swimming a swing of inputs or a move into water turned on
		// this tick, the sneak, the flight, the glide a landing body
		// comes out of — after the vitals have read the world through
		// the pose of the tick before, the one-tick lag vanilla's own
		// order runs: the pose set here is what the next tick's scans
		// stand a body in.
		c.updateSwimming()
		c.updateFallFlying()
		c.tickSpinAttack()
		// A spectator's body follows the camera it holds, and leaves it on
		// a sneak or the camera's death.
		c.tickCamera()
		// The dig a survival client left for the ticks finishes here, its
		// own update on its way before the acknowledgement behind it; then
		// the block interactions of the last tick acknowledge together,
		// behind everything they queued onto the mailbox (see ackBlock).
		c.tickDig()
		c.tickDemo()
		c.flushBlockAck()
		c.tickFlying()
		c.tickLocationAdvancements()
		c.flushAdvancements()
	}
}

// tickTime advances the world's clocks by one tick: the world's age by one
// and each registry clock by the rate it runs at, the advance_time game
// rule holding the clocks still when it is off.
func (s *Server) tickTime() {
	s.world.TickTime()
	now, _ := s.world.Time()
	for _, dim := range s.allDims() {
		if dim.plugin {
			dim.w.TickTime()
			dimNow, _ := dim.w.Time()
			dim.w.ExpireTickets(dimNow)
			continue
		}
		dim.w.ExpireTickets(now)
	}
	s.tickScheduled()
}

// tickScheduled runs the /schedule events that have come due at the world's
// age.
// vanilla: ServerLevel.tickTime
func (s *Server) tickScheduled() {
	now, _ := s.world.Time()
	s.scheduled.Tick(now, s.runScheduled)
}

// tickTimeSync broadcasts the world's age to every player often enough to
// keep the client's clock from drifting, naming no clock: the client advances
// each clock on its own at the rate a join or a /time command last carried,
// so this is a correction rather than the clock itself. The interval is
// counted in ticks, so a loop that falls behind the wall clock syncs no more
// often than it ticks.
// vanilla: MinecraftServer.tickChildren
func (s *Server) tickTimeSync() {
	n := s.ticksRun.Load()
	if s.timeSyncTicks == 0 || n-s.lastTimeSync < s.timeSyncTicks {
		return
	}
	s.lastTimeSync = n
	s.broadcastLevel(s.world, protocol.IDPlayUpdateTime, updateTimePayload(s.world))
	for _, d := range s.allDims() {
		if d.plugin {
			s.broadcastLevel(d.w, protocol.IDPlayUpdateTime, updateTimePayload(d.w))
		}
	}
}

// tickPlayerLatency broadcasts every player's latency to the whole tab list
// every 600 ticks.
// vanilla: PlayerList.tick
func (s *Server) tickPlayerLatency() {
	n := s.ticksRun.Load()
	if n-s.lastPlayerInfo < playerLatencyTicks {
		return
	}
	s.lastPlayerInfo = n
	s.mu.RLock()
	players := make([]*player.Player, 0, len(s.clients))
	for _, c := range s.clients {
		if c.player != nil {
			players = append(players, c.player)
		}
	}
	s.mu.RUnlock()
	if len(players) > 0 {
		s.broadcast(protocol.IDPlayPlayerInfoUpdate, playerInfoLatency(players))
	}
}

// updateTimePayload builds the Update Time packet for the given world: the
// world's age, followed by an entry per clock named, in the order named. The
// periodic sync names none and so carries only the age. A command that turns
// one clock names it alone — the join and an advance_time change name every
// clock, so a client learns about each of them — and the entry carries the
// clock's total, the fraction of a tick it is part-way through and the rate
// the client is to advance it by, 0 while the clock is paused or the rule
// holds the clocks still, so the client's own clock pauses with them.
// vanilla: ServerClockManager's Set Time broadcasts
func updateTimePayload(w *world.World, clocks ...string) []byte {
	worldAge, views := w.ClockViews(clocks...)
	var b protocol.Buffer
	b.PutI64(worldAge)
	b.PutVarInt(int32(len(views)))
	for _, view := range views {
		b.PutVarInt(registry.MustID("minecraft:world_clock", view.Name))
		b.PutVarLong(view.Total)
		b.PutF32(view.Fraction)
		b.PutF32(view.Rate)
	}
	return b.Bytes()
}

// tickKeepAlives runs the keep-alive exchange of every player in the world,
// pinging the ones whose interval has come round and dropping the ones whose
// ping went unanswered for too long.
func (s *Server) tickKeepAlives(now time.Time) {
	if s.keepAliveNow != nil {
		now = s.keepAliveNow()
	}
	s.tickConfigKeepAlives(now)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	// The clients are copied out so that closing a connection that has fallen
	// behind does not happen while the player list is locked, the way
	// broadcast does it.
	for _, c := range clients {
		// A client already being dropped is not dropped again: one
		// Disconnect, then the close.
		if c.keepAlive == nil || c.keepAlive.failure() != nil {
			continue
		}
		if err := c.keepAlive.tick(now,
			time.Duration(s.keepAliveInterval.Load()),
			time.Duration(s.keepAliveTimeout.Load()),
			c.sendPlayKeepAlive); err != nil {
			c.keepAlive.setFailure(err)
			// A client that stopped reading holds up only its own drop:
			// closing once its mailbox has drained unblocks the read loop,
			// which reports the reason.
			// vanilla: ServerCommonPacketListenerImpl.disconnect
			c.disconnectAfterDrain(protocol.Text{Translate: "disconnect.timeout"})
		}
		c.tickRateLimit()
		c.tickIdle()
	}
}

func (s *Server) endPluginTick(start time.Time) {
	if s.plugins != nil && s.plugins.Has(pluginruntime.ServerTickEnd) {
		mspt := float64(0)
		if !start.IsZero() {
			mspt = float64(time.Since(start)) / 1e6
		}
		s.plugins.Fire(&plugin.ServerTickEndEvent{Tick: s.plugins.Current(), MSPT: mspt})
	}
}
