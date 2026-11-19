package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func nonlivingDamageServer(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

func damageNonliving(t *testing.T, c *servertest.Client, command, key string, observe func(int32, []byte)) {
	t.Helper()
	c.ChatCommand(command)
	var chat []byte
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			chat = bytes.Clone(data)
		}
		if observe != nil {
			observe(id, data)
		}
	})
	if !bytes.Contains(chat, []byte(key)) {
		t.Errorf("/%s feedback = %q, want %s", command, chat, key)
	}
}

func TestDamageCommandHurtsArmorStand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source, tag   string
		dead, success bool
		health        float64
	}{
		{source: "1 minecraft:arrow", dead: true, success: true},
		{source: "1 minecraft:explosion", dead: true},
		{source: "1 minecraft:arrow", tag: "{Marker:1b}", health: 20},
		{source: "1 minecraft:player_attack by Tester", dead: true, success: true},
	} {
		t.Run(fmt.Sprint(tc.source, tc.tag), func(t *testing.T) {
			srv, c := nonlivingDamageServer(t)
			c.ChatCommand("summon minecraft:armor_stand 3.5 64 0.5 " + damageTag(tc.tag))
			c.Settle()
			key := "commands.damage.invulnerable"
			if tc.success {
				key = "commands.damage.success"
			}
			damageNonliving(t, c, "damage @e[type=minecraft:armor_stand,limit=1] "+tc.source, key, nil)
			stands := srv.ArmorStandsInDimension(world.DimensionName)
			if tc.dead {
				if len(stands) != 0 {
					t.Error("stand survived")
				}
				return
			}
			if len(stands) != 1 {
				t.Fatalf("stands=%+v, want one", stands)
			}
		})
	}
}

func damageTag(tag string) string {
	if tag == "" {
		return "{}"
	}
	return tag
}
