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

// vanilla: Entity.load, Entity.baseTick, Entity.setSharedFlagOnFire
func TestArmorStandDataFireWaitsForEntityTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {Invulnerable:1b,NoGravity:1b,Glowing:1b}`)
	stand := c.ExpectArmorStandSpawn()
	if stand.Flags != protocol.EntityFlagGlowing {
		t.Fatalf("spawn flags = %#x, want glowing without fire", stand.Flags)
	}

	expectFlags := func(ticks int, want uint8) {
		t.Helper()
		if !c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
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
		c.ExpectNoneWithinTicks(srv, 3, servertest.PacketAbout(stand.EntityID, protocol.IDPlayEntityMetadata))
	}

	c.ChatCommand(`tick freeze`)
	c.ChatCommand(`data merge entity @e[type=armor_stand,limit=1] {Fire:40s,HasVisualFire:0b}`)
	expectFlags(0, protocol.EntityFlagGlowing)
	c.ChatCommand(`tick step 1`)
	expectFlags(1, protocol.EntityFlagGlowing|protocol.EntityFlagOnFire)

	// Clearing Fire must keep the flames until a tick, while Glowing still
	// changes immediately. Full command metadata must preserve both facts.
	c.ChatCommand(`data merge entity @e[type=armor_stand,limit=1] {Fire:0s,Glowing:0b}`)
	expectFlags(0, protocol.EntityFlagOnFire)
	c.ChatCommand(`tick step 1`)
	expectFlags(1, 0)

	c.ChatCommand(`data merge entity @e[type=armor_stand,limit=1] {HasVisualFire:1b}`)
	expectFlags(0, 0)
	c.ChatCommand(`tick step 1`)
	expectFlags(1, protocol.EntityFlagOnFire)
	c.ChatCommand(`data merge entity @e[type=armor_stand,limit=1] {HasVisualFire:0b}`)
	expectFlags(0, protocol.EntityFlagOnFire)
	c.ChatCommand(`tick step 1`)
	expectFlags(1, 0)
}
