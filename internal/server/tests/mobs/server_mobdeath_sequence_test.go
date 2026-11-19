package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestADyingMobIsToldDyingWithZeroHealth kills a zombie with /kill and holds
// the death sequence vanilla gives its watchers: the death sound, then the
// death entity event, and metadata that sets the pose to dying and the health
// to zero.
// vanilla: LivingEntity.hurtServer, LivingEntity.die
func TestADyingMobIsToldDyingWithZeroHealth(t *testing.T) {
	t.Parallel()
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()

	id := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("kill @e[type=minecraft:zombie]")

	var order []string
	pose, health := int32(-1), float32(-1)
	record := func(pid int32, data []byte) {
		switch pid {
		case protocol.IDPlaySoundEffect:
			order = append(order, "sound")
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusDeath {
				order = append(order, "event")
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				switch fieldType := r.ReadVarInt(); {
				case index == protocol.MetadataIndexPose && fieldType == protocol.MetadataPose:
					pose = r.ReadVarInt()
				case index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat:
					health = r.ReadF32()
				default:
					return
				}
			}
		}
	}
	c.SettleAndCollect(record)

	sound, event := -1, -1
	for i, kind := range order {
		if kind == "sound" && sound < 0 {
			sound = i
		}
		if kind == "event" && event < 0 {
			event = i
		}
	}
	if event < 0 {
		t.Fatal("the killed zombie's watchers were sent no death event")
	}
	if sound < 0 || sound > event {
		t.Errorf("packet order = %v, want the death sound before the death event", order)
	}
	if pose != protocol.PoseDying {
		t.Errorf("dying pose = %d, want %d", pose, protocol.PoseDying)
	}
	if health != 0 {
		t.Errorf("dying health = %v, want 0", health)
	}
}
