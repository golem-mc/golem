package mobs

import (
	"strings"
	"testing"
)

// TestClickFittedSaddleAndBodySaveGuaranteedDropChance: fitting a saddle on a
// strider or a harness on a happy ghast by use-click writes the slot's
// guaranteed drop chance (2.0f) in drop_chances.
// vanilla: Equippable.equipOnTarget, Mob.setGuaranteedDrop
func TestClickFittedSaddleAndBodySaveGuaranteedDropChance(t *testing.T) {
	t.Parallel()
	t.Run("strider saddle", func(t *testing.T) {
		t.Parallel()
		srv, c := wolfScene(t, "Rider")
		id := srv.SpawnMob("minecraft:strider", 2.5, 64, 0.5)
		srv.SetMobNoAI(id)
		giveHand(t, srv, "Rider", "minecraft:saddle", 1)

		c.UseOn(id)
		c.Settle()

		got := savedField(t, srv, "strider", "drop_chances")
		if !strings.Contains(got, "saddle: 2.0f") {
			t.Fatalf("strider drop_chances = %q, want saddle: 2.0f", got)
		}
	})

	t.Run("happy ghast harness", func(t *testing.T) {
		t.Parallel()
		srv, c, id := harnessScene(t, "minecraft:blue_harness")
		c.UseOn(id)
		c.Settle()

		got := savedField(t, srv, "happy_ghast", "drop_chances")
		if !strings.Contains(got, "body: 2.0f") {
			t.Fatalf("happy_ghast drop_chances = %q, want body: 2.0f", got)
		}
	})
}

// TestSummonedSaddleWithoutDropChancesDoesNotDropOnKill: a pig summoned with a
// saddle in equipment and no drop_chances keeps the default 0.085 chance, so a
// non-player /kill drops no saddle.
// vanilla: Mob.dropCustomDeathLoot, DropChances.DEFAULT
func TestSummonedSaddleWithoutDropChancesDoesNotDropOnKill(t *testing.T) {
	t.Parallel()
	srv, _ := wolfScene(t, "Rider")
	srv.RunLine(`summon minecraft:pig 2.5 64 0.5 {NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	srv.RunLine(`kill @e[type=pig]`)
	srv.TickMobs(2)

	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:saddle" {
			t.Fatalf("non-player /kill dropped summoned saddle with default drop_chances: %+v", it)
		}
	}
}

// TestClickFittedSaddleDropsOnKill: a pig saddled by a player's click has a
// guaranteed drop chance and drops its saddle even on a non-player /kill.
// vanilla: Equippable.equipOnTarget, Mob.dropCustomDeathLoot
func TestClickFittedSaddleDropsOnKill(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Rider")
	id := srv.SpawnMob("minecraft:pig", 2.5, 64, 0.5)
	srv.SetMobNoAI(id)
	giveHand(t, srv, "Rider", "minecraft:saddle", 1)

	c.UseOn(id)
	c.Settle()

	if got := savedField(t, srv, "pig", "drop_chances"); !strings.Contains(got, "saddle: 2.0f") {
		t.Fatalf("pig drop_chances = %q, want saddle: 2.0f", got)
	}

	srv.RunLine(`kill @e[type=pig]`)
	srv.TickMobs(2)

	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:saddle" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("click-fitted saddle did not drop on /kill; items = %+v", srv.ItemsInDimension("minecraft:overworld"))
	}
}
