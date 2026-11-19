package world

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// timerQueueFile is the saved-data file the scheduled events live in.
// vanilla: TimerQueue.TYPE
const timerQueueFile = "data/minecraft/scheduled_events.dat"

// The callback types a saved event names.
// vanilla: TimerCallbacks.SERVER_CALLBACKS
const (
	timerFunctionType    = "minecraft:function"
	timerFunctionTagType = "minecraft:function_tag"
)

// TimerCallback is what a scheduled event runs: the function ID, or every
// function of the tag ID.
// vanilla: FunctionCallback, FunctionTagCallback
type TimerCallback struct {
	Tag bool
	ID  string
}

// timerEvent is one scheduled event; seq orders the events of one tick.
type timerEvent struct {
	time int64
	seq  uint64
	id   string
	cb   TimerCallback
}

// TimerQueue is the events /schedule keeps, saved in
// data/minecraft/scheduled_events.dat.
// vanilla: TimerQueue
type TimerQueue struct {
	root string

	mu    sync.Mutex
	queue []*timerEvent // by (time, seq)
	seq   uint64
	// rows holds each event by id and trigger time, making a second event at
	// the same id and time a no-op.
	rows  map[string]map[int64]*timerEvent
	dirty bool
}

// NewTimerQueue loads the world's saved events under root.
func NewTimerQueue(root string) *TimerQueue {
	q := &TimerQueue{root: root, rows: map[string]map[int64]*timerEvent{}}
	q.load()
	return q
}

// Schedule queues cb under id to run at game time time, unless the id
// already has an event at that time.
// vanilla: TimerQueue.schedule
func (q *TimerQueue) Schedule(id string, time int64, cb TimerCallback) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.schedule(id, time, cb)
}

func (q *TimerQueue) schedule(id string, time int64, cb TimerCallback) {
	if _, ok := q.rows[id][time]; ok {
		return
	}
	q.seq++
	ev := &timerEvent{time: time, seq: q.seq, id: id, cb: cb}
	if q.rows[id] == nil {
		q.rows[id] = map[int64]*timerEvent{}
	}
	q.rows[id][time] = ev
	// seq only grows, so inserting after equal times keeps scheduling order
	// within a tick.
	at := sort.Search(len(q.queue), func(i int) bool { return q.queue[i].time > time })
	q.queue = slices.Insert(q.queue, at, ev)
	q.dirty = true
}

// Remove drops every event of the id and returns how many there were.
// vanilla: TimerQueue.remove
func (q *TimerQueue) Remove(id string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	row := q.rows[id]
	q.queue = slices.DeleteFunc(q.queue, func(ev *timerEvent) bool { return row[ev.time] == ev })
	delete(q.rows, id)
	q.dirty = true
	return len(row)
}

// IDs returns the ids that have an event, sorted.
// vanilla: TimerQueue.getEventsIds
func (q *TimerQueue) IDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	ids := make([]string, 0, len(q.rows))
	for id := range q.rows {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Tick runs every event due by now, in order. No lock is held while run runs,
// so it may schedule and remove events.
// vanilla: TimerQueue.tick
func (q *TimerQueue) Tick(now int64, run func(TimerCallback)) {
	for {
		q.mu.Lock()
		if len(q.queue) == 0 || q.queue[0].time > now {
			q.mu.Unlock()
			return
		}
		ev := q.queue[0]
		q.queue = q.queue[1:]
		// The row is dropped under now, not the event's time, so an event that
		// ran late keeps its row until a clear.
		// vanilla: TimerQueue.tick, events.remove(id, currentTick)
		if row := q.rows[ev.id]; row != nil {
			delete(row, now)
			if len(row) == 0 {
				delete(q.rows, ev.id)
			}
		}
		q.dirty = true
		q.mu.Unlock()
		run(ev.cb)
	}
}

// Save writes the events when they changed since the last save.
func (q *TimerQueue) Save() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.dirty {
		return
	}
	events := make([]nbt.Tag, len(q.queue))
	for i, ev := range q.queue {
		typ := timerFunctionType
		if ev.cb.Tag {
			typ = timerFunctionTagType
		}
		events[i] = nbt.Compound{
			"trigger_time": nbt.Long(ev.time),
			"id":           nbt.String(ev.id),
			"callback":     nbt.Compound{"type": nbt.String(typ), "id": nbt.String(ev.cb.ID)},
		}
	}
	data := nbt.Compound{"events": nbt.List{Elem: nbt.TypeCompound, Items: events}}
	if err := writeSavedData(q.root, timerQueueFile, data); err != nil {
		slog.Error("could not save the scheduled events", "error", err)
		return
	}
	q.dirty = false
}

// load schedules the saved events; one undecodable event drops the whole file.
// vanilla: TimerQueue.codec
func (q *TimerQueue) load() {
	data, err := readSavedData(q.root, timerQueueFile)
	if errors.Is(err, errNoSavedData) {
		return
	} else if err != nil {
		slog.Warn("could not read scheduled_events.dat, using none", "error", err)
		return
	}
	list, _ := nbt.Get[nbt.List](data, "events")
	loaded := make([]*timerEvent, 0, len(list.Items))
	for _, tag := range list.Items {
		ev, err := decodeTimerEvent(tag)
		if err != nil {
			slog.Warn("could not read scheduled_events.dat, using none", "error", err)
			return
		}
		loaded = append(loaded, ev)
	}
	for _, ev := range loaded {
		q.schedule(ev.id, ev.time, ev.cb)
	}
	q.dirty = false
}

func decodeTimerEvent(tag nbt.Tag) (*timerEvent, error) {
	c, ok := tag.(nbt.Compound)
	if !ok {
		return nil, errors.New("event is not a compound")
	}
	trigger, okTime := nbt.Get[nbt.Long](c, "trigger_time")
	id, okID := nbt.Get[nbt.String](c, "id")
	cb, okCB := nbt.Get[nbt.Compound](c, "callback")
	if !okTime || !okID || !okCB {
		return nil, errors.New("event is missing trigger_time, id or callback")
	}
	typ, okType := nbt.Get[nbt.String](cb, "type")
	fn, okFn := nbt.Get[nbt.String](cb, "id")
	if !okType || !okFn {
		return nil, errors.New("callback is missing type or id")
	}
	ev := &timerEvent{time: int64(trigger), id: string(id), cb: TimerCallback{ID: registry.Qualify(string(fn))}}
	switch registry.Qualify(string(typ)) {
	case timerFunctionType:
	case timerFunctionTagType:
		ev.cb.Tag = true
	default:
		return nil, fmt.Errorf("unknown callback type %s", typ)
	}
	return ev, nil
}
