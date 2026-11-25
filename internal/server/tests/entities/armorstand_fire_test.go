package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.baseTick, Entity.setSharedFlagOnFire, ServerEntity.sendDirtyEntityData
func TestArmorStandFireMetadataReachesExistingWatchers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {Invulnerable:1b,Glowing:1b}`)
	stand := c.ExpectArmorStandSpawn()
	if stand.Flags != protocol.EntityFlagGlowing {
		t.Fatalf("spawn flags = %#x, want glowing without fire", stand.Flags)
	}

	expectFlags := func(want uint8) {
		t.Helper()
		if !c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
			if id != protocol.IDPlayEntityMetadata {
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != stand.EntityID {
				return false
			}
			entries, ok := servertest.ReadMetadata(r)
			if !ok {
				t.Fatal("invalid armor stand metadata")
			}
			for _, e := range entries {
				if e.Index == protocol.MetadataIndexEntityFlags {
					if e.ByteVal != want {
						t.Fatalf("flags = %#x, want %#x", e.ByteVal, want)
					}
					return true
				}
			}
			return false
		}) {
			t.Fatalf("no metadata updating stand flags to %#x", want)
		}
	}

	srv.SetBlock(5, 64, 5, registry.BlockLava.DefaultState())
	expectFlags(protocol.EntityFlagGlowing | protocol.EntityFlagOnFire)
	// An unchanged burning flag must not be sent every tick.
	c.ExpectNoneWithinTicks(srv, 3, servertest.PacketAbout(stand.EntityID, protocol.IDPlayEntityMetadata))
	srv.SetBlock(5, 64, 5, registry.BlockWater.DefaultState())
	expectFlags(protocol.EntityFlagGlowing)
	c.ExpectNoneWithinTicks(srv, 3, servertest.PacketAbout(stand.EntityID, protocol.IDPlayEntityMetadata))

	// The /data reply preserves the current fire flag until a tick; expiry
	// must then compare with the flag that tick sent.
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] {Fire:2s,NoGravity:1b}`)
	expectFlags(protocol.EntityFlagGlowing)
	expectFlags(protocol.EntityFlagGlowing | protocol.EntityFlagOnFire)
	expectFlags(protocol.EntityFlagGlowing)

	// Expiry of actual fire must preserve the HasVisualFire override.
	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,limit=1] {Fire:2s,HasVisualFire:1b}`)
	expectFlags(protocol.EntityFlagGlowing)
	expectFlags(protocol.EntityFlagGlowing | protocol.EntityFlagOnFire)
	c.ExpectNoneWithinTicks(srv, 3, servertest.PacketAbout(stand.EntityID, protocol.IDPlayEntityMetadata))
}

// vanilla: Entity.baseTick, ArmorStand.hurtServer
func TestMarkerArmorStandFireExpires(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {Marker:1b,Fire:2s,Tags:["markcheck"]}`)
	stand := c.ExpectArmorStandSpawn()
	fire := func() string {
		t.Helper()
		return standCommandText(t, c, "data get entity @e[tag=markcheck,limit=1] Fire", "commands.data.entity.query").With[1].Plain()
	}
	sawFlags := func(want uint8) bool {
		return c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			if id != protocol.IDPlayEntityMetadata {
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != stand.EntityID {
				return false
			}
			entries, _ := servertest.ReadMetadata(r)
			for _, e := range entries {
				if e.Index == protocol.MetadataIndexEntityFlags && e.ByteVal&protocol.EntityFlagOnFire == want {
					return true
				}
			}
			return false
		})
	}
	if !sawFlags(protocol.EntityFlagOnFire) {
		t.Fatalf("on-fire bit not sent on the first tick")
	}
	if got := fire(); got != "1s" {
		t.Fatalf("Fire after one tick = %s, want 1s", got)
	}
	if !sawFlags(0) {
		t.Fatalf("on-fire bit not cleared after fire expired")
	}
	if got := fire(); got != "0s" {
		t.Fatalf("Fire after two ticks = %s, want 0s", got)
	}
}
