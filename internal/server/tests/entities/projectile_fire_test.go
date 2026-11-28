package entities

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// projectileFire summons the entity with the NBT at (5.5, 64, 5.5), runs two
// ticks and reads back its Fire and whether a metadata packet set the on-fire
// bit; gone is true when `data get` finds nothing.
func projectileFire(t *testing.T, id, nbtText string, lava bool) (fire int, flagged, gone bool) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	if lava {
		srv.SetBlock(5, 64, 5, registry.BlockLava.DefaultState())
	}
	c.ChatCommand(`summon minecraft:` + id + ` 5.5 64.2 5.5 ` + nbtText)
	// The summon's reply says the body exists before any tick is counted.
	c.SettleAndCollect(func(int32, []byte) {})
	servertest.RunTicks(t, srv, 2)
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		entries, _ := servertest.ReadMetadata(r)
		for _, e := range entries {
			if e.Index == protocol.MetadataIndexEntityFlags && e.ByteVal&protocol.EntityFlagOnFire != 0 {
				flagged = true
			}
		}
	})
	c.ChatCommand(`data get entity @e[type=minecraft:` + id + `,limit=1] Fire`)
	var reply protocol.Text
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlaySystemChat {
			reply, _ = protocol.ReadText(protocol.NewReader(data))
		}
	})
	if namesTranslation(reply, "commands.data.entity.query") {
		fire, _ = strconv.Atoi(strings.TrimSuffix(reply.With[1].Plain(), "s"))
		return fire, flagged, false
	}
	return 0, flagged, true
}

// vanilla: AbstractHurtingProjectile.tick, Entity.baseTick
func TestASmallFireballBurnsEveryTick(t *testing.T) {
	t.Parallel()
	fire, flagged, gone := projectileFire(t, "small_fireball", `{Motion:[0d,0d,0.1d]}`, false)
	if gone || fire != 20 || !flagged {
		t.Fatalf("Fire = %d (flag sent %v, gone %v), want 20 with the on-fire bit sent", fire, flagged, gone)
	}
}

// vanilla: LavaFluid.entityInside, Entity.lavaIgnite
func TestAProjectileInLavaIsSetAlight(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"ender_pearl", "wind_charge"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			fire, flagged, gone := projectileFire(t, id, `{NoGravity:1b}`, true)
			if gone || fire < 290 || fire > 300 {
				t.Fatalf("Fire = %d (gone %v), want about 299", fire, gone)
			}
			if id == "ender_pearl" && !flagged {
				t.Fatal("no metadata set the on-fire bit")
			}
		})
	}
}

// vanilla: LavaFluid.entityInside, ShulkerBullet.hurtServer
func TestAShulkerBulletInLavaIsDestroyed(t *testing.T) {
	t.Parallel()
	if _, _, gone := projectileFire(t, "shulker_bullet", `{NoGravity:1b}`, true); !gone {
		t.Fatal("shulker bullet survived lava")
	}
}

// vanilla: LavaFluid.entityInside
func TestAFallingBlockInLavaIsSetAlight(t *testing.T) {
	t.Parallel()
	fire, _, gone := projectileFire(t, "falling_block", `{BlockState:{Name:"minecraft:sand"},NoGravity:1b,Time:1}`, true)
	if gone || fire < 290 {
		t.Fatalf("Fire = %d (gone %v), want about 300", fire, gone)
	}
}
