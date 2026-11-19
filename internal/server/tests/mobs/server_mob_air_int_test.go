package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestMobAirReadsIntLikeVanilla writes Air as other numeric tag types, as
// Entity.load's getIntOr accepts, and checks the widened value on the wire.
// vanilla: Entity.load
func TestMobAirReadsIntLikeVanilla(t *testing.T) {
	t.Parallel()
	srv, c, id := summonInPool(t, "cow")
	identity, ok := srv.MobUUID(id)
	if !ok {
		t.Fatal("summoned cow has no UUID")
	}

	for _, tc := range []struct {
		tag  string
		want int32
	}{
		{tag: "{Air:70000}", want: 70000},
		{tag: "{Air:-1.5f}", want: -2},
	} {
		c.ChatCommand("data merge entity " + identity + " " + tc.tag)
		for {
			entry := servertest.MetaOf(c.AwaitMetadataFor(id), protocol.MetadataIndexAir)
			if entry == nil {
				continue
			}
			if entry.Typ != protocol.MetadataVarInt {
				t.Fatalf("air field type after data merge = %d, want %d (a varint)", entry.Typ, protocol.MetadataVarInt)
			}
			if entry.VarintVal != tc.want {
				t.Fatalf("air after merging %s = %d, want %d", tc.tag, entry.VarintVal, tc.want)
			}
			break
		}
	}
}
