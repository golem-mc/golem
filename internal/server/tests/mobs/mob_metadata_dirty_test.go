package mobs

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: SynchedEntityData.set, SynchedEntityData.packDirty, ServerEntity.sendDirtyEntityData
func TestMobMetadataBulkLoadPreservesBroadcastsAndTickDeltas(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Smith"))
	actor, watcher := connect().Join("Smith"), connect().Join("Watcher")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 7.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("cow did not spawn")
	}
	uuid, _ := srv.MobUUID(id)
	// Establish the NoAI baseline before testing unchanged writes.
	servertest.RunTicks(t, srv, 1)
	actor.Settle()
	watcher.Settle()
	check := func(t *testing.T, flags ...byte) {
		t.Helper()
		for i, c := range []*servertest.Client{actor, watcher} {
			count := 0
			c.SettleAndCollect(func(packetID int32, data []byte) {
				if packetID != protocol.IDPlayEntityMetadata {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return
				}
				count++
				if count > len(flags) {
					t.Errorf("client %d: unchanged metadata resent", i)
					return
				}
				var want protocol.Buffer
				want.PutVarInt(id)
				want.PutMetadata(protocol.MetadataIndexMobFlags, protocol.MetadataByte)
				want.PutU8(flags[count-1])
				want.EndMetadata()
				if !bytes.Equal(data, want.Bytes()) {
					t.Errorf("client %d: metadata = %x, want %x", i, data, want.Bytes())
				}
			})
			expected := len(flags)
			if count != expected {
				t.Errorf("client %d: got %d updates, want %d", i, count, expected)
			}
		}
	}
	left, right := byte(protocol.EntityFlagNoAI|protocol.EntityFlagLeftHanded), byte(protocol.EntityFlagNoAI)
	for _, step := range []struct {
		name   string
		writes []int
		flags  []byte
	}{
		{"unchanged right", []int{0}, nil},
		{"set left", []int{1}, []byte{left}},
		{"unchanged left", []int{1}, nil},
		{"set right", []int{0}, []byte{right}},
		{"revert to right", []int{1, 0}, []byte{left, right}},
		{"set left after revert", []int{1}, []byte{left}},
		{"revert to left", []int{0, 1}, []byte{right, left}},
	} {
		t.Run(step.name, func(t *testing.T) {
			for _, left := range step.writes {
				actor.ChatCommand(fmt.Sprintf("data merge entity %s {LeftHanded:%db}", uuid, left))
				actor.SettleKeeping()
			}
			// Command broadcasts advance both baselines, including each reverted write.
			check(t, step.flags...)
			servertest.RunTicks(t, srv, 1)
			check(t)
		})
	}
}
