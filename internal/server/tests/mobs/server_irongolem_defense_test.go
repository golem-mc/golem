package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestAnIronGolemDefendsVillagersAgainstAnUnpopularPlayer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gossip  int
		built   bool
		attacks bool
	}{
		{"reputation threshold", 20, false, true},
		{"above threshold", 19, false, false},
		{"player built", 20, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "GolemDefense"
			srv, c, playerID := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			words := nbt.UUIDToIntArray(protocol.OfflineUUID(name))
			c.ChatCommand(fmt.Sprintf(`summon minecraft:villager -2.5 64 0.5 {NoAI:1b,Gossips:[{Target:[I;%d,%d,%d,%d],Type:"major_negative",Value:%d}]}`, words[0], words[1], words[2], words[3], tc.gossip))
			c.AwaitSpawn(villagerType(t))
			c.ChatCommand(fmt.Sprintf("summon minecraft:iron_golem 1.5 64 0.5 {PlayerCreated:%t}", tc.built))
			typ, _ := registry.EntityTypeID("minecraft:iron_golem")
			id := c.AwaitSpawn(typ).EntityID
			attacked, lifted, swung := false, false, false
			collect := func(pid int32, data []byte) {
				r := protocol.NewReader(data)
				switch pid {
				case protocol.IDPlayEntityEvent:
					if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusIronGolemAttack {
						attacked = true
					}
				case protocol.IDPlayEntityAnimation:
					if r.ReadVarInt() == id {
						swung = true
					}
				case protocol.IDPlaySetEntityMotion:
					if r.ReadVarInt() == playerID {
						_, vy, _ := r.ReadLpVec3()
						lifted = lifted || vy > 0.7
					}
				}
			}
			for range 25 {
				servertest.RunTicks(t, srv, 1)
				c.SettleAndCollect(collect)
				if srv.PlayerHealth(name) < 20 {
					break
				}
			}
			hurt := srv.PlayerHealth(name) < 20
			if attacked != tc.attacks || hurt != tc.attacks || lifted != tc.attacks || swung {
				t.Fatalf("attack/hurt/lift/arm swing = %v/%v/%v/%v, want %v/%v/%v/false", attacked, hurt, lifted, swung, tc.attacks, tc.attacks, tc.attacks)
			}
		})
	}
}

func TestAnIronGolemThrowsAHostileMobAfterHittingIt(t *testing.T) {
	t.Parallel()
	for _, resistance := range []float64{0, 0.5, 1} {
		t.Run(fmt.Sprint(resistance), func(t *testing.T) {
			srv, _ := villagerYard(t)
			golem := srv.SpawnMobSeeded("minecraft:iron_golem", 4.5, 64, 0.5, 2162)
			zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 0.5)
			srv.SetMobNoAI(zombie)
			srv.SetMobKnockbackResistance(zombie, resistance)
			for range 40 {
				servertest.RunTicks(t, srv, 1)
				if health, _ := srv.MobHealth(zombie); health < 20 {
					_, vy, _, ok := srv.MobVelocity(zombie)
					if !ok || resistance == 1 && vy > 0.01 || resistance < 1 && vy < 0.3*(1-resistance) {
						t.Fatalf("golem %d hit mob with resistance %v: vy = %v, present %v", golem, resistance, vy, ok)
					}
					return
				}
			}
			t.Fatal("golem never hit the zombie")
		})
	}
}
