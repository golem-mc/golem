package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

func TestBroadcastReusesOneCompressedFrame(t *testing.T) {
	t.Parallel()
	s := &Server{clients: make(map[uuid.UUID]*client)}
	var clients []*client
	for i := range 50 {
		c, _ := mailboxClient()
		c.srv = s
		c.conn.EnableCompression(256)
		s.assignPlayerSlot(c)
		s.clients[uuid.UUID{byte(i)}] = c
		clients = append(clients, c)
	}
	data := bytes.Repeat([]byte{1, 2, 3}, 17067)
	s.broadcast(protocol.IDPlayMapData, data)
	shared := clients[0].out.queue[0].shared
	if shared == nil {
		t.Fatal("broadcast did not share its frame")
	}
	for _, c := range clients {
		p := c.out.queue[0]
		if p.shared != shared {
			t.Fatal("recipient recompressed the broadcast")
		}
		if !c.out.writeBatch() {
			t.Fatal("drain failed")
		}
	}
	// Small, sparse broadcasts keep the ordinary framing path.
	sendToAll(nil, clients[:2], outbound{id: 1, data: []byte{1}})
	if clients[0].out.queue[0].shared != nil {
		t.Fatal("small sparse packet used a shared frame")
	}
}

func TestSharedFanoutKeepsWorldVisibilityAndAccounting(t *testing.T) {
	t.Parallel()
	a, _ := mailboxClient()
	b, _ := mailboxClient()
	w, elsewhere := &world.World{}, &world.World{}
	a.dim.Store(&dimension{w: w})
	b.dim.Store(&dimension{w: elsewhere})
	var chat protocol.Buffer
	chat.PutText(protocol.Text{Text: string(bytes.Repeat([]byte("message"), 100))})
	chat.PutBool(false)
	data := chat.Bytes()
	a.out.queued = mailboxLimit - len(data)
	packets := []outbound{{id: protocol.IDPlaySystemChat, data: data}}
	sendPacketsToAll(w, []*client{a, b}, packets)
	if len(a.out.queue) != 1 || a.out.queue[0].shared == nil || a.out.queued != mailboxLimit {
		t.Fatal("shared payload changed mailbox accounting")
	}
	if len(b.out.queue) != 0 {
		t.Fatal("shared packet crossed a dimension change")
	}
	a.out.queue, a.out.queued = nil, 0
	a.chatMode.Store(protocol.ChatModeHidden)
	sendPacketsToAll(w, []*client{a, b}, packets)
	if len(a.out.queue) != 0 {
		t.Fatal("shared system chat bypassed visibility")
	}
	a.sendUnfiltered(protocol.IDPlaySystemChat, data)
	if len(a.out.queue) != 1 {
		t.Fatal("hidden-chat refusal was filtered")
	}
}

func TestCachedDirectWriteFollowsSharedBroadcast(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"joined", "held", "joining"} {
		t.Run(mode, func(t *testing.T) {
			c, wire := mailboxClient()
			c.conn.EnableCompression(256)
			switch mode {
			case "held":
				release, err := c.holdWriter()
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			case "joining":
				c.out.ready = make(chan struct{})
				c.out.released.Store(false)
			}
			data := bytes.Repeat([]byte("broadcast"), 100)
			c.sendOutboundIn(nil, outbound{id: 1, data: data, shared: protocol.NewCachedPacket(1, data)})
			direct := bytes.Repeat([]byte("chunk"), 100)
			if err := c.writePacketCached(2, "cached reply", nil, protocol.NewCachedPacket(2, direct)); err != nil {
				t.Fatal(err)
			}
			reader := protocol.NewConn(wire)
			reader.EnableCompression(256)
			ids := []int32{1, 2}
			if mode != "joined" {
				ids = []int32{2}
			}
			for _, id := range ids {
				gotID, payload, err := reader.ReadPacket()
				want := direct
				if id == 1 {
					want = data
				}
				if err != nil || gotID != id || !bytes.Equal(payload, want) {
					t.Fatalf("packet = %d %x, %v; want %d %x", gotID, payload, err, id, want)
				}
			}
			remaining := 0
			if mode != "joined" {
				remaining = 1
			}
			if wire.Len() != 0 || wire.writes != 1 || len(c.out.queue) != remaining || c.out.queued != remaining*len(data) {
				t.Fatal("cached reply changed mailbox draining or accounting")
			}
		})
	}
}
