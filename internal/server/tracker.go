package server

import (
	"fmt"
	"iter"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// One generic tracker for the bodies a kind lock guards: who is shown a
// body, the pairing a newcomer gets, the movement and velocity packets on
// the kind's schedule, and the removal for a watcher gone out of range.
// vanilla: ChunkMap.TrackedEntity, ServerEntity
//
// Every function here runs under the kind's lock and only gathers packets;
// the kind sends them after the lock is down. register and teleport take
// the locks themselves, s.mu before the kind's.

// bodyPose is a body as its movement packets tell it.
type bodyPose struct {
	x, y, z float64
	// vx, vy, vz is the velocity a Teleport Entity carries: zero for a kind
	// whose own motion packets carry it.
	vx, vy, vz float64
	yaw, pitch float32
	onGround   bool
}

// tracked converts a pose into the units its watchers are told it in.
func (p bodyPose) tracked() trackedPosition {
	return trackedPosition{
		x: encodeStep(p.x), y: encodeStep(p.y), z: encodeStep(p.z),
		yaw: encodeAngle(p.yaw), pitch: encodeAngle(p.pitch),
		onGround: p.onGround, known: true,
	}
}

// toldBody is what one watcher was last told of a body: where, and the
// kind's own state S its metadata deltas are measured against.
type toldBody[S any] struct {
	trackedPosition
	own S
}

// entityOrigin records where an entity was first spawned, for Entity.Origin.
// Paper: Entity.origin
type entityOrigin struct {
	set     bool
	loaded  bool // loaded from saved NBT: leave unset when Paper.Origin was absent
	dim     string
	x, y, z float64
}

type originHolder interface {
	entityOrigin() entityOrigin
}

// bodyTrack is one body's tracking record: what each watcher was last told,
// the movement schedule's counters, and the velocity last sent.
// vanilla: ServerEntity.tickCount, teleportDelay, lastSentMovement
type bodyTrack[S any] struct {
	tracked watcherSet[toldBody[S]]

	movementSchedule
	// needsSync is vanilla's needsSync: a /data load asks for a movement
	// tick off the period. The kind's tick hands takeSync to schedule.
	// vanilla: Entity.load, ServerEntity.sendChanges
	needsSync bool

	sentVX, sentVY, sentVZ float64
	origin                 entityOrigin
}

func (t *bodyTrack[S]) entityOrigin() entityOrigin { return t.origin }

// movementSchedule is ServerEntity's movement cadence, shared by every tracked
// kind. vanilla: ServerEntity.tickCount, teleportDelay
type movementSchedule struct {
	tickCount     int
	teleportDelay int
	// teleported marks a teleport the watchers have not been told of: the
	// next movement tick sends it absolute, and many teleports in between
	// cost one packet. The kind's tick takes it through schedule step or
	// takeTeleport.
	teleported bool
	// wasRiding marks a passenger's movement tick; the first one off the
	// seat is owed absolute. vanilla: ServerEntity.wasRiding
	wasRiding bool
}

// takeSync reports and clears needsSync.
func (t *bodyTrack[S]) takeSync() bool {
	n := t.needsSync
	t.needsSync = false
	return n
}

// motionDue reports whether the velocity has drifted from the one last sent
// far enough to send again, and takes it as sent if so. stopping also sends
// any change that brings the body to rest.
// vanilla: ServerEntity.sendChanges
func (t *bodyTrack[S]) motionDue(vx, vy, vz float64, stopping bool) bool {
	dx, dy, dz := vx-t.sentVX, vy-t.sentVY, vz-t.sentVZ
	diff := dx*dx + dy*dy + dz*dz
	if diff > motionThreshold || (stopping && diff > 0 && vx == 0 && vy == 0 && vz == 0) {
		t.sentVX, t.sentVY, t.sentVZ = vx, vy, vz
		return true
	}
	return false
}

// trackedKind is what the tracker reads off a kind of body B whose watchers
// each keep S.
type trackedKind[B any, S any] struct {
	rangeChunks int
	// rangeOf, if not nil, is each body's own range in chunks in place of
	// rangeChunks; a body of range 0 is never shown.
	// vanilla: ChunkMap.addEntity
	rangeOf func(B) int
	// passengerRange, if not nil, is the largest range in chunks among the
	// body's indirect passengers, 0 for none; the body is seen at least that
	// far, so a rider is never shown without its vehicle.
	// vanilla: ChunkMap.TrackedEntity.getEffectiveRange
	passengerRange func(B) int
	// interval is the movement schedule's period in ticks; intervalOf, if
	// not nil, is each body's own in its place.
	interval   int
	intervalOf func(B) int
	// alwaysPosition keeps the scheduled corrections even at rest or frozen,
	// as ServerEntity.sendChanges does for AbstractArrow.
	alwaysPosition bool
	// frozenSchedule keeps the movement cadence running while physics is held,
	// including the initial correction of a client's predicted birth step.
	frozenSchedule bool

	lock  func(*Server) *sync.Mutex
	list  func(*Server) *[]B
	track func(B) *bodyTrack[S]
	world func(B) *world.World
	id    func(B) int32
	dead  func(B) bool
	pose  func(B) bodyPose
	// readBody optionally reads the four hot tracking inputs in one dispatch.
	readBody func(B) (*bodyTrack[S], *world.World, int32, bodyPose)
	// portalDimensions, if set, maps the body's offset through nether portals.
	portalDimensions func(B) (width, height float64)
	// portalCentered clears the perpendicular entry offset before exit collisions.
	// vanilla: LivingEntity.resetForwardDirectionOfRelativePortalPosition
	portalCentered bool
	// invalidatePiston drops cached boxes under the kind lock after a position
	// or dimension change, including commands interleaved with physics effects.
	invalidatePiston func(*dimension)
	// tellPose, if not nil, is the pose an absolute Teleport Entity tells in
	// place of pose, for a body whose range is measured from another point
	// than the one the client places it at.
	tellPose func(B) bodyPose
	pairing  func(B) []outbound
	// own is the kind's state a pairing tells; nil for a kind with none.
	own func(B) S
	// all, if not nil, walks the bodies in place of list, for a kind kept
	// in more than one list.
	all func(*Server) iter.Seq[B]
	// chunks, filing and uuid are set for kinds covered by a chunk index.
	chunks func(*Server) *entity.ChunkIndex[B]
	filing func(B) *entity.Filing
	uuid   func(B) uuid.UUID
	// anchor, if not nil, is the point a block-anchored body is filed under
	// and queried at, in place of pose: where its snapshot reports it.
	anchor func(B) (x, z float64)
}

// chunkOf is the chunk the index files b under.
func (k *trackedKind[B, S]) chunkOf(b B) world.ChunkPos {
	if k.anchor != nil {
		return entity.ChunkAt(k.anchor(b))
	}
	p := k.pose(b)
	return entity.ChunkAt(p.x, p.z)
}

// file puts a body in the kind's chunk index under its pose. The caller holds
// the kind's lock.
func (k *trackedKind[B, S]) file(s *Server, b B) {
	if k.chunks == nil {
		return
	}
	k.chunks(s).Add(b, k.filing(b), k.chunkOf(b), k.uuid(b))
}

// unfile takes a body out of the chunk index. The caller holds the kind's lock.
func (k *trackedKind[B, S]) unfile(b B) {
	if k.chunks != nil {
		k.filing(b).Unfile()
	}
}

// chunkDrift checks chunk membership and UUID lookup for each indexed body.
func (k *trackedKind[B, S]) chunkDrift(s *Server) error {
	if k.chunks == nil {
		return nil
	}
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	ix, list := k.chunks(s), *k.list(s)
	if ix.Len() != len(list) {
		return fmt.Errorf("%T: %d bodies, %d filed", *new(B), len(list), ix.Len())
	}
	for _, b := range list {
		f := k.filing(b)
		if !ix.Holds(f, k.chunkOf(b)) {
			return fmt.Errorf("%T id %d not filed under its chunk", b, k.id(b))
		}
		// A cross-dimension twin can take over the UUID before the tick
		// sweeps the dead original out of its chunk.
		if got, ok := ix.ByUUID(k.uuid(b)); !ok || (k.filing(got) != f && !k.dead(b)) {
			return fmt.Errorf("%T id %d not filed under its UUID", b, k.id(b))
		}
	}
	return nil
}

// trackedBody is one body as the tracker reads it, once for all its
// watchers. The caller holds the kind's lock.
type trackedBody[B any, S any] struct {
	k  *trackedKind[B, S]
	b  B
	t  *bodyTrack[S]
	w  *world.World
	id int32
	p  bodyPose
	// now is p in the units the watchers are told, worked out on first use:
	// a tick that tells nobody anything never needs it.
	now           trackedPosition
	nowOK         bool
	rangeRead     bool
	rangeChunks   int
	broadcastRead bool
	broadcastPct  int
}

// sees reports whether a watcher at (x, z) seeing radius chunks is in
// range of b standing at p, the range scaled by pct percent.
func (k *trackedKind[B, S]) sees(b B, p bodyPose, x, z float64, radius int32, pct int) bool {
	r := k.effectiveRange(b)
	return r >= 0 && inTrackingRange(x, z, p.x, p.z, r, pct, radius)
}

// effectiveRange includes current indirect passengers; -1 disables tracking.
func (k *trackedKind[B, S]) effectiveRange(b B) int {
	r := k.rangeChunks
	if k.rangeOf != nil {
		if r = k.rangeOf(b); r == 0 {
			return -1
		}
	}
	if k.passengerRange != nil {
		r = max(r, k.passengerRange(b))
	}
	return r
}

// bodies walks every body of the kind.
func (k *trackedKind[B, S]) bodies(s *Server) iter.Seq[B] {
	if k.all != nil {
		return k.all(s)
	}
	return slices.Values(*k.list(s))
}

func (k *trackedKind[B, S]) body(b B) trackedBody[B, S] {
	if k.readBody != nil {
		t, w, id, p := k.readBody(b)
		return trackedBody[B, S]{k: k, b: b, t: t, w: w, id: id, p: p}
	}
	return trackedBody[B, S]{k: k, b: b, t: k.track(b), w: k.world(b), id: k.id(b), p: k.pose(b)}
}

func (v *trackedBody[B, S]) sees(c *client, x, z float64, radius int32) bool {
	if c.hides(v.id) {
		return false
	}
	if !v.rangeRead {
		v.rangeRead = true
		v.rangeChunks = v.k.effectiveRange(v.b)
	}
	if v.rangeChunks < 0 {
		return false
	}
	if !v.broadcastRead {
		v.broadcastPct = int(c.srv.entityBroadcastRange.Load())
		v.broadcastRead = true
	}
	return inTrackingRange(x, z, v.p.x, v.p.z, v.rangeChunks, v.broadcastPct, radius)
}

func (v *trackedBody[B, S]) position() trackedPosition {
	if !v.nowOK {
		v.now, v.nowOK = v.p.tracked(), true
	}
	return v.now
}

// mark records c as told of the body now and returns its record.
func (v *trackedBody[B, S]) mark(c *client) *toldBody[S] {
	r := v.record()
	v.t.tracked.set(c, &r)
	return &r
}

func (v *trackedBody[B, S]) record() toldBody[S] {
	var own S
	if v.k.own != nil {
		own = v.k.own(v.b)
	}
	return toldBody[S]{trackedPosition: v.position(), own: own}
}

// moveTick is one tick of the movement schedule: gate is a movement tick,
// forced one that sends a position unmoved, resync one that sends it
// absolute, teleported one after a teleport, which sends any move or turn
// absolute so the commanded place and angles are exact. rotationOnly limits it
// to a turn, for a passenger, whose position is its vehicle's to carry;
// dismounted is the first movement tick after one, which sends the position
// absolute as the client's base stayed where the body boarded.
type moveTick struct {
	gate, forced, resync, teleported, rotationOnly, dismounted bool
}

// schedule advances the body's movement schedule one tick. synced, if not
// nil, is asked for a movement tick off the period, and only off it.
func (v *trackedBody[B, S]) schedule(synced func() bool) moveTick {
	interval := v.k.interval
	if v.k.intervalOf != nil {
		interval = v.k.intervalOf(v.b)
	}
	return v.t.step(interval, synced)
}

// teleportTick is the movement tick of a kind with no schedule of its own: it
// carries a pending teleport and nothing else, and takes the mark.
func (v *trackedBody[B, S]) teleportTick() moveTick {
	return v.t.takeTeleport()
}

// step advances one movement tick and reports what this one owes. synced, if
// not nil, is asked for a movement tick off the period.
func (m *movementSchedule) step(interval int, synced func() bool) moveTick {
	return m.advance(m.tickCount%interval == 0 || (synced != nil && synced()))
}

// stepDue takes an already available sync flag without a callback dispatch.
func (m *movementSchedule) stepDue(interval int, synced bool) moveTick {
	return m.advance(m.tickCount%interval == 0 || synced)
}

func (m *movementSchedule) advance(due bool) moveTick {
	mt := moveTick{
		gate:   due,
		forced: m.tickCount%forcedPosPeriod == 0,
	}
	m.tickCount++
	mt.teleported = m.teleported
	if mt.gate {
		m.teleported = false
		m.teleportDelay++
		if m.teleportDelay > teleportResyncAfter {
			m.teleportDelay = 0
			mt.resync = true
		}
	}
	return mt
}

// ride marks a movement tick of a body that is seated or not: a seated one is
// limited to a turn, and the first one off the seat is a dismounted one that
// restarts the resync count.
// vanilla: ServerEntity.sendChanges, createMovePacket
func (m *movementSchedule) ride(mt moveTick, seated bool) moveTick {
	if !mt.gate {
		return mt
	}
	switch {
	case seated:
		mt.rotationOnly = true
		m.wasRiding = true
	case m.wasRiding:
		mt.dismounted = true
		m.wasRiding = false
		m.teleportDelay = 0
	}
	return mt
}

// takeTeleport takes a pending teleport without advancing the cadence for a
// kind whose body does not tick while frozen.
func (m *movementSchedule) takeTeleport() moveTick {
	mt := moveTick{teleported: m.teleported}
	m.teleported = false
	return mt
}

// trackSends is what a tick's tracking gathers: pairings and removals, then
// movements, sent in that order once the kind's lock is down.
type trackSends struct {
	shows   []outbound
	showTos []showTo
	moves   sendList
}

func (o *trackSends) send() {
	sendShows(o.shows, o.showTos)
	o.moves.send()
}

// sendList is packets each bound for one client, sent in order. Its entries
// are pooled across ticks: a client's mailbox takes each packet by value, so
// once send returns nothing refers to them. A list is used through a pointer
// and sent once; a copy would share the entries it gives back.
type sendList struct{ entries *[]sendEntry }

type sendEntry struct {
	to *client
	p  outbound
}

var sendEntries = sync.Pool{New: func() any { return new([]sendEntry) }}

func (l *sendList) add(c *client, p outbound) {
	if l.entries == nil {
		l.entries = sendEntries.Get().(*[]sendEntry)
	}
	*l.entries = append(*l.entries, sendEntry{c, p})
}

// drain hands every queued packet to f instead of sending it, and releases
// the list.
func (l *sendList) drain(f func(c *client, p outbound)) {
	if l.entries == nil {
		return
	}
	for _, e := range *l.entries {
		f(e.to, e.p)
	}
	clear(*l.entries)
	*l.entries = (*l.entries)[:0]
	sendEntries.Put(l.entries)
	l.entries = nil
}

func (l *sendList) send() {
	if l.entries == nil {
		return
	}
	for _, e := range *l.entries {
		e.to.sendOutboundIn(nil, e.p)
	}
	// Cleared, so a pooled list holds no client or payload alive.
	clear(*l.entries)
	*l.entries = (*l.entries)[:0]
	sendEntries.Put(l.entries)
	l.entries = nil
}

// settle pairs the body with a client standing at (x, z) that the range has
// brought in and unpairs it from one the range has left. It reports whether
// the client sees the body, and its record if so.
func (v *trackedBody[B, S]) settle(c *client, x, z float64, radius int32, out *trackSends) (*toldBody[S], bool) {
	was, seen, packets := v.settlePackets(c, x, z, radius)
	for _, p := range packets {
		out.shows = append(out.shows, p)
		out.showTos = append(out.showTos, showTo{c, v.w})
	}
	return was, seen
}

// settlePackets shares visibility bookkeeping with callers collecting their own sends.
func (v *trackedBody[B, S]) settlePackets(c *client, x, z float64, radius int32) (*toldBody[S], bool, []outbound) {
	seen := v.sees(c, x, z, radius)
	was, told := v.t.tracked.get(c)
	var packets []outbound
	switch {
	case seen && !told:
		was = v.mark(c)
		packets = v.k.pairing(v.b)
	case !seen && told:
		v.t.tracked.delete(c)
		packets = []outbound{{id: protocol.IDPlayRemoveEntities, data: removeEntitiesPayload([]int32{v.id})}}
		was = nil
	}
	return was, seen, packets
}

// moveTold gathers what one watcher, whose record is was, is told on a
// movement tick; see moveDiff. Only what was sent moves the record.
// vanilla: ServerEntity.sendChanges
func (v *trackedBody[B, S]) moveTold(was *toldBody[S], c *client, mt moveTick, out *trackSends) {
	if packet, ok := v.movePacket(was, mt); ok {
		out.moves.add(c, packet)
	}
}

// movePacket updates the baseline and returns the movement, if one is owed.
func (v *trackedBody[B, S]) movePacket(was *toldBody[S], mt moveTick) (outbound, bool) {
	mt.forced = mt.forced || v.k.alwaysPosition
	position := v.position()
	if !mt.forced && !mt.resync && !mt.dismounted && !mt.teleported && !mt.rotationOnly && position == was.trackedPosition {
		return outbound{}, false
	}
	p := v.p
	if v.k.tellPose != nil {
		p = v.k.tellPose(v.b)
	}
	now, packet, ok := moveDiff(v.id, was.trackedPosition, position, p, mt)
	if !ok && !mt.rotationOnly {
		return outbound{}, false
	}
	was.trackedPosition = now
	return packet, ok
}

// moveDiff builds the packet a watcher last told of was is owed for a body now
// at now: an absolute Teleport Entity carrying abs when the step does not fit
// the relative packets, the on-ground flag has flipped, the body has left a seat
// or the resync is due; a
// relative move when the body has moved far enough to see or the forced tick
// has come; a rotation when it has only turned. told is the baseline after the
// packet; ok is false when nothing is owed. A rotationOnly step (a passenger)
// moves the position baseline to now whether or not a packet is owed, but
// leaves onGround alone.
// vanilla: ServerEntity.sendChanges
func moveDiff(id int32, was, now trackedPosition, abs bodyPose, mt moveTick) (told trackedPosition, packet outbound, ok bool) {
	dx, dy, dz := now.x-was.x, now.y-was.y, now.z-was.z
	moved := dx*dx+dy*dy+dz*dz >= movedThresholdSq
	turned := now.yaw != was.yaw || now.pitch != was.pitch

	var buf protocol.Buffer
	var packetID int32
	toldPosition := true
	switch {
	case mt.rotationOnly:
		told = was
		told.x, told.y, told.z = now.x, now.y, now.z
		if !turned {
			return told, outbound{}, false
		}
		packetID = protocol.IDPlayEntityRotation
		buf.Grow(entityPositionRotLen)
		protocol.PutMoveEntityRot(&buf, id, now.yaw, now.pitch, now.onGround)
	case mt.resync || mt.dismounted || mt.teleported && (dx != 0 || dy != 0 || dz != 0 || turned) ||
		now.onGround != was.onGround || !inOneStep(dx, dy, dz):
		packetID = protocol.IDPlayTeleportEntity
		buf.Grow(teleportEntityLen)
		protocol.PutEntityPositionSync(&buf, id, abs.x, abs.y, abs.z, abs.yaw, abs.pitch, now.onGround)
	case moved || mt.forced:
		buf.Grow(entityPositionRotLen)
		if turned {
			packetID = protocol.IDPlayEntityPositionRot
			protocol.PutMoveEntityPosRot(&buf, id, int16(dx), int16(dy), int16(dz), now.yaw, now.pitch, now.onGround)
		} else {
			packetID = protocol.IDPlayEntityPosition
			protocol.PutMoveEntityPos(&buf, id, int16(dx), int16(dy), int16(dz), now.onGround)
		}
	case turned:
		packetID = protocol.IDPlayEntityRotation
		buf.Grow(entityPositionRotLen)
		protocol.PutMoveEntityRot(&buf, id, now.yaw, now.pitch, now.onGround)
		toldPosition = false
	default:
		return was, outbound{}, false
	}

	if !mt.rotationOnly {
		told = was
		if toldPosition {
			told.x, told.y, told.z, told.onGround = now.x, now.y, now.z, now.onGround
		}
	}
	told.yaw, told.pitch = now.yaw, now.pitch
	return told, outbound{id: packetID, data: buf.Bytes()}, true
}

// register puts a built body into its kind's list and shows it to every
// player of its world in range.
func (k *trackedKind[B, S]) register(s *Server, b B) {
	w := k.world(b)
	type spot struct {
		c           *client
		x, z        float64
		chunkRadius int32
	}
	s.mu.RLock()
	var spots []spot
	for _, c := range s.clients {
		if c.theWorld() != w {
			continue
		}
		pos := c.player.Position()
		spots = append(spots, spot{c: c, x: pos.X, z: pos.Z, chunkRadius: c.chunkRadius()})
	}

	mu := k.lock(s)
	mu.Lock()
	list := k.list(s)
	*list = append(*list, b)
	k.file(s, b)
	v := k.body(b)
	v.t.origin.stamp(w, v.p.x, v.p.y, v.p.z)
	var recipients []*client
	for _, sp := range spots {
		if !k.sees(b, v.p, sp.x, sp.z, sp.chunkRadius, int(s.entityBroadcastRange.Load())) {
			continue
		}
		recipients = append(recipients, sp.c)
		v.mark(sp.c)
	}
	// Built under the lock, from the body as it is; the tick may have moved
	// it by the time the sends run.
	shown := k.pairing(b)
	mu.Unlock()
	s.mu.RUnlock()

	sendPacketsToAll(w, recipients, shown)
}

// stamp records the body's initial spawn position unless it already has one or
// was loaded from a save without one.
func (o *entityOrigin) stamp(w *world.World, x, y, z float64) {
	if !o.set && !o.loaded {
		*o = entityOrigin{set: true, dim: w.DimensionName(), x: x, y: y, z: z}
	}
}

func (b *bodyTrack[S]) markOriginLoaded() { b.origin.loaded = true }

func markOriginsLoaded[T interface{ markOriginLoaded() }](ents []T) {
	for _, e := range ents {
		e.markOriginLoaded()
	}
}

// showLocked marks b told for every watcher of its world in range and
// returns them, for a registration that holds more than the kind's lock.
// The caller holds the kind's lock and sends the pairing after it.
func (k *trackedKind[B, S]) showLocked(b B, watchers []trackingWatcher) []*client {
	v := k.body(b)
	v.t.origin.stamp(v.w, v.p.x, v.p.y, v.p.z)
	var shown []*client
	for _, wh := range watchers {
		if wh.c.theWorld() != v.w || !k.sees(b, v.p, wh.x, wh.z, wh.chunkRadius, int(wh.c.srv.entityBroadcastRange.Load())) {
			continue
		}
		v.mark(wh.c)
		shown = append(shown, wh.c)
	}
	return shown
}

// sendJoin shows a joining or arriving client every body of the kind in its
// world within range.
func (k *trackedKind[B, S]) sendJoin(c *client) error {
	w := c.theWorld()
	here := c.player.Position()
	radius := c.chunkRadius()
	var shown []outbound
	mu := k.lock(c.srv)
	mu.Lock()
	for b := range k.bodies(c.srv) {
		if k.dead(b) || k.world(b) != w {
			continue
		}
		v := k.body(b)
		if !k.sees(b, v.p, here.X, here.Z, radius, int(c.srv.entityBroadcastRange.Load())) {
			v.t.tracked.delete(c)
			continue
		}
		if _, told := v.t.tracked.get(c); told {
			// Already shown: the wake's own register beat the join to it.
			continue
		}
		v.mark(c)
		shown = append(shown, k.pairing(b)...)
	}
	mu.Unlock()
	return c.writeAll(shown)
}

// forget drops a leaving player from every body's record.
func (k *trackedKind[B, S]) forget(s *Server, c *client) {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	for b := range k.bodies(s) {
		k.track(b).tracked.delete(c)
	}
}

// held appends to ids the bodies of the kind a watcher was told of.
func (k *trackedKind[B, S]) held(s *Server, c *client, ids []int32) []int32 {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	for b := range k.bodies(s) {
		if _, told := k.track(b).tracked.get(c); told {
			ids = append(ids, k.id(b))
		}
	}
	return ids
}

// observerTracker is a trackedKind as the player-side records see it,
// whatever its body and state types.
type observerTracker interface {
	forget(s *Server, c *client)
	held(s *Server, c *client, ids []int32) []int32
}

// observerTrackers is every kind the tracker follows, built on use: the
// trackers' own functions reach the respawn that reads this.
func observerTrackers() []observerTracker {
	return []observerTracker{
		&itemTracker, &mobTracker, &arrowTracker, &thrownTracker, &bobberTracker,
		&cloudTracker, &orbTracker, &tntTracker, &fireworkTracker, &boatTracker,
		&minecartTracker, &fallingTracker, &paintingTracker, &frameTracker,
		&knotTracker, &standTracker, &endCrystalTracker, &mannequinTracker, &eyeTracker, &fangsTracker, &boltTracker, &fixtureTracker,
	}
}

// frozenPass settles every watcher's view of the kind's bodies, and tells
// the teleports, while a frozen world holds their own tick.
// vanilla: ChunkMap.tick
func (k *trackedKind[B, S]) frozenPass(s *Server, watchers []trackingWatcher) {
	k.pass(s, watchers, false)
}

// dormantPass keeps pairing and movement synchronization independent of
// simulation for the bodies the kind's tick held still this tick.
// vanilla: ChunkMap.tick
func (k *trackedKind[B, S]) dormantPass(s *Server, held []B) {
	if len(held) == 0 {
		return
	}
	watchers := s.trackingWatchers()
	near := nearbyTracking(watchers)
	defer near.release()
	var out trackSends
	mu := k.lock(s)
	s.lockTick(mu)
	for _, b := range held {
		if !k.dead(b) {
			k.settleBody(b, watchers, near, false, &out)
		}
	}
	s.unlockTick(mu)
	out.send()
}

// pass settles watchers and, while running, advances the movement schedule.
func (k *trackedKind[B, S]) pass(s *Server, watchers []trackingWatcher, running bool) {
	near := nearbyTracking(watchers)
	defer near.release()
	var out trackSends
	mu := k.lock(s)
	s.lockTick(mu)
	for _, b := range *k.list(s) {
		if !k.dead(b) {
			k.settleBody(b, watchers, near, running, &out)
		}
	}
	s.unlockTick(mu)
	out.send()
}

// settleBody runs one live body's share of a pass, under the kind lock.
func (k *trackedKind[B, S]) settleBody(b B, watchers []trackingWatcher, near *nearbyPlayers, running bool, out *trackSends) {
	v := k.body(b)
	var mt moveTick
	if running || k.alwaysPosition || k.frozenSchedule {
		mt = v.schedule(v.t.takeSync)
	} else {
		mt = v.teleportTick()
	}
	for _, i := range v.watchers(near) {
		wt := watchers[i]
		if wt.c.theWorld() != v.w {
			continue // crossed to another dimension since the snapshot
		}
		if was, seen := v.settle(wt.c, wt.x, wt.z, wt.chunkRadius, out); seen && (mt.teleported || mt.gate) {
			v.moveTold(was, wt.c, mt, out)
		}
	}
}

// teleport moves a body with set, run under the kind's lock, and marks it for
// the kind's next tracking pass to tell every watcher where it now stands.
// vanilla: Entity.teleportTo, ServerEntity.sendChanges
func (k *trackedKind[B, S]) teleport(s *Server, b B, set func()) {
	mu := k.lock(s)
	mu.Lock()
	set()
	k.invalidatePistonGrid(s, b)
	k.track(b).teleported = true
	mu.Unlock()
}

// invalidatePistonGrid runs under the kind lock, like the box cache it drops.
func (k *trackedKind[B, S]) invalidatePistonGrid(s *Server, b B) {
	if k.invalidatePiston == nil {
		return
	}
	w := k.world(b)
	for _, dim := range s.allDims() {
		if dim.w == w {
			k.invalidatePiston(dim)
			return
		}
	}
}
