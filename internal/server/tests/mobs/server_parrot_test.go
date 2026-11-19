package mobs

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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

// TestSummonedParrotCarriesAVariant: a summoned parrot is told with a variant
// in 0..4 at the index after the tamable fields.
// vanilla: Parrot.finalizeSpawn
func TestSummonedParrotCarriesAVariant(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	id, ok := registry.EntityTypeID(parrotType)
	if !ok {
		t.Fatal("no parrot entity type")
	}
	srv.StopTick()
	c.ChatCommand(`summon minecraft:parrot 3.5 64 0.5`)
	parrot := c.ExpectMobSpawn(id)
	// A variant of 0, the default, is left out of the spawn.
	if parrot.Variant < -1 || parrot.Variant > 4 {
		t.Errorf("variant = %d, want 0..4", parrot.Variant)
	}
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

// TestACookieKillsAParrotAndTheKillDropsFeathers: the cookie is spent and the
// parrot dies, leaving one or two feathers.
// vanilla: Parrot.mobInteract, entities/parrot
func TestACookieKillsAParrotAndTheKillDropsFeathers(t *testing.T) {
	t.Parallel()
	srv, c := parrotScene(t, "minecraft:cookie", 3)
	id := srv.SpawnMob(parrotType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	c.UseOn(id)
	c.Settle()
	if left := srv.InventorySlot("Keeper", player.HotbarSlot).Count; left != 2 {
		t.Errorf("the hand holds %d cookies, want one spent", left)
	}
	if hp, ok := srv.MobHealth(id); ok && hp > 0 {
		t.Fatalf("the parrot has %v health after a cookie, want it dead", hp)
	}
	servertest.RunTicks(t, srv, 30)
	// The keeper stands beside the body and may pick a feather up.
	feathers := 0
	feather := servertest.MustItemID(t, "minecraft:feather")
	for slot := range 46 {
		if st := srv.InventorySlot("Keeper", slot); st.ItemID == feather {
			feathers += int(st.Count)
		}
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:feather" {
			feathers += int(it.Stack.Count)
		}
	}
	if feathers < 1 || feathers > 2 {
		t.Errorf("the parrot dropped %d feathers, want 1 or 2", feathers)
	}
}

// perchedParrot tames a parrot at the player's feet, sets its variant, and
// runs the loop until it has landed on the shoulder.
func perchedParrot(t *testing.T, srv *server.Server, variant int) int32 {
	t.Helper()
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 3)
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
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 3)
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

// vanilla: ServerPlayer.setGameMode, removeEntitiesOnShoulder
func TestSpectatorModeDropsTheShoulderParrot(t *testing.T) {
	t.Parallel()
	for _, ticks := range []int{0, 25} {
		t.Run(fmt.Sprintf("after_%d_ticks", ticks), func(t *testing.T) {
			srv, c := parrotScene(t, "minecraft:air", 0)
			perchedParrot(t, srv, 2)
			servertest.RunTicks(t, srv, ticks)
			c.Settle()

			if reply := srv.RunLine("gamemode spectator Keeper"); len(reply) != 1 || reply[0].Translate != "commands.gamemode.success.other" {
				t.Fatalf("gamemode feedback = %+v", reply)
			}
			checkShoulderParrotDrop(t, srv, c, ticks > 20)
		})
	}
}

// Rain permits riptide without water dropping the parrot before the release.
// vanilla: Player.startAutoSpinAttack, ServerPlayer.removeEntitiesOnShoulder
func TestARiptideSpinDropsTheShoulderParrot(t *testing.T) {
	t.Parallel()
	for _, ticks := range []int{0, 25} {
		t.Run(fmt.Sprintf("after_%d_ticks", ticks), func(t *testing.T) {
			srv, c := parrotScene(t, "minecraft:trident", 1)
			perchedParrot(t, srv, 2)
			servertest.RunTicks(t, srv, ticks)
			srv.SetWeatherState(true, false, 100000, 100000, 0)
			for range 100 {
				srv.TickWeather()
			}
			rip, ok := registry.EnchantmentID("minecraft:riptide")
			if !ok {
				t.Fatal("no riptide enchantment")
			}
			srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{
				ItemID: servertest.MustItemID(t, "minecraft:trident"), Count: 1,
				Enchantments: map[int32]int32{rip: 1},
			})
			servertest.DrawBow(c, 0, 0)
			c.Settle()
			servertest.RunTicks(t, srv, 12)
			if left, _ := srv.ShoulderParrots("Keeper"); left != 2 {
				t.Fatal("the parrot left the shoulder before the riptide release")
			}
			c.Settle()

			servertest.ReleaseBow(c)
			c.SettleKeeping()
			if !srv.PlayerSpinAttack("Keeper") {
				t.Fatal("the release in rain did not start a spin")
			}
			checkShoulderParrotDrop(t, srv, c, ticks > 20)
			pos := srv.PlayerPosition("Keeper")
			if math.Abs(pos.Y-65.2) > 1e-6 {
				t.Errorf("grounded launch Y = %v, want 65.2", pos.Y)
			}
			ids := srv.MobIDsOfKind(parrotType)
			// The client predicts the grounded lift before its first flight step.
			c.MoveToGround(0.5, pos.Y+0.48, 1.0, false)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			if ticks > 20 {
				if len(ids) != 1 {
					t.Fatalf("%d parrots at release, want one", len(ids))
				}
				if health, ok := srv.MobHealth(ids[0]); !ok || health != 6 {
					t.Errorf("parrot health = %v (present %v), want 6", health, ok)
				}
			}
			if !srv.PlayerSpinAttack("Keeper") {
				t.Error("the spin ended on its first movement")
			}
		})
	}
}

func checkShoulderParrotDrop(t *testing.T, srv *server.Server, c *servertest.Client, dropped bool) {
	t.Helper()
	left, right := srv.ShoulderParrots("Keeper")
	ids := srv.MobIDsOfKind(parrotType)
	if !dropped {
		if left != 2 || right != -1 || len(ids) != 0 {
			t.Fatalf("unsettled parrot: shoulders = %d, %d, world parrots = %d; want 2, -1, 0", left, right, len(ids))
		}
		return
	}
	if left != -1 || right != -1 {
		t.Errorf("shoulders = %d, %d, want both free", left, right)
	}
	if len(ids) != 1 {
		t.Fatalf("%d parrots in the world, want the dropped one", len(ids))
	}
	st := tamable(t, srv, ids[0])
	if !st.Tame || st.Owner != protocol.OfflineUUID("Keeper").String() {
		t.Errorf("dropped parrot tame state = %+v, want tame to the player", st)
	}
	var cleared bool
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("could not read player metadata")
				}
				l := servertest.MetaOf(meta, protocol.MetadataIndexShoulderLeft)
				r := servertest.MetaOf(meta, protocol.MetadataIndexShoulderRight)
				cleared = cleared || l != nil && r != nil && l.VarintVal == 0 && r.VarintVal == 0
			}
		}
		return false
	})
	if !cleared {
		t.Error("no metadata cleared both shoulders")
	}
}

// TestKillingTheOwnerDropsTheShoulderParrot: a fatal blow puts the parrot
// back into the world. The hurt path drops the shoulders before die runs, so
// this does not isolate die's own call.
// vanilla: ServerPlayer.die
func TestKillingTheOwnerDropsTheShoulderParrot(t *testing.T) {
	t.Parallel()
	srv, _ := parrotScene(t, "minecraft:air", 0)
	perchedParrot(t, srv, 1)
	servertest.RunTicks(t, srv, 25)
	srv.Hurt("Keeper", 1000)
	if left, right := srv.ShoulderParrots("Keeper"); left != -1 || right != -1 {
		t.Errorf("shoulders = %d, %d after death, want both free", left, right)
	}
	if n := len(srv.MobIDsOfKind(parrotType)); n != 1 {
		t.Errorf("%d parrots in the world after the owner died, want 1", n)
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

// TestShoulderMetadataIsInTheSpawnPayload: a second client joining after the
// landing sees the variant in the owner's spawn metadata.
func TestShoulderMetadataIsInTheSpawnPayload(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	perchedParrot(t, srv, 2)
	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	shown := watcher.JoinCapturing("Watcher", map[int32]string{protocol.EntityTypePlayer: "owner"})
	got := int32(-1)
	for _, e := range servertest.CapturedOr(t, watcher, shown, "owner", protocol.EntityTypePlayer).Meta {
		if e.Index == protocol.MetadataIndexShoulderLeft {
			got = e.VarintVal
		}
	}
	if got != 3 {
		t.Errorf("spawn shoulder metadata = %d, want 3", got)
	}
}

// TestWaterAtTheOwnerDropsTheShoulderParrot: standing in water puts the perched
// parrot back into the world.
// vanilla: ServerPlayer.doTick
func TestWaterAtTheOwnerDropsTheShoulderParrot(t *testing.T) {
	t.Parallel()
	srv, _ := parrotScene(t, "minecraft:air", 0)
	perchedParrot(t, srv, 2)
	servertest.RunTicks(t, srv, 25)
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	srv.SetBlock(0, 64, 0, water)
	srv.SetBlock(0, 65, 0, water)
	servertest.UntilTicks(t, srv, 5, "the shoulder empties", func() bool {
		left, right := srv.ShoulderParrots("Keeper")
		return left == -1 && right == -1
	})
	if n := len(srv.MobIDsOfKind(parrotType)); n != 1 {
		t.Errorf("%d parrots in the world after the water, want 1", n)
	}
}

// shoulderSoundScene perches a parrot and drains its world sounds before listening.
func shoulderSoundScene(t *testing.T, silent bool) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := parrotScene(t, "minecraft:air", 0)
	id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 3)
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

func TestAShoulderParrotChirps(t *testing.T) {
	t.Parallel()
	for _, side := range []string{"left", "right"} {
		t.Run(side, func(t *testing.T) {
			srv, c := shoulderSoundScene(t, side == "right")
			if side == "right" {
				id := srv.SpawnMobSeeded(parrotType, 0.5, 64, 0.5, 6)
				srv.TameMobFor(id, "Keeper")
				servertest.UntilTicks(t, srv, 1000, "the right shoulder fills", func() bool {
					_, right := srv.ShoulderParrots("Keeper")
					return right >= 0
				})
				c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
			}
			// One chirp is expected per ~200 ticks per shoulder; 3000 bounds the random wait.
			if !c.ReadTicks(srv, 3000, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
				if name != "minecraft:entity.parrot.ambient" || category != protocol.SoundCategoryPlayer {
					return false
				}
				if category != protocol.SoundCategoryPlayer || x != 4 || y != 512 || z != 4 || volume != 1 || pitch < 0.8 || pitch > 1.2 {
					t.Errorf("shoulder chirp = category %d at (%d,%d,%d), volume %g pitch %g", category, x, y, z, volume, pitch)
				}
				return true
			}) {
				t.Fatal("the shoulder parrot never chirped")
			}
			for _, m := range srv.Mobs() {
				if m.Kind == parrotType {
					t.Error("a perched parrot remains in the world")
				}
			}
		})
	}
}

func TestASilentShoulderParrotStaysQuiet(t *testing.T) {
	t.Parallel()
	srv, c := shoulderSoundScene(t, true)
	// A speaking shoulder would chirp roughly every 200 ticks; observe 3000.
	c.ReadTicks(srv, 3000, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(name, "minecraft:entity.parrot.") {
				t.Errorf("silent shoulder parrot played %s", name)
			}
		}
		return false
	})
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

func TestAShoulderParrotFallsBackForSilentOrDistantMobs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		x      float64
		silent bool
	}{
		{"silent", 2.5, true},
		{"distant", 21.2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := shoulderSoundScene(t, false)
			id := srv.SpawnMob("minecraft:zombie", tc.x, 64, 0.5)
			srv.SetMobNoAI(id)
			srv.SetMobSilent(id, tc.silent)
			srv.SetDayTime(18000)
			chirped := false
			// 3000 ticks cover roughly fifteen shoulder rolls; fallback keeps volume 1.
			c.ReadTicks(srv, 3000, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySoundEffect {
					return false
				}
				name, category, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
				if category == protocol.SoundCategoryPlayer && strings.HasPrefix(name, "minecraft:entity.parrot.") {
					if volume != 1 {
						t.Errorf("imitated a %s mob at volume %g", tc.name, volume)
					}
					chirped = chirped || name == "minecraft:entity.parrot.ambient"
				}
				return false
			})
			if !chirped {
				t.Error("the shoulder parrot never fell back to chirping")
			}
		})
	}
}
