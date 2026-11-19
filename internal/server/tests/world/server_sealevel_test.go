package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The sea level the Login (play) and Respawn packets carry: each dimension's
// own noise-settings level, which the client uses for fog and its horizon,
// rather than the overworld's constant for every world.

// loginTail is the tail of a Login (play) packet: its superflat flag, whether
// a death location follows and its sea level.
type loginTail struct {
	superflat bool
	hasDeath  bool
	seaLevel  int32
}

// readLoginTail parses a Login (play) packet down to its tail.
func readLoginTail(t *testing.T, data []byte) loginTail {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	for range r.ReadVarInt() {
		r.ReadString(32767) // dimension name
	}
	r.ReadVarInt()      // max players
	r.ReadVarInt()      // view distance
	r.ReadVarInt()      // simulation distance
	r.ReadBool()        // reduced debug screen
	r.ReadBool()        // respawn screen
	r.ReadBool()        // limited crafting
	r.ReadVarInt()      // dimension type
	r.ReadString(32767) // dimension name
	r.ReadI64()         // hashed seed
	r.ReadVarInt()      // game mode
	r.ReadVarInt()      // previous game mode
	r.ReadBool()        // debug world
	tail := loginTail{
		superflat: r.ReadBool(),
		hasDeath:  r.ReadBool(),
	}
	r.ReadVarInt() // portal cooldown
	tail.seaLevel = r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}
	return tail
}

// TestLoginSeaLevelFollowsTheDimension checks the Login (play) packet's sea
// level against the world's own noise settings: the nether's 32 and a noise
// overworld's 63.
func TestLoginSeaLevelFollowsTheDimension(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		levelType string
		dimension string
		want      int32
	}{
		{"the nether", "", world.NetherName, 32},
		// A level-type the harness does not fold into its flat test ground,
		// so the join describes a noise overworld rather than a flat one.
		{"a noise overworld", "minecraft:large_biomes", world.DimensionName, 63},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.LevelName = t.TempDir()
			if tc.levelType != "" {
				cfg.LevelType = tc.levelType
			}

			const name = "Seafarer"
			if tc.dimension != world.DimensionName {
				servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
					"Dimension": nbt.String(tc.dimension),
				})
			}

			_, connect := servertest.Start(t, cfg)
			c := connect()
			c.Login(name)
			c.Configure()
			if got := readLoginTail(t, c.Expect(protocol.IDPlayLogin)).seaLevel; got != tc.want {
				t.Errorf("login sea level in %s = %d, want %d", tc.name, got, tc.want)
			}
			c.Disconnect()
		})
	}
}
