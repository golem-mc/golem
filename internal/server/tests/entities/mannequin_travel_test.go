package entities

import (
	"fmt"
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
		if packet == protocol.IDPlayTeleportEntity {
			if sync := servertest.ParseEntityPositionSync(data); sync.ID == id {
				moved = moved || sync.Y == 64
			}
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

// mannequinMotionPacket is a Set Entity Motion of id, decoded.
func mannequinMotionPacket(data []byte, id int32) (v [3]float64, ok bool) {
	r := protocol.NewReader(data)
	if r.ReadVarInt() != id {
		return v, false
	}
	v[0], v[1], v[2] = r.ReadLpVec3()
	return v, true
}

// An absolute /tp stops the drift along every axis and the fall, so the
// watcher is told zero motion on the next movement gate, ahead of the teleport
// sync, and the straight fall again after it.
func TestACommandTeleportResetsAFallingMannequinMotionForItsWatcher(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Stopping")
	c.ChatCommand("summon mannequin 3.5 90 3.5 {Motion:[0.4d,0.0d,0.4d]}")
	id := awaitMannequin(t, c, srv).id
	if !c.ReadTicks(srv, 30, func(packet int32, data []byte) bool {
		v, ok := mannequinMotionPacket(data, id)
		return packet == protocol.IDPlaySetEntityMotion && ok && v[0] > 0 && v[1] < 0 && v[2] > 0
	}) {
		t.Fatal("no drifting downward Set Entity Motion while the mannequin fell")
	}
	// The match was a movement gate (every second tick) and the command runs in
	// the next tick's start, so one idle tick puts it on the following gate.
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	c.ChatCommand("tp @e[type=mannequin,limit=1] 3.5 120 3.5")
	c.Settle()
	// One tick at a time: the stop and the sync on the command's gate, nothing
	// on the tick between, the straight fall on the next gate with a sync, as
	// the ground flag clears (one step of 0.0784 down).
	tick := func() string {
		var events []string
		c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
			switch packet {
			case protocol.IDPlaySetEntityMotion:
				if v, ok := mannequinMotionPacket(data, id); ok {
					switch {
					case v == [3]float64{}:
						events = append(events, "stop")
					case v[0] == 0 && v[1] < 0 && v[2] == 0:
						events = append(events, "fall")
					default:
						events = append(events, fmt.Sprintf("motion%v", v))
					}
				}
			case protocol.IDPlayTeleportEntity:
				if sync := servertest.ParseEntityPositionSync(data); sync.ID == id {
					if sync.X == 3.5 && sync.Y == 120 && sync.Z == 3.5 {
						events = append(events, "sync")
					} else {
						events = append(events, fmt.Sprintf("sync(%v %v %v)", sync.X, sync.Y, sync.Z))
					}
				}
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
				if protocol.NewReader(data).ReadVarInt() == id {
					events = append(events, "move")
				}
			}
			return false
		})
		return fmt.Sprint(events)
	}
	for i, want := range []string{"[stop sync]", "[]", "[fall sync(3.5 119.9216 3.5)]"} {
		if got := tick(); got != want {
			t.Fatalf("tick %d after the teleport: watcher saw %s, want %s", i, got, want)
		}
	}
}

// The first step after a /tp keeps the command's ground flag for the sync but
// is no landing, so a fall from height does no damage in midair.
func TestACommandTeleportDoesNotLandAFallingMannequinInMidair(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Midair")
	c.ChatCommand("summon mannequin 3.5 90 3.5")
	awaitMannequin(t, c, srv)
	servertest.RunTicks(t, srv, 14)
	c.ChatCommand("tp @e[type=mannequin,limit=1] 3.5 120 3.5")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if health, _ := nbt.FloatField(mannequinTravelData(t, c, srv), "Health"); health != 20 {
		t.Errorf("health = %v after the first step of a teleported fall, want 20", health)
	}
	if nbt.BoolField(mannequinTravelData(t, c, srv), "OnGround") {
		t.Error("OnGround is set after the first step of a teleported fall, want clear")
	}
}

// A mannequin summoned with motion is spawned with it, and the watcher is told
// the stop when physics brings it to rest.
func TestAMannequinSummonedWithMotionTellsItsWatcherTheStop(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Drifting")
	c.ChatCommand("summon mannequin 3.5 70 3.5 {NoGravity:1b,Motion:[0.0d,-0.002d,0.0d]}")
	var spawn mannequinArrival
	var stopped bool
	c.ReadTicks(srv, 8, func(packet int32, data []byte) bool {
		switch packet {
		case protocol.IDPlaySpawnEntity:
			if arrival, ok := mannequinSpawnOf(data); ok {
				spawn = arrival
			}
		case protocol.IDPlaySetEntityMotion:
			if v, ok := mannequinMotionPacket(data, spawn.id); ok && spawn.id != 0 && v == [3]float64{} {
				stopped = true
			}
		}
		return stopped
	})
	if spawn.id == 0 || spawn.vy > -0.001 || spawn.vy < -0.003 {
		t.Errorf("spawned mannequin %d with vy %v, want about -0.002", spawn.id, spawn.vy)
	}
	if !stopped {
		t.Error("watcher got no stop after the mannequin came to rest")
	}
}

// A watcher who comes into range on the movement gate that samples the stop,
// or after it, is spawned with the rest velocity, not the one the mannequin
// last moved with, and is not told the stop again.
func TestAMannequinSpawnsWithItsCurrentMotionForALateWatcher(t *testing.T) {
	t.Parallel()
	// Two setup ticks pair the watcher on the stop's gate, three after it.
	for _, setup := range []int{2, 3} {
		t.Run(fmt.Sprint(setup), func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, fmt.Sprint("Unwatched", setup))
			// Diagonally beyond the 32-block tracking range, inside the joined chunk view.
			c.ChatCommand("summon mannequin 30.5 70 30.5 {NoGravity:1b,Motion:[0.0d,-0.002d,0.0d]}")
			c.ExpectNoneWithinTicks(srv, setup, func(packet int32, data []byte) bool {
				_, ok := mannequinSpawnOf(data)
				return packet == protocol.IDPlaySpawnEntity && ok
			})
			c.ChatCommand("tp @s 15.5 70 15.5")
			var spawn mannequinArrival
			var spawned, retold bool
			c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
				switch packet {
				case protocol.IDPlaySpawnEntity:
					if arrival, ok := mannequinSpawnOf(data); ok {
						spawn, spawned = arrival, true
					}
				case protocol.IDPlaySetEntityMotion:
					_, ok := mannequinMotionPacket(data, spawn.id)
					retold = retold || spawned && ok
				}
				return false
			})
			if !spawned || spawn.vx != 0 || spawn.vy != 0 || spawn.vz != 0 || retold {
				t.Errorf("late watcher saw spawn=%v with motion (%v %v %v), motion again=%v; want a spawn at rest and no motion packet",
					spawned, spawn.vx, spawn.vy, spawn.vz, retold)
			}
		})
	}
}
