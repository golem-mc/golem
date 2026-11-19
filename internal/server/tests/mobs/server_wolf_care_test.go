package mobs

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The wolf's care, black box: the collar dye, healing by meat, and the
// breeding of tame wolves, with the loop held.

// giveHand puts the stack in the hotbar slot the scene's player holds.
func giveHand(t *testing.T, srv *server.Server, name, item string, count int32) {
	t.Helper()
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: count})
}

func wolfState(t *testing.T, srv *server.Server, id int32) server.TestWolf {
	t.Helper()
	st, ok := srv.WolfState(id)
	if !ok {
		t.Fatal("the wolf is not in the herd")
	}
	return st
}

// TestADyeRecoloursTheOwnersWolfCollar: the owner's dye sets the collar and
// is spent; the same colour again is neither spent nor a sit toggle; a
// stranger's dye does nothing. The collar is told to watchers and saved.
// vanilla: Wolf.mobInteract, Wolf.addAdditionalSaveData
func TestADyeRecoloursTheOwnersWolfCollar(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect, owner := wolfSceneWith(t, "Owner", cfg)
	stranger := connect().Join("Stranger")
	owner.ExpectPlayerAdded("Stranger")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	if got := wolfState(t, srv, id).Collar; got != "red" {
		t.Fatalf("a new wolf's collar = %q, want red", got)
	}

	giveHand(t, srv, "Stranger", "minecraft:blue_dye", 4)
	stranger.UseOn(id)
	stranger.Settle()
	if got := wolfState(t, srv, id).Collar; got != "red" {
		t.Errorf("a stranger's dye made the collar %q", got)
	}

	giveHand(t, srv, "Owner", "minecraft:blue_dye", 4)
	var told *wolfMeta
	owner.UseOn(id)
	owner.Settle()
	owner.ReadTicks(srv, 2, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				if m := readWolfMeta(t, r); m.collar >= 0 {
					told = &m
				}
			}
		}
		return false
	})
	if got := wolfState(t, srv, id).Collar; got != "blue" {
		t.Fatalf("collar after a blue dye = %q, want blue", got)
	}
	if told == nil || told.collar != 11 {
		t.Errorf("collar metadata = %+v, want the blue dye id 11", told)
	}
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 3 {
		t.Errorf("dyes left = %d, want 3", left)
	}
	sitting := tamable(t, srv, id).OrderedToSit

	owner.UseOn(id)
	owner.Settle()
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 3 {
		t.Errorf("a dye the collar already wears was spent: %d left", left)
	}
	if tamable(t, srv, id).OrderedToSit != sitting {
		t.Error("a dye the collar already wears toggled the sitting")
	}

	uid, _ := srv.MobUUID(id)
	srv.FlushMobs()
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)[uuid.MustParse(uid)]
	if saved == nil {
		t.Fatal("the wolf is not in the saved entities")
	}
	if collar, _ := nbt.Get[nbt.Byte](saved, "CollarColor"); collar != 11 {
		t.Errorf("saved CollarColor = %d, want 11", collar)
	}
	if variant, _ := nbt.Get[nbt.String](saved, "variant"); variant == "" {
		t.Error("the saved wolf has no variant")
	}
	if sound, _ := nbt.Get[nbt.String](saved, "sound_variant"); sound == "" {
		t.Error("the saved wolf has no sound variant")
	}
}

// TestMeatHealsAHurtTameWolfByTwiceItsNutrition: a hurt tame wolf eats the
// meat and heals 2 x nutrition, capped at its maximum; at full health the
// meat sets it in love instead.
// vanilla: Wolf.mobInteract, TamableAnimal.feed
func TestMeatHealsAHurtTameWolfByTwiceItsNutrition(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	srv.SetMobHealth(id, 10)
	giveHand(t, srv, "Owner", "minecraft:cooked_beef", 8) // nutrition 8

	c.UseOn(id)
	c.Settle()
	if hp, _ := srv.MobHealth(id); hp != 26 {
		t.Errorf("health after a steak = %v, want 26", hp)
	}
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 7 {
		t.Errorf("steaks left = %d, want 7", left)
	}
	if tamable(t, srv, id).OrderedToSit {
		t.Error("feeding toggled the sitting")
	}
	srv.SetMobHealth(id, 35)
	c.UseOn(id)
	c.Settle()
	if hp, _ := srv.MobHealth(id); hp != 40 {
		t.Errorf("health after the last steak = %v, want the 40 maximum", hp)
	}
	// At full health the meat is love, not healing.
	c.UseOn(id)
	c.Settle()
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 5 {
		t.Errorf("steaks left = %d, want 5 after a heal and a love feeding", left)
	}
	var loving int
	for _, m := range srv.Mobs() {
		if m.ID == id {
			loving = m.InLove
		}
	}
	if loving <= 0 {
		t.Error("a full-health tame wolf fed meat is not in love")
	}
}

// TestTwoFedTameWolvesBreedATamePup: loving tame wolves breed; the pup is a
// baby, tame, owned by the mother's owner, takes one parent's variant and
// wears the parents' collars mixed.
// vanilla: Wolf.canMate, Wolf.getBreedOffspring
func TestTwoFedTameWolvesBreedATamePup(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	a := srv.SpawnMob(wolfType, 1.5, 64, 2.5)
	b := srv.SpawnMob(wolfType, 2.5, 64, 2.5)
	srv.SetWolfVariant(a, "minecraft:ashen", "red")
	srv.SetWolfVariant(b, "minecraft:rusty", "yellow")
	srv.TameMobFor(a, "Owner")
	srv.TameMobFor(b, "Owner")
	srv.SetMobHealth(a, 40)
	srv.SetMobHealth(b, 40)
	giveHand(t, srv, "Owner", "minecraft:beef", 8)
	c.UseOn(a)
	c.UseOn(b)
	c.Settle()

	var pup server.TestMob
	servertest.UntilTicks(t, srv, 400, "a pup", func() bool {
		for _, m := range srv.Mobs() {
			if m.Kind == wolfType && m.ID != a && m.ID != b {
				pup = m
				return true
			}
		}
		return false
	})
	if !pup.Baby {
		t.Error("the pup is not a baby")
	}
	if pup.Variant != "minecraft:ashen" && pup.Variant != "minecraft:rusty" {
		t.Errorf("pup variant = %q, want a parent's", pup.Variant)
	}
	st := tamable(t, srv, pup.ID)
	if !st.Tame || st.Owner != protocol.OfflineUUID("Owner").String() {
		t.Errorf("pup tame state = %+v, want tame and owned by the parents' owner", st)
	}
	if got := wolfState(t, srv, pup.ID).Collar; got != "orange" {
		t.Errorf("pup collar = %q, want red and yellow mixed to orange", got)
	}
	if hp := wolfState(t, srv, pup.ID).Health; hp != 40 {
		t.Errorf("pup health = %v, want a tame wolf's 40", hp)
	}
}

// TestWildWolvesAndSittingWolvesDoNotBreed: love between wolves that are
// not both tame and standing ends in no pup.
// vanilla: Wolf.canMate
func TestWildWolvesAndSittingWolvesDoNotBreed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tame func(srv *server.Server, c *servertest.Client, a, b int32)
	}{
		{"wild", func(*server.Server, *servertest.Client, int32, int32) {}},
		{"one wild", func(srv *server.Server, _ *servertest.Client, a, _ int32) { srv.TameMobFor(a, "Owner") }},
		{"one sitting", func(srv *server.Server, c *servertest.Client, a, b int32) {
			srv.TameMobFor(a, "Owner")
			srv.TameMobFor(b, "Owner")
			giveHand(c.T, srv, "Owner", "minecraft:stick", 1)
			c.UseOn(b)
			c.Settle()
			servertest.RunTicks(c.T, srv, 2) // the sit goal starts on a tick
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := wolfScene(t, "Owner")
			a := srv.SpawnMob(wolfType, 1.5, 64, 2.5)
			b := srv.SpawnMob(wolfType, 2.5, 64, 2.5)
			tc.tame(srv, c, a, b)
			srv.SetMobHealth(a, 40)
			srv.SetMobHealth(b, 40)
			giveHand(t, srv, "Owner", "minecraft:beef", 8)
			c.UseOn(a)
			c.UseOn(b)
			c.Settle()
			servertest.RunTicks(t, srv, 200)
			if n := srv.MobCount(); n != 2 {
				t.Errorf("%d wolves stand after the courtship, want the two parents", n)
			}
		})
	}
}

// TestAWolfsVariantAndSoundVariantAreShownToANewWatcher: a wolf of a
// non-default variant arrives with its variant and sound variant in the
// metadata, as registry IDs.
// vanilla: Wolf.DATA_VARIANT_ID, DATA_SOUND_VARIANT_ID
func TestAWolfsVariantAndSoundVariantAreShownToANewWatcher(t *testing.T) {
	t.Parallel()
	srv, connect, _ := wolfSceneWith(t, "Owner", servertest.MobTestConfig())
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetWolfVariant(id, "minecraft:ashen", "")
	srv.SetMobSoundVariant(id, "minecraft:grumpy")
	late := connect()
	late.Login("Late")
	late.Configure()
	seen, ok := late.EnterWorldSeeingMob("Late", protocol.EntityTypeWolf)
	if !ok {
		t.Fatal("the joining player was not shown the wolf")
	}
	if want := registry.MustID("minecraft:wolf_variant", "minecraft:ashen"); seen.Variant != want {
		t.Errorf("variant ID = %d, want %d (ashen)", seen.Variant, want)
	}
	if want := registry.MustID("minecraft:wolf_sound_variant", "minecraft:grumpy"); seen.SoundVariant != want {
		t.Errorf("sound variant ID = %d, want %d (grumpy)", seen.SoundVariant, want)
	}
}
