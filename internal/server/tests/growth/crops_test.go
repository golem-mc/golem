package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The farm blocks, black-box: the grass that dies under what shuts the sky
// out and spreads to the dirt beside it, the farmland that takes and loses
// its water and dies under what stands on it, the crops and the melon and
// pumpkin stems that ripen and the bone meal that ripens them faster, the
// stems' bearing of their fruit, the cane and the cactus that grow a
// block at a time and break where the ground stops being theirs, and the
// farmland a heavy landing tramples flat. The dice are pinned the way the
// leaves' tests pin them, and the tick rule is turned up so a pass count
// the test can afford is a verdict the dice cannot flip.

// TestWheatRipensOnWateredFarmland checks the growth: a young wheat on wet
// farmland in the open sky ripens under the random ticks, and the wet
// farmland beneath it stays wet for the water beside it.
func TestWheatRipensOnWateredFarmland(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			servertest.SetFarmBlock(t, c, srv, 6+dx, 63, dz, "minecraft:farmland", map[string]string{"moisture": "7"})
		}
	}
	servertest.SetFarmBlock(t, c, srv, 3, 63, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})

	srv.TickRandomTicks(20000)

	if age := servertest.CropsProps(t, srv.BlockAt(6, 64, 0), "minecraft:wheat")["age"]; age != "7" {
		t.Errorf("the wheat ripened to age=%s, want 7", age)
	}
	if moisture := servertest.CropsProps(t, srv.BlockAt(6, 63, 0), "minecraft:farmland")["moisture"]; moisture != "7" {
		t.Errorf("the farmland under the wheat sits at moisture=%s, want 7", moisture)
	}
}

// stemFruitOffset is the cell a stem's facing names, where its fruit
// stands.
var stemFruitOffset = map[string][2]int32{
	"north": {0, -1}, "south": {0, 1}, "west": {-1, 0}, "east": {1, 0},
}

// TestStemsGrowAndBearFruitOnRandomTicks checks the stems' own random tick:
// a melon stem on wet farmland in the open sky climbs the same eight ages
// a crop does, and once at seven bears a melon beside it on the fruit
// ground, turning into the attached stem that faces the melon. A pumpkin
// stem on a patch of its own does the same. The passes run in batches of a
// hundred so the loop can stop once both stems have borne their fruit.
func TestStemsGrowAndBearFruitOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	patches := []struct {
		stem, fruit, attached string
		z                     int32
		mature                bool
	}{
		{"minecraft:melon_stem", "minecraft:melon", "minecraft:attached_melon_stem", 0, false},
		{"minecraft:pumpkin_stem", "minecraft:pumpkin", "minecraft:attached_pumpkin_stem", -8, false},
	}
	// Wet farmland in the three-by-three under each stem: it is the fruit
	// ground on every side the fruit roll could pick, and wet it makes the
	// ripening the crops' speed. Water three cells out keeps it wet.
	for _, p := range patches {
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				servertest.SetFarmBlock(t, c, srv, 6+dx, 63, p.z+dz, "minecraft:farmland",
					map[string]string{"moisture": "7"})
			}
		}
		servertest.SetFarmBlock(t, c, srv, 3, 63, p.z, "minecraft:water", nil)
		servertest.SetFarmBlock(t, c, srv, 6, 64, p.z, p.stem, map[string]string{"age": "0"})
	}

	for range 300 {
		pending := false
		for i := range patches {
			p := &patches[i]
			name := servertest.CropsNameOf(t, srv.BlockAt(6, 64, p.z))
			switch {
			case name == p.attached:
				// An attached stem proves the age-seven tick landed, even
				// when the batch took it there from below seven.
				p.mature = true
			case !p.mature && name == p.stem &&
				servertest.CropsProps(t, srv.BlockAt(6, 64, p.z), p.stem)["age"] == "7":
				p.mature = true
				pending = true
			default:
				pending = true
			}
		}
		if !pending {
			break
		}
		srv.TickRandomTicks(100)
	}

	air := servertest.CropsStateOf(t, "minecraft:air", nil)
	for _, p := range patches {
		if !p.mature {
			t.Errorf("the %s never came to stand at age 7", p.stem)
		}
		stemAt := srv.BlockAt(6, 64, p.z)
		facing := servertest.CropsProps(t, stemAt, p.attached)["facing"]
		fruitAt := servertest.CropsStateOf(t, p.fruit, nil)
		off := stemFruitOffset[facing]
		if got := srv.BlockAt(6+off[0], 64, p.z+off[1]); got != fruitAt {
			t.Errorf("the block %s faces at (%d, 64, %d) = %d, want %d (%s)",
				p.attached, 6+off[0], p.z+off[1], got, fruitAt, p.fruit)
		}
		melons := 0
		for _, d := range [][2]int32{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
			got := srv.BlockAt(6+d[0], 64, p.z+d[1])
			if got == fruitAt {
				melons++
			} else if got != air {
				t.Errorf("the fruit roll left the %s at (%d, 64, %d) = %d",
					p.stem, 6+d[0], p.z+d[1], got)
			}
		}
		if melons != 1 {
			t.Errorf("%s beside the %s, want 1", strconv.Itoa(melons), p.stem)
		}
		if moisture := servertest.CropsProps(t, srv.BlockAt(6, 63, p.z), "minecraft:farmland")["moisture"]; moisture != "7" {
			t.Errorf("the farmland under the %s sits at moisture=%s, want 7", p.stem, moisture)
		}
	}
}

// TestBreakingTheFruitRevertsTheAttachedStem checks the fruit's loss: with the
// melon and the pumpkin beside their attached stems broken, each attached stem
// turns back into its plain stem at age 7 and the change reaches the wire.
func TestBreakingTheFruitRevertsTheAttachedStem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	patches := []struct {
		attached, stem, fruit string
		x, z                  int32
		facing                string
		fx, fz                int32
	}{
		{"minecraft:attached_melon_stem", "minecraft:melon_stem", "minecraft:melon", 4, 0, "west", 3, 0},
		{"minecraft:attached_pumpkin_stem", "minecraft:pumpkin_stem", "minecraft:pumpkin", 4, 2, "north", 4, 1},
	}
	for _, p := range patches {
		servertest.SetFarmBlock(t, c, srv, p.x, 63, p.z, "minecraft:farmland", map[string]string{"moisture": "7"})
		servertest.SetFarmBlock(t, c, srv, p.x, 64, p.z, p.attached, map[string]string{"facing": p.facing})
		servertest.SetFarmBlock(t, c, srv, p.fx, 64, p.fz, p.fruit, nil)
	}
	for _, p := range patches {
		c.BreakBlock(p.fx, 64, p.fz)

		plain := servertest.CropsStateOf(t, p.stem, map[string]string{"age": "7"})
		servertest.AwaitBlockUpdate(t, c, p.x, 64, p.z, plain)
		if got := srv.BlockAt(p.x, 64, p.z); got != plain {
			t.Errorf("the %s after the %s broke = %d, want %d (%s age 7)",
				p.attached, p.fruit, got, plain, p.stem)
		}
	}
}

// TestRemovingTheFarmlandBreaksTheAttachedStem checks the ground's loss: the
// farmland under an attached stem taken out leaves it standing on nothing, and
// it breaks at once, dropping its seeds onto the wire.
func TestRemovingTheFarmlandBreaksTheAttachedStem(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	srv.LootSeed(2)

	servertest.SetFarmBlock(t, c, srv, 4, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	servertest.SetFarmBlock(t, c, srv, 4, 64, 0, "minecraft:attached_melon_stem", map[string]string{"facing": "north"})
	servertest.SetFarmBlock(t, c, srv, 4, 64, -1, "minecraft:melon", nil)

	c.BreakBlock(4, 63, 0)

	drops := servertest.AwaitCropBreak(t, c, 4, 64, 0, 0)
	if got := srv.BlockAt(4, 64, 0); got != 0 {
		t.Errorf("the attached stem over broken farmland = %d, want 0 (air)", got)
	}
	if len(drops) != 1 {
		t.Fatalf("the attached stem's break laid down %d drops, want 1", len(drops))
	}
	if name, _ := registry.ItemName(drops[0].ItemID); name != "minecraft:melon_seeds" {
		t.Errorf("the attached stem's break dropped %s, want minecraft:melon_seeds", name)
	}
	if drops[0].Count < 1 || drops[0].Count > 3 {
		t.Errorf("the attached stem's break dropped %d melon seeds, want one to three", drops[0].Count)
	}
}

// TestGrassDiesUnderDirt checks the death: a grass block with a full block
// over it is alive no longer, and the random tick that lands on it turns it
// to the dirt the family decays into.
func TestGrassDiesUnderDirt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:dirt", nil)

	srv.TickRandomTicks(20000)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	servertest.AwaitBlockUpdate(t, c, 5, 63, 0, dirt)
	if got := srv.BlockAt(5, 63, 0); got != dirt {
		t.Errorf("the grass under the dirt = %d, want %d (dirt)", got, dirt)
	}
}

// TestGrassSpreadsOntoDirt checks the spread: a grass block in the open sky
// throws its four tries a tick, and the dirt beside it that could keep the
// family alive takes the grass on, snowy the way the sky above it says.
func TestGrassSpreadsOntoDirt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 63, 1, "minecraft:dirt", nil)

	srv.TickRandomTicks(20000)

	spread := servertest.CropsStateOf(t, "minecraft:grass_block", map[string]string{"snowy": "false"})
	for _, at := range [][2]int32{{6, 0}, {6, 1}} {
		if got := srv.BlockAt(6, 63, at[1]); got != spread {
			t.Errorf("the dirt at (6, 63, %d) = %d, want %d (grass)", at[1], got, spread)
		}
	}
}

// TestFarmlandDriesBackToDirt checks the water's loss: farmland with no
// water in the field around it dries one step a tick, and the dry square
// nothing maintains turns back to dirt under the same ticks.
func TestFarmlandDriesBackToDirt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})

	srv.TickRandomTicks(20000)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	servertest.AwaitBlockEventually(t, c, 5, 63, 0, dirt)
	if got := srv.BlockAt(5, 63, 0); got != dirt {
		t.Errorf("the dried farmland = %d, want %d (dirt)", got, dirt)
	}
}

// TestAHoeTillsTheGround checks the tool: a hoe's right-click on tillable
// ground turns it over to farmland where it stands, the update and the
// acknowledgement both reaching the wire.
func TestAHoeTillsTheGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 6, 63, 0, "minecraft:dirt", nil)
	servertest.GiveFarmItem(t, c, "minecraft:iron_hoe")
	c.UseBlock(6, 63, 0)

	farmland := servertest.CropsStateOf(t, "minecraft:farmland", nil)
	servertest.AwaitBlockUpdate(t, c, 6, 63, 0, farmland)
	if got := srv.BlockAt(6, 63, 0); got != farmland {
		t.Errorf("the tilled ground = %d, want %d (farmland)", got, farmland)
	}
}

// TestBoneMealRipensWheat checks the item: bone meal's right-click on a
// young wheat ripens it by the draw the vanilla increase makes — two to
// five steps — and the new age and the particles both reach the wire.
func TestBoneMealRipensWheat(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:wheat", map[string]string{"age": "0"})
	servertest.GiveFarmItem(t, c, "minecraft:bone_meal")

	c.UseBlock(5, 64, 0)

	ripened := servertest.AwaitBlockState(t, c, 5, 64, 0)
	age, err := strconv.Atoi(servertest.CropsProps(t, ripened, "minecraft:wheat")["age"])
	if err != nil {
		t.Fatalf("the ripened wheat carries no age: %v", err)
	}
	if age < 2 || age > 5 {
		t.Errorf("the bone meal ripened the wheat to age=%d, want two to five", age)
	}
	if got := srv.BlockAt(5, 64, 0); got != ripened {
		t.Errorf("the world's wheat = %d where the wire said %d", got, ripened)
	}
}

// TestCaneGrowsToThree checks the cane's growth: cane with water beside its
// ground grows a block at a time until the third stands, and the fourth
// never comes.
func TestCaneGrowsToThree(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 63, 0, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:sugar_cane", nil)

	srv.TickRandomTicks(40000)

	cane := servertest.CropsStateOf(t, "minecraft:sugar_cane", nil)
	for _, y := range []int32{64, 65, 66} {
		if got := srv.BlockAt(5, y, 0); got != cane {
			t.Errorf("the cane at y=%d = %d, want %d (cane)", y, got, cane)
		}
	}
	if got := srv.BlockAt(5, 67, 0); got != 0 {
		t.Errorf("the cane grew a fourth block: %d, want 0 (air)", got)
	}
}

// TestCactusGrowsAndStandsThreeTall checks the cactus's growth: one hit on
// a nearly ripe cactus grows the second block, and a full three-tall
// cactus under the passes grows nothing further, whatever flowers the
// dice may plant over it.
func TestCactusGrowsAndStandsThreeTall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:cactus", map[string]string{"age": "14"})

	srv.TickRandomTicks(2000)

	if got := servertest.CropsNameOf(t, srv.BlockAt(5, 65, 0)); got != "minecraft:cactus" {
		t.Fatalf("the ripe cactus grew to y=65 = %s, want minecraft:cactus", got)
	}

	servertest.SetFarmBlock(t, c, srv, 5, 66, 0, "minecraft:cactus", map[string]string{"age": "0"})
	srv.TickRandomTicks(40000)

	for _, y := range []int32{64, 65, 66} {
		if got := servertest.CropsNameOf(t, srv.BlockAt(5, y, 0)); got != "minecraft:cactus" {
			t.Errorf("the cactus at y=%d = %s after the passes, want minecraft:cactus", y, got)
		}
	}
	if servertest.CropsNameOf(t, srv.BlockAt(5, 67, 0)) == "minecraft:cactus" {
		t.Error("the three-tall cactus grew a fourth block")
	}
}

// TestCactusBreaksBesideASolidBlock checks the cactus's end: a solid block
// set beside it is a place the cactus cannot stand, and the neighbour
// change's one-tick visit breaks it with its loot on the wire.
func TestCactusBreaksBesideASolidBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	srv.LootSeed(servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:cactus", nil)
	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:stone", nil)

	// Drive the engine until the visit has broken it.
	for i := 0; i < 4 && srv.BlockAt(5, 64, 0) != 0; i++ {
		srv.TickPhysics(1)
	}

	drops := servertest.AwaitCropBreak(t, c, 5, 64, 0, 0)
	if got := srv.BlockAt(5, 64, 0); got != 0 {
		t.Errorf("the cactus beside the stone = %d, want 0 (air)", got)
	}
	if len(drops) != 1 {
		t.Fatalf("the cactus's break laid down %d drops, want 1", len(drops))
	}
	if name, _ := registry.ItemName(drops[0].ItemID); name != "minecraft:cactus" {
		t.Errorf("the cactus's break dropped %s, want minecraft:cactus", name)
	}
}

// TestAFallingPlayerTramplesFarmland checks the landing: a player who falls
// onto a wet farm square rolls against how far the fall went, and the roll
// a ten-block fall cannot lose turns the square to dirt. The faller is in
// survival, the mode the server's fall book runs for: creative and
// spectator players cannot be hurt, and the fall tracking stops with it.
func TestAFallingPlayerTramplesFarmland(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 0, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})
	var events []string
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:block_change" && x == 0.5 && y == 63.5 && z == 0.5 {
			events = append(events, event)
		}
	})

	c.MoveTo(0.5, 74, 0.5, 0, 0)
	c.MoveTo(0.5, 64, 0.5, 0, 0)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	servertest.AwaitBlockUpdate(t, c, 0, 63, 0, dirt)
	if len(events) != 1 {
		t.Errorf("block_change events at the trampled cell = %v, want one", events)
	}
	if got := srv.BlockAt(0, 63, 0); got != dirt {
		t.Errorf("the farmland under the landing = %d, want %d (dirt)", got, dirt)
	}
}

// TestAMobTramplesFarmland checks the mob's landing: a cow heavy enough by
// its own shape, falling onto a farm square while the mob griefing rule
// stands, tramples the square under its feet the way the player's landing
// does.
func TestAMobTramplesFarmland(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	servertest.SetFarmBlock(t, c, srv, 0, 63, 0, "minecraft:farmland", map[string]string{"moisture": "7"})

	srv.SpawnMob("minecraft:cow", 0.5, 74, 0.5)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	// A random tick can dry the square through its moistures before the
	// cow lands, so the wait skips those updates and takes the trample.
	servertest.AwaitBlockEventually(t, c, 0, 63, 0, dirt)
	if got := srv.BlockAt(0, 63, 0); got != dirt {
		t.Errorf("the farmland under the cow = %d, want %d (dirt)", got, dirt)
	}
}

// caneTower stands a three-high cane on sand with water beside the sand,
// the survival player who will break it opped for the /setblock that lays
// it, and reports the cane's state.
func caneTower(t *testing.T) (*server.Server, *servertest.Client, uint16) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Cutter", servertest.OpsListing(t, "Cutter"))

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 4, 63, 0, "minecraft:water", nil)
	var cane uint16
	for _, y := range []int32{64, 65, 66} {
		cane = servertest.SetFarmBlock(t, c, srv, 5, y, 0, "minecraft:sugar_cane", nil)
	}
	return srv, c, cane
}

// expectCaneDrops checks the drops are n stacks of one sugar cane each.
func expectCaneDrops(t *testing.T, drops []protocol.ItemStack, n int) {
	t.Helper()
	if len(drops) != n {
		t.Fatalf("the cane laid down %d drops, want %d", len(drops), n)
	}
	for _, d := range drops {
		if name, _ := registry.ItemName(d.ItemID); name != "minecraft:sugar_cane" || d.Count != 1 {
			t.Errorf("the cane dropped %d %s, want 1 minecraft:sugar_cane", d.Count, name)
		}
	}
}

// TestBreakingTheTopCaneDropsIt checks a survival click on the top of a
// cane: the instant break takes it on the started dig and drops one cane.
func TestBreakingTheTopCaneDropsIt(t *testing.T) {
	t.Parallel()
	srv, c, cane := caneTower(t)

	c.StartDigging(5, 66, 0)

	expectCaneDrops(t, servertest.AwaitCropBreak(t, c, 5, 66, 0, 0), 1)
	for _, y := range []int32{64, 65} {
		if got := srv.BlockAt(5, y, 0); got != cane {
			t.Errorf("the cane at y=%d = %d, want %d (still standing)", y, got, cane)
		}
	}
}
