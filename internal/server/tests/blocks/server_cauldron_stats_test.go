package blocks

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCauldronUsesCountTheirStatistic: each cauldron interaction adds one to
// the statistic vanilla names for it and to no other.
// vanilla: CauldronInteractions.bootStrap, emptyBucket, fillBucket
func TestCauldronUsesCountTheirStatistic(t *testing.T) {
	t.Parallel()
	banner := servertest.ItemStack(t, "minecraft:white_banner", 1)
	banner.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: 0, Dye: 1}})
	helmet := servertest.ItemStack(t, "minecraft:leather_helmet", 1)
	helmet.SetDyedColor(0x3b5dc6)

	stats := []string{
		"minecraft:fill_cauldron", "minecraft:use_cauldron", "minecraft:clean_armor",
		"minecraft:clean_banner", "minecraft:clean_shulker_box",
	}
	for _, tc := range []struct {
		name  string
		block string
		held  protocol.ItemStack
		want  string
	}{
		{"water bucket fills", "minecraft:cauldron", servertest.ItemStack(t, "minecraft:water_bucket", 1), "minecraft:fill_cauldron"},
		{"lava bucket fills", "minecraft:cauldron", servertest.ItemStack(t, "minecraft:lava_bucket", 1), "minecraft:fill_cauldron"},
		{"bucket takes water", "minecraft:water_cauldron[level=3]", servertest.ItemStack(t, "minecraft:bucket", 1), "minecraft:use_cauldron"},
		{"bottle takes water", "minecraft:water_cauldron[level=2]", servertest.ItemStack(t, "minecraft:glass_bottle", 1), "minecraft:use_cauldron"},
		{"water bottle pours", "minecraft:cauldron", servertest.WaterBottle(t), "minecraft:use_cauldron"},
		{"armour washes", "minecraft:water_cauldron[level=2]", helmet, "minecraft:clean_armor"},
		{"banner washes", "minecraft:water_cauldron[level=2]", banner, "minecraft:clean_banner"},
		{"shulker box washes", "minecraft:water_cauldron[level=2]", servertest.ItemStack(t, "minecraft:red_shulker_box", 1), "minecraft:clean_shulker_box"},
		{"refused click", "minecraft:water_cauldron[level=2]", servertest.ItemStack(t, "minecraft:stick", 1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
			srv.StopTick()
			srv.SetBlock(3, 64, 0, cauldronState(t, tc.block))
			srv.GiveSlot("Testificate", player.HotbarSlot, tc.held)
			w.Settle()

			w.UseBlock(3, 64, 0)
			w.Settle()
			for _, key := range stats {
				want := int32(0)
				if key == tc.want {
					want = 1
				}
				if got := customStat(srv, key); got != want {
					t.Errorf("%s = %d, want %d", key, got, want)
				}
			}
		})
	}
}

// cauldronState parses "name" or "name[level=2]" into a block state.
func cauldronState(t *testing.T, spec string) uint16 {
	t.Helper()
	name, props, found := strings.Cut(strings.TrimSuffix(spec, "]"), "[")
	if !found {
		return servertest.MustState(t, name, nil)
	}
	key, value, _ := strings.Cut(props, "=")
	return servertest.MustState(t, name, map[string]string{key: value})
}
