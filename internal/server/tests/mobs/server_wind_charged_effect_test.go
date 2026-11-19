package mobs

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func windChargedStart(t *testing.T, name string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()
	return srv, c, id
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
