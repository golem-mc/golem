package mobs

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// gustBurst is an Explosion packet's header and the particle and sound that
// follow its knockback.
type gustBurst struct {
	x, y, z  float64
	radius   float32
	pushed   bool
	particle int32
	sound    int32
}

func readGustBurst(data []byte) gustBurst {
	r := protocol.NewReader(data)
	g := gustBurst{x: r.ReadF64(), y: r.ReadF64(), z: r.ReadF64(), radius: r.ReadF32()}
	r.ReadI32()
	if g.pushed = r.ReadBool(); g.pushed {
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
	}
	g.particle, g.sound = r.ReadVarInt(), r.ReadVarInt()
	return g
}

// collectBursts steps the mob pass n ticks and returns the Explosion packets
// the client was sent.
func collectBursts(srv *server.Server, c *servertest.Client, n int) []gustBurst {
	var out []gustBurst
	for range n {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayExplosion {
				out = append(out, readGustBurst(data))
			}
		})
	}
	return out
}

func windChargedStart(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()
	return srv, c, id
}

// TestAWindChargedBodyBurstsWhenItsCorpseLeaves kills a wind_charged zombie
// and holds the gust at the corpse's last tick: radius 3 to 5, centred at
// half the hull's height, the large emitter and the breeze's sound, pushing
// the player beside it.
// vanilla: WindChargedMobEffect.onMobRemoved
func TestAWindChargedBodyBurstsWhenItsCorpseLeaves(t *testing.T) {
	t.Parallel()
	srv, c, _ := windChargedStart(t, "Gusty")
	c.ChatCommand("summon minecraft:zombie 5.5 64 5.5 {NoAI:1b}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.Give("effect give @e[type=minecraft:zombie] minecraft:wind_charged 60", spawn.EntityID)
	c.ChatCommand("kill @e[type=minecraft:zombie]")
	c.Settle()
	for range 19 {
		if bursts := collectBursts(srv, c, 1); len(bursts) != 0 {
			t.Fatalf("a gust came while the corpse still lay: %+v", bursts)
		}
	}
	bursts := collectBursts(srv, c, 3)
	if len(bursts) != 1 {
		t.Fatalf("%d explosions around the corpse's end, want one gust", len(bursts))
	}
	g := bursts[0]
	if g.radius < 3 || g.radius >= 5 {
		t.Errorf("gust radius = %v, want 3 up to 5", g.radius)
	}
	if g.x != 5.5 || g.z != 5.5 || g.y < 64.9 || g.y > 65.0 {
		t.Errorf("gust at %v %v %v, want the hull's middle above (5.5, 64, 5.5)", g.x, g.y, g.z)
	}
	if g.particle != protocol.ParticleGustEmitterLarge || g.sound != protocol.SoundEventBreezeWindBurst {
		t.Errorf("gust particle %d sound %d, want the large emitter and the breeze burst", g.particle, g.sound)
	}
	if !g.pushed {
		t.Error("the gust did not push the player three blocks away")
	}
}

// TestAWindChargedCreeperBurstsAfterItsBlast blows up a wind_charged creeper:
// the explosion is a KILLED removal, so a gust follows its own blast.
// vanilla: Creeper.explodeCreeper, WindChargedMobEffect.onMobRemoved
func TestAWindChargedCreeperBurstsAfterItsBlast(t *testing.T) {
	t.Parallel()
	srv, c, _ := windChargedStart(t, "Gusty")
	c.ChatCommand("summon minecraft:creeper 5.5 64 5.5 {NoAI:1b,ignited:1b,Fuse:2}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	c.Give("effect give @e[type=minecraft:creeper] minecraft:wind_charged 60", spawn.EntityID)
	var all []gustBurst
	for range 6 {
		all = append(all, collectBursts(srv, c, 5)...)
	}
	gust := -1
	for i, g := range all {
		if g.sound != protocol.SoundEventBreezeWindBurst {
			continue
		}
		if gust >= 0 {
			t.Fatalf("more than one gust: %+v", all)
		}
		gust = i
	}
	if gust < 1 {
		t.Fatalf("gust at index %d of %+v, want one after the creeper's blast", gust, all)
	}
	g := all[gust]
	if g.particle != protocol.ParticleGustEmitterLarge || g.radius < 3 || g.radius >= 5 || g.y < 64.8 || g.y > 65.0 {
		t.Errorf("gust %+v, want the large emitter, radius 3 up to 5, y in 64.8..65", g)
	}
}

// TestAWindChargedPlayerBurstsOnRespawn kills a wind_charged player and
// holds the gust at the death spot when the respawn removes the body.
// vanilla: WindChargedMobEffect.onMobRemoved
func TestAWindChargedPlayerBurstsOnRespawn(t *testing.T) {
	t.Parallel()
	const name = "Gusty"
	_, c, id := windChargedStart(t, name)
	c.Give("effect give "+name+" minecraft:wind_charged 60", id)
	c.ChatCommand("kill")
	servertest.ExpectDeathSequence(t, c, id, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	c.PerformRespawn()
	var bursts []gustBurst
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayExplosion {
			bursts = append(bursts, readGustBurst(data))
		}
	})
	if len(bursts) != 1 {
		t.Fatalf("%d explosions on the respawn, want one gust", len(bursts))
	}
	g := bursts[0]
	if g.x != 2.5 || g.z != 5.5 || g.y < 64.8 || g.y > 65.0 {
		t.Errorf("gust at %v %v %v, want the player's middle above (2.5, 64, 5.5)", g.x, g.y, g.z)
	}
	if g.particle != protocol.ParticleGustEmitterLarge || g.sound != protocol.SoundEventBreezeWindBurst {
		t.Errorf("gust particle %d sound %d, want the large emitter and the breeze burst", g.particle, g.sound)
	}
}

// creeperBlastCloud blows up a creeper (with speed when effects is set) and
// returns what the client was told of area effect clouds in the next ticks.
func creeperBlastCloud(t *testing.T, effects bool) (srv *server.Server, c *servertest.Client, spawns []uuid.UUID, at [3]float64) {
	t.Helper()
	srv, c, _ = windChargedStart(t, "Cloudy")
	c.ChatCommand("summon minecraft:creeper 5.5 64 5.5 {NoAI:1b,ignited:1b,Fuse:2}")
	spawn := c.ExpectMobSpawn(protocol.EntityTypeCreeper)
	if effects {
		c.Give("effect give @e[type=minecraft:creeper] minecraft:speed 60", spawn.EntityID)
		// A stronger, shorter speed leaves the first as the hidden effect.
		c.Give("effect give @e[type=minecraft:creeper] minecraft:speed 20 1", spawn.EntityID)
	}
	for range 6 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySpawnEntity {
				return
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			identity := r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeAreaEffectCloud {
				spawns = append(spawns, identity)
				at = [3]float64{r.ReadF64(), r.ReadF64(), r.ReadF64()}
			}
		})
	}
	return srv, c, spawns, at
}

// flatText is a component's text with its extras, in order.
func flatText(c protocol.Text) string {
	out := c.Text
	for _, e := range c.Extra {
		out += flatText(e)
	}
	return out
}

// TestACreeperWithEffectsLeavesACloudAfterItsBlast blows up a creeper under
// speed: a 2.5-radius area effect cloud stands where it was.
// vanilla: Creeper.spawnLingeringCloud
func TestACreeperWithEffectsLeavesACloudAfterItsBlast(t *testing.T) {
	t.Parallel()
	srv, c, spawns, at := creeperBlastCloud(t, true)
	if len(spawns) != 1 || srv.CloudCount() != 1 {
		t.Fatalf("%d cloud spawns, %d clouds, want one of each", len(spawns), srv.CloudCount())
	}
	if at != [3]float64{5.5, 64, 5.5} {
		t.Errorf("cloud at %v, want the creeper's (5.5, 64, 5.5)", at)
	}
	if _, radius, ok := srv.CloudState(spawns[0]); !ok || radius > 2.5 || radius < 2.4 {
		t.Errorf("cloud radius %v (found %v), want 2.5 less a few ticks' shrink", radius, ok)
	}
	c.ChatCommand("data get entity " + spawns[0].String())
	var raw string
	if !c.ReadTicks(srv, 200, func(id int32, body []byte) bool {
		text, ok := protocol.ReadText(protocol.NewReader(body))
		if id != protocol.IDPlaySystemChat || !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
			return false
		}
		raw = flatText(text.With[1])
		return true
	}) {
		t.Fatal("no /data get entity feedback for the cloud arrived")
	}
	data, err := nbt.ParseSNBT(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if scale, _ := nbt.FloatField(data, "potion_duration_scale"); scale != 0.25 {
		t.Errorf("potion_duration_scale %v, want 0.25", scale)
	}
	contents, ok := data["potion_contents"].(nbt.Compound)
	if !ok {
		t.Fatalf("potion_contents %v, want a compound", data["potion_contents"])
	}
	if _, has := contents["potion"]; has {
		t.Errorf("potion_contents %v has a potion, want custom effects only", contents)
	}
	custom, _ := contents["custom_effects"].(nbt.List)
	if len(custom.Items) != 1 {
		t.Fatalf("custom_effects %v, want speed alone", custom)
	}
	speed, _ := custom.Items[0].(nbt.Compound)
	if amp, _ := nbt.IntField(speed, "amplifier"); speed["id"] != nbt.String("minecraft:speed") || amp != 1 {
		t.Errorf("effect %v, want speed II", speed)
	}
	// The cloud saves through PotionContents.CODEC, which drops hidden effects.
	if _, has := speed["hidden_effect"]; has {
		t.Errorf("effect %v, want no hidden_effect", speed)
	}
}

// TestACreeperWithoutEffectsLeavesNoCloud blows up a bare creeper.
// vanilla: Creeper.spawnLingeringCloud
func TestACreeperWithoutEffectsLeavesNoCloud(t *testing.T) {
	t.Parallel()
	srv, _, spawns, _ := creeperBlastCloud(t, false)
	if len(spawns) != 0 || srv.CloudCount() != 0 {
		t.Fatalf("%d cloud spawns, %d clouds, want none", len(spawns), srv.CloudCount())
	}
}
