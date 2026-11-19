package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The riptide trident over the wire: the release in water or rain that
// pushes the player along the look vector instead of throwing, the spin
// flag and pose the other clients draw it from, and the hit that ends it.
// Every test holds the tick loop so the ticks counted are exact.

// riptideFixture is a player facing south (+Z) holding a riptide trident,
// the loop held, with water standing at their feet when wet is set.
type riptideFixture struct {
	srv  *server.Server
	c    *servertest.Client
	id   int32
	wet  bool
	name string
}

func newRiptideFixture(t *testing.T, level int32, wet bool) *riptideFixture {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), "Diver")
	giveRiptideTrident(srv, "Diver", level)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: wet, name: "Diver"}
	if wet {
		f.setWater(true)
	}
	c.Settle()
	servertest.HoldTicks(t, srv)
	return f
}

// setWater fills or empties the two cells of the player's body.
func (f *riptideFixture) setWater(on bool) {
	state := uint16(0)
	if on {
		var ok bool
		state, ok = registry.FindState("minecraft:water", map[string]string{"level": "0"})
		if !ok {
			panic("no water state")
		}
	}
	f.srv.SetBlock(0, 64, 0, state)
	f.srv.SetBlock(0, 65, 0, state)
}

// charge draws the trident for the ticks given, past the attack cooldown
// when asked for 25.
func (f *riptideFixture) charge(t *testing.T, ticks int) {
	t.Helper()
	servertest.DrawBow(f.c, 0, 0)
	f.c.Settle()
	servertest.RunTicks(t, f.srv, ticks)
}

// riptideSeen is what a release put on the wire for the diver.
type riptideSeen struct {
	motion    [3]float64
	moved     bool
	sound     int32
	sounded   bool
	handState uint8
	handSeen  bool
	pose      int32
	thrown    bool
}

// release lets the draw go and reads ticks of traffic.
func (f *riptideFixture) release(t *testing.T, ticks int) riptideSeen {
	t.Helper()
	servertest.ReleaseBow(f.c)
	f.c.SettleKeeping()
	return f.read(t, ticks)
}

func (f *riptideFixture) read(t *testing.T, ticks int) riptideSeen {
	t.Helper()
	var seen riptideSeen
	f.c.ReadTicks(f.srv, ticks, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySetEntityMotion:
			if r.ReadVarInt() == f.id {
				x, y, z := r.ReadLpVec3()
				seen.motion, seen.moved = [3]float64{x, y, z}, true
			}
		case protocol.IDPlaySoundEffect:
			seen.sound, seen.sounded = readTridentSound(f.c, data).id, true
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != f.id {
				return false
			}
			riptideMetadata(r, &seen)
		case protocol.IDPlaySpawnEntity:
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeTrident {
				seen.thrown = true
			}
		}
		return false
	})
	return seen
}

func (f *riptideFixture) wear() int32 { return f.srv.InventorySlot(f.name, 36).Damage }

// TestARiptideReleaseInWaterSpinsThePlayerInsteadOfThrowing releases a
// riptide trident of each level in water: the player is pushed along the look
// vector by 1.5 + 0.75 per level above the first, hears that level's sound,
// is drawn in the spin pose with the spin bit set, loses one point of wear
// and keeps the trident.
// vanilla: TridentItem.releaseUsing, Enchantments.RIPTIDE
func TestARiptideReleaseInWaterSpinsThePlayerInsteadOfThrowing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		level    int32
		strength float64
		sound    string
	}{
		{1, 1.5, "minecraft:item.trident.riptide_1"},
		{2, 2.25, "minecraft:item.trident.riptide_2"},
		{3, 3.0, "minecraft:item.trident.riptide_3"},
	} {
		t.Run(tc.sound, func(t *testing.T) {
			t.Parallel()
			f := newRiptideFixture(t, tc.level, true)
			f.charge(t, 12)
			_, beforeY, _ := f.srv.PlayerVelocity(f.name)
			seen := f.release(t, 1)

			if !seen.moved {
				t.Fatal("the release pushed the diver with no Set Entity Motion")
			}
			if math.Abs(seen.motion[0]) > 0.02 || math.Abs(seen.motion[1]-beforeY) > 0.02 ||
				math.Abs(seen.motion[2]-tc.strength) > 0.02 {
				t.Errorf("push = %v, want (0, %v, %v) retaining vertical motion", seen.motion, beforeY, tc.strength)
			}
			if !seen.sounded || seen.sound != tridentSoundOf(t, tc.sound) {
				t.Errorf("sound = %d (heard %v), want %s", seen.sound, seen.sounded, tc.sound)
			}
			if seen.handState&protocol.EntityFlagSpinAttack == 0 {
				t.Errorf("hand state = %#x (sent %v), want the spin bit", seen.handState, seen.handSeen)
			}
			if seen.pose != protocol.PoseSpinAttack {
				t.Errorf("pose = %d, want the spin attack's %d", seen.pose, protocol.PoseSpinAttack)
			}
			if seen.thrown {
				t.Error("a riptide release threw the trident")
			}
			if count, _ := tridentsHeld(f.srv, f.name); count != 1 {
				t.Errorf("the diver holds %d tridents, want the one kept", count)
			}
			if got := f.wear(); got != 1 {
				t.Errorf("wear = %d, want 1", got)
			}
		})
	}
}

// TestARiptideSpinLastsTwentyTicksThenEnds counts the spin out: still on
// after nineteen ticks, off at the twentieth with the hand state cleared.
// vanilla: LivingEntity.aiStep, Player.startAutoSpinAttack(20, ...)
func TestARiptideSpinLastsTwentyTicksThenEnds(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 12)
	f.release(t, 0)
	servertest.RunTicks(t, f.srv, 18)
	if !f.srv.PlayerSpinAttack(f.name) {
		t.Fatal("the spin ended before its twentieth tick")
	}
	seen := f.read(t, 3)
	if f.srv.PlayerSpinAttack(f.name) {
		t.Error("the spin outlasted its twenty ticks")
	}
	if !seen.handSeen || seen.handState&protocol.EntityFlagSpinAttack != 0 {
		t.Errorf("hand state = %#x (sent %v), want it cleared on the wire", seen.handState, seen.handSeen)
	}
	if seen.pose == protocol.PoseSpinAttack {
		t.Error("the pose stayed the spin attack's")
	}
}

// TestARiptideReleaseOutOfWaterDoesNothing fills the water for the charge and
// empties it before the release: no push, no spin, no wear.
// vanilla: TridentItem.releaseUsing (isInWaterOrRain)
func TestARiptideReleaseOutOfWaterDoesNothing(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 12)
	f.setWater(false)
	seen := f.release(t, 2)
	if seen.moved || seen.thrown || f.srv.PlayerSpinAttack(f.name) || f.wear() != 0 {
		t.Errorf("a dry release moved=%v thrown=%v spinning=%v wear=%d, want nothing",
			seen.moved, seen.thrown, f.srv.PlayerSpinAttack(f.name), f.wear())
	}
}

// TestARiptideSpinWorksInTheRain spins on dry land under a raining sky.
// vanilla: Entity.isInWaterOrRain
func TestARiptideSpinWorksInTheRain(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, false)
	f.srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 100 {
		f.srv.TickWeather()
	}
	if !f.srv.IsRaining() {
		t.Fatal("the world is not raining")
	}
	f.charge(t, 12)
	seen := f.release(t, 1)
	if !seen.moved || !f.srv.PlayerSpinAttack(f.name) {
		t.Fatal("a release in the rain did not spin the diver")
	}
}

// vanilla: TridentItem.releaseUsing, Entity.move
func TestARiptideGroundLaunchClipsAgainstTheCeiling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		grounded bool
		ceiling  bool
		wantY    float64
	}{
		{"grounded", true, false, 65.2},
		{"ceiling", true, true, 64.2},
		{"airborne", false, false, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRiptideFixture(t, 1, true)
			f.c.MoveToGround(0.5, 64, 0.5, tc.grounded)
			if tc.ceiling {
				f.srv.SetBlock(0, 66, 0, servertest.MustState(t, "minecraft:stone", nil))
			}
			f.c.Settle()
			f.charge(t, 12)
			f.release(t, 0)
			pos := f.srv.PlayerPosition(f.name)
			if math.Abs(pos.Y-tc.wantY) > 1e-6 {
				t.Errorf("launch Y = %v, want %v", pos.Y, tc.wantY)
			}
			if pos.OnGround || !f.srv.PlayerSpinAttack(f.name) {
				t.Errorf("on ground = %v, spinning = %v, want false, true", pos.OnGround, f.srv.PlayerSpinAttack(f.name))
			}
		})
	}
}

// TestARiptideReleaseOnABoatDoesNothing releases in water while seated on a
// boat: a passenger is refused.
// vanilla: TridentItem.releaseUsing (isPassenger)
func TestARiptideReleaseOnABoatDoesNothing(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	boat := f.srv.SpawnBoat(0.5, 64, 0.5, "minecraft:oak_boat")
	f.c.UseOn(boat)
	f.c.Settle()
	if got := f.srv.BoatPassengerIDs(boat); len(got) != 1 {
		t.Fatalf("the boat holds %v, want the diver aboard", got)
	}
	f.charge(t, 12)
	seen := f.release(t, 2)
	if seen.moved || f.srv.PlayerSpinAttack(f.name) || f.wear() != 0 {
		t.Errorf("a seated release moved=%v spinning=%v wear=%d, want nothing",
			seen.moved, f.srv.PlayerSpinAttack(f.name), f.wear())
	}
}

// TestARiptideSpinHitsAMobOnceAndEnds spins into a cow after a full-cooldown
// charge: the cow takes the spin's 8 once, the spin ends on the hit and the
// diver's velocity is bounced back.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.attack (autoSpinAttackDmg)
func TestARiptideSpinHitsAMobOnceAndEnds(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 25)
	// Planted after the charge so the cow has not wandered off the body.
	cow := f.srv.SpawnMob("minecraft:cow", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(cow)
	f.release(t, 1)
	servertest.RunTicks(t, f.srv, 1)

	after, _ := f.srv.MobHealth(cow)
	if got := before - after; math.Abs(got-8) > 0.01 {
		t.Errorf("the cow lost %v, want the spin's 8", got)
	}
	if f.srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on after its hit")
	}
	if _, _, z := f.srv.PlayerVelocity(f.name); z >= 0 {
		t.Errorf("velocity z = %v after the hit, want it bounced back", z)
	}
	servertest.RunTicks(t, f.srv, 15)
	if again, _ := f.srv.MobHealth(cow); again != after {
		t.Errorf("the cow is at %v after the spin ended, want %v: one hit", again, after)
	}
}

// vanilla: LivingEntity.checkAutoSpinAttack, getDimensions
func TestARiptideSpinUsesThePlayersScale(t *testing.T) {
	t.Parallel()
	for _, scale := range []string{"1", "2"} {
		t.Run(scale, func(t *testing.T) {
			srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))
			giveRiptideTrident(srv, "Diver", 1)
			c.MoveTo(0.5, 64, 0.5, 0, 0)
			f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
			f.setWater(true)
			c.ChatCommand("attribute @s minecraft:scale base set " + scale)
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			servertest.HoldTicks(t, srv)
			f.charge(t, 25)
			// The cow's near edge is x=0.95: outside the unscaled spin's
			// x=0.8 edge, inside the doubled spin's x=1.1 edge.
			cow := srv.SpawnMob("minecraft:cow", 1.4, 64, 0.5)
			srv.SetMobNoAI(cow)
			before, _ := srv.MobHealth(cow)
			f.release(t, 1)
			servertest.RunTicks(t, srv, 1)
			after, _ := srv.MobHealth(cow)
			wantDamage := 0.0
			if scale == "2" {
				wantDamage = 8
			}
			if got := before - after; math.Abs(got-wantDamage) > 0.01 {
				t.Errorf("cow lost %v health at scale %s, want %v", got, scale, wantDamage)
			}
			if spinning := srv.PlayerSpinAttack(f.name); spinning != (wantDamage == 0) {
				t.Errorf("spinning = %v at scale %s, want %v", spinning, scale, wantDamage == 0)
			}
		})
	}
}

// TestARiptideSpinHitsWithTheStackItStartedWith swaps to an empty slot after
// the release: the hit still carries the trident's sharpness, the stack the
// spin began with.
// vanilla: Player.startAutoSpinAttack, Player.getWeaponItem
func TestARiptideSpinHitsWithTheStackItStartedWith(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	sharp, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the enchantment registry has no sharpness")
	}
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	f.srv.GiveSlot(f.name, 36, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Enchantments: map[int32]int32{riptide: 1, sharp: 5},
	})
	f.charge(t, 25)
	f.release(t, 0)
	f.c.SwapHotbarSlot(1)
	f.c.Settle()
	// The slot change restarts the attack cooldown; let it recover.
	servertest.RunTicks(t, f.srv, 12)
	golem := f.srv.SpawnMob("minecraft:iron_golem", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(golem)
	servertest.RunTicks(t, f.srv, 2)
	after, _ := f.srv.MobHealth(golem)
	// A golem, for the health a cow lacks to show 11. Sharpness V adds 1 + 0.5 * (5 - 1) to the spin's 8.
	if got := before - after; math.Abs(got-11) > 0.01 {
		t.Errorf("the golem lost %v, want 11: the spin's 8 and the trident's sharpness", got)
	}
}

// TestARiptideSpinFromTheOffHandHitsWithTheOffHandTrident spins with an
// off-hand trident and a stick in the main hand: the hit carries the
// trident's sharpness, not the stick's nothing.
// vanilla: TridentItem.releaseUsing, Player.getWeaponItem
func TestARiptideSpinFromTheOffHandHitsWithTheOffHandTrident(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	sharp, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the enchantment registry has no sharpness")
	}
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	f.srv.GiveSlot(f.name, 36, servertest.ItemStack(t, "minecraft:stick", 1))
	f.srv.GiveSlot(f.name, 45, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Enchantments: map[int32]int32{riptide: 1, sharp: 5},
	})
	f.c.Settle()
	f.c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandOff)
		b.PutVarInt(1)
		b.PutF32(0)
		b.PutF32(0)
	})
	f.c.Settle()
	servertest.RunTicks(t, f.srv, 25)
	f.release(t, 0)
	golem := f.srv.SpawnMob("minecraft:iron_golem", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(golem)
	servertest.RunTicks(t, f.srv, 2)
	after, _ := f.srv.MobHealth(golem)
	// Sharpness V adds 1 + 0.5 * (5 - 1) to the spin's 8.
	if got := before - after; math.Abs(got-11) > 0.01 {
		t.Errorf("the golem lost %v, want 11: the spin's 8 and the off-hand trident's sharpness", got)
	}
}

// TestARiptideSpinFromTheOffHandWearsTheTrident spins with a trident in the
// off hand and a sword in the main hand: only the spinning trident wears.
// vanilla: Player.itemAttackInteraction, ItemStack.hurtAndBreak
func TestARiptideSpinFromTheOffHandWearsTheTrident(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	sword := servertest.ItemStack(t, "minecraft:iron_sword", 1)
	sword.Damage = 0
	f.srv.GiveSlot(f.name, 36, sword)
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	f.srv.GiveSlot(f.name, 45, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Damage:       0,
		Enchantments: map[int32]int32{riptide: 1},
	})
	f.c.Settle()
	// Creative skips the release's own wear; switching back for the hit
	// isolates the wear the blow itself spends.
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeCreative)
	f.c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandOff)
		b.PutVarInt(1)
		b.PutF32(0)
		b.PutF32(0)
	})
	f.c.Settle()
	servertest.RunTicks(t, f.srv, 25)
	f.release(t, 0)
	servertest.RunTicks(t, f.srv, 1)
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeSurvival)
	zombie := f.srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(zombie)
	servertest.RunTicks(t, f.srv, 2)
	after, _ := f.srv.MobHealth(zombie)
	if got := before - after; got == 0 {
		t.Fatal("the zombie lost no health: the spin did not land")
	}
	if got := f.srv.InventorySlot(f.name, 45).Damage; got != 1 {
		t.Errorf("the trident's damage = %d, want the blow's 1", got)
	}
	if got := f.srv.InventorySlot(f.name, 36).Damage; got != 0 {
		t.Errorf("the sword's damage = %d, want the sword unworn", got)
	}
}

// TestARiptideSpinWearsTheStackThatMovedOutOfTheMainHand swaps two
// tridents during a spin: the hit wears the stack that started the spin,
// not whichever trident lands in its starting slot.
// vanilla: ServerGamePacketListenerImpl.swapHands, Player.itemAttackInteraction
func TestARiptideSpinWearsTheStackThatMovedOutOfTheMainHand(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	f.srv.GiveSlot(f.name, 45, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Damage:       100,
		Enchantments: map[int32]int32{riptide: 1},
	})
	f.c.Settle()
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeCreative)
	f.charge(t, 25)
	f.release(t, 0)
	f.c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	f.c.Settle()
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeSurvival)
	zombie := f.srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(zombie)
	servertest.RunTicks(t, f.srv, 2)
	after, _ := f.srv.MobHealth(zombie)
	if got := before - after; got == 0 {
		t.Fatal("the zombie lost no health: the spin did not land")
	}
	if got := f.srv.InventorySlot(f.name, 45).Damage; got != 1 {
		t.Errorf("the starting trident's damage = %d, want the blow's 1", got)
	}
	if got := f.srv.InventorySlot(f.name, 36).Damage; got != 100 {
		t.Errorf("the replacement trident's damage = %d, want 100", got)
	}
}

// TestARiptideSpinWearsTheStackThatMovedIntoTheMainHand starts with a
// trident in the off hand and swaps it before the blow: it, not the sword
// replacing it in the old slot, alone wears.
// vanilla: ServerGamePacketListenerImpl.swapHands, Player.itemAttackInteraction
func TestARiptideSpinWearsTheStackThatMovedIntoTheMainHand(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	sword := servertest.ItemStack(t, "minecraft:iron_sword", 1)
	sword.Damage = 0
	f.srv.GiveSlot(f.name, 36, sword)
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	f.srv.GiveSlot(f.name, 45, protocol.ItemStack{
		ItemID:       servertest.MustItem("minecraft:trident"),
		Count:        1,
		Damage:       0,
		Enchantments: map[int32]int32{riptide: 1},
	})
	f.c.Settle()
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeCreative)
	f.c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandOff)
		b.PutVarInt(1)
		b.PutF32(0)
		b.PutF32(0)
	})
	f.c.Settle()
	servertest.RunTicks(t, f.srv, 25)
	f.release(t, 0)
	f.c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	f.c.Settle()
	f.srv.SetPlayerGameMode(f.name, protocol.GameModeSurvival)
	zombie := f.srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(zombie)
	servertest.RunTicks(t, f.srv, 2)
	after, _ := f.srv.MobHealth(zombie)
	if got := before - after; got == 0 {
		t.Fatal("the zombie lost no health: the spin did not land")
	}
	if got := f.srv.InventorySlot(f.name, 36).Damage; got != 1 {
		t.Errorf("the starting trident's damage = %d, want the blow's 1", got)
	}
	if got := f.srv.InventorySlot(f.name, 45).Damage; got != 0 {
		t.Errorf("the sword's damage = %d, want the replacement unworn", got)
	}
}

// TestARiptideSpinDoesNotWearAnotherPlayersTrident transfers a previously
// spun trident through a drop and pickup before the recipient's first spin.
// vanilla: Player.startAutoSpinAttack, Player.itemAttackInteraction
func TestARiptideSpinDoesNotWearAnotherPlayersTrident(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	donor := connect()
	donorID := donor.JoinAs("Donor")
	diver := connect()
	diver.JoinAs("Diver")
	donor.MoveTo(0.5, 64, 0.5, 0, 0)
	diver.MoveTo(8.5, 64, 0.5, 0, 0)
	giveRiptideTrident(srv, "Donor", 1)
	f := &riptideFixture{srv: srv, c: donor, id: donorID, wet: true, name: "Donor"}
	f.setWater(true)
	donor.Settle()
	diver.Settle()
	servertest.HoldTicks(t, srv)
	f.charge(t, 25)
	f.release(t, 20)
	if got := srv.InventorySlot("Donor", 36).Damage; got != 1 {
		t.Fatalf("donor's released trident damage = %d, want 1", got)
	}
	donor.DropKey(true)
	donor.Settle()
	donor.MoveTo(8.5, 64, 8.5, 0, 0)
	donor.Settle()
	servertest.RunTicks(t, srv, 50)
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 {
		t.Fatalf("dropped items = %d, want 1", len(items))
	}
	diver.MoveTo(items[0].X, items[0].Y, items[0].Z, 0, 0)
	diver.Settle()
	servertest.UntilTicks(t, srv, 20, "donor's trident pickup", func() bool {
		return !srv.InventorySlot("Diver", 36).Empty()
	})
	if got := srv.InventorySlot("Diver", 36).Damage; got != 1 {
		t.Fatalf("received trident damage = %d, want 1", got)
	}
	riptide, _ := registry.EnchantmentID("minecraft:riptide")
	srv.GiveSlot("Diver", 45, protocol.ItemStack{
		ItemID: servertest.MustItem("minecraft:trident"), Count: 1,
		Enchantments: map[int32]int32{riptide: 1},
	})
	diver.MoveTo(0.5, 64, 0.5, 0, 0)
	diver.Settle()
	diver.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandOff)
		b.PutVarInt(1)
		b.PutF32(0)
		b.PutF32(0)
	})
	diver.Settle()
	servertest.RunTicks(t, srv, 25)
	servertest.ReleaseBow(diver)
	diver.Settle()
	zombie := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.0)
	before, _ := srv.MobHealth(zombie)
	servertest.RunTicks(t, srv, 2)
	after, _ := srv.MobHealth(zombie)
	if after >= before {
		t.Fatal("the spin did not hurt the zombie")
	}
	if got := srv.InventorySlot("Diver", 45).Damage; got != 2 {
		t.Errorf("spinning trident damage = %d, want release plus hit = 2", got)
	}
	if got := srv.InventorySlot("Diver", 36).Damage; got != 1 {
		t.Errorf("received trident damage = %d, want unchanged = 1", got)
	}
}

// riptideDuel is a diver in water beside a victim in the given game mode.
type riptideDuel struct {
	*riptideFixture
	victim *servertest.Client
}

func newRiptideDuel(t *testing.T, diverMode, victimMode string) *riptideDuel {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Diver"))
	c := connect()
	id := c.JoinAs("Diver")
	victim := connect()
	victim.JoinAs("Victim")
	giveRiptideTrident(srv, "Diver", 1)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	victim.MoveToGround(0.5, 64, 1.0, true)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
	f.setWater(true)
	for name, mode := range map[string]string{"Diver": diverMode, "Victim": victimMode} {
		if mode == "survival" {
			continue
		}
		c.ChatCommand("gamemode " + mode + " " + name)
		want := map[string]int32{"creative": protocol.GameModeCreative, "spectator": protocol.GameModeSpectator}[mode]
		servertest.UntilTicks(t, srv, 1200, "game mode", func() bool {
			return srv.PlayerGameMode(name) == want
		})
	}
	c.Settle()
	victim.Settle()
	servertest.HoldTicks(t, srv)
	return &riptideDuel{riptideFixture: f, victim: victim}
}

// TestARiptideSpinHitsAPlayer spins into a survival player: they take the
// spin's 8 once and the spin ends.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.attack
func TestARiptideSpinHitsAPlayer(t *testing.T) {
	t.Parallel()
	d := newRiptideDuel(t, "survival", "survival")
	d.charge(t, 25)
	before := d.srv.PlayerHealth("Victim")
	d.release(t, 1)
	servertest.RunTicks(t, d.srv, 1)
	if got := before - d.srv.PlayerHealth("Victim"); math.Abs(got-8) > 0.01 {
		t.Errorf("the victim lost %v, want the spin's 8", got)
	}
	if d.srv.PlayerSpinAttack(d.name) {
		t.Error("the spin went on after its hit")
	}
}

// TestARiptideSpinHitsAPlayerInCreative spins in creative into a survival
// player: the blow lands and costs the trident no wear.
// vanilla: Player.attack, ItemStack.hurtAndBreak (creative)
func TestARiptideSpinHitsAPlayerInCreative(t *testing.T) {
	t.Parallel()
	d := newRiptideDuel(t, "creative", "survival")
	d.charge(t, 25)
	before := d.srv.PlayerHealth("Victim")
	d.release(t, 1)
	servertest.RunTicks(t, d.srv, 1)
	if got := before - d.srv.PlayerHealth("Victim"); math.Abs(got-8) > 0.01 {
		t.Errorf("the victim lost %v, want the spin's 8", got)
	}
	if got := d.wear(); got != 0 {
		t.Errorf("wear = %d in creative, want 0", got)
	}
}

// TestARiptideSpinPassesThroughASpectator spins into a spectator: nothing is
// hit and the spin runs its full course.
// vanilla: Entity.isSpectator (spectators are not hit by attacks)
func TestARiptideSpinPassesThroughASpectator(t *testing.T) {
	t.Parallel()
	d := newRiptideDuel(t, "survival", "spectator")
	d.charge(t, 25)
	before := d.srv.PlayerHealth("Victim")
	d.release(t, 1)
	servertest.RunTicks(t, d.srv, 3)
	if got := d.srv.PlayerHealth("Victim"); got != before {
		t.Errorf("the spectator went from %v to %v", before, got)
	}
	if !d.srv.PlayerSpinAttack(d.name) {
		t.Error("the spin ended on a spectator")
	}
}

// TestARiptideSpinIntoAWallEnds sends a move whose horizontal-collision
// flag is set mid-spin: the spin ends within the tick, for the server and
// for a watcher.
// vanilla: LivingEntity.checkAutoSpinAttack (horizontalCollision)
func TestARiptideSpinIntoAWallEnds(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c := connect()
	id := c.JoinAs("Diver")
	watcher := connect().Join("Watcher")
	giveRiptideTrident(srv, "Diver", 1)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	watcher.MoveToGround(6.5, 64, 0.5, true)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
	f.setWater(true)
	c.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	f.charge(t, 25)
	f.release(t, 1)
	if !srv.PlayerSpinAttack(f.name) {
		t.Fatal("the release did not start a spin")
	}
	watcher.Settle()

	c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) { b.PutU8(0x02) })
	c.Settle()
	var seen riptideSeen
	watcher.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		if pid == protocol.IDPlayEntityMetadata && r.ReadVarInt() == id {
			riptideMetadata(r, &seen)
		}
		return false
	})
	if srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on after running into a wall")
	}
	if !seen.handSeen || seen.handState&protocol.EntityFlagSpinAttack != 0 {
		t.Errorf("watcher saw hand state %#x (sent %v), want the spin bit cleared", seen.handState, seen.handSeen)
	}
}

// TestARiptideSpinIntoAWallWithNonLivingEntities keeps spinning when a
// non-living body overlaps the sweep, even outside the current player box.
// vanilla: LivingEntity.checkAutoSpinAttack, EntityGetter.getEntities
func TestARiptideSpinIntoAWallWithNonLivingEntities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, summon string
		spinning     bool
	}{
		{"item in swept box", `summon minecraft:item 0.5 64.2 0.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s,NoGravity:1b}`, true},
		{"arrow in swept box", `summon minecraft:arrow 0.5 64.2 0.5 {NoGravity:1b}`, true},
		{"orb in swept box", `summon minecraft:experience_orb 0.5 64.2 0.5 {Value:1}`, true},
		{"item outside swept box", `summon minecraft:item 6.5 64.2 0.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s,NoGravity:1b}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRiptideFixture(t, 1, true)
			f.charge(t, 25)
			f.release(t, 1)
			if !f.srv.PlayerSpinAttack(f.name) {
				t.Fatal("the release did not start a spin")
			}
			if reply := f.srv.RunLine(tc.summon); len(reply) != 1 || reply[0].Translate != "commands.summon.success" {
				t.Fatalf("summon feedback = %+v", reply)
			}
			// Only the old end of the sweep overlaps the entity.
			f.c.MoveTo(2.5, 64, 0.5, 0, 0)
			f.c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) { b.PutU8(0x02) })
			f.c.Settle()
			servertest.RunTicks(t, f.srv, 1)
			if got := f.srv.PlayerSpinAttack(f.name); got != tc.spinning {
				t.Errorf("spinning after wall collision = %v, want %v", got, tc.spinning)
			}
		})
	}
}

// vanilla: EntityGetter.getEntities (NO_SPECTATORS)
func TestARiptideSpinIntoAWallIgnoresASpectator(t *testing.T) {
	t.Parallel()
	d := newRiptideDuel(t, "survival", "spectator")
	d.charge(t, 25)
	d.release(t, 1)
	d.c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) { b.PutU8(0x02) })
	d.c.Settle()
	servertest.RunTicks(t, d.srv, 1)
	if d.srv.PlayerSpinAttack(d.name) {
		t.Error("the spectator prevented the spin from ending on a wall")
	}
}

// TestARiptideSpinStrikesAnArmorStand spins into an armor stand: the stand
// shakes and the spin ends.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.attack, ArmorStand.hurtServer
func TestARiptideSpinStrikesAnArmorStand(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 25)
	stand := f.srv.SpawnArmorStand(0.5, 64, 1.0)
	f.c.Settle()
	servertest.ReleaseBow(f.c)
	f.c.Settle()
	shaken := false
	f.c.ReadTicks(f.srv, 2, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		if body, status := r.ReadI32(), r.ReadU8(); body == stand && status == protocol.EntityStatusArmorStandShake {
			shaken = true
		}
		return false
	})
	if !shaken {
		t.Error("the spin did not shake the armor stand")
	}
	if f.srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on through the armor stand")
	}
}

// TestARiptideSpinStrikesAMannequin spins into a summoned mannequin and
// checks the hurt, the end of the spin and its bounce.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.doAutoAttackOnTouch
func TestARiptideSpinStrikesAMannequin(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))
	giveRiptideTrident(srv, "Diver", 1)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
	f.setWater(true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	f.charge(t, 25)

	c.ChatCommand("summon minecraft:mannequin 0.5 64 1.0")
	c.SettleKeeping()
	var mannequin int32
	c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			body := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeMannequin {
				mannequin = body
			}
		}
		return false
	})
	if mannequin == 0 {
		t.Fatal("the summoned mannequin was not shown")
	}

	servertest.ReleaseBow(c)
	c.SettleKeeping()
	if !srv.PlayerSpinAttack(f.name) {
		t.Fatal("the release did not start a spin")
	}
	var hit, healthSent bool
	var health float32
	c.ReadTicks(srv, 2, func(packet int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packet {
		case protocol.IDPlayDamageEvent:
			if r.ReadVarInt() == mannequin {
				r.ReadVarInt() // damage type
				hit = r.ReadVarInt() == id+1
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() == mannequin {
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("could not read the mannequin's metadata")
				}
				if e := servertest.MetaOf(meta, protocol.MetadataIndexLivingHealth); e != nil {
					health, healthSent = e.FloatVal, true
				}
			}
		}
		return false
	})
	if !hit {
		t.Error("the spin sent no mannequin damage event caused by the diver")
	}
	if !healthSent || health != 12 {
		t.Errorf("mannequin health sent=%v value=%v, want 12 after the spin's 8 damage", healthSent, health)
	}
	if srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on through the mannequin")
	}
	if _, _, z := srv.PlayerVelocity(f.name); z >= 0 {
		t.Errorf("velocity z = %v after the hit, want it bounced back", z)
	}
}

// riptideMetadata reads the fields of a player's Set Entity Metadata that the
// spin changes, past the entity ID, stopping at a field type it cannot step
// over.
func riptideMetadata(r *protocol.Reader, seen *riptideSeen) {
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			return
		}
		switch typ := r.ReadVarInt(); typ {
		case protocol.MetadataByte:
			v := r.ReadU8()
			if index == protocol.MetadataIndexHandState {
				seen.handState, seen.handSeen = v, true
			}
		case protocol.MetadataPose:
			seen.pose = r.ReadVarInt()
		case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			r.ReadBool()
		default:
			return
		}
	}
}
