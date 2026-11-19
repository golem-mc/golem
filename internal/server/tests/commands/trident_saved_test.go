package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: ThrownTrident.addAdditionalSaveData, readAdditionalSaveData
func TestTridentDealtDamageSave(t *testing.T) {
	t.Parallel()
	// Recorded vanilla order for the trident fixture in nonlivingCompoundHistory.
	// DealtDamage is its 25th field, growing the root HashMap to 64 buckets.
	const keys = "NoGravity damage Owner shake Invulnerable PortalCooldown life SoundEvent UUID Tags Motion PierceLevel item inGround pickup fall_distance Air OnGround Rotation inBlockState crit Pos HasBeenShot Fire DealtDamage"
	const selector = "@e[type=minecraft:trident,limit=1]"
	for _, tc := range []struct {
		name, extra string
		want        nbt.Byte
	}{
		{"fresh", "", 0},
		{"spent", "DealtDamage:1b,", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.LevelName = t.TempDir()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon trident 4.5 70 4.5 {` + tc.extra + `NoGravity:1b,Owner:[I;1,2,3,4],HasBeenShot:1b,inBlockState:{Name:"minecraft:stone"},Tags:["order"]}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
			c.Settle()
			check := func(c *servertest.Client, wantKeys string) {
				t.Helper()
				for i, text := range expectNonlivingHistory(t, c, "trident", wantKeys) {
					tag, err := nbt.ParseSNBT(text)
					if err != nil {
						t.Fatalf("query %d: parse compound: %v", i, err)
					}
					if got := tag["DealtDamage"]; got != tc.want {
						t.Errorf("query %d: DealtDamage = %v, want %db", i, got, tc.want)
					}
				}
			}
			check(c, keys)
			// Make the projectile's owner check stable across its wake-up tick.
			c.ChatCommand("data merge entity " + selector + " {LeftOwner:1b}")
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
			const savedKeys = "NoGravity damage Owner shake Invulnerable PortalCooldown life SoundEvent UUID Tags Motion PierceLevel item inGround pickup LeftOwner fall_distance Air OnGround Rotation inBlockState crit Pos HasBeenShot Fire DealtDamage"
			check(c, savedKeys)
			srv.FlushMobs()
			c.Disconnect()
			srv.Shutdown()
			servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:trident", func(tag nbt.Compound) {
				if got := tag["DealtDamage"]; got != tc.want {
					t.Errorf("saved DealtDamage = %v, want %db", got, tc.want)
				}
			})
			again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
			servertest.FreezeUntilHeld(t, again)
			client := connect().Join("Alice")
			servertest.HoldTicks(t, again)
			servertest.UntilTicks(t, again, 20, "saved trident wakes", func() bool {
				return len(again.SelectedEntities("minecraft:trident")) == 1
			})
			client.Settle()
			check(client, savedKeys)
		})
	}
}
