package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFireballsMotionCarriesItsProjectilePower summons a fireball with an
// acceleration power of its own: its motion goes out in a bundle with a
// Projectile Power carrying that power, so the client predicts the flight
// with it.
// vanilla: ServerEntity.sendChanges
func TestAFireballsMotionCarriesItsProjectilePower(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Pyro", servertest.OpsListing(t, "Pyro"))
	c.ChatCommand(`summon minecraft:fireball 4.5 70 4.5 {acceleration_power:0.5d,Motion:[0.5d,0d,0d]}`)
	fireball := c.AwaitSpawn(protocol.EntityTypeFireball).EntityID
	c.WatchBundles = true

	var ids []int32
	var power float64
	found := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		ids = append(ids, id)
		if id != protocol.IDPlayProjectilePower {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != fireball {
			return false
		}
		power = r.ReadF64()
		return true
	})
	if !found {
		t.Fatal("no projectile_power for the fireball within 20 ticks")
	}
	if power != 0.5 {
		t.Errorf("projectile_power carries %v, want the summoned 0.5", power)
	}
	n := len(ids)
	if n < 3 || ids[n-3] != protocol.IDPlayBundleDelimiter || ids[n-2] != protocol.IDPlaySetEntityMotion {
		t.Errorf("projectile_power follows % X, want a bundle delimiter and Set Entity Motion", ids[max(0, n-3):])
	}
	next := int32(-1)
	c.ReadTicks(srv, 0, func(id int32, _ []byte) bool { next = id; return true })
	if next != protocol.IDPlayBundleDelimiter {
		t.Errorf("projectile_power is followed by 0x%02X, want the closing bundle delimiter", next)
	}
}
