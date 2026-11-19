package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// expectPlayerRotation reads until a Player Rotation arrives or the timeout
// runs out, and returns its yaw, pitch and the two relative flags.
func expectPlayerRotation(t *testing.T, c *servertest.Client) (yaw, pitch float32, relYaw, relPitch bool) {
	t.Helper()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayPlayerRotation)
	if !ok {
		t.Fatal("no player rotation arrived")
	}
	r := protocol.NewReader(data)
	y, relY, p, relP := r.ReadF32(), r.ReadBool(), r.ReadF32(), r.ReadBool()
	return y, p, relY, relP
}

// TestRotateTurnsTheRunner checks the plain form of /rotate: the player's
// client is told the new angles outright, and the feedback names the target
// as vanilla words it.
func TestRotateTurnsTheRunner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// /rotate is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()

	c.ChatCommand("rotate @s 90 0")
	servertest.AwaitCommandFeedbackText(t, c, "commands.rotate.success", servertest.PlayerDisplayComponent("Testificate"))
	if yaw, pitch, relYaw, relPitch := expectPlayerRotation(t, c); yaw != 90 || pitch != 0 || relYaw || relPitch {
		t.Errorf("rotation = %v %v rel=%v/%v, want 90 0 absolute", yaw, pitch, relYaw, relPitch)
	}
	c.Settle()

	// Relative angles turn from where the player is now.
	c.ChatCommand("rotate @s ~-90 ~10")
	if yaw, pitch, _, _ := expectPlayerRotation(t, c); yaw != 0 || pitch != 10 {
		t.Errorf("relative rotation = %v %v, want 0 10", yaw, pitch)
	}
	c.Settle()

	// The pitch clamps where the yaw wraps.
	c.ChatCommand("rotate @s 0 200")
	if _, pitch, _, _ := expectPlayerRotation(t, c); pitch != 90 {
		t.Errorf("pitch 200 = %v, want 90", pitch)
	}
	c.Disconnect()
}

// rotateZombieUUID is the identity TestRotateFacingAnchors summons its zombie
// under.
var rotateZombieUUID = uuid.UUID{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4}

// TestRotateMobTellsEveryWatcher checks that each player watching a rotated mob
// is sent both the Entity Rotation and the Head Rotation.
func TestRotateMobTellsEveryWatcher(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	a, b := connect(), connect()
	for i, c := range []*servertest.Client{a, b} {
		c.Login([]string{"Testificate", "Watcher"}[i])
		c.Configure()
		c.SpawnedAt()
		c.SkipJoin()
	}

	a.ChatCommand("summon minecraft:zombie 3.5 64 0.5 {NoAI:1b,UUID:[I;1,2,3,4]}")
	a.Settle()
	b.Settle()
	a.ChatCommand("rotate @e[type=minecraft:zombie,limit=1] 90 0")
	servertest.AwaitCommandFeedbackText(t, a, "commands.rotate.success", servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", rotateZombieUUID))

	// 90 degrees is 64 in the byte angle the packets carry.
	const wantAngle = 64
	for name, c := range map[string]*servertest.Client{"first": a, "second": b} {
		rotYaw, headYaw := -1, -1
		c.SettleAndCollect(func(id int32, data []byte) {
			r := protocol.NewReader(data)
			switch id {
			case protocol.IDPlayEntityRotation:
				r.ReadVarInt()
				r.ReadBool() // onGround
				rotYaw = int(r.ReadU8())
			case protocol.IDPlayHeadRotation:
				r.ReadVarInt()
				headYaw = int(r.ReadU8())
			}
		})
		if rotYaw != wantAngle || headYaw != wantAngle {
			t.Errorf("%s watcher got entity yaw %d, head yaw %d, want both %d", name, rotYaw, headYaw, wantAngle)
		}
	}
}
