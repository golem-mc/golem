package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The fox's hunting, fleeing, trust and meals, black box with the loop held.

func mobAlive(srv *server.Server, id int32) bool {
	m, ok := mobWithID(srv, id)
	return ok && m.ID == id
}

// TestAFoxStalksAndPouncesOnAChicken: a fox crouches in sight of a chicken,
// leaps and kills it.
// vanilla: Fox.StalkPreyGoal, Fox.FoxPounceGoal, landTargetGoal
func TestAFoxStalksAndPouncesOnAChicken(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	chicken := srv.SpawnMobSeeded("minecraft:chicken", foxX+10, 64, 0.5, 4)
	var crouched, pounced bool
	for range 600 {
		servertest.RunTicks(t, srv, 1)
		flags := foxState(t, srv, fox).Flags
		crouched = crouched || flags&protocol.FoxCrouching != 0
		pounced = pounced || flags&protocol.FoxPouncing != 0
		if !mobAlive(srv, chicken) {
			break
		}
	}
	if mobAlive(srv, chicken) {
		t.Fatal("the fox never killed the chicken")
	}
	if !crouched || !pounced {
		t.Errorf("crouched = %v, pounced = %v, want both before the kill", crouched, pounced)
	}
}

// TestAFoxLeavesAMobItDoesNotHunt: a pig is no prey.
func TestAFoxLeavesAMobItDoesNotHunt(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	pig := srv.SpawnMobSeeded("minecraft:pig", foxX+4, 64, 0.5, 4)
	servertest.RunTicks(t, srv, 300)
	if srv.MobTargetID(fox) != 0 || !mobAlive(srv, pig) {
		t.Error("the fox went after a pig")
	}
}

// TestAFoxFleesAPlayerUnlessItTrustsThem: a fox with a player in sight runs
// off; one that trusts the player stays.
// vanilla: Fox.registerGoals' AvoidEntityGoal<Player>, Fox.trusts
func TestAFoxFleesAPlayerUnlessItTrustsThem(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		srv := foxScene(t, false)
		srv.SetSpawning(false)
		fox := srv.SpawnMobSeeded(foxType, 8.5, 64, 0.5, 8)
		if trusted {
			srv.TrustFox(fox, "Fox")
		}
		servertest.RunTicks(t, srv, 80)
		st := foxState(t, srv, fox)
		away := math.Hypot(st.X-0.5, st.Z-0.5) > 14
		if away == trusted {
			t.Errorf("trusted = %v: fox at (%.1f, %.1f), fled = %v", trusted, st.X, st.Z, away)
		}
	}
}

// TestAFoxFleesAWildWolfButNotATamedOne.
// vanilla: Fox.registerGoals' AvoidEntityGoal<Wolf>
func TestAFoxFleesAWildWolfButNotATamedOne(t *testing.T) {
	t.Parallel()
	for _, tame := range []bool{false, true} {
		srv := foxScene(t, false)
		srv.SetSpawning(false)
		fox := srv.SpawnMobSeeded(foxType, 12.5, 64, 0.5, 8)
		srv.TrustFox(fox, "Fox")
		wolf := srv.SpawnMobSeeded("minecraft:wolf", 5.5, 64, 0.5, 4)
		if tame {
			srv.TameMobFor(wolf, "Fox")
		}
		servertest.RunTicks(t, srv, 30)
		st := foxState(t, srv, fox)
		// A wild wolf also hunts the fox, so the distance shrinks again;
		// the fox's run away from the wolf is what shows.
		if fled := st.X > 13; fled == tame {
			t.Errorf("tame = %v: fox at x = %.1f, fled = %v", tame, st.X, fled)
		}
	}
}

// TestAThunderstormWakesAFox.
// vanilla: Fox.tick
func TestAThunderstormWakesAFox(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, true)
	stoneRoom(t, srv, foxBlock, 0)
	id := srv.SpawnMob(foxType, foxX, 64, 0.5)
	servertest.RunTicks(t, srv, 400)
	if !foxState(t, srv, id).Sleeping {
		t.Fatal("the fox did not fall asleep")
	}
	srv.SetWeatherState(true, true, 100000, 100000, 0)
	servertest.RunTicks(t, srv, 200)
	if foxState(t, srv, id).Sleeping {
		t.Error("the fox slept through the thunder")
	}
}

// TestFedFoxesBreedAKitThatTrustsTheirPlayer: two foxes loved by a player
// have a baby fox that trusts the player and does not flee them.
// vanilla: Fox.FoxBreedGoal.breed
func TestFedFoxesBreedAKitThatTrustsTheirPlayer(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	a := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	b := srv.SpawnMobSeeded(foxType, foxX+2, 64, 0.5, 4)
	srv.SetMobInLoveBy(a, 600, "Fox")
	srv.SetMobInLoveBy(b, 600, "Fox")
	var kit int32
	for range 400 {
		servertest.RunTicks(t, srv, 1)
		for _, m := range srv.Mobs() {
			if m.Kind == foxType && m.Baby {
				kit = m.ID
			}
		}
		if kit != 0 {
			break
		}
	}
	if kit == 0 {
		t.Fatal("no kit was born")
	}
	if !srv.FoxTrusts(kit, "Fox") {
		t.Error("the kit does not trust the player who fed its parents")
	}
}

// TestATrustedFoxDefendsItsPlayer: whatever strikes a trusted player becomes
// the fox's target.
// vanilla: Fox.DefendTrustedTargetGoal
func TestATrustedFoxDefendsItsPlayer(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, 12.5, 64, 0.5, 3)
	srv.TrustFox(fox, "Fox")
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 1.5, 64, 0.5, 4)
	servertest.UntilTicks(t, srv, 400, "the fox to take the zombie as its target", func() bool {
		return srv.MobTargetID(fox) == zombie
	})
	if foxState(t, srv, fox).Flags&protocol.FoxDefending == 0 {
		t.Error("the defending fox does not show it")
	}
}

// TestAFoxFinishesAMealWithItsEffectsAndRemainder: a fox holding a honey
// bottle swallows it after 600 ticks and keeps the glass bottle; a golden
// apple leaves regeneration.
// vanilla: Fox.aiStep, Consumable.onConsume, UseRemainder
func TestAFoxFinishesAMealWithItsEffectsAndRemainder(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	bottleFox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	appleFox := srv.SpawnMobSeeded(foxType, foxX, 64, 6.5, 4)
	srv.SetFoxMouth(bottleFox, "minecraft:honey_bottle")
	srv.SetFoxMouth(appleFox, "minecraft:golden_apple")
	servertest.RunTicks(t, srv, 590)
	if foxState(t, srv, bottleFox).Mouth != servertest.MustItemID(t, "minecraft:honey_bottle") {
		t.Fatal("the fox ate before the 600 ticks")
	}
	servertest.RunTicks(t, srv, 40)
	if got, want := foxState(t, srv, bottleFox).Mouth, registry.MustItem("minecraft:glass_bottle"); got != want {
		t.Errorf("mouth after the meal = %d, want the glass bottle %d", got, want)
	}
	if foxState(t, srv, appleFox).Mouth != 0 {
		t.Error("the apple was not swallowed")
	}
	if _, _, ok := srv.MobEffectOf(appleFox, "minecraft:regeneration"); !ok {
		t.Error("the golden apple left no regeneration")
	}
}

// vanilla: Fox.aiStep, Item.finishUsingItem, Consumable.onConsume
func TestAFoxMealUsesItsStacksConsumable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, consumable, sound string
		custom, inline          bool
	}{
		{"empty effects", `{on_consume_effects:[]}`, "minecraft:entity.generic.eat", false, false},
		{"custom effects and drink sound", `{animation:"drink",sound:"minecraft:entity.experience_orb.pickup",on_consume_effects:[{type:"minecraft:apply_effects",effects:[{id:"minecraft:speed",amplifier:1,duration:200}]},{type:"minecraft:play_sound",sound:"minecraft:block.note_block.pling"}]}`, "minecraft:entity.experience_orb.pickup", true, false},
		{"inline known sounds", `{animation:"drink",sound:{sound_id:"minecraft:entity.experience_orb.pickup",range:32},on_consume_effects:[{type:"apply_effects",effects:[{id:"speed",amplifier:1,duration:200}]},{type:"play_sound",sound:{sound_id:"minecraft:block.note_block.pling",range:32}}]}`, "minecraft:entity.experience_orb.pickup", true, true},
		{"inline unknown sound", `{animation:"drink",sound:{sound_id:"test:fox_meal",range:32},on_consume_effects:[{type:"apply_effects",effects:[{id:"speed",amplifier:1,duration:200}]},{type:"play_sound",sound:{sound_id:"minecraft:block.note_block.pling",range:32}}]}`, "test:fox_meal", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := foxSceneClient(t, false)
			srv.SetSpawning(false)
			stoneRoom(t, srv, 6, 0)
			id := srv.SpawnMobSeeded(foxType, 6.5, 64, 0.5, 3)
			srv.TrustFox(id, "Fox")
			srv.RunLine("item replace entity @e[type=minecraft:fox,limit=1] weapon.mainhand with minecraft:golden_apple[consumable=" + tc.consumable + "]")
			if _, ok := srv.MobHand(id).ConsumableOf(); !ok {
				t.Fatal("the fox was not given its patched consumable")
			}
			var consumeCount int
			var heardExtra bool
			c.ReadTicks(srv, 630, func(packetID int32, data []byte) bool {
				if packetID != protocol.IDPlaySoundEffect {
					return false
				}
				packet := servertest.DecodePlaySoundEffect(t, data)
				sound := packet.Name
				if packet.RegistryID >= 0 {
					sound, _ = servertest.SoundEventName(packet.RegistryID)
				}
				volume, pitch := packet.Volume, packet.Pitch
				if tc.inline && (sound == tc.sound || sound == "minecraft:block.note_block.pling") {
					if packet.RegistryID != -1 || !packet.HasFixedRange || packet.FixedRange != 32 {
						t.Errorf("component sound holder = %+v, want inline range 32", packet)
					}
				}
				if sound == tc.sound {
					consumeCount++
					if tc.custom && consumeCount == 1 && (volume != 0.5 || pitch < 0.9 || pitch > 1) {
						t.Errorf("drink volume/pitch = %v/%v, want 0.5/0.9..1", volume, pitch)
					}
					if consumeCount == 2 && (volume != 1 || pitch < 0.6 || pitch > 1.4) {
						t.Errorf("food listener volume/pitch = %v/%v, want 1/0.6..1.4", volume, pitch)
					}
				}
				heardExtra = heardExtra || sound == "minecraft:block.note_block.pling"
				if tc.custom && sound == "minecraft:entity.generic.eat" {
					t.Error("the patched meal played the item's default consume sound")
				}
				return false
			})
			if !srv.MobHand(id).Empty() {
				t.Fatal("the fox did not swallow its meal")
			}
			for _, effect := range []string{"minecraft:regeneration", "minecraft:absorption"} {
				if _, _, ok := srv.MobEffectOf(id, effect); ok {
					t.Errorf("the patched meal applied default %s", effect)
				}
			}
			if duration, amplifier, ok := srv.MobEffectOf(id, "minecraft:speed"); ok != tc.custom || tc.custom && (duration <= 0 || amplifier != 1) {
				t.Errorf("speed = %d/%d/%v, want present = %v with amplifier 1", duration, amplifier, ok, tc.custom)
			}
			if consumeCount != 2 || heardExtra != tc.custom {
				t.Errorf("consume count/extra sound = %d/%v, want 2/%v", consumeCount, heardExtra, tc.custom)
			}
		})
	}
}

// vanilla: Consumable.onConsume, FoodProperties.onConsume, PlaySoundConsumeEffect.apply
func TestAFoxMealPlaysFoodListenerBeforeConsumeEffects(t *testing.T) {
	t.Parallel()
	srv, c := foxSceneClient(t, false)
	srv.SetSpawning(false)
	stoneRoom(t, srv, 6, 0)
	id := srv.SpawnMobSeeded(foxType, 6.5, 64, 0.5, 3)
	srv.TrustFox(id, "Fox")
	srv.RunLine(`item replace entity @e[type=minecraft:fox,limit=1] weapon.mainhand with minecraft:golden_apple[consumable={animation:"drink",sound:{sound_id:"minecraft:block.amethyst_block.chime",range:32},on_consume_effects:[{type:"minecraft:play_sound",sound:{sound_id:"minecraft:block.note_block.pling",range:24}}]}]`)
	type heardSound struct {
		name     string
		category int32
		volume   float32
		pitch    float32
		y        int32
	}
	var heard []heardSound
	c.ReadTicks(srv, 630, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlaySoundEffect {
			return false
		}
		packet := servertest.DecodePlaySoundEffect(t, data)
		if packet.Name == "minecraft:block.amethyst_block.chime" || packet.Name == "minecraft:block.note_block.pling" {
			heard = append(heard, heardSound{packet.Name, packet.Category, packet.Volume, packet.Pitch, packet.Y})
		}
		return false
	})
	if !srv.MobHand(id).Empty() {
		t.Fatal("the fox did not swallow its meal")
	}
	if len(heard) != 3 {
		t.Fatalf("meal sounds = %+v, want initial chime, food listener chime, effect pling", heard)
	}
	for i, sound := range heard {
		name, volume, minPitch, maxPitch := "minecraft:block.amethyst_block.chime", float32(1), float32(0.6), float32(1.4)
		if i == 0 {
			volume, minPitch, maxPitch = 0.5, 0.9, 1
		} else if i == 2 {
			name, minPitch, maxPitch = "minecraft:block.note_block.pling", 1, 1
		}
		if sound.name != name || sound.category != protocol.SoundCategoryNeutral || sound.volume != volume || sound.pitch < minPitch || sound.pitch > maxPitch {
			t.Errorf("sound %d = %+v, want %s category neutral volume %v pitch %v..%v", i, sound, name, volume, minPitch, maxPitch)
		}
		// The fox stands on the floor (whole-block y): the effect is centred on
		// its block, half a block up (4 eighths); the food listener is at the fox.
		// vanilla: PlaySoundConsumeEffect.apply, FoodProperties.onConsume
		if wantCentred := i == 2; (sound.y%8 == 4) != wantCentred {
			t.Errorf("sound %d y = %d eighths, centred = %v, want %v", i, sound.y, sound.y%8 == 4, wantCentred)
		}
	}
}

// vanilla: FoodProperties.onConsume uses Level.playSound, bypassing Entity.Silent.
func TestASilentFoxStillPlaysItsFoodListenerSound(t *testing.T) {
	t.Parallel()
	srv, c := foxSceneClient(t, false)
	srv.SetSpawning(false)
	stoneRoom(t, srv, 6, 0)
	id := srv.SpawnMobSeeded(foxType, 6.5, 64, 0.5, 3)
	srv.TrustFox(id, "Fox")
	srv.SetMobSilent(id, true)
	srv.RunLine(`item replace entity @e[type=minecraft:fox,limit=1] weapon.mainhand with minecraft:golden_apple[consumable={animation:"drink",sound:{sound_id:"minecraft:block.amethyst_block.chime",range:32},on_consume_effects:[]}]`)
	var heard int
	c.ReadTicks(srv, 630, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlaySoundEffect {
			packet := servertest.DecodePlaySoundEffect(t, data)
			if packet.Name == "minecraft:block.amethyst_block.chime" {
				heard++
				if packet.RegistryID != -1 || !packet.HasFixedRange || packet.FixedRange != 32 {
					t.Errorf("listener sound holder = %+v, want inline range 32", packet)
				}
				if packet.Category != protocol.SoundCategoryNeutral || packet.Volume != 1 || packet.Pitch < 0.6 || packet.Pitch > 1.4 {
					t.Errorf("listener category/volume/pitch = %v/%v/%v, want neutral/1/0.6..1.4", packet.Category, packet.Volume, packet.Pitch)
				}
			}
		}
		return false
	})
	if !srv.MobHand(id).Empty() || heard != 1 {
		t.Errorf("mouth = %+v, listener sounds = %d, want empty mouth and one sound", srv.MobHand(id), heard)
	}
}

// vanilla: PlaySoundConsumeEffect.apply, Consumable.emitParticlesAndSounds
func TestASilentFoxPlaysConsumeEffectSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, sound string
		inline      bool
	}{
		{"registered", `"minecraft:block.note_block.pling"`, false},
		{"inline", `{sound_id:"minecraft:block.note_block.pling",range:24}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := foxSceneClient(t, false)
			srv.SetSpawning(false)
			stoneRoom(t, srv, 6, 0)
			id := srv.SpawnMobSeeded(foxType, 6.5, 64, 0.5, 3)
			srv.TrustFox(id, "Fox")
			srv.SetMobSilent(id, true)
			srv.RunLine(`item replace entity @e[type=minecraft:fox,limit=1] weapon.mainhand with minecraft:golden_apple[consumable={animation:"drink",sound:{sound_id:"minecraft:block.amethyst_block.chime",range:32},on_consume_effects:[{type:"play_sound",sound:` + tc.sound + `}]}]`)
			var listeners, effects int
			c.ReadTicks(srv, 630, func(packetID int32, data []byte) bool {
				if packetID != protocol.IDPlaySoundEffect {
					return false
				}
				packet := servertest.DecodePlaySoundEffect(t, data)
				sound := packet.Name
				if packet.RegistryID >= 0 {
					sound, _ = servertest.SoundEventName(packet.RegistryID)
				}
				switch sound {
				case "minecraft:entity.fox.eat":
					t.Error("Silent fox played its bite sound")
				case "minecraft:block.amethyst_block.chime":
					listeners++
					// The food listener is audible; the entity's half-volume sip is muted.
					if packet.Volume != 1 {
						t.Errorf("Silent fox played its sip sound: %+v", packet)
					}
				case "minecraft:block.note_block.pling":
					effects++
					if packet.Category != protocol.SoundCategoryNeutral || packet.Volume != 1 || packet.Pitch != 1 {
						t.Errorf("effect sound = %+v, want neutral category, volume 1 and pitch 1", packet)
					}
					if tc.inline && (packet.RegistryID != -1 || !packet.HasFixedRange || packet.FixedRange != 24) {
						t.Errorf("effect holder = %+v, want inline range 24", packet)
					}
				}
				return false
			})
			if !srv.MobHand(id).Empty() {
				t.Fatal("the fox did not swallow its meal")
			}
			if listeners != 1 || effects != 1 {
				t.Errorf("listener/effect sounds = %d/%d, want 1/1", listeners, effects)
			}
		})
	}
}

// TestAFoxRunsAtClosePreyWithoutCrouching: prey within six blocks is chased, not
// stalked.
// vanilla: Fox.StalkPreyGoal.canUse
func TestAFoxRunsAtClosePreyWithoutCrouching(t *testing.T) {
	t.Parallel()
	srv := foxScene(t, false)
	srv.SetSpawning(false)
	fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 3)
	srv.SpawnMobSeeded("minecraft:chicken", foxX+4, 64, 0.5, 4)
	for range 30 {
		servertest.RunTicks(t, srv, 1)
		if foxState(t, srv, fox).Flags&protocol.FoxCrouching != 0 {
			t.Fatal("the fox crouched for prey four blocks away")
		}
	}
}

// foxStruckByCow is a trusted fox far from the player and a cow that has just
// hit it, 30 blocks off so the cow is not an alertable neighbour.
func foxStruckByCow(t *testing.T, srv *server.Server, struck bool) int32 {
	t.Helper()
	fox := srv.SpawnMobSeeded(foxType, foxX, 64, 0.5, 6)
	srv.TrustFox(fox, "Fox")
	if struck {
		cow := srv.SpawnMobSeeded("minecraft:cow", foxX-30, 64, 0.5, 4)
		if !srv.StrikeMobWith(cow, fox, 1) {
			t.Fatal("the cow's blow did not land")
		}
	}
	return fox
}

// TestAHurtFoxDoesNotPerchOrFetchItems: for 100 ticks after a blow a fox
// neither sits to look about nor walks to an item (laid on the side away from where a struck fox panics); an unhurt one does both.
// vanilla: Fox.PerchAndSearchGoal.canUse, Fox.FoxSearchForItemsGoal.canUse
func TestAHurtFoxDoesNotPerchOrFetchItems(t *testing.T) {
	t.Parallel()
	for _, struck := range []bool{false, true} {
		t.Run(map[bool]string{false: "unhurt", true: "hurt"}[struck], func(t *testing.T) {
			t.Parallel()
			srv := foxScene(t, false)
			srv.SetSpawning(false)
			fox := foxStruckByCow(t, srv, struck)
			srv.SpawnItemEntityDelay(foxX-4, 64, 0.5, "minecraft:stick", 3, 0)
			sat := false
			for range 90 {
				servertest.RunTicks(t, srv, 1)
				sat = sat || foxState(t, srv, fox).Flags&protocol.FoxSitting != 0
			}
			fetched := srv.MobHand(fox).ItemID != 0
			if struck && (sat || fetched) {
				t.Errorf("a hurt fox sat = %v, fetched = %v", sat, fetched)
			}
			if !struck && (!sat || !fetched) {
				t.Errorf("an unhurt fox sat = %v, fetched = %v", sat, fetched)
			}
		})
	}
}
