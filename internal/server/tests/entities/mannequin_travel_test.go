package entities

import (
	"math"
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

func TestAMannequinFallsAfterLosingSupport(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "LostSupport")
	c.ChatCommand("setblock 3 68 3 stone")
	c.ChatCommand("summon mannequin 3.5 69 3.5")
	id := awaitMannequin(t, c, srv).id
	servertest.UntilTicks(t, srv, 3, "mannequin standing on support", func() bool {
		return nbt.BoolField(mannequinTravelData(t, c, srv), "OnGround")
	})
	c.ChatCommand("setblock 3 68 3 air")
	c.Settle()
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

func TestADamageSourceKnocksAMannequinBack(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Shoving")
	c.ChatCommand("summon mannequin 3.5 64 3.5")
	awaitMannequin(t, c, srv)
	c.ChatCommand("summon pig 1.5 64 3.5 {NoAI:1b}")
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	c.ChatCommand("damage @e[type=mannequin,limit=1] 1 minecraft:mob_attack by @e[type=pig,limit=1]")
	c.Settle()
	servertest.RunTicks(t, srv, 30)
	tag := mannequinTravelData(t, c, srv)
	pos := mannequinTravelPosition(t, tag)
	if pos[0] < 5 || pos[0] > 6 || math.Abs(pos[2]-3.5) > 1e-6 || pos[1] != 64 {
		t.Errorf("knocked position = %v, want about two blocks away from the pig on the floor", pos)
	}
	if health, _ := nbt.FloatField(tag, "Health"); health != 19 {
		t.Errorf("health = %v, want 19", health)
	}
}

func TestAnImmovableMannequinStaysPut(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Immovable")
	c.ChatCommand("summon mannequin 3.5 69 3.5 {immovable:1b,Motion:[0.2d,0.1d,0d]}")
	awaitMannequin(t, c, srv)
	c.ChatCommand("damage @e[type=mannequin,limit=1] 1 minecraft:mob_attack at 1.5 69 3.5")
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if pos := mannequinTravelPosition(t, mannequinTravelData(t, c, srv)); pos != [3]float64{3.5, 69, 3.5} {
		t.Errorf("immovable position = %v, want [3.5 69 3.5]", pos)
	}
}

func TestAStandStillFallsAndTravelsWithItsPush(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "StandTravel")
	c.ChatCommand("summon armor_stand 3.5 69 3.5 {Motion:[0.1d,0d,0d]}")
	id := c.ExpectArmorStandSpawn().EntityID
	servertest.RunTicks(t, srv, 40)
	stand := armorStandAt(t, srv, id)
	if stand.Y != 64 || stand.X < 4.3 || stand.X > 4.5 {
		t.Errorf("stand after fall and push: %+v", stand)
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

func TestAMannequinIsPushedByNearbyBodies(t *testing.T) {
	for _, tc := range []struct{ name, command string }{
		{"player", "tp @s 3.0 64 3.5"},
		{"mob", "summon pig 3.0 64 3.5 {NoAI:1b}"},
		{"mannequin", "summon mannequin 3.0 64 3.5 {immovable:1b}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "Crowded")
			c.ChatCommand("summon mannequin 3.5 64 3.5 {Tags:[target]}")
			awaitMannequin(t, c, srv)
			c.ChatCommand(tc.command)
			c.Settle()
			servertest.RunTicks(t, srv, 12)
			if !selects(c, srv, "@e[type=mannequin,tag=target,x=3.6,y=64,z=3,dx=3,dy=2,dz=1]", "pushed") {
				t.Fatal("the overlapping body did not push the mannequin along X")
			}
		})
	}
}

func TestAMannequinMeetsAWallAndFollowsWater(t *testing.T) {
	for _, tc := range []struct {
		name, setup, tag string
	}{
		{"wall", "fill 5 64 2 5 67 4 stone", "{Motion:[0.4d,0d,0d]}"},
		{"current", "setblock 2 64 3 water", "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "FlowTravel")
			c.ChatCommand(tc.setup)
			c.Settle()
			if tc.name == "current" {
				servertest.RunTicks(t, srv, 20)
			}
			c.ChatCommand("summon mannequin 3.5 64 3.5 " + tc.tag)
			awaitMannequin(t, c, srv)
			servertest.UntilTicks(t, srv, 40, tc.name+" moving mannequin", func() bool {
				pos := mannequinTravelPosition(t, mannequinTravelData(t, c, srv))
				if tc.name == "wall" {
					return math.Abs(pos[0]-4.7) <= 1e-6
				}
				return pos[0] > 3.6
			})
		})
	}
}

func TestAMannequinBelowTheWorldDies(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "VoidTravel")
	c.ChatCommand("summon mannequin 3.5 -130 3.5 {immovable:1b}")
	awaitMannequin(t, c, srv)
	servertest.UntilTicks(t, srv, 80, "mannequin removed below the world", func() bool {
		return !selects(c, srv, "@e[type=mannequin,limit=1]", "survived")
	})
}
