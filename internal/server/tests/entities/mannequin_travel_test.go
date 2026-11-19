package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func mannequinTravelData(t *testing.T, c *servertest.Client, srv *server.Server) nbt.Compound {
	t.Helper()
	text := "{Pos:" + dataOfMannequin(t, c, srv, "Pos") + ",Health:" + dataOfMannequin(t, c, srv, "Health") + ",OnGround:" + dataOfMannequin(t, c, srv, "OnGround") + "}"
	tag, err := nbt.ParseSNBT(text)
	if err != nil {
		t.Fatalf("mannequin data: %v", err)
	}
	return tag
}

func mannequinTravelPosition(t *testing.T, tag nbt.Compound) [3]float64 {
	t.Helper()
	list, ok := nbt.Get[nbt.List](tag, "Pos")
	if !ok || len(list.Items) != 3 {
		t.Fatal("missing mannequin Pos")
	}
	return [3]float64{float64(list.Items[0].(nbt.Double)), float64(list.Items[1].(nbt.Double)), float64(list.Items[2].(nbt.Double))}
}

func TestAMannequinFallsAndTellsItsWatcher(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Falling")
	c.ChatCommand("summon mannequin 3.5 69 3.5")
	id := awaitMannequin(t, c, srv).id
	checkMannequinLanding(t, c, srv, id)
}

func checkMannequinLanding(t *testing.T, c *servertest.Client, srv *server.Server, id int32) {
	t.Helper()
	var moved, damaged, fallSound bool
	c.ReadTicks(srv, 40, func(packet int32, data []byte) bool {
		r := protocol.NewReader(data)
		if packet == protocol.IDPlayTeleportEntity && r.ReadVarInt() == id {
			r.ReadVarInt()
			r.ReadF64()
			moved = moved || r.ReadF64() == 64
		}
		if packet == protocol.IDPlayDamageEvent && r.ReadVarInt() == id {
			damaged = true
		}
		if packet == protocol.IDPlaySoundEffect {
			sound, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			fallSound = fallSound || sound == "minecraft:entity.generic.small_fall"
		}
		return moved && damaged && fallSound
	})
	tag := mannequinTravelData(t, c, srv)
	if pos := mannequinTravelPosition(t, tag); pos != [3]float64{3.5, 64, 3.5} {
		t.Errorf("landed position = %v, want [3.5 64 3.5]", pos)
	}
	if health, _ := nbt.FloatField(tag, "Health"); health != 18 {
		t.Errorf("health = %v, want 18 after a five-block fall", health)
	}
	if !moved || !damaged || !fallSound || !nbt.BoolField(tag, "OnGround") {
		t.Errorf("watcher saw landing=%v damage=%v fall sound=%v, OnGround=%v", moved, damaged, fallSound, nbt.BoolField(tag, "OnGround"))
	}
}

func TestAMannequinTakesAPunchKnockback(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "PunchTravel")
	id := summonPunchable(t, c, srv, "PunchTravel")
	start := mannequinTravelPosition(t, mannequinTravelData(t, c, srv))
	c.Attack(id)
	c.Settle()
	servertest.RunTicks(t, srv, 30)
	pos := mannequinTravelPosition(t, mannequinTravelData(t, c, srv))
	if dz := pos[2] - start[2]; dz < 1.9 || dz > 2.1 || pos[1] != 64 {
		t.Errorf("punch moved mannequin from %v to %v, want about two blocks along Z", start, pos)
	}
}
