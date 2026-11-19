package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestARabbitShowsItsVariant checks Rabbit.DATA_TYPE_ID on the wire: a
// summoned rabbit carries the RabbitType of its tag, and an id Rabbit.Variant
// does not know reads as the brown the client defaults to.
func TestARabbitShowsItsVariant(t *testing.T) {
	t.Parallel()
	const name = "Warden"
	for _, tc := range []struct {
		tag     string
		want    int32
		present bool
	}{
		{"{RabbitType:2}", 2, true},
		{"{RabbitType:99}", 99, true},
		{"{RabbitType:42}", 0, false},
		{"{}", 0, false},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			_, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
			c.Settle()
			c.ChatCommand("summon minecraft:rabbit 5.5 64 5.5 " + tc.tag)
			rabbit := c.ExpectMobSpawn(protocol.EntityTypeRabbit)
			if rabbit.HasRabbitType != tc.present || rabbit.RabbitType != tc.want {
				t.Errorf("%s: type %d (sent %v), want %d (sent %v)", tc.tag, rabbit.RabbitType, rabbit.HasRabbitType, tc.want, tc.present)
			}
		})
	}
}
