package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The mob voice on the wire: the sound variant a farm animal's finalize
// chose decides its ambient, hurt and death events, a wound and a death
// speak at getSoundVolume and getVoicePitch, a sound reaches sixteen blocks
// and no farther, and the Silent tag holds a body's voice in.

// TestASilentMobMakesNoSoundAndKeepsItsTag plants a cow tagged Silent and
// holds the tag's two halves: the idle voice never plays however long the
// body lives, and the flag is written back to the entity file.
func TestASilentMobMakesNoSoundAndKeepsItsTag(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(6, 2, 6, 2)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:cow"),
		"UUID": servertest.UuidWords(id),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5),
		}},
		"Health": nbt.Float(10),
		"NoAI":   nbt.Byte(1),
		"Silent": nbt.Byte(1),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Quiet")
	c.Configure()
	c.JoinSeeingMobs("Quiet", id)
	srv.StopTick()

	for tick := 0; tick < servertest.AmbientTicks; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
			if packetID != protocol.IDPlaySoundEffect {
				return false
			}
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); strings.HasPrefix(name, "minecraft:entity.cow") {
				t.Fatalf("a Silent cow spoke %s", name)
			}
			return false
		})
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if silent, _ := nbt.Get[nbt.Byte](compound, "Silent"); silent != 1 {
			t.Errorf("the Silent cow saved Silent = %v, want 1", compound["Silent"])
		}
	})
}

// The Silent tag on the paths the ambient test does not take: a player's
// swing, an arrow, a thrown thing, the fire's bite and the burn it leaves, a
// landed fall and the death any of them brings. Each reaches the wire through
// a different wrapper — broadcastVoice for the blows, mobVoiceSend for the
// body's own hurts and death, the lava's own burn — so each gets a silent body
// of its own.

// TestAMobsSoundReachesOnlyWithinItsEarshot plants a NoAI cow and kills it
// with the loop stopped: the players just inside PlayerList.broadcast's
// sixteen blocks hear the death, the one just outside does not, and neither
// does a player standing at the same coordinates in another dimension.
func TestAMobsSoundReachesOnlyWithinItsEarshot(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Inside"))

	inside := connect().Join("Inside")
	outside := connect().Join("Outside")
	away := connect()
	away.JoinAs("Away")
	srv.MovePlayerToDimension("Away", world.NetherName, 0, 64, 0)

	// The cow stands at x=0.5: the inside player 15.75 blocks off hears, the
	// edge one exactly sixteen off does not, the strict line broadcast reads.
	inside.MoveTo(16.25, 64, 0.5, 0, 0)
	outside.MoveTo(16.5, 64, 0.5, 0, 0)
	inside.Settle()
	outside.Settle()
	away.Settle()

	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 0.5)
	inside.ExpectMobSpawn(protocol.EntityTypeCow)

	near, far, other := []string{}, []string{}, []string{}
	inside.ChatCommand("kill @e[type=cow]")
	// Death sounds arrive during the command, before its settle barrier.
	inside.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				near = append(near, name)
			}
		}
	})
	outside.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				far = append(far, name)
			}
		}
	})
	away.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.cow") {
				other = append(other, name)
			}
		}
	})

	if len(near) == 0 {
		t.Errorf("the cow at entity %d spoke nothing to the player 15.75 blocks off, want the death within earshot", cowID)
	}
	if len(far) > 0 {
		t.Errorf("the cow's voice reached the player exactly sixteen blocks off: %v", far)
	}
	if len(other) > 0 {
		t.Errorf("the cow's voice reached a player in another dimension: %v", other)
	}
}
