package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractArrow.readAdditionalSaveData, SpectralArrow.getDefaultPickupItem
func TestASummonedArrowKeepsItsDefaultPickupItem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, extra string
		entityType        int32
		item              registry.Item
	}{
		{"arrow", "arrow", "", protocol.EntityTypeArrow, registry.ItemArrow},
		{"spectral", "spectral_arrow", "", protocol.EntityTypeSpectralArrow, registry.ItemSpectralArrow},
		{"trident", "trident", "", protocol.EntityTypeTrident, registry.ItemTrident},
		{"explicit-item", "spectral_arrow", `,item:{id:"minecraft:diamond",count:1}`, protocol.EntityTypeSpectralArrow, registry.ItemDiamond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.SurvivalConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Collector", servertest.OpsListing(t, "Collector"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(fmt.Sprintf("summon minecraft:%s 3.5 65 0.5 {pickup:1b%s}", tc.kind, tc.extra))
			spawn := c.AwaitSpawn(tc.entityType)
			c.ExpectCommandFeedbackText("commands.summon.success",
				servertest.EntityDisplayComponent("minecraft:"+tc.kind, "entity.minecraft."+tc.kind, spawn.Uuid))

			// Reading the saved form catches a wrong default even when the
			// pickup path can fall back to the entity type on its own.
			c.ChatCommand(fmt.Sprintf("data get entity @e[type=minecraft:%s,limit=1] item.id", tc.kind))
			c.ExpectCommandFeedbackText("commands.data.entity.query",
				servertest.EntityDisplayComponent("minecraft:"+tc.kind, "entity.minecraft."+tc.kind, spawn.Uuid),
				servertest.NBTArg(fmt.Sprintf("%q", tc.item.Name())))

			c.MoveTo(3.5, 64, 0.5, 0, 0)
			r := protocol.NewReader(c.ExpectWithinTicks(srv, 20, protocol.IDPlayContainerSlot))
			window := r.ReadVarInt()
			r.ReadVarInt() // state ID
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			if !ok || r.Err() != nil {
				t.Fatalf("pickup slot: %v", r.Err())
			}
			if window != 0 || slot != 36 || registry.Item(stack.ItemID) != tc.item || stack.Count != 1 {
				t.Fatalf("pickup window %d slot %d = %+v, want one %s in hotbar slot 36", window, slot, stack, tc.item.Name())
			}
			c.ExpectWithinTicks(srv, 0, protocol.IDPlayPickupItem)
		})
	}
}
