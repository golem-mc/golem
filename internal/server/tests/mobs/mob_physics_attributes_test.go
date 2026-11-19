package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestMobGravityAttributeControlsFall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig 1.5 100 0.5 {Tags:["slow"]}`)
	slow := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand(`summon minecraft:pig 4.5 100 0.5`)
	normal := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("attribute @e[tag=slow,limit=1] minecraft:gravity base set 0.01")
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	for _, tc := range []struct {
		id   int32
		slow bool
	}{{slow.EntityID, true}, {normal.EntityID, false}} {
		_, y, _, ok := srv.MobPos(tc.id)
		if !ok {
			t.Fatal("pig disappeared")
		}
		fallen := 100 - y
		if tc.slow && fallen >= 1 || !tc.slow && fallen <= 3 {
			t.Errorf("slow=%v: pig fell %g blocks in ten ticks", tc.slow, fallen)
		}
	}
}

func TestMobFallAttributeBases(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	for _, tc := range []struct {
		kind             string
		entityType       int32
		safe, multiplier string
	}{
		{"horse", protocol.EntityTypeHorse, "6.0", "0.5"},
		{"camel", protocol.EntityTypeCamel, "6.0", "0.5"},
		{"fox", protocol.EntityTypeFox, "5.0", "1.0"},
	} {
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s 1.5 64 0.5 {NoAI:1b}", tc.kind))
		c.ExpectMobSpawn(tc.entityType)
		for _, attr := range []struct{ name, want string }{{"safe_fall_distance", tc.safe}, {"fall_damage_multiplier", tc.multiplier}} {
			c.ChatCommand(fmt.Sprintf("attribute @e[type=%s,limit=1] minecraft:%s get", tc.kind, attr.name))
			value := ""
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySystemChat {
					return
				}
				if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
					value = text.With[len(text.With)-1].Text
				}
			})
			if value != attr.want {
				t.Errorf("%s %s = %q, want %s", tc.kind, attr.name, value, attr.want)
			}
		}
	}
}

func TestMobJumpStrengthAttributeControlsPathJump(t *testing.T) {
	t.Parallel()
	for _, strength := range []float64{0, 0.42} {
		t.Run(fmt.Sprint(strength), func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Prey", servertest.OpsListing(t, "Prey"))
			srv.SetDayTime(18000)
			stone := registry.BlockStone.DefaultState()
			for x := int32(1); x <= 8; x++ {
				for z := int32(-3); z <= 3; z++ {
					srv.SetBlock(x, 64, z, stone)
				}
			}
			c.MoveToGround(6.5, 65, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			zombie := srv.SpawnMob("minecraft:zombie", -1.5, 64, 0.5)
			srv.SetMobBaby(zombie, false)
			c.ExpectMobSpawn(protocol.EntityTypeZombie)
			c.ChatCommand(fmt.Sprintf("attribute @e[type=zombie,limit=1] minecraft:jump_strength base set %g", strength))
			c.Settle()
			maxY := 64.0
			for range 80 {
				servertest.RunTicks(t, srv, 1)
				_, y, _, ok := srv.MobPos(zombie)
				if !ok {
					t.Fatal("zombie disappeared")
				}
				maxY = math.Max(maxY, y)
			}
			if strength == 0 && maxY > 64+1e-6 {
				t.Errorf("zero jump strength zombie rose to %g", maxY)
			}
			if strength > 0 && maxY < 65 {
				t.Errorf("control zombie never jumped onto the ledge: max y=%g", maxY)
			}
		})
	}
}
