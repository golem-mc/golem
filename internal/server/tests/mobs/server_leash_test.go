package mobs

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// The leash tests: a lead ties a cow to the player that used it, the tie
// arriving as a Set Entity Link; a mob at arm's length is pulled back by
// the elastic stretch of the lead, and one walked twelve blocks out drops
// the lead back as the item it is; a fence holds the lead as a leash knot,
// which hands its mob back over and comes down with its fence's loss; a
// monster refuses the lead, and a holder who dies lets go of the animals.
// The readers here are the mob tests' in spirit but their own in
// tolerance: the lead's uses swing an arm and answer a block click, and a
// holder's death interrupts, and the waiting steps over all of that the
// way the mob tests step over the hurt of a landed hit.

// giveLead puts a stack of leads in the hotbar slot a use holds, so each
// of these tests starts its cow a lead's reach away with the means to tie
// it.
func giveLead(t *testing.T, srv *server.Server, name string, count int32) {
	t.Helper()
	lead := servertest.ItemIDOrDie(t, "minecraft:lead")
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: lead, Count: count})
}

// TestUseLeadOnACowTiesItToThePlayer checks the lead's use on a cow: the
// hold arrives as a Set Entity Link naming the cow attached to the player
// that used it, and one lead of the stack is spent.
func TestUseLeadOnACowTiesItToThePlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != cowID {
		t.Errorf("the cow's spawn = entity %d, want the one the server spawned (%d)", cow.EntityID, cowID)
	}

	c.UseOn(cowID)
	// The tie's telling: the hold naming the cow on its holder, and the
	// hand's spent count — whichever order the wire carries them in.
	attached, holding, slot, stack := c.ExpectTieAnswer()
	if attached != cowID {
		t.Errorf("the link attaches entity %d, want the cow %d", attached, cowID)
	}
	if holding != c.PlayerEntityID {
		t.Errorf("the link holds entity %d, want the player's own %d", holding, c.PlayerEntityID)
	}
	if slot != int16(player.HotbarSlot) {
		t.Errorf("the spent lead was told to slot %d, want the held %d", slot, player.HotbarSlot)
	}
	if stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || stack.Count != 1 {
		t.Errorf("the hand after the tie = %+v, want one of the two leads", stack)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the server's slot holds %+v, want one lead", got)
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("the mob count after the tie = %d, want the one cow", got)
	}
}

// TestHostileMobsRefuseALead checks vanilla's Mob.canBeLeashed: a zombie
// is a monster, and a lead used on one is unhappened — no link, nothing
// spent — where the same lead on a cow beside it ties fine.
func TestHostileMobsRefuseALead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tamer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Tamer", 3)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 6.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	// The control: the cow ties.
	c.UseOn(cowID)
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != c.PlayerEntityID {
		t.Fatalf("the cow's link holds (%d, %d), want the cow %d on the holder", attached, holding, cowID)
	}
	// The tie's own slot update follows its link onto the wire; read it
	// here, or the refusal below collects it as a spend.
	c.Settle()
	if got := srv.InventorySlot("Tamer", player.HotbarSlot); got.Count != 2 {
		t.Fatalf("the hand after the tie = %+v, want two of the three leads", got)
	}

	// The refusal: the zombie beside it neither ties nor spends.
	c.UseOn(zombieID)
	srv.TickMobs(3)
	var linked, spent bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlaySetEntityLink:
			linked = true
		case protocol.IDPlayContainerSlot:
			spent = true
		}
	})
	if linked {
		t.Error("a lead on a zombie tied a link, which monsters refuse")
	}
	if spent {
		t.Error("the lead was spent on a zombie, which refuses it")
	}
	if got := srv.InventorySlot("Tamer", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the hand after the refused lead = %+v, want the two leads the tie left", got)
	}
}

// TestFishTadpolesAndBatsRefuseALead checks that a lead on a cod, tadpole or
// bat neither ties nor is spent, while a squid beside them ties.
// vanilla: WaterAnimal.canBeLeashed, AmbientCreature.canBeLeashed, Squid.canBeLeashed
func TestFishTadpolesAndBatsRefuseALead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tamer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Tamer", 3)
	codID := srv.SpawnMob("minecraft:cod", 3.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCod)
	tadpoleID := srv.SpawnMob("minecraft:tadpole", 4.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeTadpole)
	batID := srv.SpawnMob("minecraft:bat", 2.5, 64, 6.5)
	c.ExpectMobSpawn(protocol.EntityTypeBat)
	squidID := srv.SpawnMob("minecraft:squid", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeSquid)

	for name, id := range map[string]int32{"cod": codID, "tadpole": tadpoleID, "bat": batID} {
		c.UseOn(id)
		srv.TickMobs(3)
		var linked, spent bool
		c.SettleAndCollect(func(pid int32, _ []byte) {
			switch pid {
			case protocol.IDPlaySetEntityLink:
				linked = true
			case protocol.IDPlayContainerSlot:
				spent = true
			}
		})
		if linked || spent {
			t.Errorf("a lead on a %s: linked=%v spent=%v, want neither", name, linked, spent)
		}
		if got := srv.InventorySlot("Tamer", player.HotbarSlot); got.Count != 3 {
			t.Errorf("the hand after a lead on a %s = %+v, want three leads", name, got)
		}
	}

	c.UseOn(squidID)
	if attached, holding := c.ExpectLinkPacket(); attached != squidID || holding != c.PlayerEntityID {
		t.Fatalf("the squid's link holds (%d, %d), want the squid %d on the holder", attached, holding, squidID)
	}
}

// TestTheLeashedMobIsPulledTowardItsHolder is the elastic stretch: a cow
// held eight blocks out is dragged back by the lead, arriving closer to
// the holder than it started.
// vanilla: Leashable.checkElasticInteractions
func TestTheLeashedMobIsPulledTowardItsHolder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the pre-walk link holds entity %d, want the holder", holding)
	}

	// Eight blocks out: past the elastic band the lead slack allows, well
	// short of the twelve it snaps at.
	c.MoveToGround(-2.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(15)

	x, _, z, ok := srv.MobPos(cowID)
	if !ok {
		t.Fatal("the cow is gone from the world")
	}
	me := srv.PlayerPosition("Herder")
	walked := servertest.Abs64(me.X-5.5) + servertest.Abs64(me.Z-5.5)
	arrived := servertest.Abs64(me.X-x) + servertest.Abs64(me.Z-z)
	if arrived >= walked {
		t.Errorf("the cow stands %f from its holder after fifteen ticks, want it dragged closer than the %f it started", arrived, walked)
	}
}

// TestALeashedCowComesAfterItsHolder checks that a cow inside the elastic
// slack walks to within two blocks of its holder and takes the lead's home.
// vanilla: PathfinderMob.closeRangeLeashBehaviour, PathfinderMob.whenLeashedTo
func TestALeashedCowComesAfterItsHolder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	c.ExpectLinkPacket()

	c.MoveToGround(1.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(60)

	x, _, _, ok := srv.MobPos(cowID)
	if !ok {
		t.Fatal("the cow is gone from the world")
	}
	// The walk ends within navReach of the cell two blocks short (x 3.5), so
	// the cow halts in cell 4.
	if x > 4.9 {
		t.Errorf("the cow stands at x %f, want it walked in from 5.5 to cell 4", x)
	}
	hx, hy, hz, r, set, _ := srv.MobHome(cowID)
	if hx != 1 || hy != 64 || hz != 5 || r != 5 || !set {
		t.Errorf("home = (%d, %d, %d) radius %d set %v, want (1, 64, 5) radius 5 set", hx, hy, hz, r, set)
	}
}

// TestTheLeadSnapsTwelveBlocksOut checks the reach of a lead: walked
// thirteen blocks away, the holder's pull ends in the lead dropping back
// as the item it is, at the mob that was tied, and no link held.
func TestTheLeadSnapsTwelveBlocksOut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the pre-walk link holds entity %d, want the holder", holding)
	}
	cowX, cowY, cowZ, _ := srv.MobPos(cowID)

	// Thirteen blocks: past the twelve a lead runs out at.
	c.MoveToGround(-7.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(2)

	// The lead drops at the mob: an item reading back as the lead, laid
	// ahead of the hold's telling gone — the item first is the vanilla
	// order.
	// vanilla: Leashable.tickLeash, Leashable.dropLeash
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	if servertest.Abs64(drop.X-cowX) > 1.5 || servertest.Abs64(drop.Z-cowZ) > 1.5 || servertest.Abs64(drop.Y-cowY) > 1.5 {
		t.Errorf("the lead dropped at (%f, %f, %f), want the cow's feet (%f, %f, %f)",
			drop.X, drop.Y, drop.Z, cowX, cowY, cowZ)
	}
	attached, holding := c.ExpectLinkPacket()
	if attached != cowID {
		t.Errorf("the detach links entity %d, want the cow %d", attached, cowID)
	}
	if holding != 0 {
		t.Errorf("the detach holds entity %d, want none", holding)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the hand after the snap = %+v, want the one lead not spent on the tie", got)
	}
}

// TestAHappyGhastsLeadHoldsPastTwelveBlocks checks the happy ghast's longer
// lead: fourteen blocks out it still holds, with a 9-block home, and
// twenty-two out it snaps.
// vanilla: HappyGhast.leashSnapDistance, PathfinderMob.whenLeashedTo
func TestAHappyGhastsLeadHoldsPastTwelveBlocks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	id := srv.SpawnMob("minecraft:happy_ghast", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeHappyGhast)
	c.UseOn(id)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie's link holds entity %d, want the holder", holding)
	}

	// Fourteen blocks: past a cow's twelve, inside the ghast's sixteen.
	c.MoveToGround(-8.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(2)
	if holder, ok := srv.MobLeashHolder(id); !ok || holder != c.PlayerEntityID {
		t.Fatalf("fourteen blocks out the ghast's lead holder = %d (ok %v), want the player %d",
			holder, ok, c.PlayerEntityID)
	}
	if _, _, _, r, set, _ := srv.MobHome(id); r != 9 || !set {
		t.Errorf("home radius %d set %v, want 9", r, set)
	}

	// Twenty-two blocks from the tie: the elastic pull has drawn the ghast
	// about two blocks in, and this is still past sixteen from there.
	c.MoveToGround(-16.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(2)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
}

// TestTheHolderTakesTheirLeashBack checks the second use on a tied mob:
// the holder's own click unties the lead, which drops back as the item at
// the mob's feet, and the hold is told gone.
func TestTheHolderTakesTheirLeashBack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	cowX, cowY, cowZ, _ := srv.MobPos(cowID)

	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	// The untie: the lead drops at the mob before the hold is told gone —
	// the item first, the packet after, vanilla's order.
	// vanilla: Leashable.dropLeash
	c.UseOn(cowID)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	if servertest.Abs64(drop.X-cowX) > 1.5 || servertest.Abs64(drop.Z-cowZ) > 1.5 || servertest.Abs64(drop.Y-cowY) > 1.5 {
		t.Errorf("the lead dropped at (%f, %f, %f), want the cow's feet", drop.X, drop.Y, drop.Z)
	}
	attached, holding := c.ExpectLinkPacket()
	if attached != cowID || holding != 0 {
		t.Errorf("the untie links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	// The tie spent one of the two; the untie is the holder's own and
	// spends none.
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the hand after the untie = %+v, want the one lead the tie left", got)
	}
}

// TestALeadOnAFenceTiesTheMobToAKnot checks the fence half of the lead: a
// holder that right-clicks a fence with a lead moves its tied mob onto a
// knot the fence carries — no lead spent, the mob's hold told to the
// knot, the knot shown as the entity it is — and a use on the knot hands
// the mob back to the holder, the knot coming down with the last of it.
func TestALeadOnAFenceTiesTheMobToAKnot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// A fence beside the cow, stood by the command rather than a click, so
	// the hand is the lead's alone through the test.
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	// The lead's use on the fence: the mob moves onto the knot it stands.
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	// The spawn packet carries the fence's cell corner, vanilla's own
	// getAddEntityPacket, not the body's standing centre.
	if servertest.Abs64(knot.X-6.0) > 1e-6 || servertest.Abs64(knot.Y-64.0) > 1e-6 || servertest.Abs64(knot.Z-5.0) > 1e-6 {
		t.Errorf("the knot spawn sits at (%f, %f, %f), want the fence's corner (6, 64, 5)", knot.X, knot.Y, knot.Z)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != knot.EntityID {
		t.Errorf("the fence's link holds (%d, %d), want the cow %d on the knot %d",
			attached, holding, cowID, knot.EntityID)
	}
	// The click on the fence spent nothing: the lead is still one in hand.
	c.Settle()
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || got.Count != 1 {
		t.Errorf("the hand after the knot = %+v, want the one lead the fence did not spend", got)
	}

	srv.StopTick()
	// The use on the knot hands the mob back to the holder, and with the
	// last of its mobs gone the knot comes down.
	c.UseOn(knot.EntityID)
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != c.PlayerEntityID {
		t.Errorf("the take-back links (%d, %d), want the cow %d on its holder", attached, holding, cowID)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stayed up after the last of its mobs was taken back")
	}
	if _, _, _, ok := srv.MobPos(cowID); !ok {
		t.Error("the cow did not survive being handed back")
	}
}

// TestASneakingLeadStillTiesTheMobToAFence checks vanilla's split between
// the fence's own use and the lead's: the sneak key skips only the block's
// useWithoutItem, and the lead's useOn — run for a lead in hand — still
// ties the mobs the holder leads onto the knot the fence carries.
// vanilla: ServerPlayerGameMode.useItemOn, LeadItem.useOn
func TestASneakingLeadStillTiesTheMobToAFence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	// The sneak held over the fence click: the block's own use is skipped,
	// the held lead's useOn still runs. The pose metadata the sneak draws
	// comes in before the click does.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != knot.EntityID {
		t.Errorf("the sneaking fence click links (%d, %d), want the cow %d on the knot %d",
			attached, holding, cowID, knot.EntityID)
	}
}

// TestASneakingItemOtherThanALeadBuilds checks the sneak's other half: a
// sneak with anything but a lead skips the fence's own useWithoutItem, so
// the mobs the player leads stay on their holder, and runs that item's use
// instead, laying the block against the fence. Only LeadItem.useOn ties.
// vanilla: ServerPlayerGameMode.useItemOn, LeadItem.useOn
func TestASneakingItemOtherThanALeadBuilds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	// The player switches to stone and sneaks: the fence's use is skipped
	// and the lead is never consulted, so the click lays the stone on the
	// fence's top face and leaves the cow on the player.
	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stone"), Count: 1})
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
	c.UseBlock(6, 64, 5)
	c.Settle()

	if state := srv.BlockAt(6, 65, 5); state == 0 {
		t.Error("the sneaking stone click placed no block on the fence's top face")
	}
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the cow's holder after the stone click = (%d, %v), want the player %d",
			holder, ok, c.PlayerEntityID)
	}
}

// TestAKnotPopsWhenItsFenceGoes checks the knot's footing: a fence taken
// out from under its knot brings the knot down on the periodic survives
// check, and the mob it held drops its lead back as the item.
// vanilla: BlockAttachedEntity.tick
func TestAKnotPopsWhenItsFenceGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	c.ExpectLinkPacket()
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	c.ExpectLinkPacket()
	cowX, cowY, cowZ, _ := srv.MobPos(cowID)

	srv.StopTick()
	c.ChatCommand("setblock 6 64 5 minecraft:air")
	c.Settle()
	// The survives poll runs every hundred ticks of the knot's own life,
	// so the pump walks past a whole hundred of them.
	for range 105 {
		srv.TickKnots(1)
		srv.TickMobs(1)
	}

	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Fatal("the knot of a lost fence stayed up")
	}
	// The mob it held drops the lead at itself, and the hold is told gone.
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	if servertest.Abs64(drop.X-cowX) > 1.5 || servertest.Abs64(drop.Z-cowZ) > 1.5 || servertest.Abs64(drop.Y-cowY) > 1.5 {
		t.Errorf("the lead dropped at (%f, %f, %f), want the cow's feet (%f, %f, %f)",
			drop.X, drop.Y, drop.Z, cowX, cowY, cowZ)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the knot's fall links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
}

// TestACreativeHolderSpendsAndDropsNothing checks the creative halves of
// the tie: the hand that ties out of an infinite purse keeps its own lead,
// and its untie lets go without laying one down.
func TestACreativeHolderSpendsAndDropsNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The tie: the link arrives, the hand keeps its lead.
	c.UseOn(cowID)
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != c.PlayerEntityID {
		t.Fatalf("the creative tie links (%d, %d), want the cow %d on its holder", attached, holding, cowID)
	}
	var handTold bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayContainerSlot {
			handTold = true
		}
	})
	if handTold {
		t.Error("the creative tie told the hand a count, which it does not spend")
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the server's slot after the tie = %+v, want the one lead kept", got)
	}

	// The untie: the hold is told gone, nothing is laid down.
	c.UseOn(cowID)
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the creative untie links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	var laid bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			laid = true
		}
	})
	if laid {
		t.Error("the creative untie laid the lead down, which it does not")
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("the mob count after the untie = %d, want the one cow", got)
	}
}

// TestAKilledHolderLetsGoOfItsLeash checks the holder's part in the tie:
// a player that dies in the world they led animals in lets the lead go —
// it drops at the mob, and no link is held after them.
// vanilla: Entity.canInteractWithLevel, Leashable.tickLeash
func TestAKilledHolderLetsGoOfItsLeash(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	cowX, cowY, cowZ, _ := srv.MobPos(cowID)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.Hurt("Herder", 1000)
	srv.TickMobs(2)

	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the dropped lead = %+v, want one lead", drop.Stack)
	}
	// The lead lands at the cow's feet: the wire carries an item's feet,
	// and the server's own position is the box centre the feet sit half an
	// item below.
	if servertest.Abs64(drop.X-cowX) > 1e-6 || servertest.Abs64(drop.Y-cowY) > 1e-6 || servertest.Abs64(drop.Z-cowZ) > 1e-6 {
		t.Errorf("the lead dropped at (%f, %f, %f), want the cow's feet (%f, %f, %f)",
			drop.X, drop.Y, drop.Z, cowX, cowY, cowZ)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the holder's death links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
}

// TestAnyHandTiesTheMobToTheFence checks the fence block's own use: a
// player that spent the last lead on a cow still ties it to the fence's
// knot with a hand holding something other than a lead, the block doing
// the tying for any hand rather than the lead item's own use.
// vanilla: FenceBlock.useWithoutItem
func TestAnyHandTiesTheMobToTheFence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	// The player switches to something that is not a lead: the fence still
	// takes the click and stands the knot.
	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:stick"), Count: 1})

	srv.StopTick()
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != knot.EntityID {
		t.Errorf("the non-lead fence use links (%d, %d), want the cow %d on the knot %d",
			attached, holding, cowID, knot.EntityID)
	}
}

// TestAKilledLeashedMobLetsGoOfItsLead checks a dying leashable: a cow tied
// to a knot that is killed drops its lead at itself, the hold told gone,
// and leaves the knot with nothing to hold, so it comes down.
// vanilla: Entity.canInteractWithLevel, Leashable.tickLeash
func TestAKilledLeashedMobLetsGoOfItsLead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	c.ExpectLinkPacket()
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	c.ExpectLinkPacket()
	cowX, cowY, cowZ, _ := srv.MobPos(cowID)

	srv.StopTick()
	if !srv.KillMob(cowID) {
		t.Fatal("the kill found no cow to kill")
	}
	srv.TickMobs(2)

	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the killed mob dropped %+v, want one lead", drop.Stack)
	}
	if servertest.Abs64(drop.X-cowX) > 1.5 || servertest.Abs64(drop.Y-cowY) > 1.5 || servertest.Abs64(drop.Z-cowZ) > 1.5 {
		t.Errorf("the lead dropped at (%f, %f, %f), want the cow's feet", drop.X, drop.Y, drop.Z)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the death links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot that held the killed cow stayed up")
	}
}

// TestEntityDropsDoesNotGateASnap checks that a lead run out lays its item
// whatever entity_drops says: only a holder that cannot hold is gated.
// vanilla: Leashable.tickLeash, Leashable.dropLeash
func TestEntityDropsDoesNotGateASnap(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	srv.SetGameRule(world.RuleEntityDrops.String(), false)

	// Thirteen blocks: past the twelve a lead runs out at.
	c.MoveToGround(-7.5, 64, 5.5, true)
	c.Settle()
	srv.TickMobs(2)

	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the snap with entity_drops off dropped %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the snap links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
}

// TestEntityDropsGatesALostHolder checks the one path that consults the
// rule: a holder that died drops no lead with entity_drops off, though the
// hold is still told gone.
// vanilla: Leashable.tickLeash
func TestEntityDropsGatesALostHolder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	srv.SetGameRule(world.RuleEntityDrops.String(), false)

	srv.Hurt("Herder", 1000)
	srv.TickMobs(2)

	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the lost holder links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	var laid bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			laid = true
		}
	})
	if laid {
		t.Error("the lost holder with entity_drops off laid a lead, which it should not")
	}
}

// TestAHolderInAnotherDimensionKeepsItsLeash checks the level half of the
// holder check: a player that crosses to the nether is neither dead nor
// gone, so the cow left in the overworld keeps its lead — vanilla only
// skips the pull across the two levels.
// vanilla: Leashable.tickLeash
func TestAHolderInAnotherDimensionKeepsItsLeash(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect().Join("Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// A second player stays behind in the overworld, keeping the cow's
	// chunk loaded while its holder crosses away.
	watcher := connect().Join("Watcher")
	watcher.Settle()
	// Drain the first player's view of the watcher's join.
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.MovePlayerToDimension("Herder", world.NetherName, 5, 64, 5)
	if got := srv.PlayerDimension("Herder"); got != world.NetherName {
		t.Fatalf("the holder's dimension = %q, want the nether", got)
	}
	srv.TickMobs(3)

	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the cow's holder after the holder crossed = %d (%v), want the player %d still holding it",
			holder, ok, c.PlayerEntityID)
	}
}

// TestALeashedMobBoardingABoatLosesItsLead checks Mob.startRiding's own
// drop: the tick that sweeps a leashed cow into a boat takes its lead off,
// laying the item and telling the hold gone.
// vanilla: Mob.startRiding, AbstractBoat.tick
func TestALeashedMobBoardingABoatLosesItsLead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.TickBoats(1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.TickBoats(1)
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != cowID {
		t.Fatalf("the boat's passengers = %v, want the cow %d pushed aboard", got, cowID)
	}
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the boarded cow dropped %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the boarding links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
}

// TestTyingASeatedMobSetsItOffTheBoat checks setLeashedTo's stopRiding: a
// cow already seated in a boat and then tied to a player is set off the
// hull, the seat told empty, and stays leashed.
// vanilla: Leashable.setLeashedTo
func TestTyingASeatedMobSetsItOffTheBoat(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.TickBoats(1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.TickBoats(1)
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != cowID {
		t.Fatalf("the boat's passengers = %v, want the cow %d seated", got, cowID)
	}
	c.Settle()

	giveLead(t, srv, "Herder", 1)
	c.UseOn(cowID)
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != c.PlayerEntityID {
		t.Errorf("the tie links (%d, %d), want the cow %d on its holder", attached, holding, cowID)
	}
	if got := srv.BoatPassengerIDs(boatID); len(got) != 0 {
		t.Errorf("the boat still holds %v after the tie, want the cow set off", got)
	}
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the seated cow's holder = %d (%v), want the player %d", holder, ok, c.PlayerEntityID)
	}
}

// TestALeadTiesABoatAndItsHolderLetsItGo checks the hull as a leashable in
// its own right: a lead's use ties the boat to the player that used it —
// the hold arriving as a Set Entity Link — and the player's own click lets
// it go, the lead dropping back as the item.
// vanilla: Entity.interact, AbstractBoat.interact
func TestALeadTiesABoatAndItsHolderLetsItGo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	boatID := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.TickBoats(1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	giveLead(t, srv, "Herder", 3)
	c.UseOn(boatID)
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != c.PlayerEntityID {
		t.Fatalf("the boat's tie links (%d, %d), want the boat %d on the player %d",
			attached, holding, boatID, c.PlayerEntityID)
	}
	if holder, ok := srv.BoatLeashHolder(boatID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the boat's holder = %d (%v), want the player %d", holder, ok, c.PlayerEntityID)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the hand after the tie = %+v, want two of the three leads", got)
	}

	// The holder's own click lets it go: the lead drops at the hull and the
	// hold is told gone.
	c.UseOn(boatID)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") || drop.Stack.Count != 1 {
		t.Errorf("the unleashing dropped %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != boatID || holding != 0 {
		t.Errorf("the untie links (%d, %d), want the boat %d held by none", attached, holding, boatID)
	}
	if holder, ok := srv.BoatLeashHolder(boatID); !ok || holder != 0 {
		t.Errorf("the boat's holder after the untie = %d (%v), want none", holder, ok)
	}
}

// TestALeashedMobCrossingAPortalDropsItsLead checks the lead a dimension
// travel carries: the twin holds it as an unresolved lead while no holder
// of its new dimension claims it, and once the hundred-tick grace passes the
// lead drops, laid at the mob in the nether.
// vanilla: Entity.teleportCrossDimension, Leashable.restoreLeashFromSave
func TestALeashedMobCrossingAPortalDropsItsLead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	// A nether portal block at the cow's cell, placed direct: a non-player
	// crosses on its first tick standing in one.
	_, portalX, _, _ := servertest.PortalStates(t)
	if !srv.SetBlock(6, 64, 5, portalX) {
		t.Fatal("placing the portal block failed")
	}

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 6.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.TickMobs(1)
	mobs := srv.MobsInDimension(world.NetherName)
	if len(mobs) != 1 || mobs[0].Kind != "minecraft:cow" {
		t.Fatalf("the nether holds %+v, want the one cow that crossed", mobs)
	}
	twinID := mobs[0].ID
	// No player is in the nether, so its chunk is forced for the twin to
	// tick (vanilla: DistanceManager.inEntityTickingRange).
	srv.ForceChunkInDimension(world.NetherName, int32(math.Floor(mobs[0].X))>>4, int32(math.Floor(mobs[0].Z))>>4)
	// Frozen, so the twin cannot wander off the arrival platform.
	srv.SetMobNoAI(twinID)
	if holder, ok := srv.MobLeashHolderInDimension(world.NetherName, twinID); !ok || holder != c.PlayerEntityID {
		t.Fatalf("the twin's holder = %d (%v), want the player %d still holding it", holder, ok, c.PlayerEntityID)
	}

	// The holder stayed behind, so the lead waits out the hundred-tick
	// grace, still held at fifty.
	srv.TickMobs(50)
	if holder, ok := srv.MobLeashHolderInDimension(world.NetherName, twinID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the twin's holder after fifty ticks = %d (%v), want the player %d", holder, ok, c.PlayerEntityID)
	}
	srv.TickMobs(51)
	if holder, ok := srv.MobLeashHolderInDimension(world.NetherName, twinID); !ok || holder != 0 {
		t.Errorf("the twin's holder after the grace = %d (%v), want the lead let go", holder, ok)
	}
	// The lead lands in the portal the arrival stands in, and a dropped
	// item crosses on its first tick in one, so it may have come back to
	// the overworld by the time it is read. Either side holds it.
	found := false
	for _, dim := range []string{world.NetherName, world.DimensionName} {
		for _, it := range srv.ItemsInDimension(dim) {
			if it.Item == "minecraft:lead" && it.Count == 1 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("neither dimension holds the dropped lead: nether %+v, overworld %+v",
			srv.ItemsInDimension(world.NetherName), srv.ItemsInDimension(world.DimensionName))
	}
}

// TestALeashedMobKeepsItsLeadWhenTheHolderFollows checks the other end of the
// carried lead: a holder that crosses into the mob's new dimension within the
// grace claims the twin's lead, so it stays held instead of dropping.
// vanilla: Entity.teleportCrossDimension, Leashable.restoreLeashFromSave
func TestALeashedMobKeepsItsLeadWhenTheHolderFollows(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	_, portalX, _, _ := servertest.PortalStates(t)
	if !srv.SetBlock(6, 64, 5, portalX) {
		t.Fatal("placing the portal block failed")
	}

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 6.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.TickMobs(1)
	mobs := srv.MobsInDimension(world.NetherName)
	if len(mobs) != 1 || mobs[0].Kind != "minecraft:cow" {
		t.Fatalf("the nether holds %+v, want the one cow that crossed", mobs)
	}
	twinID := mobs[0].ID

	// The holder follows to the twin within the grace: a holder of the
	// mob's own dimension claims the lead, which then rides out the whole
	// grace and stays.
	srv.MovePlayerToDimension("Herder", world.NetherName, mobs[0].X, mobs[0].Y, mobs[0].Z)
	srv.TickMobs(1)
	if holder, ok := srv.MobLeashHolderInDimension(world.NetherName, twinID); !ok || holder != c.PlayerEntityID {
		t.Fatalf("the twin's holder after the holder followed = %d (%v), want the player %d", holder, ok, c.PlayerEntityID)
	}
	srv.TickMobs(101)
	if holder, ok := srv.MobLeashHolderInDimension(world.NetherName, twinID); !ok || holder != c.PlayerEntityID {
		t.Errorf("the twin's holder after the grace = %d (%v), want the player %d still holding it", holder, ok, c.PlayerEntityID)
	}
}

// leashedCowOnAKnot stands a fence at (x, 64, 5), ties the holder's cow to
// it and moves the cow onto the knot the fence carries, returning the cow
// and the knot. The holder stands where moveToGround put them.
func leashedCowOnAKnot(t *testing.T, srv *server.Server, c *servertest.Client, x int32) (int32, servertest.SpawnedKnot) {
	t.Helper()
	c.ChatCommand(fmt.Sprintf("setblock %d 64 5 minecraft:oak_fence", x))
	c.Settle()
	giveLead(t, srv, "Herder", 2)
	cowID := srv.SpawnMob("minecraft:cow", float64(x)-0.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	c.UseBlock(x, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != knot.EntityID {
		t.Fatalf("the fence's link holds (%d, %d), want the cow %d on the knot %d",
			attached, holding, cowID, knot.EntityID)
	}
	return cowID, knot
}

// TestAKnotBroughtDownByAnAttack checks the knot's hurt: a player's swing in
// reach brings it down with the untie said where it stood, and the mob it
// held drops its lead.
// vanilla: BlockAttachedEntity.hurtServer
func TestAKnotBroughtDownByAnAttack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	_, knot := leashedCowOnAKnot(t, srv, c, 6)
	srv.StopTick()
	// Into the knot's reach before the swing; out of it the blow is spent.
	c.MoveTo(5.0, 64, 5.5, 0, 0)
	c.Settle()
	c.Attack(knot.EntityID)
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Fatal("the knot stood after a blow in reach")
	}
	srv.TickMobs(1)
	drop := c.ExpectLeashDrop()
	if drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") {
		t.Errorf("the mob dropped %+v, want the lead the knot's fall let go", drop.Stack)
	}
}

// TestAKnotBroughtDownByAnArrow checks the knot's hurt by a shaft: a
// player's arrow into its box brings it down, and the mob it held drops its
// lead.
// vanilla: BlockAttachedEntity.hurtServer
func TestAKnotBroughtDownByAnArrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	_, _ = leashedCowOnAKnot(t, srv, c, 6)
	srv.StopTick()
	if id := srv.ShootPlayerArrow("Herder", 2.5, 64.4, 5.5, 10, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}
	for range 40 {
		if srv.KnotCount() == 0 {
			break
		}
		srv.RunTick()
	}
	if got := srv.KnotCount(); got != 0 {
		t.Errorf("the knot count after a player's shaft = %d, want it down", got)
	}
}

// TestAKnotBroughtDownByAnExplosion checks the knot's hurt by a blast: one
// in range brings it down, and the mob it held drops its lead.
// vanilla: BlockAttachedEntity.hurtServer
func TestAKnotBroughtDownByAnExplosion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	_, knot := leashedCowOnAKnot(t, srv, c, 6)
	srv.StopTick()
	srv.BlastAt(6.5, 64.375, 5.5, 1)
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Fatal("the knot stood after a blast in range")
	}
}

// TestAKnotPollsForItsFenceOnTheHundredAndFirstTick checks the knot's
// survives poll: the fence's loss is only seen on the tick after the
// hundred-tick interval, the knot standing through the hundredth.
// vanilla: BlockAttachedEntity.tick
func TestAKnotPollsForItsFenceOnTheHundredAndFirstTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// The loop stops before the knot stands, so its poll count starts at
	// zero and the hundredth tick is the hundredth of its life.
	srv.StopTick()
	_, _ = leashedCowOnAKnot(t, srv, c, 6)
	c.ChatCommand("setblock 6 64 5 minecraft:air")
	c.Settle()

	srv.TickKnots(100)
	if got := srv.KnotCount(); got != 1 {
		t.Errorf("the knot count on the hundredth tick = %d, want it still standing", got)
	}
	srv.TickKnots(1)
	if got := srv.KnotCount(); got != 0 {
		t.Errorf("the knot count on the hundred-and-first tick = %d, want it down", got)
	}
}

// TestShearsCutALead: shears on a leashed cow lay
// the lead, tell the hold gone and wear the shears one point.
// vanilla: Entity.interact, Entity.shearOffAllLeashConnections
func TestShearsCutALead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shears"), Count: 1})
	c.UseOn(cowID)
	if drop := c.ExpectLeashDrop(); drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") {
		t.Errorf("the dropped item = %+v, want a lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != cowID || holding != 0 {
		t.Errorf("the cut links (%d, %d), want the cow %d held by none", attached, holding, cowID)
	}
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != 0 {
		t.Errorf("the cow's holder after the shears = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}
}

// TestShearsCutTheLeadsOfAKnot: shears on a knot lay the lead of the mob it
// holds and bring the emptied knot down.
// vanilla: LeashFenceKnotEntity.interact, Entity.shearOffAllLeashConnections
func TestShearsCutTheLeadsOfAKnot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	cowID, knot := leashedCowOnAKnot(t, srv, c, 6)
	srv.StopTick()
	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shears"), Count: 1})
	c.UseOn(knot.EntityID)
	if drop := c.ExpectLeashDrop(); drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") {
		t.Errorf("the dropped item = %+v, want a lead", drop.Stack)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stood after its last lead was cut")
	}
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != 0 {
		t.Errorf("the cow's holder after the shears = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}
}

// TestShearsOnAKnotedSheepCutTheLeadAndKeepTheWool: shears on a sheep held by
// a knot cut the lead and bring the emptied knot down, and the sheep keeps its
// wool.
// vanilla: Entity.interact, Entity.shearOffAllLeashConnections
func TestShearsOnAKnotedSheepCutTheLeadAndKeepTheWool(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.ChatCommand("setblock 6 64 5 minecraft:oak_fence")
	c.Settle()
	giveLead(t, srv, "Herder", 1)
	sheepID := srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeSheep)
	c.UseOn(sheepID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}
	c.UseBlock(6, 64, 5)
	knot := c.ExpectKnotSpawn()
	if attached, holding := c.ExpectLinkPacket(); attached != sheepID || holding != knot.EntityID {
		t.Fatalf("the fence's link holds (%d, %d), want the sheep %d on the knot %d",
			attached, holding, sheepID, knot.EntityID)
	}

	srv.GiveSlot("Herder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shears"), Count: 1})
	c.UseOn(sheepID)
	if drop := c.ExpectLeashDrop(); drop.Stack.ItemID != servertest.ItemIDOrDie(t, "minecraft:lead") {
		t.Errorf("the dropped item = %+v, want a lead", drop.Stack)
	}
	if !c.AwaitKnotRemoval(knot.EntityID) {
		t.Error("the knot stood after its last lead was cut")
	}
	if holder, ok := srv.MobLeashHolder(sheepID); !ok || holder != 0 {
		t.Errorf("the sheep's holder after the shears = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
	}
	// Neither wool nor the sheared flag may follow the cut.
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySpawnEntity:
			t.Errorf("an entity spawned after the cut: % X", data)
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() == sheepID {
				t.Errorf("the sheep's metadata changed after the cut: % X", data)
			}
		}
	})
}

// TestADispenserCutsTheLeadOfACow: a dispenser's shears cut the lead of a
// mob it cannot shear and wear one point.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity
func TestADispenserCutsTheLeadOfACow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.ChatCommand("setblock 5 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	servertest.HoldTicks(t, srv)

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	if !srv.SetInventoryAt(5, 64, 4, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != 0 {
		t.Errorf("the cow's holder after the dispense = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Empty() || got.Damage != 1 {
		t.Errorf("the dispenser's shears = %+v, want them worn one point", got)
	}
}

// TestADispenserCutsTheLeadOfABoat: a dispenser's shears cut a boat's lead
// and wear one point.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity
func TestADispenserCutsTheLeadOfABoat(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.ChatCommand("setblock 5 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	servertest.HoldTicks(t, srv)

	boat := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	servertest.RunTicks(t, srv, 1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	giveLead(t, srv, "Herder", 1)
	c.UseOn(boat)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	if !srv.SetInventoryAt(5, 64, 4, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	if holder, ok := srv.BoatLeashHolder(boat); !ok || holder != 0 {
		t.Errorf("the boat's holder after the dispense = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Empty() || got.Damage != 1 {
		t.Errorf("the dispenser's shears = %+v, want them worn one point", got)
	}
}

// TestADispenserCutsTheLeadAPlayerInTheCellHolds: a dispenser's shears cut
// the lead held by a player standing in the target cell and wear one point.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity, Entity.shearOffAllLeashConnections
func TestADispenserCutsTheLeadAPlayerInTheCellHolds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(5.5, 64, 5.5, true)
	c.ChatCommand("setblock 5 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	servertest.HoldTicks(t, srv)

	giveLead(t, srv, "Herder", 1)
	cowID := srv.SpawnMob("minecraft:cow", 9.5, 64, 9.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(cowID)
	if _, holding := c.ExpectLinkPacket(); holding != c.PlayerEntityID {
		t.Fatalf("the tie holds entity %d, want the holder", holding)
	}

	if !srv.SetInventoryAt(5, 64, 4, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(5, 64, 4, protocol.FaceSouth)
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != 0 {
		t.Errorf("the cow's holder after the dispense = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventoryAt(5, 64, 4)[0]; got.Empty() || got.Damage != 1 {
		t.Errorf("the dispenser's shears = %+v, want them worn one point", got)
	}
}

// TestADispenserCutsTheLeadsOfAKnot: a dispenser's shears cut the leads a
// knot in the cell holds and wear one point.
// vanilla: ShearsDispenseItemBehavior.tryShearEntity
func TestADispenserCutsTheLeadsOfAKnot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.ChatCommand("setblock 6 64 4 minecraft:dispenser[facing=south]")
	c.Settle()
	servertest.HoldTicks(t, srv)

	cowID, _ := leashedCowOnAKnot(t, srv, c, 6)
	if !srv.SetInventoryAt(6, 64, 4, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(6, 64, 4, protocol.FaceSouth)
	if holder, ok := srv.MobLeashHolder(cowID); !ok || holder != 0 {
		t.Errorf("the cow's holder after the dispense = (%d, %v), want none", holder, ok)
	}
	if got := srv.InventoryAt(6, 64, 4)[0]; got.Empty() || got.Damage != 1 {
		t.Errorf("the dispenser's shears = %+v, want them worn one point", got)
	}
}

// tieCowToCow sets up a lead on cow a, led by the player, and a second cow
// b beside it, then sneak-clicks b: a's lead moves onto b.
func tieCowToCow(t *testing.T) (*server.Server, *servertest.Client, int32, int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	a := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	b := srv.SpawnMob("minecraft:cow", 3.5, 64, 7.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(a)
	c.ExpectLinkPacket()
	c.UseOnSneaking(b)
	if attached, holding := c.ExpectLinkPacket(); attached != a || holding != b {
		t.Fatalf("the sneaking click links (%d, %d), want (%d, %d)", attached, holding, a, b)
	}
	return srv, c, a, b
}

// TestASneakingClickTiesTheLedMobOntoAnotherMob checks Entity.interact's
// first branch: the leads the player holds move onto the sneak-clicked mob,
// spending nothing, and stay there.
func TestASneakingClickTiesTheLedMobOntoAnotherMob(t *testing.T) {
	t.Parallel()
	srv, _, a, b := tieCowToCow(t)
	if holder, _ := srv.MobLeashHolder(a); holder != b {
		t.Errorf("the led cow's holder = %d, want %d", holder, b)
	}
	if got := srv.InventorySlot("Herder", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand = %+v, want it empty", got)
	}
	srv.TickMobs(5)
	if holder, _ := srv.MobLeashHolder(a); holder != b {
		t.Errorf("after ticks the holder = %d, want %d", holder, b)
	}
}

// TestALeadOnAMobDropsWhenThatMobDies checks a mob-held lead falls when its
// holder dies.
func TestALeadOnAMobDropsWhenThatMobDies(t *testing.T) {
	t.Parallel()
	srv, c, a, b := tieCowToCow(t)
	if !srv.KillMob(b) {
		t.Fatal("the kill found no cow to kill")
	}
	srv.TickMobs(2)
	drop := c.ExpectLeashDrop()
	if drop.Stack.Count != 1 {
		t.Errorf("the drop = %+v, want one lead", drop.Stack)
	}
	if attached, holding := c.ExpectLinkPacket(); attached != a || holding != 0 {
		t.Errorf("the drop links (%d, %d), want (%d, 0)", attached, holding, a)
	}
}

// TestASneakingClickOnAnAngryWolfMovesNoLead checks that the clicked mob's own
// canBeLeashed gates the move: an angry wolf takes no lead from the player.
// vanilla: Entity.interact, Wolf.canBeLeashed
func TestASneakingClickOnAnAngryWolfMovesNoLead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	giveLead(t, srv, "Herder", 1)
	a := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	wolf := srv.SpawnMob("minecraft:wolf", 3.5, 64, 7.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectMobSpawn(protocol.EntityTypeWolf)
	b := srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.UseOn(a)
	c.ExpectLinkPacket()
	if !srv.AngerMob(wolf, 400) {
		t.Fatal("the wolf could not be angered")
	}
	c.UseOnSneaking(wolf)
	// Clicks are handled in order: the cow's tie is the first link after the wolf's click.
	c.UseOnSneaking(b)
	if attached, holding := c.ExpectLinkPacket(); attached != a || holding != b {
		t.Errorf("the first link after the clicks is (%d, %d), want (%d, %d): the wolf took the lead", attached, holding, a, b)
	}
}
