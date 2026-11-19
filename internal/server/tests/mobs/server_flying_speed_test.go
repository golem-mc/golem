package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFlyingSpeedBases reads flying_speed through /attribute for the kinds
// whose createAttributes add it, and a fox's pairing movement_speed, which
// vanilla writes as the float literal 0.3f.
// vanilla: Bee, Allay, WitherBoss and Fox.createAttributes
func TestFlyingSpeedBases(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand("summon minecraft:fox 1.5 64 0.5 {NoAI:1b}")
	if fox := c.ExpectMobSpawn(protocol.EntityTypeFox); fox.SpeedAttribute != float64(float32(0.3)) {
		t.Errorf("fox pairing movement_speed = %v, want %v", fox.SpeedAttribute, float64(float32(0.3)))
	}

	for _, k := range []struct {
		name   string
		typeID int32
		want   float64
	}{
		{"bee", protocol.EntityTypeBee, float64(float32(0.6))},
		{"allay", protocol.EntityTypeAllay, float64(float32(0.1))},
		{"wither", protocol.EntityTypeWither, float64(float32(0.6))},
	} {
		c.ChatCommand("summon minecraft:" + k.name + " -2.5 64 0.5 {NoAI:1b}")
		c.ExpectMobSpawn(k.typeID)
		c.ChatCommand("attribute @e[type=minecraft:" + k.name + ",limit=1] minecraft:flying_speed get")
		value := ""
		c.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID != protocol.IDPlaySystemChat {
				return
			}
			if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
				value = text.With[len(text.With)-1].Text
			}
		})
		// The command prints the value as a double; a float32 0.6 reads back
		// as 0.6000000238418579.
		if v, err := strconv.ParseFloat(value, 64); err != nil || v != k.want {
			t.Errorf("%s flying_speed = %q, want %v", k.name, value, k.want)
		}
	}
}
