package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The mace's smash attack: a blow struck after a fall of more than a block
// and a half. vanilla: MaceItem

// maceSwingRecovered is the count of ticks a mace's swing has wound all the
// way back up at: its attack speed of 0.6 a second spends 33 and a third.
const maceSwingRecovered = 34

// smashFixture is a mace-wielding attacker with its swing wound up, a
// watcher two blocks past the victim, and a still iron golem at (4.5, 64,
// 5.5) for the victim: a hundred health and no armour, so the blow's damage
// reads straight off its health.
func smashFixture(t *testing.T) (srv *server.Server, attacker, watcher *servertest.Client, golem int32) {
	t.Helper()
	srv, attacker, watcher, golem, _ = smashFixtureOn(t, servertest.CombatTestConfig())
	return srv, attacker, watcher, golem
}

// smashFixtureOn is smashFixture on cfg, also returning the connector.
func smashFixtureOn(t *testing.T, cfg *config.Config) (srv *server.Server, attacker, watcher *servertest.Client, golem int32, connect func() *servertest.Client) {
	t.Helper()
	srv, attacker, watcher, connect = feedbackFixtureOn(t, cfg)
	watcher.MoveToGround(6.5, 64, 5.5, true)
	watcher.MoveToGround(6.5, 64, 5.5, true)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:mace"), Count: 1})
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks("Fencer") >= maceSwingRecovered
	})
	golem = srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 5.5)
	if !srv.SetMobNoAI(golem) {
		t.Fatal("the golem was not planted")
	}
	attacker.Settle()
	watcher.Settle()
	return srv, attacker, watcher, golem, connect
}

// fallSixOnto drops the attacker six blocks, a step at a time, to y=66 by
// the golem's head, and swings at it there, still in the air.
func fallSixOnto(t *testing.T, srv *server.Server, attacker *servertest.Client, target int32) {
	t.Helper()
	attacker.MoveToGround(2.5, 72, 5.5, false)
	attacker.MoveToGround(2.5, 69, 5.5, false)
	attacker.MoveToGround(2.5, 66, 5.5, false)
	attacker.Settle()
	if got := srv.PlayerFallDistance("Fencer"); math.Abs(got-6) > 1e-9 {
		t.Fatalf("the fall banked before the blow = %v, want 6", got)
	}
	attacker.Attack(target)
	attacker.Settle()
}

func TestAMaceSmashAddsTheFallToTheBlow(t *testing.T) {
	t.Parallel()
	srv, attacker, _, golem := smashFixture(t)
	fallSixOnto(t, srv, attacker, golem)

	// The fist's one and the mace's five, plus twelve for the first three
	// blocks and two apiece for the next three, all half again for the
	// critical: (6 + 18) * 1.5.
	health, ok := srv.MobHealth(golem)
	if !ok {
		t.Fatal("the golem is gone")
	}
	if want := 100.0 - 36; math.Abs(health-want) > 1e-6 {
		t.Errorf("the golem's health after the smash = %v, want %v", health, want)
	}

	// The smash spent the fall: the rest of the way down to the ground is
	// two blocks, inside the safe three, so the landing hurts nothing.
	if got := srv.PlayerFallDistance("Fencer"); got != 0 {
		t.Errorf("the fall left after the smash = %v, want 0", got)
	}
	attacker.MoveToGround(2.5, 64, 5.5, true)
	attacker.Settle()
	if got := srv.PlayerHealth("Fencer"); got != 20 {
		t.Errorf("the attacker's health after landing = %v, want the whole 20", got)
	}
}

func TestAShortFallIsNoSmash(t *testing.T) {
	t.Parallel()
	srv, attacker, _, golem := smashFixture(t)
	attacker.MoveToGround(2.5, 65.5, 5.5, false)
	attacker.MoveToGround(2.5, 64.5, 5.5, false)
	attacker.Settle()
	attacker.Attack(golem)
	attacker.Settle()

	// A one-block fall is a critical but no smash: 6 * 1.5, and the fall
	// is kept.
	health, _ := srv.MobHealth(golem)
	if want := 100.0 - 9; math.Abs(health-want) > 1e-6 {
		t.Errorf("the golem's health after a short fall's blow = %v, want %v", health, want)
	}
	if got := srv.PlayerFallDistance("Fencer"); got != 1 {
		t.Errorf("the fall after a blow that did not smash = %v, want the 1 kept", got)
	}
}

func TestAMaceSmashPushesTheBodiesAround(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher, golem := smashFixture(t)
	heavy, ok := registry.SoundEventID(soundMaceSmashHeavy)
	if !ok {
		t.Fatal("the sound registry has no heavy smash")
	}
	fallSixOnto(t, srv, attacker, golem)

	var pushed, heard, dust bool
	var vx, vy, vz float64
	watcher.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySetEntityMotion:
			if r.ReadVarInt() == watcher.PlayerEntityID {
				pushed = true
				vx, vy, vz = r.ReadLpVec3()
			}
		case protocol.IDPlaySoundEffect:
			if r.ReadVarInt()-1 == heavy {
				heard = true
			}
		case protocol.IDPlayWorldEvent:
			if r.ReadI32() == 2013 {
				x, y, z := r.ReadPosition()
				if x != 4 || y != 63 || z != 5 {
					t.Errorf("the smash's dust at (%d, %d, %d), want the golem's footing (4, 63, 5)", x, y, z)
				}
				if n := r.ReadI32(); n != 750 {
					t.Errorf("the smash's dust count = %d, want 750", n)
				}
				dust = true
			}
		}
	})
	if !heard {
		t.Error("the watcher did not hear the heavy smash of a six-block fall")
	}
	if !dust {
		t.Error("the watcher saw no smash dust")
	}
	// Two blocks off a heavy smash: (3.5 - 2) * 0.7 * 2 along the offset,
	// and 0.7 up added to the resting fall speed.
	if !pushed {
		t.Fatal("the watcher two blocks from the victim was not pushed")
	}
	if math.Abs(vx-2.1) > 0.01 || math.Abs(vy-(0.7-0.0784)) > 0.01 || math.Abs(vz) > 0.01 {
		t.Errorf("the watcher's push = (%v, %v, %v), want (2.1, 0.6216, 0)", vx, vy, vz)
	}
}

// TestAMaceSmashSparesTheAttackersPets checks the smash's push leaves a
// wolf the attacker tamed where an untamed one as near is thrown.
// vanilla: MaceItem.knockbackPredicate, TamableAnimal.considersEntityAsAlly
func TestAMaceSmashSparesTheAttackersPets(t *testing.T) {
	t.Parallel()
	srv, attacker, _, golem := smashFixture(t)
	pet := srv.SpawnMob("minecraft:wolf", 4.5, 64, 7.5)
	stray := srv.SpawnMob("minecraft:wolf", 4.5, 64, 3.5)
	for _, id := range []int32{pet, stray} {
		srv.SetMobNoAI(id)
	}
	if !srv.TameMobFor(pet, "Fencer") {
		t.Fatal("the pet was not tamed")
	}
	fallSixOnto(t, srv, attacker, golem)

	speed := func(id int32) float64 {
		vx, vy, vz, _ := srv.MobVelocity(id)
		return math.Abs(vx) + math.Abs(vy) + math.Abs(vz)
	}
	if speed(stray) < 0.5 {
		t.Fatalf("the untamed wolf was pushed at %v, want a smash's shove", speed(stray))
	}
	if speed(pet) > 1e-9 {
		t.Errorf("the attacker's wolf was pushed at %v, want none", speed(pet))
	}
}

// soundMaceSmashHeavy is the sound of a smash after a fall past five blocks
// onto a victim on the ground.
const soundMaceSmashHeavy = "minecraft:item.mace.smash_ground_heavy"

func TestAMaceSmashLandsOnAPlayer(t *testing.T) {
	t.Parallel()
	srv, attacker, watcher := feedbackFixture(t)
	watcher.MoveToGround(4.5, 64, 5.5, true)
	watcher.MoveToGround(4.5, 64, 5.5, true)
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:mace"), Count: 1})
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks("Fencer") >= maceSwingRecovered
	})
	pet := srv.SpawnMob("minecraft:wolf", 4.5, 64, 7.5)
	srv.SetMobNoAI(pet)
	if !srv.TameMobFor(pet, "Watcher") {
		t.Fatal("the struck player's wolf was not tamed")
	}
	watcher.Settle()
	fallSixOnto(t, srv, attacker, watcher.PlayerEntityID)

	// The struck player's pet is spared the push, as the attacker's is.
	// vanilla: MaceItem.knockbackPredicate
	if vx, vy, vz, _ := srv.MobVelocity(pet); math.Abs(vx)+math.Abs(vy)+math.Abs(vz) > 1e-9 {
		t.Errorf("the struck player's wolf was pushed at (%v, %v, %v), want none", vx, vy, vz)
	}
	// (6 + 18) * 1.5 is past the twenty health a plain critical's nine
	// would leave eleven of.
	if got := srv.PlayerHealth("Watcher"); got > 0 {
		t.Errorf("the struck player's health after the smash = %v, want none left", got)
	}
	if got := srv.PlayerFallDistance("Fencer"); got != 0 {
		t.Errorf("the fall left after smashing a player = %v, want 0", got)
	}
	expectFatalDeath(srv, watcher, watcher.PlayerEntityID, protocol.Text{
		Translate: "death.attack.mace_smash",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Watcher"), servertest.PlayerDisplayComponent("Fencer")},
	})
}

// smashDust is the block the smash's dust event names, or ok false when the
// watcher saw none.
func smashDust(watcher *servertest.Client) (x, y, z int32, ok bool) {
	watcher.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		if id == protocol.IDPlayWorldEvent && r.ReadI32() == 2013 {
			x, y, z = r.ReadPosition()
			ok = true
		}
	})
	return x, y, z, ok
}

func TestWindBurstMaceLaunchesTheAttacker(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		level int32
		fall  float64
		burst bool
		push  float64
	}{
		{name: "plain smash", fall: 3},
		{name: "wind burst I", level: 1, fall: 3, burst: true},
		{name: "wind burst III", level: 3, fall: 3, burst: true},
		{name: "short fall", level: 1, fall: 1},
		{name: "exact threshold", level: 1, fall: 1.5, burst: true},
	}
	t.Run("hits", func(t *testing.T) {
		for i := range cases {
			tc := &cases[i]
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				srv, attacker, _ := feedbackFixture(t)
				held := protocol.ItemStack{ItemID: int32(registry.ItemMace), Count: 1}
				if tc.level > 0 {
					held = servertest.EnchantedStack(t, "minecraft:mace", "minecraft:wind_burst", tc.level)
				}
				srv.GiveSlot("Fencer", player.HotbarSlot, held)
				servertest.UntilTicks(t, srv, 100, "mace swing recovered", func() bool {
					return srv.AttackTicks("Fencer") >= maceSwingRecovered
				})
				servertest.HoldTicks(t, srv)
				zombie := srv.SpawnMob("minecraft:zombie", 4.5, 64, 5.5)
				if !srv.SetMobNoAI(zombie) {
					t.Fatal("zombie missing")
				}
				attacker.MoveToGround(2.5, 65+tc.fall, 5.5, false)
				attacker.MoveToGround(2.5, 65, 5.5, false)
				attacker.Settle()
				if got := srv.PlayerFallDistance("Fencer"); got != tc.fall {
					t.Fatalf("fall distance = %v, want %v", got, tc.fall)
				}
				ground := srv.BlockAt(2, 63, 5)
				attacker.Attack(zombie)
				var bursts int
				attacker.SettleAndCollect(func(id int32, data []byte) {
					switch id {
					case protocol.IDPlaySetEntityMotion:
						r := protocol.NewReader(data)
						if r.ReadVarInt() == attacker.PlayerEntityID {
							_, tc.push, _ = r.ReadLpVec3()
						}
					case protocol.IDPlayExplosion:
						bursts++
						b := servertest.ReadWindBurst(attacker, data)
						if b.X != 2.5 || b.Y != 65 || b.Z != 5.5 || b.Radius != 3.5 {
							t.Errorf("burst centre/radius = (%v, %v, %v)/%v", b.X, b.Y, b.Z, b.Radius)
						}
						if !b.Pushed || b.KY <= 0 || b.KX != 0 || b.KZ != 0 {
							t.Errorf("burst push = %v (%v, %v, %v)", b.Pushed, b.KX, b.KY, b.KZ)
						}
						if b.Particle != protocol.ParticleGustEmitterLarge || b.Sound != protocol.SoundEventWindChargeBurst || b.Alternatives != 0 {
							t.Errorf("unexpected wind burst payload: %+v", b)
						}
						tc.push = b.KY
					}
				})
				if (bursts == 1) != tc.burst || bursts > 1 {
					t.Errorf("bursts = %d, want burst = %v", bursts, tc.burst)
				}
				if got := srv.BlockAt(2, 63, 5); got != ground {
					t.Errorf("ground after burst = %v, want %v", got, ground)
				}
				if got := srv.PlayerHealth("Fencer"); got != 20 {
					t.Errorf("attacker health = %v, want 20", got)
				}
				wantFall := tc.fall
				if tc.fall > 1.5 {
					wantFall = 0
				}
				if got := srv.PlayerFallDistance("Fencer"); got != wantFall {
					t.Errorf("fall after hit = %v, want %v", got, wantFall)
				}
			})
		}
	})
	if cases[1].push <= cases[0].push || cases[2].push <= cases[1].push {
		t.Errorf("upward push: plain %v, wind burst I %v, III %v; want increasing", cases[0].push, cases[1].push, cases[2].push)
	}
}

// breachStrike swings the held stack once at the mob and returns what its
// health lost. The swing is wound up before and spent by the blow.
func breachStrike(t *testing.T, srv *server.Server, attacker *servertest.Client, held protocol.ItemStack, mob int32) float64 {
	t.Helper()
	srv.GiveSlot("Fencer", player.HotbarSlot, held)
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks("Fencer") >= maceSwingRecovered
	})
	before, _ := srv.MobHealth(mob)
	attacker.Attack(mob)
	attacker.Settle()
	after, _ := srv.MobHealth(mob)
	return before - after
}

func TestABreachMaceMakesArmourLessEffective(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Fencer"))
	attacker := connect()
	attacker.JoinAs("Fencer")
	servertest.HoldTicks(t, srv)
	attacker.MoveToGround(2.5, 64, 5.5, true)
	attacker.MoveToGround(2.5, 64, 5.5, true)
	attacker.Settle()

	var armoured [2]int32
	for i := range armoured {
		attacker.ChatCommand(`summon minecraft:zombie 4.5 64 5.5 {equipment:{head:{id:"minecraft:iron_helmet",count:1},` +
			`chest:{id:"minecraft:iron_chestplate",count:1},legs:{id:"minecraft:iron_leggings",count:1},` +
			`feet:{id:"minecraft:iron_boots",count:1}}}`)
		armoured[i] = attacker.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
		if !srv.SetMobNoAI(armoured[i]) {
			t.Fatal("the zombie was not planted")
		}
	}
	golems := [2]int32{srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 5.5), srv.SpawnMob("minecraft:iron_golem", 4.5, 64, 5.5)}
	for _, g := range golems {
		srv.SetMobNoAI(g)
	}
	attacker.Settle()

	plain := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:mace"), Count: 1}
	breach := servertest.EnchantedStack(t, "minecraft:mace", "minecraft:breach", 4)

	// Full iron is 15 armour points on the zombie's natural 2: 17 less
	// 6/2 gives 0.56, so 6 * (1 - 0.56) = 2.64 plain, and with Breach IV the
	// fraction 0.56 - 0.6 clamps to nothing, so the whole 6 lands.
	if got := breachStrike(t, srv, attacker, plain, armoured[0]); math.Abs(got-2.64) > 1e-4 {
		t.Errorf("the plain mace's blow on the armoured zombie = %v, want 2.64", got)
	}
	if got := breachStrike(t, srv, attacker, breach, armoured[1]); math.Abs(got-6) > 1e-4 {
		t.Errorf("the Breach IV mace's blow on the armoured zombie = %v, want 6", got)
	}
	// No armour at all: the delta clamps to nothing.
	a := breachStrike(t, srv, attacker, plain, golems[0])
	b := breachStrike(t, srv, attacker, breach, golems[1])
	if math.Abs(a-b) > 1e-6 || math.Abs(a-6) > 1e-4 {
		t.Errorf("the golem lost %v to the plain mace and %v to Breach IV, want 6 both", a, b)
	}
}

// blockDust collects the block-particle bursts in watcher's stream as counts.
func blockDust(watcher *servertest.Client) (counts []int32) {
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayLevelParticles {
			return
		}
		r := protocol.NewReader(data)
		particle := r.ReadVarInt()
		r.ReadVarInt()
		r.ReadBool()
		r.ReadBool()
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		dx, dy, dz := r.ReadF32(), r.ReadF32(), r.ReadF32()
		r.ReadF32()
		r.ReadF32()
		r.ReadF32()
		n := r.ReadVarInt()
		if particle == protocol.ParticleBlock && dx == 0.3 && dy == 0.3 && dz == 0.3 {
			counts = append(counts, n)
		}
	})
	return counts
}
