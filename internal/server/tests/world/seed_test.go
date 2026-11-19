package world

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// seededKillDrops stands a server up on the test's seed, summons a cow and
// kills it the same dispatch — the summon and the kill ride the one
// command, so the cow dies before its goals have rolled — and reads the
// stacks the death laid down off the wire, each with the spot the pop dice
// scattered it to. The dice the drops come off are the ones the seed gave,
// and the world runs no tick before the command but the held ones, so two
// servers that agree on the seed agree here whatever pace their joins ran at.
func seededKillDrops(t *testing.T) []servertest.ItemDrop {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Executioner"))
	servertest.FreezeUntilHeld(t, srv)
	c.Join("Executioner")
	servertest.HoldTicks(t, srv)
	c.Settle()

	c.ChatCommand("execute summon minecraft:cow run kill @s")

	var (
		drops       []servertest.ItemDrop
		pending     int32
		pendingDrop servertest.ItemDrop
	)
	// The death is the tick pass's work, so the drops land a tick or two
	// behind the command; the held ticks wait them out.
	c.ReadTicks(srv, killDropTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			if drop := servertest.ParseItemSpawn(data); drop.EntityID != 0 {
				pending, pendingDrop = drop.EntityID, drop
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != pending {
				return false // another body's state; the drop's is still to come
			}
			d := pendingDrop
			pending, pendingDrop = 0, servertest.ItemDrop{}
			if index := r.ReadU8(); index != protocol.MetadataIndexItemSlot {
				t.Fatalf("the cow's drop carried metadata field %d, want %d (the Item slot)", index, protocol.MetadataIndexItemSlot)
			}
			if r.ReadVarInt() != protocol.MetadataSlot {
				t.Fatalf("the cow's drop carried a field of another kind, want %d (a slot)", protocol.MetadataSlot)
			}
			stack, ok := r.ReadSlot()
			if !ok {
				t.Fatalf("the cow's drop did not read back")
			}
			d.Stack = stack
			drops = append(drops, d)
		}
		return false
	})
	return drops
}

// killDropTicks is how many ticks seededKillDrops reads the drops for, a
// second of game time.
const killDropTicks = 20

// TestTheSameSeedDropsTheSameKill runs the same one-command kill on two
// servers of the same seed and holds the two deaths' drops against each
// other: the rolled stacks and the spots they were scattered to, none of it
// anything a random source outside the seed could have moved.
func TestTheSameSeedDropsTheSameKill(t *testing.T) {
	t.Parallel()
	first := seededKillDrops(t)
	second := seededKillDrops(t)
	if len(first) == 0 || len(second) == 0 {
		t.Fatalf("the killed cows laid down %d and %d stacks, want at least one of each", len(first), len(second))
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("the same seed dropped differently:\nfirst  %s\nsecond %s", describeDrops(first), describeDrops(second))
	}
}

// describeDrops reads a drop list back as the text a failure names it by:
// what each stack held, how many of it, and where it was scattered to.
func describeDrops(drops []servertest.ItemDrop) string {
	var b bytes.Buffer
	for _, d := range drops {
		name, _ := registry.ItemName(d.Stack.ItemID)
		fmt.Fprintf(&b, "%s x%d at %.2f,%.2f,%.2f; ", name, d.Stack.Count, d.X, d.Y, d.Z)
	}
	return b.String()
}
