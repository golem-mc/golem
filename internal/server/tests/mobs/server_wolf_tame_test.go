package mobs

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The wolf's tame side, black box: the bone's roll, the sitting toggle, the
// follow and the teleport, and the owner's quarrels.

const wolfType = "minecraft:wolf"

// wolfScene joins a survival player holding bones at (0.5, 64, 0.5) with
// the loop held.
func wolfScene(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, _, c := wolfSceneWith(t, name, servertest.MobTestConfig())
	return srv, c
}

// wolfSceneWith is wolfScene on the config given, also handing back the
// function that connects another client.
func wolfSceneWith(t *testing.T, name string, cfg *config.Config) (*server.Server, func() *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c := connect().Join(name)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bone"), Count: 64})
	c.Settle()
	return srv, connect, c
}

// tamable reads the wolf's state, failing if it is gone.
func tamable(t *testing.T, srv *server.Server, id int32) server.TestTamable {
	t.Helper()
	st, ok := srv.MobTamable(id)
	if !ok {
		t.Fatal("the wolf is not in the herd")
	}
	return st
}

// wolfStatuses runs the click and reads the tame statuses the wolf was told
// to show, in order.
func useAndStatuses(c *servertest.Client, srv *server.Server, id int32) []uint8 {
	c.T.Helper()
	c.UseOn(id)
	var got []uint8
	c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadI32() == id {
			got = append(got, r.ReadU8())
		}
		return false
	})
	return got
}

// TestABoneTamesAWolfAndSitsIt: bones are spent one a click, each click
// showing smoke or hearts, until a roll succeeds; the tamed wolf is the
// player's, sits, and has forty health.
// vanilla: Wolf.mobInteract, tryToTame
func TestABoneTamesAWolfAndSitsIt(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	spent, smoke := 0, 0
	for ; !tamable(t, srv, id).Tame; spent++ {
		if spent == 60 {
			t.Fatal("60 bones did not tame the wolf")
		}
		got := useAndStatuses(c, srv, id)
		switch {
		case len(got) == 1 && got[0] == protocol.EntityEventTamed:
		case len(got) == 1 && got[0] == protocol.EntityEventTameFailed:
			smoke++
		default:
			t.Fatalf("a bone drew statuses %v, want one of smoke or hearts", got)
		}
		if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; int(left) != 64-spent-1 {
			t.Fatalf("after %d bones the hand holds %d, want one spent a click", spent+1, left)
		}
	}
	if smoke != spent-1 {
		t.Errorf("%d clicks, %d smoke, want every click but the last to fail", spent, smoke)
	}
	st := tamable(t, srv, id)
	if st.Owner != protocol.OfflineUUID("Owner").String() {
		t.Errorf("owner = %s, want the clicking player", st.Owner)
	}
	if !st.OrderedToSit {
		t.Error("a freshly tamed wolf stands, want it sitting")
	}
	if hp, _ := srv.MobHealth(id); hp != 40 || st.MaxHealth != 40 {
		t.Errorf("tamed wolf health %v of %v, want 40 of 40", hp, st.MaxHealth)
	}
	// A bone on a tame wolf is the toggle, not another roll.
	before := srv.InventorySlot("Owner", player.HotbarSlot).Count
	if got := useAndStatuses(c, srv, id); len(got) != 0 {
		t.Errorf("a bone on a tame wolf drew statuses %v, want none", got)
	}
	if after := srv.InventorySlot("Owner", player.HotbarSlot).Count; after != before {
		t.Errorf("a bone on a tame wolf was spent: %d to %d", before, after)
	}
	if tamable(t, srv, id).OrderedToSit {
		t.Error("the owner's click did not stand the wolf up")
	}
}

// TestOnlyTheOwnerTogglesASittingWolf: an empty hand from the owner flips
// the order, a stranger's does nothing.
// vanilla: Wolf.mobInteract
func TestOnlyTheOwnerTogglesASittingWolf(t *testing.T) {
	t.Parallel()
	srv, connect, owner := wolfSceneWith(t, "Owner", servertest.MobTestConfig())
	other := connect().Join("Stranger")
	owner.ExpectPlayerAdded("Stranger")
	srv.GiveSlot("Stranger", player.HotbarSlot, protocol.ItemStack{})
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
	other.UseOn(id)
	other.Settle()
	if tamable(t, srv, id).OrderedToSit {
		t.Fatal("a stranger sat the wolf")
	}
	owner.UseOn(id)
	owner.Settle()
	if !tamable(t, srv, id).OrderedToSit {
		t.Fatal("the owner's click did not sit the wolf")
	}
	other.UseOn(id)
	other.Settle()
	if !tamable(t, srv, id).OrderedToSit {
		t.Fatal("a stranger stood the wolf up")
	}
}

func dist(ax, az, bx, bz float64) float64 { return math.Hypot(ax-bx, az-bz) }

// TestATameWolfFollowsItsOwnerUnlessSitting: a standing wolf ten blocks off
// walks to within a few of its owner; a sitting one stays where it is.
// vanilla: FollowOwnerGoal
func TestATameWolfFollowsItsOwnerUnlessSitting(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	walker := srv.SpawnMob(wolfType, 10.5, 64, 0.5)
	sitter := srv.SpawnMob(wolfType, 2.5, 64, 0.5)
	for _, id := range []int32{walker, sitter} {
		srv.TameMobFor(id, "Owner")
	}
	c.UseOn(sitter)
	c.Settle()
	if !tamable(t, srv, sitter).OrderedToSit {
		t.Fatal("the owner's click did not sit the wolf")
	}
	srv.MoveMob(sitter, 10.5, 4.5)
	// Once close the wolf strolls again, so the nearest it came is read.
	nearest := math.Inf(1)
	for range 120 {
		servertest.RunTicks(t, srv, 1)
		x, _, z, _ := srv.MobPos(walker)
		nearest = min(nearest, dist(x, z, 0.5, 0.5))
	}
	if nearest > 3 {
		t.Errorf("the standing wolf came no nearer than %.1f blocks to its owner, want within 3", nearest)
	}
	sx, _, sz, _ := srv.MobPos(sitter)
	if sx != 10.5 || sz != 4.5 {
		t.Errorf("the sitting wolf moved to (%v, %v)", sx, sz)
	}
	if !tamable(t, srv, sitter).SittingPose {
		t.Error("the sitting wolf is not in the sitting pose")
	}
}

// TestATameWolfFarFromItsOwnerTeleportsToThem: from twelve blocks the wolf
// is put two or three blocks off the owner.
// vanilla: TamableAnimal.tryToTeleportToOwner
func TestATameWolfFarFromItsOwnerTeleportsToThem(t *testing.T) {
	t.Parallel()
	srv, _ := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 15.5, 64, 0.5)
	srv.TameMobFor(id, "Owner")
	servertest.RunTicks(t, srv, 8)
	x, _, z, _ := srv.MobPos(id)
	d := dist(x, z, 0.5, 0.5)
	if d > 5 || (math.Abs(x-0.5) < 2 && math.Abs(z-0.5) < 2) {
		t.Errorf("the wolf stands at (%v, %v), %.1f from its owner, want a spot two or three blocks off", x, z, d)
	}
}

// TestAPanickingTameWolfFarFromItsOwnerTeleportsToThem: the panic holds the
// move flag, so the teleport comes from the panic goal itself.
// vanilla: TamableAnimal.TamableAnimalPanicGoal.tick
func TestAPanickingTameWolfFarFromItsOwnerTeleportsToThem(t *testing.T) {
	t.Parallel()
	srv, _ := wolfScene(t, "Owner")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(wolfType, 20.5, 64, 0.5)
	srv.TameMobFor(id, "Owner")
	srv.HurtMobByWorld(id, 1, "minecraft:in_fire")
	servertest.RunTicks(t, srv, 4)
	x, _, z, _ := srv.MobPos(id)
	if d := dist(x, z, 0.5, 0.5); d > 5 {
		t.Errorf("the panicking wolf stands at (%v, %v), %.1f from its owner, want within 5", x, z, d)
	}
}

// TestATameWolfTakesUpItsOwnersQuarrels: the wolf targets whoever struck its
// owner and whoever its owner struck, but not while sitting, and its pack
// mate of another owner is not called.
// vanilla: OwnerHurtByTargetGoal, OwnerHurtTargetGoal
func TestATameWolfTakesUpItsOwnersQuarrels(t *testing.T) {
	t.Parallel()
	for _, sitting := range []bool{false, true} {
		for _, byAttacker := range []bool{true, false} {
			srv, c := wolfScene(t, "Owner")
			srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
			wolf := srv.SpawnMob(wolfType, 3.5, 64, 3.5)
			srv.TameMobFor(wolf, "Owner")
			foe := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
			srv.SetMobNoAI(foe)
			if sitting {
				c.UseOn(wolf)
				c.Settle()
			}
			if byAttacker {
				srv.StrikePlayerWith(foe, "Owner", 1)
			} else {
				c.Attack(foe)
				c.Settle()
			}
			servertest.RunTicks(t, srv, 6)
			want := foe
			if sitting {
				want = 0
			}
			if got := srv.MobTargetID(wolf); got != want {
				t.Errorf("sitting=%v struck=%v: the wolf targets %d, want %d", sitting, byAttacker, got, want)
			}
		}
	}
}

// TestATameWolfIsSavedWithItsOwner: the file keeps the owner and the order.
// vanilla: TamableAnimal.addAdditionalSaveData
func TestATameWolfIsSavedWithItsOwner(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _, c := wolfSceneWith(t, "Owner", cfg)
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
	id := srv.SpawnMob(wolfType, 3.5, 64, 3.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	c.UseOn(id)
	c.Settle()
	uid, _ := srv.MobUUID(id)
	srv.FlushMobs()
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)[uuid.MustParse(uid)]
	if saved == nil {
		t.Fatal("the wolf is not in the saved entities")
	}
	words, _ := nbt.Get[nbt.IntArray](saved, "Owner")
	if got := servertest.FixtureUUID(words...); got != protocol.OfflineUUID("Owner") {
		t.Errorf("saved owner = %v, want the owner's UUID", got)
	}
	if sitting, _ := nbt.Get[nbt.Byte](saved, "Sitting"); sitting != 1 {
		t.Errorf("saved Sitting = %d, want 1", sitting)
	}
}

// wolfMeta is the tame fields of a wolf's Set Entity Metadata packet.
type wolfMeta struct {
	flags    int
	collar   int
	owner    uuid.UUID
	hasOwner bool
	// begging and begTold are the interested flag and whether it was sent.
	begging, begTold bool
	// angerEnd and angerTold are the anger end time (index 22) and whether it
	// was sent.
	angerEnd  int64
	angerTold bool
}

// readWolfMeta parses the entries a wolf's metadata carries, keeping the
// tamable byte (18) and the owner reference (19).
func readWolfMeta(t *testing.T, r *protocol.Reader) wolfMeta {
	t.Helper()
	m := wolfMeta{flags: -1, collar: -1}
	for {
		index := r.ReadU8()
		if index == 0xFF {
			break
		}
		switch typ := r.ReadVarInt(); typ {
		case protocol.MetadataByte:
			v := r.ReadU8()
			if index == protocol.MetadataIndexTamableFlags {
				m.flags = int(v)
			}
		case protocol.MetadataVarInt:
			if v := r.ReadVarInt(); index == protocol.MetadataIndexWolfCollar {
				m.collar = int(v)
			}
		case protocol.MetadataVarLong:
			if v := r.ReadVarLong(); index == protocol.MetadataIndexWolfAngerEnd {
				m.angerEnd, m.angerTold = v, true
			}
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			v := r.ReadBool()
			if index == protocol.MetadataIndexWolfBegging {
				m.begging, m.begTold = v, true
			}
		case protocol.MetadataOptionalLivingEntityReference:
			if m.hasOwner = r.ReadBool(); m.hasOwner {
				m.owner = r.ReadUUID()
			}
		default:
			if !servertest.IsVariantMetadata(typ) {
				t.Fatalf("wolf metadata entry %d has type %d, which the test does not read", index, typ)
			}
			r.ReadVarInt()
		}
		if err := r.Err(); err != nil {
			t.Fatalf("wolf metadata: %v", err)
		}
	}
	return m
}

// TestATamedWolfShowsItsOwnerAndItsSitting: the client is told the tame bit
// and the owner when the bone lands, and the sitting bit once the wolf has
// sat.
// vanilla: TamableAnimal.DATA_FLAGS_ID, DATA_OWNERUUID_ID
func TestATamedWolfShowsItsOwnerAndItsSitting(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	c.Settle()
	for !tamable(t, srv, id).Tame {
		c.UseOn(id)
		c.Settle()
	}
	var last *wolfMeta
	c.ReadTicks(srv, 3, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		m := readWolfMeta(t, r)
		if m.flags >= 0 {
			last = &m
		}
		return false
	})
	if last == nil {
		t.Fatal("no metadata told the tame bit")
	}
	if last.flags != protocol.TamableTame|protocol.TamableSitting {
		t.Errorf("tamable flags = %#x, want tame and sitting", last.flags)
	}
	if !last.hasOwner || last.owner != protocol.OfflineUUID("Owner") {
		t.Errorf("owner = %v (%v), want the clicking player", last.owner, last.hasOwner)
	}
}

// TestATeleportingWolfAvoidsLavaFireAndWater: with lava, water or fire in
// the feet cell of every spot around the owner, the wolf is not put in any
// of them and stays where it was.
// vanilla: TamableAnimal.canTeleportTo
func TestATeleportingWolfAvoidsLavaFireAndWater(t *testing.T) {
	t.Parallel()
	srv, _ := wolfScene(t, "Owner")
	stone := servertest.BlockState(t, "minecraft:stone")
	hazards := []uint16{
		servertest.BlockState(t, "minecraft:lava"),
		servertest.BlockState(t, "minecraft:water"),
		servertest.BlockState(t, "minecraft:fire"),
	}
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			for y := int32(62); y <= 65; y++ {
				srv.SetBlock(x, y, z, 0)
			}
			srv.SetBlock(x, 63, z, stone)
			srv.SetBlock(x, 64, z, hazards[(x+z+8)%3])
		}
	}
	id := srv.SpawnMob(wolfType, 15.5, 64, 0.5)
	srv.TameMobFor(id, "Owner")
	servertest.RunTicks(t, srv, 2)
	if x, _, _, _ := srv.MobPos(id); x < 10 {
		t.Errorf("the wolf was put at x=%v, in a hazard", x)
	}
}

// TestATeleportingWolfAvoidsHazardsBesideTheSpot: lava or water in a cell
// next to an otherwise free spot rules the spot out, so with a hazard in
// every other column the wolf is not put beside one.
// vanilla: TamableAnimal.canTeleportTo, WalkNodeEvaluator.checkNeighbourBlocks
func TestATeleportingWolfAvoidsHazardsBesideTheSpot(t *testing.T) {
	t.Parallel()
	for _, hazard := range []string{"minecraft:lava", "minecraft:water"} {
		t.Run(hazard, func(t *testing.T) {
			t.Parallel()
			srv, _ := wolfScene(t, "Owner")
			stone := servertest.BlockState(t, "minecraft:stone")
			h := servertest.BlockState(t, hazard)
			for x := int32(-4); x <= 4; x++ {
				for z := int32(-4); z <= 4; z++ {
					for y := int32(62); y <= 65; y++ {
						srv.SetBlock(x, y, z, 0)
					}
					srv.SetBlock(x, 63, z, stone)
					if (x+z)%2 == 0 {
						srv.SetBlock(x, 64, z, h)
					}
				}
			}
			id := srv.SpawnMob(wolfType, 15.5, 64, 0.5)
			srv.TameMobFor(id, "Owner")
			servertest.RunTicks(t, srv, 2)
			if x, _, _, _ := srv.MobPos(id); x < 10 {
				t.Errorf("the wolf was put at x=%v, beside %s", x, hazard)
			}
		})
	}
}

// TestATeleportingWolfAvoidsPathTypeBlocksAroundTheSpot: a rail, cocoa,
// powder snow or pointed dripstone in the feet cell of every spot, or a
// dripstone beside every other spot, leaves the wolf where it was.
// vanilla: TamableAnimal.canTeleportTo, WalkNodeEvaluator.getPathTypeFromState
func TestATeleportingWolfAvoidsPathTypeBlocksAroundTheSpot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, block string
		alternate   bool
	}{
		{"rail", "minecraft:rail", false},
		{"cocoa", "minecraft:cocoa", false},
		{"powder snow", "minecraft:powder_snow", false},
		{"dripstone in the cell", "minecraft:pointed_dripstone", false},
		{"dripstone beside", "minecraft:pointed_dripstone", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := wolfScene(t, "Owner")
			stone := servertest.BlockState(t, "minecraft:stone")
			h := servertest.BlockState(t, c.block)
			if c.block == "minecraft:cocoa" {
				h = servertest.BlockStateWith(t, c.block, "age=0,facing=east")
			}
			for x := int32(-4); x <= 4; x++ {
				for z := int32(-4); z <= 4; z++ {
					for y := int32(62); y <= 65; y++ {
						srv.SetBlock(x, y, z, 0)
					}
					srv.SetBlock(x, 63, z, stone)
					if c.block == "minecraft:cocoa" {
						// A roof, so the wolf has no ledge on the logs or the cocoa to stand on.
						srv.SetBlock(x, 65, z, stone)
					}
					switch {
					case c.block == "minecraft:cocoa" && x%2 != 0:
						// The cocoa hangs on the log beside it (x+1 is east).
						srv.SetBlock(x, 64, z, servertest.BlockState(t, "minecraft:jungle_log"))
					case !c.alternate || (x+z)%2 == 0:
						srv.SetBlock(x, 64, z, h)
					}
				}
			}
			id := srv.SpawnMob(wolfType, 15.5, 64, 0.5)
			srv.TameMobFor(id, "Owner")
			servertest.RunTicks(t, srv, 2)
			if x, _, _, _ := srv.MobPos(id); x < 10 {
				t.Errorf("the wolf was put at x=%v, by %s", x, c.name)
			}
		})
	}
}

// TestATamedWolfsDeathIsToldToItsOwnerOnly: the owner gets the death message
// as system chat, naming the wolf; others get nothing, and with
// show_death_messages off nobody does.
// vanilla: TamableAnimal.die
func TestATamedWolfsDeathIsToldToItsOwnerOnly(t *testing.T) {
	t.Parallel()
	for _, shown := range []bool{true, false} {
		t.Run(map[bool]string{true: "shown", false: "hidden"}[shown], func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, connect, owner := wolfSceneWith(t, "Owner", cfg)
			other := connect().Join("Other")
			other.Settle()
			owner.Settle()
			srv.SetGameRule(world.RuleShowDeathMsg.String(), shown)
			id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
			srv.SetMobNoAI(id)
			if !srv.TameMobFor(id, "Owner") {
				t.Fatal("wolf not tamed")
			}
			identity, ok := srv.MobUUID(id)
			if !ok {
				t.Fatal("wolf has no UUID")
			}
			var want protocol.Buffer
			want.PutText(protocol.Text{Translate: "death.attack.genericKill", With: []protocol.Text{
				servertest.EntityDisplayComponent(wolfType, "entity.minecraft.wolf", uuid.MustParse(identity)),
			}})
			want.PutBool(false)
			srv.KillMob(id)
			servertest.RunTicks(t, srv, 1)
			told := func(c *servertest.Client) (got bool) {
				c.SettleAndCollect(func(pid int32, data []byte) {
					if pid == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("death.attack.genericKill")) {
						got = true
						if !bytes.Equal(data, want.Bytes()) {
							t.Errorf("pet death message = % X, want % X", data, want.Bytes())
						}
					}
				})
				return got
			}
			if got := told(owner); got != shown {
				t.Errorf("owner told %v, want %v", got, shown)
			}
			if told(other) {
				t.Error("a stranger was told of the wolf's death")
			}
		})
	}
}

// A tamed wolf a player hurt and the fire then killed is told to its owner
// with the player named as the kill credit.
// vanilla: TamableAnimal.die, LivingEntity.getKillCredit
func TestATamedWolfsFireDeathNamesThePlayerWhoHurtIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, connect, owner := wolfSceneWith(t, "Owner", cfg)
	other := connect().Join("Other")
	other.Settle()
	owner.Settle()
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	if !srv.TameMobFor(id, "Owner") {
		t.Fatal("wolf not tamed")
	}
	srv.SetMobHealth(id, 2)
	for range 20 {
		srv.TickSurvival()
	}
	other.Attack(id)
	other.Settle()
	if hp, _ := srv.MobHealth(id); hp <= 0 || hp >= 2 {
		t.Fatalf("wolf health after the hit = %v, want hurt but alive", hp)
	}
	srv.HurtMobByWorld(id, 10, "minecraft:on_fire")
	servertest.RunTicks(t, srv, 1)
	var msg []byte
	owner.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("death.attack.onFire")) {
			msg = data
		}
	})
	for _, want := range []string{"death.attack.onFire.player", "entity.minecraft.wolf", "Other"} {
		if !bytes.Contains(msg, []byte(want)) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

// A tamed wolf killed by a fall is told to its owner through its combat
// tracker: a fall of 5 blocks or less is death.attack.fall, a longer one off a
// ladder names the ladder, and a hurt more than 100 ticks before a long fall
// has left the tracker, so the fall is an accident.
// vanilla: TamableAnimal.die, CombatTracker.getDeathMessage
func TestATamedWolfsFallDeathIsWordedByItsCombatTracker(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		y      float64
		ladder bool
		// cactus hurts the wolf on the ground at y 64 and drops it from y
		// after the tracker's 100-tick window.
		cactus bool
		want   string
	}{
		{"four blocks", 68, false, false, "death.attack.fall"},
		{"off a ladder", 72, true, false, "death.fell.accident.ladder"},
		{"long after a cactus", 74, false, true, "death.fell.accident.generic"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _, owner := wolfSceneWith(t, "Owner", cfg)
			if c.ladder {
				ladder := servertest.MustState(t, "minecraft:ladder", map[string]string{"facing": "north", "waterlogged": "false"})
				if !srv.SetBlock(1, int32(c.y), 0, ladder) {
					t.Fatal("the ladder was not placed")
				}
			}
			spawnY := c.y
			if c.cactus {
				spawnY = 64
			}
			id := srv.SpawnMob(wolfType, 1.5, spawnY, 0.5)
			if !srv.TameMobFor(id, "Owner") {
				t.Fatal("wolf not tamed")
			}
			srv.SetMobSitting(id, true)
			if c.cactus {
				srv.HurtMobByWorld(id, 1, "minecraft:cactus")
				servertest.RunTicks(t, srv, 110)
				srv.PlaceMob(id, 1.5, c.y, 0.5)
			}
			srv.SetMobHealth(id, 1)
			servertest.RunTicks(t, srv, 60)
			if _, ok := srv.MobHealth(id); ok {
				t.Fatal("the wolf survived the fall")
			}
			var got []string
			owner.SettleAndCollect(func(pid int32, data []byte) {
				if pid != protocol.IDPlaySystemChat {
					return
				}
				for _, key := range []string{"death.attack.fall", "death.fell.accident.ladder", "death.fell.accident.generic"} {
					if bytes.Contains(data, []byte(key)) {
						got = append(got, key)
						if !bytes.Contains(data, []byte("entity.minecraft.wolf")) {
							t.Errorf("message %q does not name the wolf", data)
						}
					}
				}
			})
			if len(got) != 1 || got[0] != c.want {
				t.Errorf("owner told %v, want [%s]", got, c.want)
			}
		})
	}
}

// TestATameWolfSparesTameAnimalsAndGhasts: the owner striking its own tame
// cat, parrot or camel, or a ghast, sets the wolf on none of them; a wild
// cat it takes up.
// vanilla: Wolf.wantsToAttack, OwnerHurtTargetGoal
func TestATameWolfSparesTameAnimalsAndGhasts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  string
		tame  func(srv *server.Server, id int32)
		spare bool
	}{
		{"minecraft:cat", func(srv *server.Server, id int32) { srv.TameMobFor(id, "Owner") }, true},
		{"minecraft:parrot", func(srv *server.Server, id int32) { srv.TameMobFor(id, "Owner") }, true},
		{"minecraft:camel", func(srv *server.Server, id int32) { srv.TameHorse(id) }, true},
		{"minecraft:ghast", nil, true},
		{"minecraft:cat", nil, false},
	} {
		srv, c := wolfScene(t, "Owner")
		srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
		wolf := srv.SpawnMob(wolfType, 3.5, 64, 3.5)
		srv.TameMobFor(wolf, "Owner")
		foe := srv.SpawnMob(tc.kind, 2.5, 64, 0.5)
		srv.SetMobNoAI(foe)
		if tc.tame != nil {
			tc.tame(srv, foe)
		}
		c.Attack(foe)
		c.Settle()
		servertest.RunTicks(t, srv, 6)
		want := foe
		if tc.spare {
			want = 0
		}
		if got := srv.MobTargetID(wolf); got != want {
			t.Errorf("%s tame=%v: the wolf targets %d, want %d", tc.kind, tc.tame != nil, got, want)
		}
	}
}
