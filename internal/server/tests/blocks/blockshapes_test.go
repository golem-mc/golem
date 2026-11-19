// The shape tests: what the collision shapes — the modelled boxes, the tall
// fences and walls and the chests' 14-sixteenth rims, and the generated
// shapes no Go model carries — mean for what a client can watch: where a
// spawn egg's mob lands, where a dropped item rests, which pose a body
// pressed against a shape keeps, and whether a placement through a shape's
// empty part goes through.
package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestASpawnEggOnATallShapeSpawnsItsMobOnTheCollision checks the egg
// placement against the shapes whose collision is taller than their cell's
// look: a fence's and a wall's collision tops stand 24 sixteenths tall, so
// the cow an egg places on either lands on the shape's top at the cell
// floor plus 1.5, not on a full cube's 1.0.
func TestASpawnEggOnATallShapeSpawnsItsMobOnTheCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		props map[string]string
	}{
		{"on an oak fence", "minecraft:oak_fence", map[string]string{
			"east": "false", "north": "false", "south": "false",
			"waterlogged": "false", "west": "false"}},
		{"on a cobblestone wall", "minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "none", "south": "none", "up": "true",
			"waterlogged": "false", "west": "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Rancher", "minecraft:cow_spawn_egg", 1)
			srv.SetBlock(2, 64, 3, servertest.MustState(t, tc.block, tc.props))
			c.PlaceBlock(2, 64, 3, protocol.FaceTop)
			cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
			if cow.X != 2.5 || cow.Y != 65.5 || cow.Z != 3.5 {
				t.Errorf("cow spawned at (%v, %v, %v), want (2.5, 65.5, 3.5)",
					cow.X, cow.Y, cow.Z)
			}
			c.ExpectBlockAck()
		})
	}
}

// TestADroppedItemRestsOnAChestsRimAndStaysOnIt drops a stick over a
// chest: the chest's collision is its own 14-sixteenths-open rim, so the
// stick rests on the rim at the chest's floor plus 0.875 and nothing in
// the ground's way moves it from there — not the escape a body buried
// inside a full cube's shape would make.
func TestADroppedItemRestsOnAChestsRimAndStaysOnIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Dropper")
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:chest",
		map[string]string{"facing": "north", "type": "single", "waterlogged": "false"})) {
		t.Fatal("the chest's chunk was not loaded")
	}
	srv.SpawnItemEntity(2.5, 66, 3.5, "minecraft:stick", 1)
	rest := func() (x, y, z float64, ok bool) {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return 0, 0, 0, false
		}
		return items[0].X, items[0].Y, items[0].Z, true
	}
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x, y, z, ok := rest()
		return ok && x == 2.5 && y == 65 && z == 3.5
	})
	x, y, z, _ := rest()
	if x != 2.5 || y != 65 || z != 3.5 {
		t.Errorf("the stick came to rest at (%v, %v, %v), want the chest's rim at (2.5, 65, 3.5)", x, y, z)
	}
	// The rest itself holds: an item lying on the shape neither sinks
	// into the cell nor gets pushed out of it.
	c.Settle()
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x2, y2, z2, ok := rest()
		return ok && x2 == 2.5 && y2 == 65 && z2 == 3.5
	})
}

// TestPressingIntoAThinShapeOverheadKeepsTheStandingPose walks a body into
// a fence, a wall and a chain standing one block up, at head height: the
// shapes are their own thin posts rather than full cubes, so the standing
// box fits into the cell with room to spare and the body keeps the pose
// its inputs ask for — standing on the walk, crouching on the sneak, and
// never drawn down into the crawl a full cube would force.
func TestPressingIntoAThinShapeOverheadKeepsTheStandingPose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		props map[string]string
		xs    []float64
	}{
		{"a fence", "minecraft:oak_fence", map[string]string{
			"east": "false", "north": "false", "south": "false",
			"waterlogged": "false", "west": "false"}, []float64{0.05, 0.95}},
		{"a wall", "minecraft:cobblestone_wall", map[string]string{
			"east": "none", "north": "none", "south": "none", "up": "true",
			"waterlogged": "false", "west": "none"}, []float64{-0.06, 1.06}},
		{"a chain", "minecraft:iron_chain", map[string]string{
			"axis": "y", "waterlogged": "false"}, []float64{0.05, 0.95}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			tick := servertest.StopTicking(t, srv)
			c := connect()
			c.JoinAs("Climber")
			if !srv.SetBlock(0, 65, 60, servertest.MustState(t, tc.block, tc.props)) {
				t.Fatal("the block's chunk was not loaded")
			}

			var poses []int32
			collect := func() {
				t.Helper()
				c.SettleAndCollect(func(id int32, data []byte) {
					switch id {
					case protocol.IDPlayEntityMetadata:
						if pose, ok := servertest.MetadataPose(t, data); ok {
							poses = append(poses, pose)
						}
					case protocol.IDPlaySyncPosition:
						t.Error("the server corrected the body's position")
					}
				})
			}

			// The body stands beside the shape's cell, its box crossing
			// into the shape's column: past the face by more than the
			// post's own width, never over it.
			for _, x := range tc.xs {
				c.MoveToGround(x, 64, 60.5, true)
				collect()
				tick(1)
				collect()
			}
			for _, pose := range poses {
				if pose != protocol.PoseStanding {
					t.Errorf("walking under %s drew pose %d, want %d (standing)",
						tc.name, pose, protocol.PoseStanding)
				}
			}

			poses = nil
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
			collect()
			tick(2)
			collect()
			if len(poses) == 0 {
				t.Fatal("the sneak sent no pose")
			}
			for _, pose := range poses {
				if pose != protocol.PoseSneaking {
					t.Errorf("sneaking under %s drew pose %d, want %d (crouching)",
						tc.name, pose, protocol.PoseSneaking)
				}
			}

			poses = nil
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(0) })
			collect()
			tick(2)
			collect()
			for _, pose := range poses {
				if pose != protocol.PoseStanding {
					t.Errorf("standing back up under %s drew pose %d, want %d (standing)",
						tc.name, pose, protocol.PoseStanding)
				}
			}
		})
	}
}

// TestCrouchingInsideScaffoldingKeepsTheCrouch stands a body inside a
// column of scaffolding and sneaks: scaffolding's collision is empty for a
// body that is not above its cell, so the plate overhead does not push the
// crouch down into the swim pose.
// vanilla: ScaffoldingBlock.getCollisionShape
func TestCrouchingInsideScaffoldingKeepsTheCrouch(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect().Join("Scaffolder")
	for _, y := range []int32{64, 65} {
		if !srv.SetBlock(0, y, 60, servertest.MustState(t, "minecraft:scaffolding",
			map[string]string{"bottom": "false", "distance": "0", "waterlogged": "false"})) {
			t.Fatal("the scaffolding's chunk was not loaded")
		}
	}
	c.MoveToGround(0.5, 64, 60.5, true)

	var poses []int32
	collect := func() {
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayEntityMetadata {
				if pose, ok := servertest.MetadataPose(t, data); ok {
					poses = append(poses, pose)
				}
			}
		})
	}
	collect()
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak)
	})
	collect()
	tick(2)
	collect()
	if len(poses) == 0 {
		t.Fatal("the sneak inside scaffolding drew no pose")
	}
	for _, pose := range poses {
		if pose != protocol.PoseSneaking {
			t.Errorf("sneaking inside scaffolding drew pose %d, want %d (crouching)",
				pose, protocol.PoseSneaking)
		}
	}
}

// TestASpawnEggStandsItsMobOnTheGeneratedShapes checks the egg placement
// against the families the generated table carries but no Go model does: a
// cake's collision is half a block tall, a flower pot's a six-sixteenth
// rim, a bed's a nine-sixteenth mattress, and the cow an egg places on
// each lands on the shape's own top rather than a full cube's. The tops
// are the vanilla reference server's getCollisionShape answers, which the
// egg reads through EntityType.getYOffset.
func TestASpawnEggStandsItsMobOnTheGeneratedShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		step  func(t *testing.T, srv *server.Server)
		wantY float64
	}{
		{"on a whole cake", func(t *testing.T, srv *server.Server) {
			srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:cake", nil))
		}, 64.5},
		{"on a flower pot", func(t *testing.T, srv *server.Server) {
			srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:flower_pot", nil))
		}, 64.375},
		{"on a bed", func(t *testing.T, srv *server.Server) {
			// facing=north: the head's cell, the foot north of it.
			srv.SetBlock(2, 64, 2, servertest.MustState(t, "minecraft:red_bed",
				map[string]string{"facing": "north", "occupied": "false", "part": "foot"}))
			srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:red_bed",
				map[string]string{"facing": "north", "occupied": "false", "part": "head"}))
		}, 64.5625},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.EggPlayer(t, "survival", "Rancher", "minecraft:cow_spawn_egg", 2)
			tc.step(t, srv)
			// The sneak key keeps the bed's own use from taking the
			// click, which is what vanilla's hold-item skip is for.
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
				b.PutU8(protocol.InputSneak)
			})
			c.PlaceBlock(2, 64, 3, protocol.FaceTop)
			cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
			if cow.X != 2.5 || cow.Y != tc.wantY || cow.Z != 3.5 {
				t.Errorf("cow spawned at (%v, %v, %v), want (2.5, %v, 3.5) standing on the shape",
					cow.X, cow.Y, cow.Z, tc.wantY)
			}
			c.ExpectBlockAck()
		})
	}
}

// TestCrouchingReadsTheShapesTheSweepReads holds the two sides of the pose
// fit against the generated shapes: a body standing on a cake — where the
// step the sweep owns puts its feet, at the cake's half-block top — keeps
// the crouch, nothing of the cake's top standing in the box above it; and
// a body beside a flower pot's rim, at the pot's own floor, is refused
// every pose down to the swim one, the pot's six-sixteenth rim standing
// exactly where the sweep would put a stop. The pose then stays exactly
// as it stood, vanilla's early return, rather than crouching through a
// rim the client would have climbed.
func TestCrouchingReadsTheShapesTheSweepReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block string
		feet  float64
		want  int32
	}{
		{"on a cake", "minecraft:cake", 64.5, protocol.PoseSneaking},
		{"beside a flower pot's rim", "minecraft:flower_pot", 64, protocol.PoseStanding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
			tick := servertest.StopTicking(t, srv)
			c := connect().Join("Crouch")
			if !srv.SetBlock(0, 64, 60, servertest.MustState(t, tc.block, nil)) {
				t.Fatal("the block's chunk was not loaded")
			}
			c.MoveToGround(0.5, tc.feet, 60.5, true)

			var poses []int32
			collect := func() {
				c.SettleAndCollect(func(id int32, data []byte) {
					if id == protocol.IDPlayEntityMetadata {
						if pose, ok := servertest.MetadataPose(t, data); ok {
							poses = append(poses, pose)
						}
					}
				})
			}
			collect()
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
				b.PutU8(protocol.InputSneak)
			})
			collect()
			tick(2)
			collect()
			for _, pose := range poses {
				if pose != tc.want {
					t.Errorf("sneaking %s drew pose %d, want %d",
						tc.name, pose, tc.want)
				}
			}
			if tc.want == protocol.PoseSneaking && len(poses) == 0 {
				t.Error("the sneak beside the shapes drew no pose at all")
			}
		})
	}
}

// TestPlacingACakeUnderABodyOnSnowOverlapsOnlyItsEmptyPart places a cake
// beside a body standing on snow layers: the cake's collision stops at the
// half block and the body's feet stand twelve sixteenths up, so the
// body's box crosses only the empty upper half of the cake's cell and the
// placement goes through, vanilla's isUnobstructed having no box to
// object to. A body standing on the ground would overlap the shape's
// solid half, and that click comes back refused.
func TestPlacingACakeUnderABodyOnSnowOverlapsOnlyItsEmptyPart(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	srv.StopTick()
	c := connect().Join("Baker")
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:snow",
		map[string]string{"layers": "7"})) {
		t.Fatal("the snow's chunk was not loaded")
	}
	cake, _ := registry.ItemID("minecraft:cake")
	srv.GiveSlot("Baker", player.HotbarSlot, protocol.ItemStack{ItemID: cake, Count: 2})
	// The body stands on the snow, twelve sixteenths above the ground, its
	// box reaching a fifteenth into the neighbouring cell beside it.
	c.MoveToGround(2.85, 64.75, 3.5, true)
	c.Settle()
	c.PlaceBlock(3, 63, 3, protocol.FaceTop)
	c.SettleKeeping()
	srv.FlushBlockAcks()
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("block ack sequence = %d, want 1", seq)
	}
	want := servertest.MustState(t, "minecraft:cake", nil)
	if got := srv.BlockAt(3, 64, 3); got != want {
		t.Errorf("block beside the body = %d, want the cake %d: only the cake's empty half crosses it", got, want)
	}
	if got := srv.InventorySlot("Baker", player.HotbarSlot).Count; got != 1 {
		t.Errorf("cakes left = %d, want 1", got)
	}
}

// TestAnItemBesideAFencePostFallsToTheFloor drops a stack inside a fence
// post's own cell but clear of the four-pixel post: the stack falls past
// the post to the floor below.
func TestAnItemBesideAFencePostFallsToTheFloor(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalConfig(), "Dropper")
	srv.SetLoadedBlock(2, 63, 3, servertest.MustState(t, "minecraft:stone", nil))
	srv.SetLoadedBlock(2, 64, 3, servertest.MustState(t, "minecraft:oak_fence", map[string]string{
		"east": "false", "north": "false", "south": "false", "waterlogged": "false", "west": "false"}))
	// The item sits in the post's cell but a fifth of a block aside its
	// four-pixel post, its box's x range clear of the post's.
	srv.SpawnItemEntity(2.2, 66, 3.5, "minecraft:stick", 1)
	rest := func() (float64, bool) {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return 0, false
		}
		return items[0].Y, true
	}
	want := 64 + entity.ItemHalfSize
	servertest.WaitForTicks(t, srv, 100, func() bool {
		y, ok := rest()
		return ok && y == want
	})
	if y, _ := rest(); y != want {
		t.Errorf("the stick beside the post rests at y %v, want the floor at %v", y, want)
	}
}

// TestADroppedItemFallsIntoTheHoppersBowl drops a stick over a hopper: the
// hopper's collision is its own funnel — a mouth whose hole passes the
// stick's box and a floor under it — so the stick falls through the mouth
// and rests inside the bowl, where the coarse whole-cube shape would leave
// it perched on the hopper's cell top.
func TestADroppedItemFallsIntoTheHoppersBowl(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Bowl")
	if !srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:hopper",
		map[string]string{"enabled": "false", "facing": "down"})) {
		t.Fatal("the hopper's chunk was not loaded")
	}
	srv.SpawnItemEntity(2.5, 66, 3.5, "minecraft:stick", 1)
	// The box the stick's centre comes to rest at: the funnel floor's top,
	// 11 sixteenths, and the stick's own half size above it.
	const restY = 64 + 11.0/16 + 0.125
	rest := func() (x, y, z float64, ok bool) {
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			return 0, 0, 0, false
		}
		return items[0].X, items[0].Y, items[0].Z, true
	}
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x, y, z, ok := rest()
		return ok && x == 2.5 && y == restY && z == 3.5
	})
	x, y, z, _ := rest()
	if x != 2.5 || y != restY || z != 3.5 {
		t.Errorf("the stick came to rest at (%v, %v, %v), want in the bowl at (2.5, %v, 3.5)",
			x, y, z, restY)
	}
	// The rest itself holds: a stick inside the bowl stays inside it.
	c.Settle()
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool {
		x2, y2, z2, ok := rest()
		return ok && x2 == 2.5 && y2 == restY && z2 == 3.5
	})
}

// TestPlacingACollidingBlockIntoYourOwnCellIsRefused places blocks whose
// collision shape is non-empty (a carpet, a coral block) into the cell the
// player stands in, which is refused, while shapeless blocks (a torch, a
// flower) are accepted.
func TestPlacingACollidingBlockIntoYourOwnCellIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item   string
		placed bool
	}{
		{"minecraft:white_carpet", false},
		{"minecraft:tube_coral_block", false},
		{"minecraft:torch", true},
		{"minecraft:poppy", true},
		{"minecraft:scaffolding", true},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
			srv.StopTick()
			c := connect().Join("Placer")
			srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:grass_block", map[string]string{"snowy": "false"}))
			item, _ := registry.ItemID(tc.item)
			srv.GiveSlot("Placer", player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 2})
			c.PlaceBlock(0, 63, 0, protocol.FaceTop)
			c.SettleKeeping()
			srv.FlushBlockAcks()
			if got := srv.BlockAt(0, 64, 0) != 0; got != tc.placed {
				t.Errorf("%s into the player's own cell placed = %v, want %v", tc.item, got, tc.placed)
			}
		})
	}
}
