package mobs

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The wolf's armor, anger, begging, prey and voices, black box, with the
// loop held.

// wolfSeen is what a client was told over some ticks about one wolf.
type wolfSeen struct {
	sounds   []string
	statuses []uint8
	body     []string // body-slot equipment updates, "" for an emptied slot
	particle bool
	meta     []wolfMeta
}

// watchWolf runs n ticks and gathers what the client heard and saw of the wolf.
func watchWolf(t *testing.T, c *servertest.Client, srv *server.Server, id int32, n int) wolfSeen {
	t.Helper()
	var seen wolfSeen
	c.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch pid {
		case protocol.IDPlaySoundEffect:
			if sound := r.ReadVarInt(); sound > 0 {
				name, _ := servertest.SoundEventName(sound - 1)
				seen.sounds = append(seen.sounds, name)
			}
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == id {
				seen.statuses = append(seen.statuses, r.ReadU8())
			}
		case protocol.IDPlayLevelParticles:
			seen.particle = true
		case protocol.IDPlayEntityEquipment:
			if r.ReadVarInt() != id {
				return false
			}
			for {
				slot := r.ReadU8()
				stack, _ := r.ReadSlot()
				if slot&0x7F == 6 {
					name, _ := registry.ItemName(stack.ItemID)
					seen.body = append(seen.body, name)
				}
				if slot&0x80 == 0 || r.Err() != nil {
					break
				}
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() == id {
				seen.meta = append(seen.meta, readWolfMeta(t, r))
			}
		}
		return false
	})
	return seen
}

func (w wolfSeen) heard(sound string) bool {
	return slices.Contains(w.sounds, sound)
}

func (w wolfSeen) status(status uint8) bool {
	return slices.Contains(w.statuses, status)
}

// ownedWolf is a still tame wolf of "Owner" beside the player.
func ownedWolf(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
	return srv, c, id
}

// TestTheOwnerDressesATameWolfInArmor: the owner's wolf armor goes on a grown
// wolf with a bare body, is spent, sounds and is told in the body slot; a
// second one, a stranger's and a pup's are refused.
// vanilla: Wolf.mobInteract
func TestTheOwnerDressesATameWolfInArmor(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	giveHand(t, srv, "Owner", "minecraft:wolf_armor", 3)
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if item, _ := srv.WolfArmor(id); item != "minecraft:wolf_armor" {
		t.Fatalf("body = %q, want wolf armor", item)
	}
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 2 {
		t.Errorf("armor left = %d, want 2", left)
	}
	if !seen.heard("minecraft:item.armor.equip_wolf") {
		t.Errorf("no equip sound in %v", seen.sounds)
	}
	if len(seen.body) == 0 || seen.body[len(seen.body)-1] != "minecraft:wolf_armor" {
		t.Errorf("body slot updates %v, want wolf armor", seen.body)
	}
	c.UseOn(id)
	watchWolf(t, c, srv, id, 1)
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 2 {
		t.Errorf("a second armor was spent: %d left", left)
	}
	srv.SetWolfArmor(id, "", 0)
	srv.SetMobBaby(id, true)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 1)
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Errorf("a pup was dressed in %q", item)
	}
}

// TestAStrangerCannotDressAWolf: only the owner puts armor on.
// vanilla: Wolf.mobInteract
func TestAStrangerCannotDressAWolf(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, connect, _ := wolfSceneWith(t, "Owner", cfg)
	stranger := connect().Join("Stranger")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.TameMobFor(id, "Owner")
	giveHand(t, srv, "Stranger", "minecraft:wolf_armor", 1)
	stranger.UseOn(id)
	watchWolf(t, stranger, srv, id, 2)
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Errorf("a stranger dressed the wolf in %q", item)
	}
}

// TestWolfArmorSoaksBlowsAndCracks: a blow wears the armor by its damage
// rounded up and leaves the health; crossing a cracking threshold plays the
// crack sound with scute particles; a type that bypasses the armor wounds the
// wolf, and the armor's hurt sound is the armor's.
// vanilla: Wolf.actuallyHurt, Crackiness.WOLF_ARMOR
func TestWolfArmorSoaksBlowsAndCracks(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	srv.SetWolfSoundVariant(id, "minecraft:classic")
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 0)
	start, _ := srv.MobHealth(id)
	srv.HurtMobByWorld(id, 2.5, "minecraft:cactus")
	if hp, _ := srv.MobHealth(id); hp != start {
		t.Errorf("health = %v, want %v (armor takes the blow)", hp, start)
	}
	if _, damage := srv.WolfArmor(id); damage != 3 {
		t.Errorf("armor damage = %d, want 3 (2.5 rounded up)", damage)
	}
	if seen := watchWolf(t, c, srv, id, 25); seen.heard("minecraft:item.wolf_armor.crack") {
		t.Error("a crack below the first threshold was heard")
	}
	if _, hurt, _ := srv.WolfVoice(id); hurt != "minecraft:item.wolf_armor.damage" {
		t.Errorf("armor hurt sound = %q", hurt)
	}
	// 64 durability: more than 5% worn crosses the first crack.
	srv.HurtMobByWorld(id, 1, "minecraft:cactus")
	seen := watchWolf(t, c, srv, id, 25)
	if !seen.heard("minecraft:item.wolf_armor.crack") || !seen.particle {
		t.Errorf("crossing a threshold: sounds %v particles %v", seen.sounds, seen.particle)
	}
	srv.HurtMobByWorld(id, 3, "minecraft:drown")
	if hp, _ := srv.MobHealth(id); hp != start-3 {
		t.Errorf("health after a bypassing blow = %v, want %v", hp, start-3)
	}
	if _, damage := srv.WolfArmor(id); damage != 4 {
		t.Errorf("a bypassing blow wore the armor: damage %d, want 4", damage)
	}
	if _, hurt, _ := srv.WolfVoice(id); hurt != "minecraft:entity.wolf.hurt" {
		t.Errorf("hurt sound after a bypassing blow = %q", hurt)
	}
}

// TestWolfArmorBreaksAtItsLimit: the blow that wears it through removes it,
// tells the body break and plays the break sound.
// vanilla: LivingEntity.onEquippedItemBroken
func TestWolfArmorBreaksAtItsLimit(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 63)
	srv.HurtMobByWorld(id, 1, "minecraft:cactus")
	seen := watchWolf(t, c, srv, id, 2)
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Errorf("body = %q after the last point, want none", item)
	}
	if !seen.status(protocol.EntityStatusBreakBody) || !seen.heard("minecraft:item.wolf_armor.break") {
		t.Errorf("break not told: statuses %v sounds %v", seen.statuses, seen.sounds)
	}
}

// TestAScuteMendsTheArmorOfASittingWolf: an eighth of the durability a scute,
// only while the wolf sits and the armor is worn.
// vanilla: Wolf.mobInteract
func TestAScuteMendsTheArmorOfASittingWolf(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 20)
	giveHand(t, srv, "Owner", "minecraft:armadillo_scute", 5)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 1)
	if _, damage := srv.WolfArmor(id); damage != 20 || srv.InventorySlot("Owner", player.HotbarSlot).Count != 5 {
		t.Fatalf("a standing wolf was mended: damage %d", damage)
	}
	srv.SetWolfStance(id, true)
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 1)
	if _, damage := srv.WolfArmor(id); damage != 12 {
		t.Errorf("damage after a scute = %d, want 12", damage)
	}
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 4 {
		t.Errorf("scutes left = %d, want 4", left)
	}
	if !seen.heard("minecraft:item.wolf_armor.repair") {
		t.Errorf("no repair sound in %v", seen.sounds)
	}
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 0)
	c.UseOn(id)
	watchWolf(t, c, srv, id, 1)
	if left := srv.InventorySlot("Owner", player.HotbarSlot).Count; left != 4 {
		t.Errorf("a scute was spent on undamaged armor: %d left", left)
	}
}

// TestShearsTakeTheArmorOffTheOwnersWolf: the armor drops beside the wolf and
// the shears wear a point; a stranger's shears do not.
// vanilla: Mob.attemptToShearEquipment
func TestShearsTakeTheArmorOffTheOwnersWolf(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 5)
	giveHand(t, srv, "Owner", "minecraft:shears", 1)
	c.UseOn(id)
	seen := watchWolf(t, c, srv, id, 2)
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Fatalf("body = %q after shearing", item)
	}
	if !seen.heard("minecraft:item.armor.unequip_wolf") {
		t.Errorf("no unequip sound in %v", seen.sounds)
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:wolf_armor" {
			found = true
		}
	}
	if !found {
		t.Error("the sheared armor did not drop")
	}
	if got := srv.InventorySlot("Owner", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("shears damage = %d, want 1", got.Damage)
	}
}

// TestAHurtWildWolfStaysAngryAndRefusesBones: a blow starts the 20 to 39
// second anger, which runs down a tick at a time; a bone on an angry wild
// wolf is neither spent nor a taming roll.
// vanilla: NeutralMob.updatePersistentAnger, Wolf.mobInteract
func TestAHurtWildWolfStaysAngryAndRefusesBones(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 6.5, 64, 0.5)
	giveHand(t, srv, "Owner", "minecraft:diamond_sword", 1)
	c.Attack(id)
	watchWolf(t, c, srv, id, 3)
	ticks := srv.MobAngerTicks(id)
	if ticks < 400-3 || ticks > 780 {
		t.Fatalf("anger after a blow = %d ticks, want 400 to 780", ticks)
	}
	giveHand(t, srv, "Owner", "minecraft:bone", 64)
	id = srv.SpawnMob(wolfType, 6.5, 64, 2.5)
	srv.SetMobNoAI(id)
	srv.AngerMob(id, 100)
	before := srv.InventorySlot("Owner", player.HotbarSlot).Count
	if got := useAndStatuses(c, srv, id); len(got) != 0 {
		t.Errorf("a bone on an angry wolf drew statuses %v", got)
	}
	if after := srv.InventorySlot("Owner", player.HotbarSlot).Count; after != before {
		t.Errorf("a bone was spent on an angry wolf: %d to %d", before, after)
	}
	if now := srv.MobAngerTicks(id); now >= 100 || now < 90 {
		t.Errorf("anger after a few ticks = %d, want it counting down from 100", now)
	}
	servertest.AwaitTicks(t, srv, 120)
	if srv.MobAngerTicks(id) != 0 {
		t.Error("the anger did not run out")
	}
	if got := useAndStatuses(c, srv, id); len(got) != 1 {
		t.Errorf("a bone on a calm wolf drew statuses %v, want a roll", got)
	}
}

// TestWolfVoicesFollowTheSoundVariant: each variant speaks its own events, a
// pup the baby set, an angry wolf growls.
// vanilla: Wolf.getAmbientSound, WolfSoundVariants
func TestWolfVoicesFollowTheSoundVariant(t *testing.T) {
	t.Parallel()
	srv, _ := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	for variant, stem := range map[string]string{
		"minecraft:classic": "wolf", "minecraft:puglin": "wolf_puglin", "minecraft:sad": "wolf_sad",
		"minecraft:angry": "wolf_angry", "minecraft:grumpy": "wolf_grumpy",
		"minecraft:big": "wolf_big", "minecraft:cute": "wolf_cute",
	} {
		srv.SetWolfSoundVariant(id, variant)
		ambient, hurt, death := srv.WolfVoice(id)
		if !strings.HasPrefix(ambient, "minecraft:entity."+stem+".") ||
			hurt != "minecraft:entity."+stem+".hurt" || death != "minecraft:entity."+stem+".death" {
			t.Errorf("%s: ambient %q hurt %q death %q", variant, ambient, hurt, death)
		}
	}
	srv.SetWolfSoundVariant(id, "minecraft:big")
	srv.AngerMob(id, 100)
	if ambient, _, _ := srv.WolfVoice(id); ambient != "minecraft:entity.wolf_big.growl" {
		t.Errorf("angry ambient = %q, want a growl", ambient)
	}
	srv.SetMobBaby(id, true)
	if _, hurt, death := srv.WolfVoice(id); hurt != "minecraft:entity.baby_wolf.hurt" || death != "minecraft:entity.baby_wolf.death" {
		t.Errorf("baby hurt %q death %q", hurt, death)
	}
}

// TestAWolfBegsForBonesAndMeat: a player within eight blocks holding a bone or
// meat tilts the wolf's head; with the hand emptied it stops.
// vanilla: BegGoal, Wolf.DATA_INTERESTED_ID
func TestAWolfBegsForBonesAndMeat(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 4.5, 64, 0.5)
	var begged, stopped bool
	for i := 0; i < 20 && !begged; i++ {
		for _, m := range watchWolf(t, c, srv, id, 4).meta {
			begged = begged || m.begTold && m.begging
		}
	}
	if !begged {
		t.Fatal("a wolf never begged for the bone in hand")
	}
	srv.GiveSlot("Owner", player.HotbarSlot, protocol.ItemStack{})
	for i := 0; i < 60 && !stopped; i++ {
		for _, m := range watchWolf(t, c, srv, id, 4).meta {
			stopped = stopped || m.begTold && !m.begging
		}
	}
	if !stopped {
		t.Error("the wolf kept begging with an empty hand")
	}
}

// TestAWildWolfHuntsSheepButATameOneDoesNot: the wild wolf turns on a sheep
// in reach; the tame one leaves it be.
// vanilla: Wolf.registerGoals, NonTameRandomTargetGoal
func TestAWildWolfHuntsSheepButATameOneDoesNot(t *testing.T) {
	t.Parallel()
	for _, tame := range []bool{false, true} {
		srv, c := wolfScene(t, "Owner")
		id := srv.SpawnMob(wolfType, 6.5, 64, 6.5)
		sheep := srv.SpawnMob("minecraft:sheep", 10.5, 64, 6.5)
		srv.SetMobNoAI(sheep)
		if tame {
			srv.TameMobFor(id, "Owner")
		}
		hunted := false
		for i := 0; i < 40 && !hunted; i++ {
			watchWolf(t, c, srv, id, 5)
			hunted = srv.MobTargetID(id) == sheep
		}
		if hunted == tame {
			t.Errorf("tame=%v: hunted the sheep = %v", tame, hunted)
		}
	}
}

// TestWolfArmorBreakingWithoutACrackStillTellsTheBreak: a blow that wears the
// armor through from the uncracked state moves no crack level, yet the break
// event and sound still go out and the crack ones do not.
// vanilla: ItemStack.hurtAndBreak, Wolf.actuallyHurt
func TestWolfArmorBreakingWithoutACrackStillTellsTheBreak(t *testing.T) {
	t.Parallel()
	srv, c, id := ownedWolf(t)
	srv.SetWolfArmor(id, "minecraft:wolf_armor", 0)
	srv.HurtMobByWorld(id, 70, "minecraft:cactus")
	seen := watchWolf(t, c, srv, id, 2)
	if item, _ := srv.WolfArmor(id); item != "" {
		t.Fatalf("body = %q after a blow past the durability", item)
	}
	if !seen.status(protocol.EntityStatusBreakBody) || !seen.heard("minecraft:item.wolf_armor.break") {
		t.Errorf("break not told: statuses %v sounds %v", seen.statuses, seen.sounds)
	}
	if seen.heard("minecraft:item.wolf_armor.crack") {
		t.Error("a crack was heard though no crack level moved")
	}
}

// TestAnAngryWolfTellsItsAngerEndTime: the anger end time (index 22) goes out
// as the game tick the anger ends at when it starts, and as -1 when it runs
// out; the client draws the angry eyes and tail from it.
// vanilla: Wolf.DATA_ANGER_END_TIME, NeutralMob.setTimeToRemainAngry
func TestAnAngryWolfTellsItsAngerEndTime(t *testing.T) {
	t.Parallel()
	srv, c := wolfScene(t, "Owner")
	id := srv.SpawnMob(wolfType, 6.5, 64, 0.5)
	srv.SetMobNoAI(id)
	now, _ := srv.Time()
	srv.AngerMob(id, 60)
	var starts, ends []int64
	for range 20 {
		for _, m := range watchWolf(t, c, srv, id, 5).meta {
			if m.angerTold && m.angerEnd > 0 {
				starts = append(starts, m.angerEnd)
			}
			if m.angerTold && m.angerEnd == -1 {
				ends = append(ends, m.angerEnd)
			}
		}
	}
	if len(starts) == 0 {
		t.Fatal("the anger start was never told")
	}
	if last := starts[len(starts)-1]; last < now+55 || last > now+65 {
		t.Errorf("anger end = %d, want about %d (now %d + 60)", last, now+60, now)
	}
	if len(ends) == 0 {
		t.Error("the end of the anger was never told")
	}
}

// TestAWolfHuntsSkeletonsRabbitsAndBabyTurtles: skeleton kinds are prey to
// every wolf, rabbits and baby turtles on land to wild ones only; a grown
// turtle is left alone.
// vanilla: Wolf.registerGoals
func TestAWolfHuntsSkeletonsRabbitsAndBabyTurtles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, kind string
		baby       bool
		wild, tame bool
	}{
		{"skeleton", "minecraft:skeleton", false, true, true},
		{"rabbit", "minecraft:rabbit", false, true, false},
		{"baby turtle", "minecraft:turtle", true, true, false},
		{"grown turtle", "minecraft:turtle", false, false, false},
	}
	for _, tc := range cases {
		for _, tame := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: " wild", true: " tame"}[tame], func(t *testing.T) {
				t.Parallel()
				srv, c := wolfScene(t, "Owner")
				id := srv.SpawnMob(wolfType, 6.5, 64, 6.5)
				prey := srv.SpawnMob(tc.kind, 10.5, 64, 6.5)
				srv.SetMobNoAI(prey)
				if tc.baby {
					srv.SetMobBaby(prey, true)
				}
				if tame {
					srv.TameMobFor(id, "Owner")
				}
				hunted := false
				for i := 0; i < 40 && !hunted; i++ {
					watchWolf(t, c, srv, id, 5)
					hunted = srv.MobTargetID(id) == prey
				}
				want := tc.wild
				if tame {
					want = tc.tame
				}
				if hunted != want {
					t.Errorf("hunted = %v, want %v", hunted, want)
				}
			})
		}
	}
}

// TestAnAngryWolfTargetsThePlayerItIsAngryAt: under universal_anger a wolf
// angered with no named target turns on the nearby player; without the rule
// it leaves them be.
// vanilla: Wolf.registerGoals, NearestAttackableTargetGoal
func TestAnAngryWolfTargetsThePlayerItIsAngryAt(t *testing.T) {
	t.Parallel()
	for _, universal := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[universal], func(t *testing.T) {
			t.Parallel()
			srv, c := wolfScene(t, "Owner")
			srv.SetGameRule("universal_anger", universal)
			id := srv.SpawnMob(wolfType, 6.5, 64, 0.5)
			srv.AngerMob(id, 600)
			targeted := false
			for i := 0; i < 20 && !targeted; i++ {
				watchWolf(t, c, srv, id, 5)
				targeted = srv.MobTargetID(id) != 0
			}
			if targeted != universal {
				t.Errorf("universal_anger %v: targeted = %v", universal, targeted)
			}
		})
	}
}
