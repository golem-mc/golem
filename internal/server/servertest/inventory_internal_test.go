package servertest

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Observer broadcasts can precede either of the direct writes that open a
// window, or a later slot correction. Queue them to cover each ordering.
func TestWindowReadersAllowObserverSwings(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"open", "content", "slot"} {
		t.Run(phase, func(t *testing.T) {
			w := &WindowedClient{Client: &Client{T: t}}
			queue := func(id int32, write func(*protocol.Buffer)) {
				var b protocol.Buffer
				write(&b)
				w.Pushback = append(w.Pushback, PushedPacket{Id: id, Data: b.Bytes()})
			}
			swing := func(at string) {
				if phase == at {
					queue(protocol.IDPlaySwingAnimation, func(b *protocol.Buffer) {
						b.PutVarInt(2) // another player's entity ID
						b.PutVarInt(protocol.HandMain)
						b.PutVarInt(registry.DefaultSwingAnimation.Type)
						b.PutVarInt(registry.DefaultSwingAnimation.Duration)
					})
				}
			}
			swing("open")
			queue(protocol.IDPlayOpenScreen, func(b *protocol.Buffer) {
				b.PutVarInt(1)
				b.PutVarInt(protocol.MenuGeneric9x3)
				b.PutText(protocol.Text{Translate: "container.chest"})
			})
			swing("content")
			queue(protocol.IDPlayContainerContent, func(b *protocol.Buffer) {
				b.PutVarInt(1)
				b.PutVarInt(3)
				b.PutVarInt(63)
				for range 63 {
					b.PutSlot(protocol.ItemStack{})
				}
				b.PutSlot(protocol.ItemStack{})
			})
			swing("slot")
			stack := protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 13}
			queue(protocol.IDPlayContainerSlot, func(b *protocol.Buffer) {
				b.PutVarInt(1)
				b.PutVarInt(4)
				b.PutI16(7)
				b.PutSlot(stack)
			})

			if slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, false, 0, 0, 0); len(slots) != 63 {
				t.Fatalf("opened with %d slots, want 63", len(slots))
			}
			if w.Window != 1 || w.StateOf(1) != 3 {
				t.Fatalf("open window = %d, state = %d; want 1, 3", w.Window, w.StateOf(1))
			}
			w.AwaitWindowSlot(7, stack)
			if w.StateOf(1) != 4 || len(w.Pushback) != 0 {
				t.Fatalf("slot update left state %d and %d unread packets", w.StateOf(1), len(w.Pushback))
			}
		})
	}
}
