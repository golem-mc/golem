package mobs

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The cod and salmon tests reuse the squid tests' pond and hand-driven
// ticks: the loop is stopped, so the fish live only the ticks the test runs.

func fishType(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID(name)
	if !ok {
		t.Fatalf("no entity type %s", name)
	}
	return id
}

func fishAt(srv *server.Server, id int32) (server.TestMob, bool) {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m, true
		}
	}
	return server.TestMob{}, false
}

// TestACodSwimsAboutItsPond: over two hundred ticks a cod wanders through the
// water, never leaving the pond's sides and ending in the water. A swim order
// that ends under a node above leaves the momentum of the climb, which can
// carry the fish up through the surface for a moment.
// vanilla: AbstractFish.FishSwimGoal, AbstractFish.FishMoveControl
func TestACodSwimsAboutItsPond(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, -8.5)
	start, _ := fishAt(srv, id)

	travelled := 0.0
	last := start
	for range 200 {
		srv.TickMobs(1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatal("the cod left the world")
		}
		if x, z := int32(math.Floor(m.X)), int32(math.Floor(m.Z)); x < pondXLo || x > pondXHi || z < pondZLo || z > pondZHi || m.Y < pondY {
			t.Fatalf("the cod swam out of the pond to (%.2f, %.2f, %.2f)", m.X, m.Y, m.Z)
		}
		travelled += math.Hypot(m.X-last.X, m.Z-last.Z)
		last = m
	}
	if end, _ := fishAt(srv, id); !rules.IsWater(srv.BlockAt(int32(math.Floor(end.X)), int32(math.Floor(end.Y)), int32(math.Floor(end.Z)))) {
		t.Errorf("the cod ended at (%.2f, %.2f, %.2f), out of the water", end.X, end.Y, end.Z)
	}
	if travelled < 3 {
		t.Errorf("the cod swam %.2f blocks in 200 ticks, want it wandering", travelled)
	}
	if hp, _ := srv.MobHealth(id); hp != 3 {
		t.Errorf("the cod has %.1f health, want its 3", hp)
	}
}

// TestAFishOutOfWaterFlopsAndSuffocates: a cod on dry land flops with its
// sound, then drowns when its breath runs out.
// vanilla: AbstractFish.aiStep, WaterAnimal.handleAirSupply
func TestAFishOutOfWaterFlopsAndSuffocates(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	const flop = "minecraft:entity.cod.flop"
	c.ChatCommand("summon minecraft:cod 14.5 64 2.5")
	servertest.AwaitSummonFeedback(t, c, "minecraft:cod", "entity.minecraft.cod")
	id := c.ExpectMobSpawn(fishType(t, "minecraft:cod")).EntityID
	flops := 0
	dead := -1
	for tick := range 400 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID != protocol.IDPlaySoundEffect {
				return
			}
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			if name, _ := servertest.SoundEventName(sound - 1); sound != 0 && name == flop {
				flops++
			}
		})
		if _, ok := fishAt(srv, id); !ok && dead < 0 {
			dead = tick
		}
	}
	if flops == 0 {
		t.Error("the stranded cod never flopped")
	}
	if dead < 0 {
		t.Fatal("the stranded cod was still alive after 400 ticks, want it suffocated")
	}
	if dead < 300 {
		t.Errorf("the stranded cod died at tick %d, want it to hold its 300 ticks of air", dead)
	}
}

func hotbarFish(t *testing.T, srv *server.Server, item string) {
	t.Helper()
	srv.GiveSlot("Striker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, item), Count: 1})
}

// TestAWaterBucketScoopsUpAFishAndPouringReleasesIt: the water bucket used on
// a salmon turns into a salmon bucket with the fish's size and data, the
// fish is gone, and pouring the bucket lets out a salmon of that size marked
// as from a bucket.
// vanilla: Bucketable.bucketMobPickup, MobBucketItem.spawn
func TestAWaterBucketScoopsUpAFishAndPouringReleasesIt(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	salmon := fishType(t, "minecraft:salmon")
	id := summonSquidTagged(t, c, "minecraft:salmon", salmon, pondCentreX, pondNorth, `{type:"large",Health:2f,NoAI:1b,Glowing:1b,Invulnerable:1b,PersistenceRequired:1b}`)
	c.Settle()
	if st, _ := srv.MobFishState(id); st.SalmonSize != 2 {
		t.Fatalf("the summoned salmon has size id %d, want the tag's large", st.SalmonSize)
	}

	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	srv.TickMobs(1)
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("the scooped salmon is still in the world")
	}
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:salmon_bucket") || bucket.Count != 1 {
		t.Fatalf("the hand holds %+v, want one salmon bucket", bucket)
	}
	if size, ok := bucket.SalmonSize(); !ok || size != 2 {
		t.Errorf("the bucket's salmon size = %d (%v), want 2", size, ok)
	}
	if hp, ok := bucket.BucketEntityData()["Health"]; !ok || fmt.Sprint(hp) != "2" {
		t.Errorf("the bucket's Health = %v (%v), want the salmon's 2", hp, ok)
	}

	// vanilla: Bucketable.saveDefaultDataToBucketTag
	c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:bucket_entity_data"`)
	line := servertest.FeedbackComponent(t, c)
	const want = `{Health: 2.0f, Invulnerable: 1b, PersistenceRequired: 1b, NoAI: 1b, Glowing: 1b}`
	if len(line.With) < 2 || line.With[len(line.With)-1].Plain() != want {
		t.Fatalf("scooped payload: %s, want %s", line.Plain(), want)
	}

	// Aimed from the ledge at the pond, the bucket pours its fish out.
	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	c.Settle()
	srv.TickMobs(1)
	var out server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:salmon" {
			out = m
		}
	}
	if out.ID == 0 {
		t.Fatal("pouring the salmon bucket let no salmon out")
	}
	st, _ := srv.MobFishState(out.ID)
	if !st.FromBucket || st.SalmonSize != 2 {
		t.Errorf("the released salmon = %+v, want from-bucket and large", st)
	}
	if hp, _ := srv.MobHealth(out.ID); hp != 2 {
		t.Errorf("the released salmon has %.1f health, want the bucket's 2", hp)
	}
	if held := srv.InventorySlot("Striker", player.HotbarSlot); held.ItemID != servertest.MustItemID(t, "minecraft:bucket") {
		t.Errorf("the hand holds item %d after the pour, want the empty bucket", held.ItemID)
	}
}

// TestFishFormSchools: a cluster of salmon sorts itself into schools of at
// most five, the followers keeping to their leader.
// vanilla: FollowFlockLeaderGoal, AbstractSchoolingFish
func TestFishFormSchools(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	salmon := fishType(t, "minecraft:salmon")
	var ids []int32
	for i := range 8 {
		ids = append(ids, summonSquid(t, c, "minecraft:salmon", salmon, pondCentreX+float64(i%3)*0.5, -8.5+float64(i/3)*0.5))
	}
	for range 700 {
		srv.TickMobs(1)
	}
	followers := 0
	for _, id := range ids {
		st, ok := srv.MobFishState(id)
		if !ok {
			t.Fatalf("salmon %d vanished", id)
		}
		if st.Follower {
			followers++
		}
		if st.SchoolSize > 5 {
			t.Errorf("a school of %d salmon, want at most 5", st.SchoolSize)
		}
	}
	if followers == 0 {
		t.Error("700 ticks among eight salmon formed no school")
	}
}

// heard is one sound effect packet as the client reads it.
type heard struct {
	name     string
	category int32
	pitch    float32
}

func collectSounds(c *servertest.Client) []heard {
	var out []heard
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySoundEffect {
			return
		}
		r := protocol.NewReader(data)
		sound := r.ReadVarInt()
		if sound == 0 {
			return
		}
		name, _ := servertest.SoundEventName(sound - 1)
		category := r.ReadVarInt()
		r.ReadI32()
		r.ReadI32()
		r.ReadI32()
		r.ReadF32()
		out = append(out, heard{name, category, r.ReadF32()})
	})
	return out
}

// vanilla: TropicalFish.saveToBucketTag, MobBucketItem.spawn
func TestTropicalFishBucketPreservesVariant(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	kind := fishType(t, "minecraft:tropical_fish")
	const want = 117506305
	id := summonTropicalFish(t, c, `{Variant:117506305}`)
	checkVariant := func(id int32) {
		t.Helper()
		entry := servertest.MetaOf(c.AwaitMetadataFor(id), 17)
		if entry == nil || entry.Typ != protocol.MetadataVarInt || entry.VarintVal != want {
			t.Fatalf("fish variant metadata = %+v, want %d", entry, want)
		}
	}
	checkVariant(id)
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	srv.TickMobs(1)
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("scooped fish still exists")
	}
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:tropical_fish_bucket") {
		t.Fatalf("held bucket = %+v", bucket)
	}
	for component, want := range map[int32]int32{
		protocol.DataComponentTropicalFishPattern:      257,
		protocol.DataComponentTropicalFishBaseColor:    1,
		protocol.DataComponentTropicalFishPatternColor: 7,
	} {
		found := false
		for _, raw := range bucket.Raw {
			if raw.Type == component {
				r := protocol.NewReader(raw.Data)
				if got := r.ReadVarInt(); got != want || r.Err() != nil {
					t.Fatalf("component %d = %d, want %d", component, got, want)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("bucket lacks component %d", component)
		}
	}
	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	released := c.AwaitSpawn(kind).EntityID
	checkVariant(released)
	st, ok := srv.MobFishState(released)
	if !ok || !st.FromBucket || st.TropicalVariant != want {
		t.Fatalf("released fish = %+v (%v)", st, ok)
	}
	if held := srv.InventorySlot("Striker", player.HotbarSlot); held.ItemID != servertest.MustItemID(t, "minecraft:bucket") {
		t.Fatalf("held after pouring = %+v", held)
	}
}

func summonTropicalFish(t *testing.T, c *servertest.Client, tag string) int32 {
	t.Helper()
	command := fmt.Sprintf("summon minecraft:tropical_fish %g 62 %g", pondCentreX, pondNorth)
	if tag != "" {
		command += " " + tag
	}
	c.ChatCommand(command)
	servertest.AwaitSummonFeedback(t, c, "minecraft:tropical_fish", "entity.minecraft.tropical_fish")
	return c.AwaitSpawn(fishType(t, "minecraft:tropical_fish")).EntityID
}

// TestAPufferfishPuffsUpNearAPlayerAndDeflatesAfter: a survival player within
// two blocks takes it to state 1 at once and state 2 after the inflate
// counter passes 40; with the player gone it deflates after 60 and 100 ticks.
// vanilla: Pufferfish.tick, PufferfishPuffGoal
func TestAPufferfishPuffsUpNearAPlayerAndDeflatesAfter(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:pufferfish", fishType(t, "minecraft:pufferfish"), pondCentreX, pondNorth)
	state := func() int { st, _ := srv.MobFishState(id); return st.PuffState }
	// The fish flees a player, so each tick the player stands two blocks off
	// it again: inside the puff box, outside the sting's.
	near := true
	var told []int32 // the puff states index 17 carried, in order
	collect := func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		entries, _ := servertest.ReadMetadata(r)
		if e := servertest.MetaOf(entries, protocol.MetadataIndexPufferfishPuff); e != nil {
			told = append(told, e.VarintVal)
		}
	}
	ticks := func(n int) {
		for range n {
			if m, ok := fishAt(srv, id); ok && near {
				c.MoveTo(m.X+2, m.Y, m.Z, 0, 0)
			}
			srv.TickMobs(1)
			c.SettleAndCollect(collect)
		}
	}
	// Goal selection runs every other tick.
	ticks(2)
	if state() != 1 {
		t.Fatalf("puff state %d two ticks after a player came near, want 1", state())
	}
	ticks(38)
	if state() != 1 {
		t.Errorf("puff state %d before the inflate counter passes 40, want 1", state())
	}
	ticks(5)
	if state() != 2 {
		t.Fatalf("puff state %d after the counter passed 40, want 2", state())
	}
	if len(told) != 2 || told[0] != 1 || told[1] != 2 {
		t.Errorf("metadata index 17 told %v, want [1 2]", told)
	}

	near = false
	c.MoveTo(playerX, 64, pondZLo+20, 0, 0)
	c.Settle()
	// The goal stops on its first selection; the deflate timer then counts.
	ticks(3)
	if state() != 2 {
		t.Fatalf("puff state %d just after the player left, want 2", state())
	}
	ticks(62)
	if state() != 1 {
		t.Errorf("puff state %d 60 ticks after the player left, want 1", state())
	}
	ticks(30)
	if state() != 1 {
		t.Errorf("puff state %d before the second threshold, want 1", state())
	}
	ticks(15)
	if state() != 0 {
		t.Errorf("puff state %d past 100 ticks, want 0", state())
	}
}

// vanilla: Player.aiStep, Pufferfish.playerTouch, TickRateManager.isEntityFrozen
func TestAFullPufferfishStingsWhileTheWorldIsFrozen(t *testing.T) {
	t.Parallel()
	const name = "Striker"
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(rules.DifficultyNormal)
	release := srv.HoldChunk(3, 172)
	t.Cleanup(release)
	stone := servertest.BlockState(t, "minecraft:stone")
	for z := int32(170); z <= 175; z++ {
		if !srv.SetBlock(3, 249, z, stone) {
			t.Fatal("the stone platform's chunk is not loaded")
		}
	}
	c.ChatCommand("tick freeze")
	servertest.AwaitSystemChatSaying(t, c, "commands.tick.status.frozen")
	c.ChatCommand("tp @s 3.5 250 172.5")
	servertest.ExpectTeleport(t, c)
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
	c.MoveToGround(3.5, 250, 172.5, true)
	c.ChatCommand("summon minecraft:pufferfish 3.5 250 171 {PuffState:2}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:pufferfish", "entity.minecraft.pufferfish")
	age, _ := srv.Time()
	before := srv.PlayerHealth(name)
	var order []string
	var poison servertest.MobEffect
	var shoveX, shoveZ float64
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayGameEvent:
			if r.ReadU8() == protocol.GameEventPufferFishSting {
				order = append(order, "sting")
			}
		case protocol.IDPlayUpdateMobEffect:
			if entityID, effect := r.ReadVarInt(), r.ReadVarInt(); entityID == c.PlayerEntityID && effect == int32(registry.MobEffectPoison) {
				order = append(order, "poison")
				poison.Amplifier, poison.Duration = r.ReadVarInt(), r.ReadVarInt()
			}
		case protocol.IDPlaySetEntityMotion:
			if r.ReadVarInt() == c.PlayerEntityID {
				order = append(order, "motion")
				shoveX, _, shoveZ = r.ReadLpVec3()
			}
		}
		return false
	})
	if now, _ := srv.Time(); now != age {
		t.Errorf("world age advanced from %d to %d while frozen", age, now)
	}
	if lost := before - srv.PlayerHealth(name); lost != 3 {
		t.Errorf("the player lost %.1f health, want 3", lost)
	}
	if got := strings.Join(order, ","); got != "sting,poison,motion" {
		t.Errorf("packet order = %q, want sting,poison,motion", got)
	}
	if poison.Duration != 120 || poison.Amplifier != 0 {
		t.Errorf("poison lasted %d ticks at amplifier %d, want 120 ticks at amplifier 0", poison.Duration, poison.Amplifier)
	}
	if math.Abs(shoveX) > 1e-3 || math.Abs(shoveZ-0.4) > 1e-3 {
		t.Errorf("horizontal shove = (%.4f, %.4f), want (0, 0.4)", shoveX, shoveZ)
	}
}

// TestAFullPufferfishHurtsAndPoisonsATouchingPlayer: the player in a state-2
// fish's box loses 3 health, is told game event 9 and is poisoned for 120
// ticks.
// vanilla: Pufferfish.playerTouch
func TestAFullPufferfishHurtsAndPoisonsATouchingPlayer(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	// Normal leaves the sting's 3 unscaled.
	srv.SetDifficulty(rules.DifficultyNormal)
	id := summonSquidTagged(t, c, "minecraft:pufferfish", fishType(t, "minecraft:pufferfish"), pondCentreX, pondNorth, "{PuffState:2}")
	c.Settle()
	if st, _ := srv.MobFishState(id); st.PuffState != 2 {
		t.Fatalf("the summoned fish has puff state %d, want the tag's 2", st.PuffState)
	}
	before := srv.PlayerHealth("Striker")
	c.MoveTo(pondCentreX, 62, pondNorth, 0, 0)
	c.Settle()
	srv.TickMobs(2) // the second tick's travel step flushes the pending shove
	var stung bool
	var poison servertest.MobEffect
	var shove []float64
	var order []string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == c.PlayerEntityID {
				order = append(order, "motion")
				x, y, z := r.ReadLpVec3()
				if shove == nil {
					shove = []float64{x, y, z}
				}
			}
		case protocol.IDPlayGameEvent:
			if protocol.NewReader(data).ReadU8() == protocol.GameEventPufferFishSting {
				stung = true
				order = append(order, "sting")
			}
		case protocol.IDPlayUpdateMobEffect:
			r := protocol.NewReader(data)
			order = append(order, "effect")
			poison = servertest.MobEffect{Entity: r.ReadVarInt(), Effect: r.ReadVarInt(), Amplifier: r.ReadVarInt(), Duration: r.ReadVarInt()}
		}
	})
	if lost := before - srv.PlayerHealth("Striker"); lost != 3 {
		t.Errorf("the player lost %.1f health, want 3", lost)
	}
	if !stung {
		t.Error("no game event 9 reached the player")
	}
	if want := registry.MustMobEffect("minecraft:poison"); poison.Effect != want || poison.Duration != 120 || poison.Amplifier != 0 {
		t.Errorf("the effect update = %+v, want poison for 120 ticks at amplifier 0", poison)
	}
	// The shove follows the sting and the poison, at full strength: the
	// default knockback's 0.4.
	// vanilla: ServerEntity.sendChanges
	if got := strings.Join(order, ","); !strings.HasPrefix(got, "sting,effect,motion") {
		t.Errorf("packet order = %s, want the sting and the poison before the motion", got)
	}
	if len(shove) != 3 {
		t.Fatal("no motion packet reached the stung player")
	}
	if h := math.Hypot(shove[0], shove[2]); math.Abs(h-0.4) > 1e-3 {
		t.Errorf("the sting's horizontal shove is %.4f, want 0.4", h)
	}
}

// TestANoAIFullPufferfishHurtsAndPoisonsATouchingPlayer: the touch runs from
// the player and the fish's aiStep, neither of which needs AI.
// vanilla: Pufferfish.playerTouch, Mob.isEffectiveAi
func TestANoAIFullPufferfishHurtsAndPoisonsATouchingPlayer(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	srv.SetDifficulty(rules.DifficultyNormal)
	summonSquidTagged(t, c, "minecraft:pufferfish", fishType(t, "minecraft:pufferfish"), pondCentreX, pondNorth, "{PuffState:2,NoAI:1b,Silent:1b}")
	c.Settle()
	before := srv.PlayerHealth("Striker")
	c.MoveTo(pondCentreX, 62, pondNorth, 0, 0)
	c.Settle()
	srv.TickMobs(1)
	if lost := before - srv.PlayerHealth("Striker"); lost != 3 {
		t.Errorf("the player lost %.1f health, want 3", lost)
	}
	if d, a, ok := srv.PlayerEffect("Striker", "minecraft:poison"); !ok || d != 120 || a != 0 {
		t.Errorf("poison = %d ticks at amplifier %d (present %v), want 120 at 0", d, a, ok)
	}
}

// TestANoAIFullPufferfishHurtsAndPoisonsANearbyMob: the aiStep sting reaches
// a scary mob without AI.
// vanilla: Pufferfish.aiStep
func TestANoAIFullPufferfishHurtsAndPoisonsANearbyMob(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	srv.SetDifficulty(rules.DifficultyNormal)
	summonSquidTagged(t, c, "minecraft:pufferfish", fishType(t, "minecraft:pufferfish"), pondCentreX, pondNorth, "{PuffState:2,NoAI:1b,Silent:1b}")
	pig := summonSquidTagged(t, c, "minecraft:pig", fishType(t, "minecraft:pig"), pondCentreX, pondNorth, "{NoAI:1b,Silent:1b}")
	c.Settle()
	before, _ := srv.MobHealth(pig)
	srv.TickMobs(1)
	after, _ := srv.MobHealth(pig)
	if before-after != 3 {
		t.Errorf("the pig lost %.1f health, want 3", before-after)
	}
	if d, a, ok := srv.MobEffectOf(pig, "minecraft:poison"); !ok || d != 120 || a != 0 {
		t.Errorf("poison = %d ticks at amplifier %d (present %v), want 120 at 0", d, a, ok)
	}
}

// TestANoAIFullPufferfishStingsAPlayerRidingABoat: a rider touches within the
// boat's box too, past the on-foot reach.
// vanilla: Player.aiStep, Pufferfish.playerTouch
func TestANoAIFullPufferfishStingsAPlayerRidingABoat(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	srv.SetDifficulty(rules.DifficultyNormal)
	c.ChatCommand("summon oak_boat 8.5 64 -2.5")
	c.Settle()
	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	c.Settle()
	// 1.8 sideways of the boat's centre: outside the rider's own 1.3.
	c.ChatCommand("summon minecraft:pufferfish 10.3 64 -2.5 {PuffState:2,NoAI:1b,Silent:1b}")
	c.Settle()
	before := srv.PlayerHealth("Striker")
	srv.TickMobs(1)
	if lost := before - srv.PlayerHealth("Striker"); lost != 3 {
		t.Errorf("the rider lost %.1f health, want 3", lost)
	}
}

// TestAScaledNoAIPufferfishStingsOnlyAPlayerWhoseSectionWalkFilesIt: the
// player's contact query visits sections 2 blocks past the player's box
// sideways, and files a fish by its position, so a huge fish filed one
// section farther is missed though its box overlaps.
// vanilla: Player.aiStep, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAScaledNoAIPufferfishStingsOnlyAPlayerWhoseSectionWalkFilesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		x    float64
		lost float64
	}{{"same section", 14.9, 3}, {"next section", 17.1, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := squidTests(t)
			srv.SetDifficulty(rules.DifficultyNormal)
			c.MoveTo(12.4, 64, -2.5, 0, 0)
			c.Settle()
			c.ChatCommand(fmt.Sprintf("summon minecraft:pufferfish %.1f 64 -2.5 {PuffState:2,NoAI:1b,Silent:1b,attributes:[{id:\"minecraft:scale\",base:16}]}", tc.x))
			c.Settle()
			before := srv.PlayerHealth("Striker")
			srv.TickMobs(1)
			if lost := before - srv.PlayerHealth("Striker"); lost != tc.lost {
				t.Errorf("the player lost %.1f health, want %.1f", lost, tc.lost)
			}
		})
	}
}
