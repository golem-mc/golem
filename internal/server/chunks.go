package server

import (
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// chunkRadius is the send radius in chunks: the server's view distance, or
// the client's render distance if shorter. All visibility, entity tracking
// included, uses it. viewDistance is atomic as the read loop sets it while
// the sender reads it.
func (c *client) chunkRadius() int32 {
	radius := c.srv.viewDistance.Load()
	if asked := c.viewDistance.Load(); asked > 0 && asked < radius {
		return asked
	}
	return radius
}

// chunkLoadRadius is how far around the player chunks are held: the
// simulation distance, or the send radius if larger.
func (c *client) chunkLoadRadius() int32 {
	load := c.srv.simulationDistance.Load()
	if radius := c.chunkRadius(); radius > load {
		return radius
	}
	return load
}

// skipsGeneration is true for a spectator with spectators_generate_chunks
// off, who then holds only already-loaded chunks.
// vanilla: ChunkMap.skipPlayer
func (c *client) skipsGeneration() bool {
	return c.player.GameMode() == protocol.GameModeSpectator &&
		!c.srv.world.BoolRule(world.RuleSpectatorsGenerateChunks)
}

// holdsChunks is false once a dead player has lain corpseTicks, which drops
// the chunk tickets the player held until the respawn.
// vanilla: ChunkMap.removeEntity, updatePlayerStatus
func (c *client) holdsChunks() bool {
	return !c.holdsDropped.Load()
}

// simulates reports whether the player is a centre that ticks chunks.
func (c *client) simulates() bool {
	return !c.skipsGeneration() && c.holdsChunks()
}

// chunkSender owns a connection's chunk sending, so the read loop never
// blocks on generation and sends never overlap; a dimension change replaces
// it. It books the view on a trigger and sends one paced batch per beat,
// taking a few load-radius holds on the same beat.
func (c *client) chunkSender(stop <-chan struct{}, trigger <-chan struct{}) {
	// The beat is the wall clock, not the tick loop's clock, so a stopped main
	// loop (a test driving passes itself) does not hold back remaining chunks.
	beat := time.NewTicker(tickInterval)
	defer beat.Stop()
	for {
		select {
		case <-stop:
			return
		case <-trigger:
			c.chunkMu.Lock()
			err := c.updateChunks()
			c.noteChunkBacklog()
			c.chunkMu.Unlock()
			if err != nil {
				c.conn.Close()
				return
			}
			c.srv.noteChunkBeat()
		case <-beat.C:
			c.chunkMu.Lock()
			// Take in a change whose trigger is still pending (a just-shrunk render
			// distance) first, so the batch is cut from the current view.
			err := c.updateChunks()
			if err == nil {
				err = c.sendChunkBatch()
			}
			c.chunkMu.Unlock()
			if err != nil {
				c.conn.Close()
				return
			}
			// Outside the mutex: takeHolds' state is this goroutine's alone, and
			// holding the mutex across a take would stall a disconnect or dimension
			// change.
			c.takeHolds()
			c.noteChunkBacklog()
			c.srv.noteChunkBeat()
		}
	}
}

// noteChunkBacklog publishes how many chunks the sender has yet to acquire,
// for the tests' settle hook. Sender goroutine only.
func (c *client) noteChunkBacklog() {
	c.chunkBacklog.Store(int64(len(c.generating) + len(c.pendingGeneration) + len(c.pendingHolds)))
	c.chunkBooked.Store(c.loaded != nil)
}

// startChunkSender starts the sender with a fresh trigger. It does nothing
// after closeChunkSender, so a dimension change outliving a disconnect or
// shutdown cannot leave an unstoppable sender.
func (c *client) startChunkSender() {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	if c.chunkClosed {
		return
	}
	// Pacing belongs to the connection, not a dimension's sender: a crossing
	// restarts the sender but keeps the rate the acknowledgements set.
	c.pacingMu.Lock()
	c.pacer.Start()
	c.pacingMu.Unlock()
	trigger := make(chan struct{}, 1)
	c.chunkTrigger.Store(&trigger)
	stop := make(chan struct{})
	done := make(chan struct{})
	c.chunkStop, c.chunkDone = sync.OnceFunc(func() { close(stop) }), done
	c.srv.chunkSenders.Add(1)
	go func() {
		defer close(done)
		c.chunkSender(stop, trigger)
	}()
}

// closeChunkSender stops the sender for good (disconnect, shutdown), after
// which startChunkSender refuses. The flag is set under the restart's lock,
// so a restart lands wholly before (and is stopped) or after (and starts
// nothing).
func (c *client) closeChunkSender() {
	c.chunkMu.Lock()
	c.chunkClosed = true
	c.chunkMu.Unlock()
	c.stopChunkSender()
}

// stopChunkSender stops the sender and waits, so the chunk set is the
// caller's to release. No-op with no sender.
func (c *client) stopChunkSender() {
	// Read the handles under the mutex but wait outside it: the sender
	// takes the same mutex around updateChunks, so waiting under it deadlocks.
	c.chunkMu.Lock()
	closeStop, done := c.chunkStop, c.chunkDone
	c.chunkMu.Unlock()
	if closeStop == nil {
		return
	}
	// The handles stay set after a stop, so every stopper closes and waits for
	// the sender's last writes.
	closeStop()
	if c.srv.chunkStopSeam != nil {
		c.srv.chunkStopSeam()
	}
	<-done
}

// sendChunksAsync triggers the sender without blocking the read loop. The
// one-slot trigger coalesces movement during a send into one follow-up,
// which reads the latest position anyway.
func (c *client) sendChunksAsync() {
	// A restart replaces the channel, and the read loop must not wait on
	// chunkMu, held by the sender across generation.
	trigger := c.chunkTrigger.Load()
	if trigger == nil {
		return
	}
	select {
	case *trigger <- struct{}{}:
	default:
	}
}

// updateChunks syncs the chunks around the player at two radii. Within the
// load radius chunks are generated and held, outside released; within the
// send radius held chunks are queued for sending, and sent ones outside it
// are unloaded client-side. The queue is rebuilt each pass, so chunks a
// shrunk radius or moved centre left simply drop out.
//
// Runs on the chunk sender under chunkMu; a dimension change stops the
// sender before releasing the set under the same mutex. No-op while the
// centre chunk and both radii are unchanged.
func (c *client) updateChunks() error {
	w := c.theWorld()
	// After a dimension change the client dropped everything on Respawn, so
	// release the old world's holds wholesale and start fresh.
	if c.loaded != nil && c.loadedWorld != w {
		c.releasePendingPackets()
		c.cancelGenerating(nil)
		for pos := range c.loaded.all() {
			c.loadedWorld.Release(pos)
		}
		c.loaded, c.sent, c.pendingHolds, c.pendingSend, c.pendingGeneration = nil, nil, nil, nil, nil
		c.clearSentView()
		c.wakeBeforeBatch = false
	}
	if !c.holdsChunks() {
		c.dropHolds()
		return nil
	}
	pos := c.player.Position()
	center := world.ChunkPos{X: world.ChunkOf(pos.X), Z: world.ChunkOf(pos.Z)}
	radius := c.chunkRadius()
	load := c.chunkLoadRadius()
	skip := c.skipsGeneration()

	if c.loaded != nil && center == c.center && radius == c.radius && load == c.load && skip == c.skipGen {
		return nil
	}
	if c.loaded == nil {
		c.loaded = newChunkSet(center, load)
		c.sent = newChunkSet(center, load)
		c.loadedWorld = w
	}
	c.center, c.radius, c.load, c.skipGen = center, radius, load, skip

	// The centre must arrive before the chunks it is about.
	if err := c.write(protocol.IDPlayCenterChunk, "set center chunk", func(b *protocol.Buffer) {
		b.PutVarInt(center.X)
		b.PutVarInt(center.Z)
	}); err != nil {
		return err
	}

	// Unload first so the client never holds both areas. Outside the load
	// radius the chunk is released; outside only the send radius it stays held
	// but is unloaded client-side.
	unloaded := 0
	for pos := range c.loaded.all() {
		if world.InViewOf(pos, center, load) {
			if request := c.generating[pos]; request != nil {
				request.SetPriority(world.DistanceTo(pos, center))
			}
			if c.sent.has(pos) && !world.InViewOf(pos, center, radius) {
				c.unmarkSent(pos)
				if err := c.writeUnload(pos); err != nil {
					return err
				}
			}
			continue
		}
		if err := c.unloadChunk(pos); err != nil {
			return err
		}
		unloaded++
	}

	// Departures are gone before wrapping the window onto the new centre.
	c.loaded = c.loaded.reframe(center, load)
	c.sent = c.sent.reframe(center, load)
	c.sentViewMu.Lock()
	if c.sentView != nil {
		c.sentView = c.sentView.reframe(center, load)
	}
	c.sentViewMu.Unlock()

	// Held chunks in send radius the client lacks, e.g. after raising its
	// render distance.
	queue := make([]world.ChunkPos, 0, 8)
	for pos := range c.loaded.all() {
		if c.sent.has(pos) {
			continue
		}
		if _, ok := c.generating[pos]; ok {
			continue
		}
		if world.InViewOf(pos, center, radius) {
			queue = append(queue, pos)
		}
	}

	// Holds queued from a smaller view that now fall in send radius are
	// acquired with it; taken as plain holds nothing would queue them to send.
	c.pendingHolds = slices.DeleteFunc(c.pendingHolds, func(pos world.ChunkPos) bool {
		return world.InViewOf(pos, center, radius)
	})
	missing := c.missingChunks(center, load)
	if skip {
		// Only chunks other holders already brought into memory.
		missing = slices.DeleteFunc(missing, func(pos world.ChunkPos) bool {
			return !world.InViewOf(pos, center, radius) || !w.ChunkLoaded(pos)
		})
	}
	// Keep unscheduled work as positions, so a moving view can discard it
	// before it consumes generation, lighting or a worker goroutine.
	c.pendingGeneration = nil
	for _, pos := range missing {
		if world.InViewOf(pos, center, radius) {
			// Ready terrain bypasses the generation window, including while
			// workers finish chunks left behind by a moving player.
			if w.HoldIfLoaded(pos) != nil {
				c.loaded.add(pos)
				queue = append(queue, pos)
			} else {
				c.pendingGeneration = append(c.pendingGeneration, pos)
			}
		} else {
			c.pendingHolds = append(c.pendingHolds, pos)
		}
	}
	c.pruneGenerationWindow()
	c.submitGeneration()
	// The join's fill ends with a whole tick over its chunks, so the entities
	// in their files wake (tickMobs) before the player; a command sent right
	// after joining finds them.
	if c.sent.len() == 0 && (len(queue) > 0 || len(c.generating) > 0 || len(c.pendingGeneration) > 0) {
		c.wakeBeforeBatch = true
	}
	sortClosestFirst(queue, center)
	c.pendingSend = queue
	c.syncPendingPackets()

	c.log.Debug("updated chunks",
		"center", center, "radius", radius, "load", load,
		"unloaded", unloaded, "queued", len(queue))
	return nil
}

// dropHolds releases every chunk without telling the client, which keeps
// drawing them behind the death screen. The empty view makes the respawn's
// pass hold and send the whole view again. vanilla: PlayerChunkSender.dropChunk
// skips a dead player.
func (c *client) dropHolds() {
	if c.loaded == nil {
		return
	}
	c.releasePendingPackets()
	c.cancelGenerating(nil)
	for pos := range c.loaded.all() {
		c.loadedWorld.Release(pos)
	}
	c.loaded, c.sent, c.pendingHolds, c.pendingSend, c.pendingGeneration = nil, nil, nil, nil, nil
	c.clearSentView()
	c.wakeBeforeBatch = false
}

// sortClosestFirst orders chunks by distance from the centre.
func sortClosestFirst(chunks []world.ChunkPos, center world.ChunkPos) {
	slices.SortFunc(chunks, func(a, b world.ChunkPos) int {
		return world.DistanceTo(a, center) - world.DistanceTo(b, center)
	})
}

// maxGenerating bounds a sender's acquired-but-not-ready chunks. The rest
// stay as positions and are reprioritised on movement. One maximum wire batch
// can prepare at once without a view-distance-sized backlog of worker tasks.
const maxGenerating = 64

// pruneGenerationWindow lets nearer pending terrain displace queued requests
// still inside the load radius. Running work keeps its slot until completion.
func (c *client) pruneGenerationWindow() {
	if len(c.pendingGeneration) == 0 || len(c.generating) == 0 {
		return
	}
	candidates := append([]world.ChunkPos(nil), c.pendingGeneration...)
	for pos := range c.generating {
		candidates = append(candidates, pos)
	}
	if len(candidates) <= maxGenerating {
		return
	}
	sortClosestFirst(candidates, c.center)
	for _, pos := range candidates[maxGenerating:] {
		request := c.generating[pos]
		if request == nil || !request.CancelQueued() {
			continue
		}
		delete(c.generating, pos)
		c.loaded.delete(pos)
		if world.InViewOf(pos, c.center, c.radius) {
			c.pendingGeneration = append(c.pendingGeneration, pos)
		} else {
			c.pendingHolds = append(c.pendingHolds, pos)
		}
	}
	sortClosestFirst(c.pendingGeneration, c.center)
}

func (c *client) submitGeneration() {
	if c.generating == nil {
		c.generating = make(map[world.ChunkPos]*world.ChunkRequest)
	}
	for len(c.pendingGeneration) > 0 && len(c.generating) < maxGenerating {
		pos := c.pendingGeneration[0]
		c.pendingGeneration = c.pendingGeneration[1:]
		if c.skipGen {
			if request := c.loadedWorld.RequestLoadedChunk(pos); request != nil {
				c.generating[pos] = request
				c.loaded.add(pos)
			}
			continue
		}
		c.loaded.add(pos)
		c.generating[pos] = c.loadedWorld.RequestChunk(pos, world.DistanceTo(pos, c.center))
	}
}

// collectGenerated moves generated chunks into the send queue, so each is
// sendable once ready rather than with the whole view. Chunks a shrunk
// radius left stay held, unsent. vanilla: ChunkMap.onChunkReadyToSend
func (c *client) collectGenerated() {
	if len(c.generating) == 0 {
		return
	}
	added := false
	for pos, request := range c.generating {
		select {
		case <-request.Ready():
		default:
			continue
		}
		delete(c.generating, pos)
		if !world.InViewOf(pos, c.center, c.load) {
			c.loaded.delete(pos)
			c.loadedWorld.Release(pos)
			continue
		}
		if world.InViewOf(pos, c.center, c.radius) {
			c.pendingSend = append(c.pendingSend, pos)
			added = true
		}
	}
	if added {
		sortClosestFirst(c.pendingSend, c.center)
	}
}

// cancelGenerating drops requests without waiting for obsolete terrain.
func (c *client) cancelGenerating(done *sync.WaitGroup) {
	for pos, request := range c.generating {
		request.CancelWait(done)
		delete(c.generating, pos)
		c.loaded.delete(pos)
	}
}

// batchSize charges one batch to the pacing and returns how many chunks it
// may hold; zero sends nothing.
func (c *client) batchSize() int {
	c.pacingMu.Lock()
	defer c.pacingMu.Unlock()
	// Charged before the write: the client's answer, resetting the quota, can
	// arrive before the batch is out.
	return c.pacer.Take(len(c.pendingSend))
}

// sendChunkBatch sends one batch on the sender's per-tick beat: nothing
// while a batch behind on acknowledgements or before the quota builds a
// whole chunk, otherwise as much of the queue, closest first, as the quota
// allows; a backlog fitting in one rate's worth goes whole.
// vanilla: PlayerChunkSender.sendNextChunks
func (c *client) sendChunkBatch() error {
	c.collectGenerated()
	c.submitGeneration()
	size := c.batchSize()
	if size == 0 {
		return nil
	}
	w := c.loadedWorld
	batch := c.pendingSend[:size:size]

	if err := c.writePacket(protocol.IDPlayChunkBatchStart, "chunk batch start", nil); err != nil {
		return err
	}
	if c.srv.chunkBatchSeam != nil {
		c.srv.chunkBatchSeam()
	}
	sent := 0
	for _, pos := range batch {
		// Marked before encoding: a change the encode misses was made after the
		// mark, and reaches the client as its own update.
		// vanilla: PlayerChunkSender.sendNextChunks
		c.markSent(pos)
		chunk, _ := w.Acquire(pos)
		err := c.writePacketCached(protocol.IDPlayChunkData, "chunk data", nil, w.CachedChunkPacket(chunk))
		c.releasePendingPacket(pos)
		// Acquire only fetched the chunk to encode; the client's hold is the one
		// the loaded set represents.
		w.Release(pos)
		if err != nil {
			return err
		}
		sent++
	}
	if c.wakeBeforeBatch && len(c.generating) == 0 && len(c.pendingGeneration) == 0 && size == len(c.pendingSend) {
		c.wakeBeforeBatch = false
		c.srv.awaitTickEnds(2, joinWakeLimit)
	}
	if err := c.write(protocol.IDPlayChunkBatchFinished, "chunk batch finished", func(b *protocol.Buffer) {
		b.PutVarInt(int32(sent))
	}); err != nil {
		return err
	}
	// Batch Finished has been written, so the chunks precede anything the tick
	// loop queues next (the dead join's held bar).
	c.chunkFillSent.Store(true)
	c.pendingSend = c.pendingSend[size:]
	return nil
}

// joinWakeLimit bounds the join batch's wait for the ticks waking its
// chunks, so a lagging loop delays a join by at most this.
const joinWakeLimit = time.Second

// handleChunkBatchReceived takes the client's acknowledgement of a batch
// and the rate it asks for (computed from its batch times), clamped to the
// pacing's range with NaN as the floor. The first acknowledgement lifts the
// batches allowed ahead of answers from one to ten.
// vanilla: ServerGamePacketListenerImpl.handleChunkBatchReceived
func (c *client) handleChunkBatchReceived(r *protocol.Reader) error {
	rate := r.ReadF32()
	if err := r.Err(); err != nil {
		return c.decodeFailure("chunk_batch_received", err)
	}
	c.pacingMu.Lock()
	c.pacer.Acknowledge(rate)
	c.pacingMu.Unlock()
	return nil
}

// holdTake is the queued load-radius holds acquired per beat. Each new
// chunk's light spreads into its neighbours under the world's lock, so
// small takes keep the tick loop on time.
const holdTake = 4

// takeHolds acquires one beat's holdTake queued load-radius holds on the
// chunk sender, dropping holds the load radius no longer covers.
func (c *client) takeHolds() {
	if len(c.pendingHolds) == 0 || c.skipGen || len(c.pendingGeneration) > 0 {
		return
	}
	kept := c.pendingHolds[:0]
	for _, pos := range c.pendingHolds {
		if world.InViewOf(pos, c.center, c.load) {
			kept = append(kept, pos)
		}
	}
	c.pendingHolds = kept
	take := min(len(c.pendingHolds), holdTake, maxGenerating-len(c.generating))
	if take == 0 {
		return
	}
	batch := c.pendingHolds[:take:take]
	c.pendingHolds = c.pendingHolds[take:]
	for _, pos := range batch {
		c.loaded.add(pos)
	}
	if seam := c.srv.chunkTakeSeam.Load(); seam != nil {
		(*seam)()
	}
	for _, pos := range batch {
		c.generating[pos] = c.loadedWorld.RequestChunk(pos, world.DistanceTo(pos, c.center))
	}
}

// missingChunks lists load-radius chunks neither held nor queued, closest
// first.
func (c *client) missingChunks(center world.ChunkPos, load int32) []world.ChunkPos {
	missing := make([]world.ChunkPos, 0, (2*load+1)*(2*load+1))
	for x := center.X - load; x <= center.X+load; x++ {
		for z := center.Z - load; z <= center.Z+load; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			if c.loaded.has(pos) {
				continue
			}
			if slices.Contains(c.pendingHolds, pos) {
				continue
			}
			missing = append(missing, pos)
		}
	}
	slices.SortFunc(missing, func(a, b world.ChunkPos) int {
		return world.DistanceTo(a, center) - world.DistanceTo(b, center)
	})
	return missing
}

// writeUnload tells the client to forget a chunk.
func (c *client) writeUnload(pos world.ChunkPos) error {
	return c.write(protocol.IDPlayUnloadChunk, "unload chunk", func(b *protocol.Buffer) {
		// Z first: the client reads both as one big-endian long.
		b.PutI32(pos.Z)
		b.PutI32(pos.X)
	})
}

// unloadChunk releases a chunk, telling the client to forget it only if it
// was sent; load-radius-only holds were never the client's.
func (c *client) unloadChunk(pos world.ChunkPos) error {
	if request, ok := c.generating[pos]; ok {
		request.Cancel()
		delete(c.generating, pos)
		c.loaded.delete(pos)
		return nil
	}
	wasSent := c.sent.has(pos)
	c.unmarkSent(pos)
	c.loaded.delete(pos)
	c.loadedWorld.Release(pos)

	if !wasSent {
		return nil
	}
	return c.writeUnload(pos)
}

// forgetChunks releases every chunk the client held, on disconnect or
// dimension change, or the world would keep them loaded for nobody. The
// releases' saves register with done, so a departure waits for only its own
// writes, not the I/O pool's backlog; done is nil on a dimension change.
func (c *client) forgetChunks(done *sync.WaitGroup) {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	if c.loaded == nil {
		return
	}
	c.releasePendingPackets()
	c.cancelGenerating(done)
	for pos := range c.loaded.all() {
		c.loadedWorld.ReleaseWait(pos, done)
	}
	c.loaded, c.sent, c.pendingHolds, c.pendingSend, c.pendingGeneration = nil, nil, nil, nil, nil
	c.clearSentView()
	c.loadedWorld = nil
}

// markSent records pos as sent, in sent and its sentView mirror. Chunk
// sender only.
func (c *client) markSent(pos world.ChunkPos) {
	c.sent.add(pos)
	c.sentViewMu.Lock()
	defer c.sentViewMu.Unlock()
	if c.sentView == nil || c.sentViewWorld != c.loadedWorld {
		c.sentView, c.sentViewWorld = newChunkSet(c.center, c.load), c.loadedWorld
	}
	c.sentView.add(pos)
}

// unmarkSent forgets pos as sent, in sent and its mirror. Chunk sender
// only.
func (c *client) unmarkSent(pos world.ChunkPos) {
	c.sent.delete(pos)
	c.sentViewMu.Lock()
	defer c.sentViewMu.Unlock()
	c.sentView.delete(pos)
}

// clearSentView empties the sentView mirror, once sent has been dropped.
func (c *client) clearSentView() {
	c.sentViewMu.Lock()
	defer c.sentViewMu.Unlock()
	c.sentView, c.sentViewWorld = nil, nil
}

// tracks reports whether the client was sent w's chunk at pos, which
// decides whether changes to it are sent. vanilla: ChunkMap.isChunkTracked
func (c *client) tracks(w *world.World, pos world.ChunkPos) bool {
	c.sentViewMu.RLock()
	defer c.sentViewMu.RUnlock()
	return c.sentViewWorld == w && c.sentView.has(pos)
}
