package advancements

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// sentAdvancement is one advancement as Update Advancements carries it.
type sentAdvancement struct {
	parent       string
	display      bool
	title        string
	icon, count  int32
	frame, flags int32
	background   string
	x, y         float32
	requirements [][]string
	telemetry    bool
}

// sentUpdate is one Update Advancements. progress holds, per advancement,
// each criterion's obtained time in epoch milliseconds, or nil for one not
// obtained.
type sentUpdate struct {
	reset    bool
	added    map[string]sentAdvancement
	removed  []string
	progress map[string]map[string]*int64
	show     bool
}

// readText reads a network text component and returns its translation key.
func readText(t *testing.T, r *protocol.Reader) (string, *protocol.Reader) {
	t.Helper()
	rest := r.Rest()
	tag, n, err := nbt.DecodeNetwork(rest)
	if err != nil {
		t.Fatalf("text component: %v", err)
	}
	text, ok := protocol.TextOf(tag)
	if !ok {
		t.Fatalf("not a text component: %v", tag)
	}
	return text.Translate, protocol.NewReader(rest[n:])
}

// parseUpdate reads Update Advancements whole, failing on bytes left over.
func parseUpdate(t *testing.T, data []byte) sentUpdate {
	t.Helper()
	r := protocol.NewReader(data)
	u := sentUpdate{reset: r.ReadBool(), added: map[string]sentAdvancement{}, progress: map[string]map[string]*int64{}}
	for range r.ReadVarInt() {
		id := r.ReadString(32767)
		var a sentAdvancement
		if r.ReadBool() {
			a.parent = r.ReadString(32767)
		}
		if a.display = r.ReadBool(); a.display {
			a.title, r = readText(t, r)
			_, r = readText(t, r)
			a.icon, a.count = r.ReadVarInt(), r.ReadVarInt()
			if added, removed := r.ReadVarInt(), r.ReadVarInt(); added != 0 || removed != 0 {
				t.Fatalf("%s: icon carries a component patch (%d, %d)", id, added, removed)
			}
			a.frame = r.ReadVarInt()
			a.flags = r.ReadI32()
			if a.flags&0x01 != 0 {
				a.background = r.ReadString(32767)
			}
		}
		for range r.ReadVarInt() {
			var group []string
			for range r.ReadVarInt() {
				group = append(group, r.ReadString(32767))
			}
			a.requirements = append(a.requirements, group)
		}
		a.telemetry = r.ReadBool()
		a.x, a.y = r.ReadF32(), r.ReadF32()
		u.added[id] = a
	}
	for range r.ReadVarInt() {
		u.removed = append(u.removed, r.ReadString(32767))
	}
	for range r.ReadVarInt() {
		id := r.ReadString(32767)
		criteria := map[string]*int64{}
		for range r.ReadVarInt() {
			name := r.ReadString(32767)
			if r.ReadBool() {
				at := r.ReadI64()
				criteria[name] = &at
			} else {
				criteria[name] = nil
			}
		}
		u.progress[id] = criteria
	}
	u.show = r.ReadBool()
	if err := r.Err(); err != nil {
		t.Fatalf("update advancements: %v", err)
	}
	if r.Remaining() != 0 {
		t.Fatalf("update advancements has %d bytes left over", r.Remaining())
	}
	return u
}

// join logs a player in and reads the join, returning the Update
// Advancements that arrived inside it, if any.
func join(t *testing.T, connect func() *servertest.Client, name string) (*servertest.Client, []byte) {
	t.Helper()
	c := connect()
	c.WatchAdvancements = true
	c.Login(name)
	c.Configure()
	var update []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayUpdateAdvancements {
			if update != nil {
				t.Fatal("two Update Advancements arrived during the join")
			}
			update = data
		}
	})
	c.MarkLoaded()
	return c, update
}

// joinUpdate is the join's first Update Advancements: the one join returned,
// or the next if the first tick's flush came after the join's chunks.
func joinUpdate(t *testing.T, srv *server.Server, c *servertest.Client, update []byte) sentUpdate {
	t.Helper()
	if update == nil {
		return awaitUpdate(t, srv, c)
	}
	return parseUpdate(t, update)
}

// done lists the added advancements whose progress meets every requirement.
func (u sentUpdate) done() []string {
	var ids []string
	for id, a := range u.added {
		if !slices.ContainsFunc(a.requirements, func(group []string) bool {
			return !slices.ContainsFunc(group, func(c string) bool { return u.progress[id][c] != nil })
		}) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// awaitUpdate waits for the next Update Advancements.
func awaitUpdate(t *testing.T, srv *server.Server, c *servertest.Client) sentUpdate {
	t.Helper()
	return parseUpdate(t, c.ExpectWithinTicks(srv, 40, protocol.IDPlayUpdateAdvancements))
}
