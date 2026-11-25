package mobs

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

const parrotType = "minecraft:parrot"

// parrotScene joins a survival player holding `held` at (0.5, 64, 0.5) with
// the loop held.
func parrotScene(t *testing.T, held string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, held), Count: count})
	c.Settle()
	return srv, c
}

// TestSeedsTameAParrotAndTheOwnerSitsIt: seeds are spent one a click, each
// showing smoke or hearts, until the parrot is the player's; the owner's next
// click toggles sitting.
// vanilla: Parrot.mobInteract
func TestSeedsTameAParrotAndTheOwnerSitsIt(t *testing.T) {
	t.Parallel()
	srv, c := parrotScene(t, "minecraft:wheat_seeds", 64)
	id := srv.SpawnMob(parrotType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	spent := 0
	for ; !tamable(t, srv, id).Tame; spent++ {
		if spent == 200 {
			t.Fatal("200 seeds did not tame the parrot")
		}
		got := useAndStatuses(c, srv, id)
		if len(got) != 1 || got[0] != protocol.EntityEventTamed && got[0] != protocol.EntityEventTameFailed {
			t.Fatalf("a seed drew statuses %v, want one of smoke or hearts", got)
		}
		if left := srv.InventorySlot("Keeper", player.HotbarSlot).Count; int(left) != 64-spent-1 {
			t.Fatalf("after %d seeds the hand holds %d, want one spent a click", spent+1, left)
		}
	}
	st := tamable(t, srv, id)
	if st.Owner != protocol.OfflineUUID("Keeper").String() {
		t.Errorf("owner = %s, want the clicking player", st.Owner)
	}
	if st.OrderedToSit {
		t.Error("a freshly tamed parrot sits, want it standing")
	}
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	c.UseOn(id)
	c.Settle()
	if !tamable(t, srv, id).OrderedToSit {
		t.Error("the owner's click did not sit the parrot")
	}
	c.UseOn(id)
	c.Settle()
	if tamable(t, srv, id).OrderedToSit {
		t.Error("the owner's second click did not stand the parrot")
	}
}

// perchedParrot tames a parrot at the player's feet, sets its variant, and
// runs the loop until it has landed on the shoulder.
func perchedParrot(t *testing.T, srv *server.Server, variant int) int32 {
	t.Helper()
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 2)
	srv.SetParrotVariant(id, variant)
	if !srv.TameMobFor(id, "Keeper") {
		t.Fatal("could not tame the parrot")
	}
	servertest.UntilTicks(t, srv, 130, "the parrot lands on the shoulder", func() bool {
		left, _ := srv.ShoulderParrots("Keeper")
		return left >= 0
	})
	return id
}

// TestATamedParrotLandsOnItsOwnersShoulder: a tamed standing parrot meeting
// its owner leaves the world once its 100-tick cooldown ran, and the owner's
// left shoulder carries its variant.
// vanilla: LandOnOwnersShoulderGoal, ShoulderRidingEntity.setEntityOnShoulder
func TestATamedParrotLandsOnItsOwnersShoulder(t *testing.T) {
	t.Parallel()
	srv, _ := parrotScene(t, "minecraft:air", 0)
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 2)
	srv.SetParrotVariant(id, 3)
	srv.TameMobFor(id, "Keeper")
	servertest.RunTicks(t, srv, 60)
	if left, _ := srv.ShoulderParrots("Keeper"); left >= 0 {
		t.Fatalf("the parrot landed after 60 ticks, want it to wait out its cooldown")
	}
	servertest.UntilTicks(t, srv, 60, "the parrot lands", func() bool {
		left, _ := srv.ShoulderParrots("Keeper")
		return left >= 0
	})
	left, right := srv.ShoulderParrots("Keeper")
	if left != 3 || right != -1 {
		t.Errorf("shoulders = %d, %d, want variant 3 on the left only", left, right)
	}
	if _, ok := srv.MobHealth(id); ok {
		t.Error("the parrot is still in the world")
	}
}

// TestHurtingTheOwnerDropsTheShoulderParrot: a blow to the owner puts the
// parrot back into the world, tame to them, once it has sat a second.
// vanilla: ServerPlayer.removeEntitiesOnShoulder
func TestHurtingTheOwnerDropsTheShoulderParrot(t *testing.T) {
	t.Parallel()
	srv, _ := parrotScene(t, "minecraft:air", 0)
	perchedParrot(t, srv, 2)
	servertest.RunTicks(t, srv, 25)
	srv.Hurt("Keeper", 1)
	if left, right := srv.ShoulderParrots("Keeper"); left != -1 || right != -1 {
		t.Errorf("shoulders = %d, %d after a hurt, want both free", left, right)
	}
	ids := srv.MobIDsOfKind(parrotType)
	if len(ids) != 1 {
		t.Fatalf("%d parrots in the world, want the dropped one", len(ids))
	}
	st := tamable(t, srv, ids[0])
	if !st.Tame || st.Owner != protocol.OfflineUUID("Keeper").String() {
		t.Errorf("dropped parrot tame state = %+v, want tame to the player", st)
	}
}

// TestAShoulderParrotSurvivesRejoining: the perched parrot is saved with the
// player as ShoulderEntityLeft and is on the shoulder again after a rejoin.
// vanilla: ServerPlayer.addAdditionalSaveData
func TestAShoulderParrotSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	perchedParrot(t, srv, 4)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Keeper"))
	saved, ok := nbt.Get[nbt.Compound](root, "ShoulderEntityLeft")
	if !ok || saved["id"] != nbt.String(parrotType) {
		t.Fatalf("ShoulderEntityLeft = %v, %v; want the parrot", saved, ok)
	}
	connect().Join("Keeper")
	if left, _ := srv.ShoulderParrots("Keeper"); left != 4 {
		t.Errorf("left shoulder = %d after rejoining, want variant 4", left)
	}
}

// shoulderField reads metadata packets for the player until one carries the
// shoulder field at index, and returns its value (variant+1, 0 for free).
func shoulderField(t *testing.T, c *servertest.Client, entityID int32, index uint8) int32 {
	t.Helper()
	for {
		for _, e := range c.AwaitMetadataFor(entityID) {
			if e.Index == index {
				return e.VarintVal
			}
		}
	}
}

// TestShoulderMetadataIsSentOnLandingAndDrop: the owner's client is told the
// variant plus one at index 19 when the parrot lands, and 0 after the drop.
// vanilla: Player.DATA_SHOULDER_PARROT_LEFT
func TestShoulderMetadataIsSentOnLandingAndDrop(t *testing.T) {
	t.Parallel()
	srv, c := parrotScene(t, "minecraft:air", 0)
	perchedParrot(t, srv, 3)
	if got := shoulderField(t, c, c.PlayerEntityID, protocol.MetadataIndexShoulderLeft); got != 4 {
		t.Errorf("left shoulder metadata = %d on landing, want 4", got)
	}
	servertest.RunTicks(t, srv, 25)
	srv.Hurt("Keeper", 1)
	if got := shoulderField(t, c, c.PlayerEntityID, protocol.MetadataIndexShoulderLeft); got != 0 {
		t.Errorf("left shoulder metadata = %d after the drop, want 0", got)
	}
}

// shoulderSoundScene perches a parrot and drains its world sounds before listening.
func shoulderSoundScene(t *testing.T, silent bool) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := parrotScene(t, "minecraft:air", 0)
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 2)
	srv.TameMobFor(id, "Keeper")
	servertest.RunTicks(t, srv, 100)
	srv.SetMobSilent(id, silent)
	servertest.UntilTicks(t, srv, 130, "the parrot lands", func() bool {
		left, _ := srv.ShoulderParrots("Keeper")
		return left >= 0
	})
	for _, m := range srv.Mobs() {
		if m.Kind == parrotType {
			t.Fatal("a perched parrot remains in the world")
		}
	}
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	srv.SetWearSeed(1)
	srv.SetEnvironmentSeed(1)
	return srv, c
}

func TestAShoulderParrotImitatesNearbyMobs(t *testing.T) {
	t.Parallel()
	srv, c := shoulderSoundScene(t, false)
	// The zombie's box intersects the inflated player box although its centre is over 20 blocks away.
	id := srv.SpawnMob("minecraft:zombie", 21, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetDayTime(18000)
	// Imitation has a 1/200 shoulder roll followed by a 1/2 nearby-mob roll.
	if !c.ReadTicks(srv, 3000, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
		if name != "minecraft:entity.parrot.imitate.zombie" || category != protocol.SoundCategoryPlayer {
			return false
		}
		if category != protocol.SoundCategoryPlayer || x != 4 || y != 512 || z != 4 || volume != 0.7 || pitch < 0.8 || pitch > 1.2 {
			t.Errorf("shoulder imitation = category %d at (%d,%d,%d), volume %g pitch %g", category, x, y, z, volume, pitch)
		}
		return true
	}) {
		t.Fatal("the shoulder parrot never imitated the nearby zombie")
	}
}
