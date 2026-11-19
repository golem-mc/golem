package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// blockEntityNumber lands a block with its tag through /setblock, holding the
// loop so nothing ticks it, and returns the saved form's numeric key as a
// long, failing when the key is missing or no number.
func blockEntityNumber(t *testing.T, block, key string) int64 {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	if strings.HasPrefix(block, "minecraft:moving_piston") {
		createMovingPiston(t, srv, c, 2, 64, 3)
	}
	srv.RunLine("setblock 2 64 3 " + block)
	tag, ok := srv.BlockEntityTagAt(2, 64, 3)
	if !ok {
		t.Fatalf("%s has no block entity", block)
	}
	v, ok := nbt.LongValue(tag[key])
	if !ok {
		t.Fatalf("%s saved %s = %v, want a number", block, key, tag[key])
	}
	return v
}

// TestABlockEntityTagReadsANumberAtAnyWidth gives each field a number at a
// width its own save does not write: getBooleanOr, getIntOr, getShortOr and
// getLong read any numeric tag, flooring a float, and the piston's facing
// goes through the byte codec, so the field holds the number rather than
// its default.
// vanilla: TagValueInput.getBooleanOr, getIntOr, getShortOr, getLong;
// Direction.LEGACY_ID_CODEC
func TestABlockEntityTagReadsANumberAtAnyWidth(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, block, key string
		want             int64
	}{
		{"waxed as an int", `minecraft:oak_sign{is_waxed:1}`, "is_waxed", 1},
		{"aligned as a float", `minecraft:oak_shelf{align_items_to_bottom:1.5f}`, "align_items_to_bottom", 1},
		{"condition met as an int", `minecraft:command_block{conditionMet:1}`, "conditionMet", 1},
		{"untracked output as an int", `minecraft:command_block{TrackOutput:0}`, "TrackOutput", 0},
		{"triggered as an int", `minecraft:crafter{triggered:1}`, "triggered", 1},
		{"triggered only at one", `minecraft:crafter{triggered:2}`, "triggered", 0},
		{"cooldown as a byte", `minecraft:hopper{TransferCooldown:5b}`, "TransferCooldown", 5},
		{"delay as a byte", `minecraft:spawner{Delay:30b}`, "Delay", 30},
		{"delay as a float", `minecraft:spawner{Delay:-1.5f}`, "Delay", -2},
		{"min delay as a float", `minecraft:spawner{MinSpawnDelay:250.7f}`, "MinSpawnDelay", 250},
		{"spawn count as a byte", `minecraft:spawner{SpawnCount:2b}`, "SpawnCount", 2},
		{"song ticks as a short", `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_cat",count:1},ticks_since_song_started:500s}`,
			"ticks_since_song_started", 500},
		{"facing as an int", `minecraft:moving_piston[facing=up]{blockState:{id:"minecraft:stone"},facing:2,extending:1b}`, "facing", 2},
		{"extending as an int", `minecraft:moving_piston[facing=up]{blockState:{id:"minecraft:stone"},facing:2b,extending:1}`, "extending", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := blockEntityNumber(t, c.block, c.key); got != c.want {
				t.Errorf("%s saved %s = %d, want %d", c.block, c.key, got, c.want)
			}
		})
	}
}

// TestABlockEntityTagFloorsAFraction gives the fields vanilla reads through
// getIntOr, getShortOr and getByteOr a negative fraction, which they floor
// where a codec read would truncate toward zero.
// vanilla: TagValueInput.getIntOr, FloatTag.intValue
func TestABlockEntityTagFloorsAFraction(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, block, key string
		want             int64
	}{
		{"brew time", `minecraft:brewing_stand{BrewTime:-1.5f}`, "BrewTime", -2},
		{"fuel", `minecraft:brewing_stand{Fuel:-0.5f}`, "Fuel", -1},
		{"crafting ticks", `minecraft:crafter{crafting_ticks_remaining:-2.5f}`, "crafting_ticks_remaining", -3},
		{"vibration frequency", `minecraft:sculk_sensor{last_vibration_frequency:-0.5f}`, "last_vibration_frequency", -1},
		{"warning level", `minecraft:sculk_shrieker{warning_level:-1.5d}`, "warning_level", -2},
		{"success count", `minecraft:command_block{SuccessCount:-0.5f}`, "SuccessCount", -1},
		{"last slot", `minecraft:chiseled_bookshelf{last_interacted_slot:-0.5f}`, "last_interacted_slot", -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := blockEntityNumber(t, c.block, c.key); got != c.want {
				t.Errorf("%s saved %s = %d, want %d", c.block, c.key, got, c.want)
			}
		})
	}
}

// TestASlowBrewAndAHugeFuelKeepTheirWidthThroughASave gives a brewing stand a
// brew time past a short and a fuel past a byte, and a furnace a counter past
// a short: all are ints in 26.3 and survive the save unclipped.
// vanilla: BrewingStandBlockEntity.saveAdditional, AbstractFurnaceBlockEntity.saveAdditional
func TestASlowBrewAndAHugeFuelKeepTheirWidthThroughASave(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		block, key string
		want       int64
	}{
		{`minecraft:brewing_stand{BrewTime:40000,Fuel:200}`, "BrewTime", 40000},
		{`minecraft:brewing_stand{BrewTime:40000,Fuel:200}`, "Fuel", 200},
		{`minecraft:furnace{lit_total_time:70000}`, "lit_total_time", 70000},
	} {
		if got := blockEntityNumber(t, c.block, c.key); got != c.want {
			t.Errorf("%s saved %s = %d, want %d", c.block, c.key, got, c.want)
		}
	}
}

// TestAComparatorKeepsItsOutputUnderOutputSignal checks the key the
// comparator's output is read from and saved under.
// vanilla: ComparatorBlockEntity.loadAdditional, saveAdditional
func TestAComparatorKeepsItsOutputUnderOutputSignal(t *testing.T) {
	t.Parallel()
	if got := blockEntityNumber(t, `minecraft:comparator{OutputSignal:7}`, "OutputSignal"); got != 7 {
		t.Errorf("the comparator saved OutputSignal = %d, want 7", got)
	}
}
