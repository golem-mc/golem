package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestSeedsOnACropAreRefusedButSeedsOnFarmlandPlace checks the placement
// survival rule for the crops: wheat seeds used on the top face of a wheat
// crop stand where no crop can survive, so the placement is refused — the
// stack stays unspent and the client is told the clicked and target cells —
// while the same seeds on the farmland beside it take, spending one.
func TestSeedsOnACropAreRefusedButSeedsOnFarmlandPlace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 1, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	wheat := servertest.SetFarmBlock(t, c, srv, 1, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	servertest.SetFarmBlock(t, c, srv, 2, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})

	seeds, _ := registry.ItemID("minecraft:wheat_seeds")
	srv.GiveSlot("Camper", player.HotbarSlot, protocol.ItemStack{ItemID: seeds, Count: 5})

	// The seeds on the crop: refused.
	c.PlaceBlock(1, 64, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused seeds acked sequence %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(1, 64, 0, protocol.FaceTop); echoes[0] != wheat || echoes[1] != 0 {
		t.Errorf("the refused seeds echoed (%d, %d), want (%d, 0) — the crop and air",
			echoes[0], echoes[1], wheat)
	}
	if got := srv.BlockAt(1, 65, 0); got != 0 {
		t.Errorf("the seeds on the crop placed %d at (1, 65, 0), want 0 (air)", got)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 5 {
		t.Errorf("held seeds after the refusal = %d of 5, want all 5 (unspent)", got.Count)
	}

	// The seeds on the farmland: placed.
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the placed seeds acked sequence %d, want 1", seq)
	}
	placed := servertest.CropsStateOf(t, "minecraft:wheat", map[string]string{"age": "0"})
	if got := srv.BlockAt(2, 64, 0); got != placed {
		t.Errorf("the seeds on farmland placed %d at (2, 64, 0), want %d (wheat age 0)",
			got, placed)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 4 {
		t.Errorf("held seeds after placing on farmland = %d of 4, want one spent", got.Count)
	}
}

// TestCaneAndCactusPlacementsUseTheirSurvivalRules checks the other plants
// with a survival rule: sugar cane and cactus used on ground that cannot hold
// them place nothing and keep the stack, and the same items on ground their
// own rules accept — cane on sand with water beside its ground, cactus on
// sand alone — take.
func TestCaneAndCactusPlacementsUseTheirSurvivalRules(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	// The flat world's ground at y=63 is stone, which holds neither cane
	// nor cactus: (1, 63, 0) and (3, 63, 0) stand on it unchanged.
	servertest.SetFarmBlock(t, c, srv, 2, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 2, 63, 1, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 63, 0, "minecraft:sand", nil)

	cane, _ := registry.ItemID("minecraft:sugar_cane")
	srv.GiveSlot("Camper", player.HotbarSlot, protocol.ItemStack{ItemID: cane, Count: 3})

	// Cane on stone with no water beside the ground: refused.
	c.PlaceBlock(1, 63, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused cane acked sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(1, 64, 0); got != 0 {
		t.Errorf("the cane on stone placed %d at (1, 64, 0), want 0 (air)", got)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 3 {
		t.Errorf("held cane after the refusal = %d of 3, want all 3 (unspent)", got.Count)
	}

	// Cane on sand with water beside the ground: placed.
	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the placed cane acked sequence %d, want 1", seq)
	}
	placedCane := servertest.CropsStateOf(t, "minecraft:sugar_cane", nil)
	if got := srv.BlockAt(2, 64, 0); got != placedCane {
		t.Errorf("the cane on sand and water placed %d at (2, 64, 0), want %d (sugar cane)",
			got, placedCane)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 2 {
		t.Errorf("held cane after placing = %d of 2, want one spent", got.Count)
	}

	cactus, _ := registry.ItemID("minecraft:cactus")
	srv.GiveSlot("Camper", player.HotbarSlot, protocol.ItemStack{ItemID: cactus, Count: 3})

	// Cactus on stone: refused.
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the refused cactus acked sequence %d, want 1", seq)
	}
	if got := srv.BlockAt(3, 64, 0); got != 0 {
		t.Errorf("the cactus on stone placed %d at (3, 64, 0), want 0 (air)", got)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 3 {
		t.Errorf("held cactus after the refusal = %d of 3, want all 3 (unspent)", got.Count)
	}

	// Cactus on sand: placed.
	c.PlaceBlock(4, 63, 0, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("the placed cactus acked sequence %d, want 1", seq)
	}
	placedCactus := servertest.CropsStateOf(t, "minecraft:cactus", nil)
	if got := srv.BlockAt(4, 64, 0); got != placedCactus {
		t.Errorf("the cactus on sand placed %d at (4, 64, 0), want %d (cactus)",
			got, placedCactus)
	}
	if got := srv.InventorySlot("Camper", player.HotbarSlot); got.Count != 2 {
		t.Errorf("held cactus after placing = %d of 2, want one spent", got.Count)
	}
}
