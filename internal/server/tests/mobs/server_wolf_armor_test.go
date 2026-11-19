package mobs

import (
	"slices"
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
