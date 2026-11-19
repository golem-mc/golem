package blocks

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

const gatewayY = 90

// stepIntoGateway stops the loop and stands the player in a gateway at
// (x, gatewayY, z).
func stepIntoGateway(t *testing.T, srv *server.Server, c *servertest.Client, x, z int32, setup func()) {
	t.Helper()
	srv.StopTick()
	srv.SetLoadedBlock(x, gatewayY, z, servertest.BlockState(t, "minecraft:end_gateway"))
	if setup != nil {
		setup()
	}
	c.MoveToGrounded(float64(x)+0.5, gatewayY, float64(z)+0.5, 30, 10, false)
	c.Settle()
}

// gatewayBeams counts the Block Actions the settle collected for the gateway
// at (x, gatewayY, z), and fails on one that is not the cooldown event.
func gatewayBeams(t *testing.T, c *servertest.Client, x, z int32) (beams int, teleported bool) {
	t.Helper()
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySyncPosition:
			teleported = true
		case protocol.IDPlayBlockAction:
			r := protocol.NewReader(data)
			bx, by, bz := r.ReadPosition()
			event, param, block := r.ReadU8(), r.ReadU8(), r.ReadVarInt()
			if bx != x || by != gatewayY || bz != z {
				return
			}
			if event != 1 || param != 0 || block != registry.BlockIDs["minecraft:end_gateway"] {
				t.Errorf("gateway block action = %d/%d on block %d, want 1/0 on the end gateway", event, param, block)
			}
			beams++
		}
	})
	return beams, teleported
}

// TestEndGatewayTeleportsToExactExit checks a player in a gateway whose
// block entity names an exact exit: the beam event, then a teleport to the
// bottom centre of that block keeping velocity and facing.
// vanilla: EndGatewayBlock.entityInside, getPortalDestination
func TestEndGatewayTeleportsToExactExit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	stepIntoGateway(t, srv, c, 3, 3, func() {
		srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
		srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	})

	srv.TickPortals()
	got := servertest.ReadSyncPosition(t, c.AwaitPlay(protocol.IDPlaySyncPosition))
	if got.X != 20.5 || got.Y != 120 || got.Z != 20.5 {
		t.Errorf("landed at (%v, %v, %v), want the exit's bottom centre (20.5, 120, 20.5)", got.X, got.Y, got.Z)
	}
}

// TestEndGatewayLandsOnTheTallestBlockBesideAnInexactExit checks the search
// around an exit that is not exact: the cell above the tallest full block
// within 5 columns of two above the exit.
// vanilla: TheEndGatewayBlockEntity.findExitPosition
func TestEndGatewayLandsOnTheTallestBlockBesideAnInexactExit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	stepIntoGateway(t, srv, c, 3, 3, func() {
		srv.SetLoadedBlock(23, 200, 17, servertest.BlockState(t, "minecraft:stone"))
		srv.SetLoadedBlock(18, 150, 22, servertest.BlockState(t, "minecraft:stone"))
		srv.SetEndGatewayExit(3, gatewayY, 3, 20, 75, 20, false)
	})

	srv.TickPortals()
	got := servertest.ReadSyncPosition(t, c.AwaitPlay(protocol.IDPlaySyncPosition))
	if got.X != 23.5 || got.Y != 201 || got.Z != 17.5 {
		t.Errorf("landed at (%v, %v, %v), want above the tallest block (23.5, 201, 17.5)", got.X, got.Y, got.Z)
	}
}

// TestEndGatewayCoolsDownForFortyTicks checks the beam event fires once per
// cooldown, again after 40 ticks, and that a gateway with no exit moves
// nobody.
// vanilla: TheEndGatewayBlockEntity.triggerCooldown, COOLDOWN_TIME
func TestEndGatewayCoolsDownForFortyTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	stepIntoGateway(t, srv, c, 3, 3, nil)

	srv.TickPortals()
	srv.TickPortals()
	if beams, teleported := gatewayBeams(t, c, 3, 3); beams != 1 || teleported {
		t.Fatalf("two touches gave %d beams and teleported=%v, want one beam and no teleport", beams, teleported)
	}
	srv.AdvanceTicksRun(39)
	srv.TickPortals()
	if beams, _ := gatewayBeams(t, c, 3, 3); beams != 0 {
		t.Fatalf("a touch 39 ticks later gave %d beams, want none", beams)
	}
	srv.AdvanceTicksRun(1)
	srv.TickPortals()
	if beams, _ := gatewayBeams(t, c, 3, 3); beams != 1 {
		t.Fatalf("a touch 40 ticks later gave %d beams, want one", beams)
	}
}

// TestEndGatewayKeepsTheStoredLook checks the teleport leaves the look the
// server stores where it was while the wire carries zero deltas.
func TestEndGatewayKeepsTheStoredLook(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	stepIntoGateway(t, srv, c, 3, 3, func() {
		srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
		srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	})

	srv.TickPortals()
	got := servertest.ReadSyncPosition(t, c.AwaitPlay(protocol.IDPlaySyncPosition))
	if got.Yaw != 0 || got.Pitch != 0 {
		t.Errorf("wire look = %v/%v, want zero deltas", got.Yaw, got.Pitch)
	}
	if p := srv.PlayerPosition("Gate"); p.Yaw != 30 || p.Pitch != 10 {
		t.Errorf("stored look = %v/%v, want 30/10", p.Yaw, p.Pitch)
	}
}

// TestEndGatewayIgnoresSpectators checks a spectator in a gateway gets no
// beam and no teleport.
// vanilla: Player.tick noPhysics
func TestEndGatewayIgnoresSpectators(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
	c.SendChangeGameMode(protocol.GameModeSpectator)
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator })
	stepIntoGateway(t, srv, c, 3, 3, func() {
		srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
		srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	})

	srv.TickPortals()
	if beams, teleported := gatewayBeams(t, c, 3, 3); beams != 0 || teleported {
		t.Errorf("spectator gave %d beams and teleported=%v, want neither", beams, teleported)
	}
}

// TestEndGatewayTouchesTheWholeBody checks a gateway at head height, not at
// the feet, still takes the player.
// vanilla: Entity.applyEffectsFromBlocks
func TestEndGatewayTouchesTheWholeBody(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	srv.StopTick()
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
	c.MoveToGrounded(3.5, gatewayY-1, 3.5, 30, 10, false)
	c.Settle()

	srv.TickPortals()
	c.AwaitPlay(protocol.IDPlaySyncPosition)
}

// TestEndGatewayUsesTheSneakingBox checks a sneaking player's shorter box
// stops short of a gateway that a standing one reaches.
// vanilla: Entity.applyEffectsFromBlocks
func TestEndGatewayUsesTheSneakingBox(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	c.SendSneak(true)
	servertest.UntilTicks(t, srv, 40, "sneaking pose", func() bool { return srv.PlayerPose("Gate") == protocol.PoseSneaking })
	srv.StopTick()
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
	// Feet at 88.4: a standing box (1.8) reaches 90.2, a sneaking one (1.5) 89.9.
	c.MoveToGrounded(3.5, gatewayY-1.6, 3.5, 30, 10, false)
	c.Settle()

	srv.TickPortals()
	if beams, teleported := gatewayBeams(t, c, 3, 3); beams != 0 || teleported {
		t.Errorf("sneaking player gave %d beams and teleported=%v, want neither", beams, teleported)
	}
}

// TestEndGatewaySweepsAFastFall checks a player who falls past a gateway
// within one tick, ending the tick well below it, is still taken.
// vanilla: Entity.checkInsideBlocks
func TestEndGatewaySweepsAFastFall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	srv.StopTick()
	c.MoveToGrounded(3.5, gatewayY+1, 3.5, 30, 10, false)
	c.Settle()
	srv.TickPortals()
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))

	// The 3.1-block drop between ticks is more than the box is tall (1.8), so
	// the end-of-tick box alone misses the gateway cell.
	c.MoveToGrounded(3.5, gatewayY-2.1, 3.5, 30, 10, false)
	c.Settle()
	srv.TickPortals()
	c.AwaitPlay(protocol.IDPlaySyncPosition)
}

// TestEndGatewaySweepDoesNotCrossATeleport checks the tick after a /tp
// sweeps from the destination, not along the line from the old place, which
// runs through a gateway.
// vanilla: Entity.clearMovementThisTick
func TestEndGatewaySweepDoesNotCrossATeleport(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate", servertest.OpsListing(t, "Gate"))
	srv.StopTick()
	c.MoveToGrounded(-2.5, gatewayY, 3.5, 30, 10, false)
	c.Settle()
	srv.TickPortals()
	srv.SetLoadedBlock(8, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(8, gatewayY, 3, 30, gatewayY, 3, true)
	srv.SetLoadedBlock(30, gatewayY, 3, servertest.BlockState(t, "minecraft:stone"))

	c.ChatCommand("tp @s 30.5 90 3.5")
	c.AwaitPlay(protocol.IDPlaySyncPosition) // the reader accepts it
	srv.TickPortals()
	if _, teleported := gatewayBeams(t, c, 8, 3); teleported {
		t.Error("the tick after /tp swept the old line through the gateway")
	}
}

// TestEndGatewaySweepIsCapped checks a move far longer than the sweep's cell
// cap does not reach a gateway beyond it.
// vanilla: Entity.maxMovementIterations
func TestEndGatewaySweepIsCapped(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	srv.StopTick()
	c.MoveToGrounded(-30.5, gatewayY, 3.5, 30, 10, false)
	c.Settle()
	srv.TickPortals()
	srv.SetLoadedBlock(0, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(0, gatewayY, 3, 20, gatewayY, 3, true)
	srv.SetLoadedBlock(20, gatewayY, 3, servertest.BlockState(t, "minecraft:stone"))

	c.MoveToGrounded(20.5, gatewayY, 3.5, 30, 10, false)
	c.Settle()
	srv.TickPortals()
	if _, teleported := gatewayBeams(t, c, 0, 3); teleported {
		t.Error("a sweep of 51 cells reached a gateway past the 16-cell cap")
	}
}

// TestEndGatewayCarriesAThrownPearlToTheExit checks a pearl flying in a
// gateway moves to the exit, stopped, and that the gateway starts its beam.
// vanilla: EndGatewayBlock.getPortalDestination
func TestEndGatewayCarriesAThrownPearlToTheExit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	srv.StopTick()
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.ThrowThrown(entity.ThrownEnderPearl, 3.5, gatewayY+0.5, 3.5, 0, 0, 0, 1)

	srv.TickEntities(1)
	if beams, _ := gatewayBeams(t, c, 3, 3); beams != 1 {
		t.Fatalf("a pearl in the gateway gave %d beams, want one", beams)
	}
	srv.WaitGatewayMoves()
	thrown := srv.ThrownInDimension("minecraft:overworld")
	if len(thrown) != 1 || thrown[0].X != 20.5 || thrown[0].Y != 120 || thrown[0].Z != 20.5 {
		t.Fatalf("pearl is at %+v, want (20.5, 120, 20.5)", thrown)
	}
}

// gatewayWithExit stops the loop and sets an exact exit at (20.5, 120, 20.5)
// for a gateway at (3, gatewayY, 3).
func gatewayWithExit(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.StopTick()
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
}

// TestEndGatewayCarriesAnItemAndAMobToTheExit checks the other bodies a
// gateway moves, each once: an item entity and a mob.
// vanilla: EndGatewayBlock.entityInside
func TestEndGatewayCarriesAnItemAndAMobToTheExit(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	gatewayWithExit(t, srv)
	srv.SpawnItemEntityMotion(3.5, gatewayY+0.5, 3.5, "minecraft:stick", 1, 0.2, 0, 0)
	mob := srv.SpawnMob("minecraft:cow", 3.5, gatewayY, 3.5)

	srv.TickEntities(1)
	// The item's touch cooled the gateway; the mob's waits it out.
	srv.AdvanceTicksRun(40)
	srv.TickMobs(3)
	srv.WaitGatewayMoves()
	items := srv.ItemsInDimension("minecraft:overworld")
	x, y, z, _ := srv.MobPos(mob)
	// The mob keeps its fall, so it may have dropped a little since landing.
	// The landing keeps the velocity and puts the box on the exit's floor.
	itemMoved := len(items) == 1 && items[0].X == 20.5 && items[0].Z == 20.5 &&
		items[0].VX > 0 && items[0].Y > 120 && items[0].Y < 120.5
	if !itemMoved || x != 20.5 || y <= 119 || y > 120 || z != 20.5 {
		t.Fatalf("items %+v and mob at (%v, %v, %v), want both at the exit", items, x, y, z)
	}
}

// TestEndGatewaySendsABeamEvery2400TicksOfAge checks an idle gateway ticks
// its age and sends the beam as the age reaches a multiple of 2400, then
// stays quiet while it cools.
// vanilla: TheEndGatewayBlockEntity.portalTick
func TestEndGatewaySendsABeamEvery2400TicksOfAge(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	gatewayWithExit(t, srv)
	srv.SetEndGatewayAge(3, gatewayY, 3, 2398)

	srv.TickBlockEntities()
	if beams, _ := gatewayBeams(t, c, 3, 3); beams != 0 {
		t.Fatalf("age 2399 gave %d beams, want none", beams)
	}
	srv.AdvanceTicksRun(1)
	srv.TickBlockEntities()
	if beams, _ := gatewayBeams(t, c, 3, 3); beams != 1 {
		t.Fatalf("age 2400 gave %d beams, want one", beams)
	}
}

// TestEndGatewayWithoutAnExitBuildsOneInTheEnd checks a pearl in an End
// gateway with no exit is sent about 1024 blocks out along the gateway's
// direction, where a return gateway now leads back.
// vanilla: TheEndGatewayBlockEntity.getPortalPosition, findOrCreateValidTeleportPos
func TestEndGatewayWithoutAnExitBuildsOneInTheEnd(t *testing.T) {
	t.Parallel()
	const end = "minecraft:the_end"
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	srv.StopTick()
	release := srv.HoldChunkInDimension(end, 0, 0)
	defer release()
	// The test world keeps nothing of an unloaded chunk, so the stretch the
	// exit can fall in stays loaded.
	for cx := int32(40); cx <= 48; cx++ {
		for cz := int32(40); cz <= 48; cz++ {
			defer srv.HoldChunkInDimension(end, cx*16, cz*16)()
		}
	}
	srv.SetBlockInDimension(end, 3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.ThrowEnderPearlInDimension(end, 3.5, gatewayY+0.5, 3.5, 0, 0, 0, uuid.Nil, 1)

	srv.TickEntities(1)
	srv.WaitGatewayMoves()
	ex, ey, ez, ok := srv.EndGatewayExitInDimension(end, 3, gatewayY, 3)
	if !ok {
		t.Fatal("the gateway got no exit")
	}
	if ex < 400 || ez < 400 {
		t.Errorf("exit at (%d, %d, %d), want about 1024 blocks out along (1, 1)", ex, ey, ez)
	}
	if srv.BlockInDimension(end, ex, ey, ez) != servertest.BlockState(t, "minecraft:end_gateway") {
		t.Errorf("no return gateway at the exit (%d, %d, %d): state %d, want %d", ex, ey, ez, srv.BlockInDimension(end, ex, ey, ez), servertest.BlockState(t, "minecraft:end_gateway"))
	}
	bx, by, bz, back := srv.EndGatewayExitInDimension(end, ex, ey, ez)
	if !back || bx != 3 || by != gatewayY || bz != 3 {
		t.Errorf("return gateway leads to (%d, %d, %d) ok=%v, want (3, %d, 3)", bx, by, bz, back, gatewayY)
	}
}
