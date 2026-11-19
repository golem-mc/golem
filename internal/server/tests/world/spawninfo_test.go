package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// spawnInfo is the common spawn info Login (play) and Respawn share, from the
// hashed seed through the death location.
type spawnInfo struct {
	seed                   int64
	gameMode               uint8
	previous               int8
	hasDeath               bool
	deathDim               string
	deathX, deathY, deathZ int32
}

// readSpawnInfo reads the fields after the dimension name, whose offset the
// caller has already skipped past.
func readSpawnInfo(t *testing.T, r *protocol.Reader) spawnInfo {
	t.Helper()
	var s spawnInfo
	s.seed = r.ReadI64()
	s.gameMode = uint8(r.ReadVarInt())
	s.previous = int8(r.ReadVarInt() - 1) // optional var int: 0 is absent
	r.ReadBool()                          // debug world
	r.ReadBool()                          // superflat world
	if s.hasDeath = r.ReadBool(); s.hasDeath {
		s.deathDim = r.ReadString(32767)
		s.deathX, s.deathY, s.deathZ = r.ReadPosition()
	}
	if err := r.Err(); err != nil {
		t.Fatalf("spawn info: %v", err)
	}
	return s
}

func loginSpawnInfo(t *testing.T, data []byte) spawnInfo {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	for range r.ReadVarInt() {
		r.ReadString(32767)
	}
	r.ReadVarInt() // max players
	r.ReadVarInt() // view distance
	r.ReadVarInt() // simulation distance
	r.ReadBool()
	r.ReadBool()
	r.ReadBool()
	r.ReadVarInt()      // dimension type
	r.ReadString(32767) // dimension name
	return readSpawnInfo(t, r)
}

func respawnSpawnInfo(t *testing.T, data []byte) spawnInfo {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt()      // dimension type
	r.ReadString(32767) // dimension name
	return readSpawnInfo(t, r)
}

// TestLoginSendsTheObfuscatedSeed checks the hashed seed against a value
// computed from vanilla's sha256-based BiomeManager.obfuscateSeed for seed 12345.
func TestLoginSendsTheObfuscatedSeed(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelSeed = "12345"
	_, c := servertest.Dial(t, cfg)
	c.Login("Hasher")
	c.Configure()
	got := loginSpawnInfo(t, c.Expect(protocol.IDPlayLogin))
	if want := int64(293737985876514017); got.seed != want {
		t.Errorf("login hashed seed = %d, want %d", got.seed, want)
	}
	if got.previous != -1 || got.hasDeath {
		t.Errorf("fresh login previous = %d, death = %v; want -1 and none", got.previous, got.hasDeath)
	}
}

// TestPreviousGameModeAndDeathLocationReachRespawnAndTheFile checks that a
// mode switch sets the previous game mode, a death sets the last death
// location, both ride the Respawn packet and the player file, and a rejoin
// sends them in Login (play).
func TestPreviousGameModeAndDeathLocationReachRespawnAndTheFile(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mortal"))

	c := connect()
	c.Login("Mortal")
	c.Configure()
	var entityID int32
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin && entityID == 0 {
			entityID = protocol.NewReader(data).ReadI32()
		}
	})
	c.MarkLoaded()

	c.ChatCommand("gamemode survival")
	c.ChatCommand("gamemode creative")
	c.ChatCommand("gamemode survival")
	c.Settle()
	c.ChatCommand("tp @s 5.5 70 -3.5")
	c.Settle()
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Mortal")},
	})
	c.PerformRespawn()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn arrived")
	}
	got := respawnSpawnInfo(t, data)
	if got.previous != 1 {
		t.Errorf("respawn previous game mode = %d, want 1 (creative)", got.previous)
	}
	if !got.hasDeath || got.deathDim != "minecraft:overworld" ||
		got.deathX != 5 || got.deathY != 70 || got.deathZ != -4 {
		t.Errorf("respawn death location = %+v, want minecraft:overworld (5, 70, -4)", got)
	}
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Mortal"))
	if prev, ok := nbt.Get[nbt.Int](root, "previousPlayerGameType"); !ok || prev != 1 {
		t.Errorf("previousPlayerGameType = %v, %v; want 1", prev, ok)
	}
	loc, ok := nbt.Get[nbt.Compound](root, "LastDeathLocation")
	if !ok {
		t.Fatal("no LastDeathLocation in the player file")
	}
	if dim, _ := nbt.Get[nbt.String](loc, "dimension"); dim != "minecraft:overworld" {
		t.Errorf("LastDeathLocation dimension = %q", dim)
	}
	if pos, _ := nbt.Get[nbt.IntArray](loc, "pos"); len(pos) != 3 || pos[0] != 5 || pos[1] != 70 || pos[2] != -4 {
		t.Errorf("LastDeathLocation pos = %v, want [5 70 -4]", pos)
	}

	c = connect()
	c.Login("Mortal")
	c.Configure()
	again := loginSpawnInfo(t, c.Expect(protocol.IDPlayLogin))
	if again.previous != 1 || !again.hasDeath || again.deathY != 70 {
		t.Errorf("rejoin login = %+v, want previous 1 and the saved death", again)
	}
	c.Disconnect()
}
