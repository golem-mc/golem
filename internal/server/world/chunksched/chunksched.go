// Package chunksched models one of the orders in which vanilla's worldgen
// thread runs the FEATURES step of chunks when a rectangle is forceloaded.
//
// Decoration across a chunk border depends on which chunk was decorated first,
// and vanilla decides that by scheduling: every chunk has a generation task
// that schedules one status layer at a time over its neighbours, a task waits
// for the last future of its layer, and one worldgen thread runs the queued
// tasks. The model replays the status pyramid, ticket levels, task creation and
// requeueing on a level change over virtual time.
//
// It is an approximation, not a port. BIOMES runs asynchronously in vanilla
// (NoiseBasedChunkGenerator.createBiomes, supplyAsync on init_biomes) and is
// modelled as worldgen-thread work; the other asynchronous steps (disk load,
// terrain, light, the main-thread FULL step) complete after fixed delays taken
// from one recorded run; the dispatcher's task priority queue is one queue; and
// the holders whose level changed are updated in x-then-z order where vanilla
// iterates a ReferenceOpenHashSet (DistanceManager.chunksToUpdateFutures), whose
// order depends on identity hashes and varies between JVM runs. Vanilla's own
// order therefore differs from run to run, and this is one of the orders it
// produces: it agrees with every pair of neighbouring chunks that all recorded
// runs order the same, and chunks whose decoration depends on an order the
// runs disagree on have no vanilla digest to pin.
package chunksched

import (
	"container/heap"
	"slices"
	"sort"
)

// Pos is a chunk position.
type Pos struct{ X, Z int32 }

// Rect is a rectangle of chunks, both corners inclusive, as one /forceload
// command adds it.
type Rect struct{ X0, Z0, X1, Z1 int32 }

type status int8

// vanilla: ChunkStatus (the statuses a generation task passes through)
const (
	statusEmpty status = iota
	statusStructureStarts
	statusStructureReferences
	statusBiomes
	statusTerrain
	statusFeatures
	statusInitializeLight
	statusLight
	statusSpawn
	statusFull
	statusCount
)

// Virtual milliseconds. Sync steps hold the worldgen thread, async steps finish
// on their own thread after the delay. They come from the chunk-generation
// timeline of the reference server; the order they produce changes in about 1%
// of neighbouring pairs when any of them moves by a factor of ten.
const (
	costStructureStarts     = 0.08
	costStructureReferences = 0.15
	costBiomes              = 2.5
	costTerrain             = 0.5
	costFeatures            = 1.5
	costLight               = 0.1
	costSpawn               = 0.3
	delayLoad               = 0.08 // one disk read, serialized on the IO thread
	delayTerrain            = 200.0
	delayLight              = 1.0
	delayFull               = 5.0 // the main thread's FULL step
)

// fullTicket is the ticket level /forceload gives a chunk, entity ticking.
// vanilla: ChunkMap.FORCED_TICKET_LEVEL
const fullTicket = 31

// dependencies lists the status a step needs at each radius around its chunk.
// vanilla: ChunkDependencies
type dependencies []status

// step is a chunk pyramid step.
// vanilla: ChunkStep
type step struct {
	target      status
	accumulated dependencies
}

// stepBuilder is ChunkStep.Builder.
type stepBuilder struct {
	status status
	parent *step
	direct dependencies
}

func newStepBuilder(s status, parent *step) *stepBuilder {
	b := &stepBuilder{status: s, parent: parent, direct: dependencies{}}
	if parent != nil {
		b.direct = dependencies{parent.target}
	}
	return b
}

// require is ChunkStep.Builder.addRequirement, which fills a longer array with
// the new status and drops the shorter one's entries beyond the old length.
func (b *stepBuilder) require(s status, radius int) *stepBuilder {
	n := radius + 1
	prev := b.direct
	if n > len(prev) {
		b.direct = make(dependencies, n)
		for i := range b.direct {
			b.direct[i] = s
		}
	}
	for i := range min(n, len(prev)) {
		b.direct[i] = max(prev[i], s)
	}
	return b
}

// build is ChunkStep.Builder.build with its accumulated dependencies.
func (b *stepBuilder) build() step {
	st := step{target: b.status, accumulated: b.direct}
	if b.parent == nil {
		return st
	}
	radiusOfParent := 0
	for i, v := range slices.Backward(b.direct) {
		if v >= b.parent.target {
			radiusOfParent = i
			break
		}
	}
	parentDeps := b.parent.accumulated
	acc := make(dependencies, max(radiusOfParent+len(parentDeps), len(b.direct)))
	for d := range acc {
		inParent := d - radiusOfParent
		switch {
		case inParent < 0 || inParent >= len(parentDeps):
			acc[d] = b.direct[d]
		case d >= len(b.direct):
			acc[d] = parentDeps[inParent]
		default:
			acc[d] = max(b.direct[d], parentDeps[inParent])
		}
	}
	st.accumulated = acc
	return st
}

type pyramid [statusCount]step

// buildPyramid builds a pyramid from each status's builder.
func buildPyramid(configure func(s status, b *stepBuilder) *stepBuilder) *pyramid {
	var p pyramid
	for s := range statusCount {
		var parent *step
		if s > statusEmpty {
			parent = &p[s-1]
		}
		p[s] = configure(s, newStepBuilder(s, parent)).build()
	}
	return &p
}

// vanilla: ChunkPyramid.GENERATION_PYRAMID
func generationPyramid() *pyramid {
	return buildPyramid(func(s status, b *stepBuilder) *stepBuilder {
		switch s {
		case statusStructureReferences, statusBiomes:
			return b.require(statusStructureStarts, 8)
		case statusTerrain:
			return b.require(statusStructureStarts, 8).require(statusBiomes, 1)
		case statusFeatures:
			return b.require(statusStructureStarts, 8).require(statusTerrain, 1)
		case statusLight:
			return b.require(statusInitializeLight, 1)
		case statusSpawn:
			return b.require(statusBiomes, 1)
		}
		return b
	})
}

// vanilla: ChunkPyramid.LOADING_PYRAMID
func loadingPyramid() *pyramid {
	return buildPyramid(func(s status, b *stepBuilder) *stepBuilder {
		if s == statusLight {
			return b.require(statusInitializeLight, 1)
		}
		return b
	})
}

// radiusOf is ChunkStep.getAccumulatedRadiusOf.
func (p *pyramid) radiusOf(target, s status) int {
	if s == target {
		return 0
	}
	r := 0
	for i, dep := range p[target].accumulated {
		if dep >= s {
			r = i
		}
	}
	return r
}

type future struct {
	done    bool
	ok      bool
	waiters []func()
}

func (f *future) complete() {
	f.done, f.ok = true, true
	w := f.waiters
	f.waiters = nil
	for _, fn := range w {
		fn()
	}
}

// failed is vanilla's UNLOADED_CHUNK_FUTURE.
var failed = &future{done: true}

// holder is a GenerationChunkHolder with its ChunkHolder levels.
type holder struct {
	pos Pos
	// level is the ticket level; oldLevel the one updateFutures last saw; queue
	// the level its tasks are queued under.
	level, oldLevel, queue int
	// allowed is the highest status the level permits, -1 for none.
	allowed int
	// started is the last status whose work began, persisted the last finished.
	started, persisted int
	futures            [statusCount]*future
	task               *task
}

func (h *holder) disallowed(s status) bool { return h.allowed < 0 || int(s) > h.allowed }

func (h *holder) future(s status) *future {
	if h.futures[s] == nil {
		h.futures[s] = &future{}
	}
	return h.futures[s]
}

// task is a ChunkGenerationTask.
type task struct {
	h         *holder
	target    status
	scheduled int
	cancelled bool
	needsGen  bool
	layer     []*future
}

type event struct {
	at  float64
	seq int
	fn  func()
}

type eventHeap []event

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	return h[i].at < h[j].at || h[i].at == h[j].at && h[i].seq < h[j].seq
}
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)   { *h = append(*h, x.(event)) }
func (h *eventHeap) Pop() any {
	old := *h
	e := old[len(old)-1]
	*h = old[:len(old)-1]
	return e
}

// entry is the tasks queued for one chunk position.
type entry struct {
	pos   Pos
	tasks []*task
	dead  bool
}

// levelQueue is one priority level of ChunkTaskPriorityQueue: chunk positions
// in the order they first queued, each holding its tasks.
// vanilla: ChunkTaskPriorityQueue
type levelQueue struct {
	order []*entry
	byPos map[Pos]*entry
	head  int
}

func (q *levelQueue) add(pos Pos, tasks ...*task) {
	if q.byPos == nil {
		q.byPos = map[Pos]*entry{}
	}
	if e := q.byPos[pos]; e != nil {
		e.tasks = append(e.tasks, tasks...)
		return
	}
	e := &entry{pos: pos, tasks: tasks}
	q.byPos[pos] = e
	q.order = append(q.order, e)
}

// take removes and returns a position's tasks.
func (q *levelQueue) take(pos Pos) []*task {
	e := q.byPos[pos]
	if e == nil {
		return nil
	}
	e.dead = true
	delete(q.byPos, pos)
	return e.tasks
}

// pop removes the first queued position, nil when empty.
func (q *levelQueue) pop() *entry {
	for q.head < len(q.order) {
		e := q.order[q.head]
		q.head++
		if !e.dead {
			delete(q.byPos, e.pos)
			return e
		}
	}
	return nil
}

// maxLevel is ChunkLevel.MAX_LEVEL: the level of a chunk at the edge of the
// generation pyramid's radius around a FULL chunk.
const maxLevel = 44

type sim struct {
	gen, load *pyramid
	holders   map[Pos]*holder
	// staged are the levels the tickets give, applied by the next update.
	staged  map[Pos]int
	pending []*task
	now     float64
	events  eventHeap
	seq     int
	queue   [maxLevel + 2]levelQueue
	ioFree  float64
	order   []Pos
	// ranFeatures is set when a task starts a FEATURES step.
	ranFeatures bool
}

func (s *sim) after(delay float64, fn func()) { s.at(s.now+delay, fn) }

func (s *sim) at(t float64, fn func()) {
	s.seq++
	heap.Push(&s.events, event{t, s.seq, fn})
}

// advance spends cost on the worldgen thread, firing the async completions that
// fall due on the way.
func (s *sim) advance(cost float64) {
	end := s.now + cost
	for len(s.events) > 0 && s.events[0].at <= end {
		e := heap.Pop(&s.events).(event)
		s.now = max(s.now, e.at)
		e.fn()
	}
	s.now = end
}

// applyStep is GenerationChunkHolder.applyStep: the first caller of a status
// does its work, later callers get the same future.
func (s *sim) applyStep(h *holder, st status) *future {
	if h.disallowed(st) {
		return failed
	}
	if h.started == int(st)-1 {
		h.started = int(st)
		f := h.future(st)
		s.run(h, st, f)
		return f
	}
	return h.future(st)
}

// run does a step's work; vanilla runs the disk load, terrain, light and FULL
// asynchronously and everything else on the worldgen thread.
// vanilla: ChunkStatusTasks
func (s *sim) run(h *holder, st status, f *future) {
	finish := func() {
		h.persisted = int(st)
		f.complete()
	}
	switch st {
	case statusEmpty:
		start := max(s.now, s.ioFree)
		s.ioFree = start + delayLoad
		s.at(s.ioFree, finish)
	case statusTerrain:
		s.advance(costTerrain)
		s.after(delayTerrain, finish)
	case statusInitializeLight, statusLight:
		s.advance(costLight)
		s.after(delayLight, finish)
	case statusFull:
		s.advance(costLight)
		s.after(delayFull, finish)
	default:
		s.advance(syncCost(st))
		if st == statusFeatures {
			s.ranFeatures = true
		}
		finish()
	}
}

func syncCost(st status) float64 {
	switch st {
	case statusStructureStarts:
		return costStructureStarts
	case statusStructureReferences:
		return costStructureReferences
	case statusBiomes:
		return costBiomes
	case statusFeatures:
		return costFeatures
	case statusSpawn:
		return costSpawn
	}
	return 0
}

// scheduleChunkGenerationTask replaces a holder's task with one for status when
// it is further than the task's target; the old task stops at its next wake.
// vanilla: GenerationChunkHolder.scheduleChunkGenerationTask
func (s *sim) scheduleChunkGenerationTask(h *holder, st status) {
	if h.disallowed(st) || h.future(st).done {
		return
	}
	if h.task != nil && st <= h.task.target {
		return
	}
	t := &task{h: h, target: st, scheduled: -1}
	s.pending = append(s.pending, t)
	if h.task != nil {
		h.task.cancelled = true
	}
	h.task = t
}

// runUntilWait runs the task's layers until one has an unfinished future, which
// it returns, or the task is done.
// vanilla: ChunkGenerationTask.runUntilWait
func (s *sim) runUntilWait(t *task) *future {
	for {
		if w := t.waitForLayer(); w != nil {
			return w
		}
		if t.cancelled || t.scheduled == int(t.target) {
			return nil
		}
		s.scheduleNextLayer(t)
	}
}

func (s *sim) scheduleNextLayer(t *task) {
	var st status
	switch {
	case t.scheduled < 0:
		st = statusEmpty
	case !t.needsGen && t.scheduled == int(statusEmpty) && t.target != statusEmpty:
		// A fresh world has nothing to load, so the task generates.
		t.needsGen = true
		st = statusEmpty
	default:
		st = status(t.scheduled + 1)
	}
	s.scheduleLayer(t, st)
	t.scheduled = int(st)
}

// scheduleLayer applies the status to every chunk of the layer, x outer and z
// inner, and keeps the futures that are not finished.
// vanilla: ChunkGenerationTask.scheduleLayer
func (s *sim) scheduleLayer(t *task, st status) {
	pyr := s.load
	if t.needsGen {
		pyr = s.gen
	}
	r := pyr.radiusOf(t.target, st)
	for x := t.h.pos.X - int32(r); x <= t.h.pos.X+int32(r); x++ {
		for z := t.h.pos.Z - int32(r); z <= t.h.pos.Z+int32(r); z++ {
			h := s.holders[Pos{x, z}]
			if h == nil || t.cancelled {
				t.cancelled = true
				return
			}
			f := s.applyStep(h, st)
			switch {
			case !f.done:
				t.layer = append(t.layer, f)
			case !f.ok:
				t.cancelled = true
				return
			}
		}
	}
}

// waitForLayer returns the last unfinished future of the layer.
func (t *task) waitForLayer() *future {
	for len(t.layer) > 0 {
		last := t.layer[len(t.layer)-1]
		if !last.done {
			return last
		}
		t.layer = t.layer[:len(t.layer)-1]
		if !last.ok {
			t.cancelled = true
		}
	}
	return nil
}

// submit queues a task under its chunk's current queue level.
// vanilla: ChunkTaskDispatcher.submit
func (s *sim) submit(t *task) { s.queue[t.h.queue].add(t.h.pos, t) }

func (s *sim) runTask(t *task) {
	s.ranFeatures = false
	w := s.runUntilWait(t)
	if s.ranFeatures && t.target == statusFull {
		s.order = append(s.order, t.h.pos)
	}
	if w != nil {
		w.waiters = append(w.waiters, func() { s.submit(t) })
	}
}

// step runs the next queued chunk's tasks, or waits for the next async
// completion; it reports false when nothing is left.
func (s *sim) step() bool {
	for lv := range s.queue {
		if e := s.queue[lv].pop(); e != nil {
			for _, t := range e.tasks {
				s.runTask(t)
			}
			return true
		}
	}
	if len(s.events) == 0 {
		return false
	}
	e := heap.Pop(&s.events).(event)
	s.now = max(s.now, e.at)
	e.fn()
	return true
}

// allowedFor is ChunkLevel.generationStatus, -1 where none is allowed.
func (s *sim) allowedFor(level int) int {
	d := level - 33
	switch {
	case d > len(s.gen[statusFull].accumulated)-1:
		return -1
	case d <= 0:
		return int(statusFull)
	}
	return int(s.gen[statusFull].accumulated[d])
}

// updateFutures requests the statuses a chunk's new level asks of its
// neighbours: FULL and the status around it once accessible, FULL around it
// once block ticking (radius 1) and entity ticking (radius 2), then requeues
// its tasks under the new level.
// vanilla: ChunkHolder.updateFutures, ChunkMap.getChunkRangeFuture
func (s *sim) updateFutures(h *holder) {
	around := func(radius int, statusAt func(distance int) status) {
		for z := -radius; z <= radius; z++ {
			for x := -radius; x <= radius; x++ {
				n := s.holders[Pos{h.pos.X + int32(x), h.pos.Z + int32(z)}]
				if n == nil {
					return
				}
				s.scheduleChunkGenerationTask(n, statusAt(max(abs(x), abs(z))))
			}
		}
	}
	full := func(int) status { return statusFull }
	if h.oldLevel > 33 && h.level <= 33 {
		around(1, func(d int) status {
			if d == 0 {
				return statusFull
			}
			return s.gen[statusFull].accumulated[d]
		})
	}
	if h.oldLevel > 32 && h.level <= 32 {
		around(1, full)
	}
	if h.oldLevel > 31 && h.level <= 31 {
		around(2, full)
	}
	// vanilla: ChunkTaskPriorityQueue.resortChunkTasks
	if h.queue != h.level {
		if moved := s.queue[h.queue].take(h.pos); len(moved) > 0 {
			s.queue[h.level].add(h.pos, moved...)
		}
	}
	h.queue, h.oldLevel = h.level, h.level
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// stage records the ticket levels a forced chunk gives its surroundings.
func (s *sim) stage(p Pos) {
	span := int32(maxLevel - fullTicket)
	for x := p.X - span; x <= p.X+span; x++ {
		for z := p.Z - span; z <= p.Z+span; z++ {
			lv := fullTicket + int(max(abs32(x-p.X), abs32(z-p.Z)))
			if old, ok := s.staged[Pos{x, z}]; !ok || lv < old {
				s.staged[Pos{x, z}] = lv
			}
		}
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// runDistanceManagerUpdates applies the staged levels, updates the changed
// chunks in x-then-z order, and queues the tasks they created.
// vanilla: ServerChunkCache.runDistanceManagerUpdates
func (s *sim) runDistanceManagerUpdates() {
	var changed []*holder
	for p, lv := range s.staged {
		h := s.holders[p]
		if h == nil {
			h = &holder{pos: p, level: maxLevel + 1, oldLevel: maxLevel + 1, queue: maxLevel + 1, allowed: -1, started: -1, persisted: -1}
			s.holders[p] = h
		}
		if lv < h.level {
			h.level = lv
			changed = append(changed, h)
		}
	}
	clear(s.staged)
	sort.Slice(changed, func(i, j int) bool {
		a, b := changed[i].pos, changed[j].pos
		return a.X < b.X || a.X == b.X && a.Z < b.Z
	})
	for _, h := range changed {
		h.allowed = s.allowedFor(h.level)
	}
	for _, h := range changed {
		s.updateFutures(h)
	}
	pending := s.pending
	s.pending = nil
	for _, t := range pending {
		s.submit(t)
	}
}

// forceChunk is ForceLoadCommand's loop body: the forced ticket, then a
// blocking getChunk for FULL, during which the main thread keeps running the
// distance manager.
// vanilla: ServerLevel.setChunkForced, ServerChunkCache.getChunk
func (s *sim) forceChunk(p Pos) {
	s.stage(p)
	h := s.holders[p]
	if h == nil || h.level > 33 {
		s.runDistanceManagerUpdates()
		h = s.holders[p]
	}
	s.scheduleChunkGenerationTask(h, statusFull)
	f := h.future(statusFull)
	for {
		s.runDistanceManagerUpdates()
		if f.done {
			return
		}
		if !s.step() {
			panic("chunksched: the forced chunk never reached FULL")
		}
	}
}

// Forceload returns the chunks whose FULL task ran the FEATURES layer, in the
// order the worldgen thread ran them, when each rectangle is added by its own
// /forceload command, one after the other: a command forces its chunks x outer
// and z inner, each waiting for the chunk to be FULL before the next. A task
// decorates the chunks of its 3x3 that no earlier task did, x outer and z
// inner, so generating the chunks in this order decorates them in vanilla's.
// Ring chunks outside the rectangles come with it: their tasks decorate too.
func Forceload(rects []Rect) []Pos {
	s := &sim{
		gen:     generationPyramid(),
		load:    loadingPyramid(),
		holders: map[Pos]*holder{},
		staged:  map[Pos]int{},
	}
	for _, r := range rects {
		for x := r.X0; x <= r.X1; x++ {
			for z := r.Z0; z <= r.Z1; z++ {
				s.forceChunk(Pos{x, z})
			}
		}
	}
	for s.step() {
	}
	return s.order
}
