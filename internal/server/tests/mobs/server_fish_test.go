package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

// TestAFishFleesAPlayerOnTheLedge: a cod in a lake beside the player's ledge
// swims away from him. The pond of the other tests is too small to hold the
// sixteen-block escape the goal picks.
// vanilla: AvoidEntityGoal(Player, 8, 1.6, 1.4)
func TestAFishFleesAPlayerOnTheLedge(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	for x := int32(-12); x <= 28; x++ {
		for z := int32(-40); z <= pondZHi; z++ {
			for y := int32(60); y <= pondY+1; y++ {
				if srv.SetBlock(x, y, z, water); srv.BlockAt(x, y, z) != water {
					t.Fatalf("the lake's chunk at (%d, %d) was not loaded", x, z)
				}
			}
		}
	}
	id := summonSquid(t, c, "minecraft:cod", fishType(t, "minecraft:cod"), pondCentreX, pondNorth)
	before, _ := fishAt(srv, id)
	for range 80 {
		srv.TickMobs(1)
	}
	after, _ := fishAt(srv, id)
	d0 := math.Hypot(before.X-playerX, before.Z-playerZ)
	d1 := math.Hypot(after.X-playerX, after.Z-playerZ)
	if d1-d0 < 4 {
		t.Errorf("the cod went from %.2f to %.2f blocks from the player, want it fled at least 4 further", d0, d1)
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

func killFish(t *testing.T, kind string) []server.TestItem {
	t.Helper()
	srv, c := squidTests(t)
	id := summonSquid(t, c, kind, fishType(t, kind), pondCentreX, -8.5)
	if !srv.KillMob(id) {
		t.Fatal("no fish to kill")
	}
	srv.TickMobs(1)
	return srv.ItemsInDimension("minecraft:overworld")
}

// TestAKilledFishDropsItsMeat: a cod or a salmon leaves its raw fish.
// vanilla: entities/cod, entities/salmon
func TestAKilledFishDropsItsMeat(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			want := kind
			for _, it := range killFish(t, kind) {
				if it.Item == want {
					return
				}
			}
			t.Errorf("the dead %s dropped no raw fish", kind)
		})
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

// TestSalmonComeInThreeSizes: a spawned salmon rolls one of the sizes, and
// the large one's box is the 1.5 scale.
// vanilla: Salmon.finalizeSpawn, Salmon.Variant
func TestSalmonComeInThreeSizes(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	salmon := fishType(t, "minecraft:salmon")
	seen := map[int]bool{}
	for i := range 40 {
		id := summonSquid(t, c, "minecraft:salmon", salmon, pondXLo+0.5+float64(i%4), pondZLo+0.5+float64(i/4))
		st, ok := srv.MobFishState(id)
		if !ok || st.SalmonSize < 0 || st.SalmonSize > 2 {
			t.Fatalf("salmon %d has size id %d", id, st.SalmonSize)
		}
		seen[st.SalmonSize] = true
	}
	if len(seen) != 3 {
		t.Errorf("40 salmon rolled sizes %v, want all three", seen)
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

// TestScoopingASilentFishMakesNoPickupSound: Entity.playSound is skipped for
// a silent body.
// vanilla: Bucketable.bucketMobPickup, Entity.playSound
func TestScoopingASilentFishMakesNoPickupSound(t *testing.T) {
	t.Parallel()
	const pickup = "minecraft:item.bucket.fill_fish"
	for _, silent := range []bool{false, true} {
		srv, c := squidTests(t)
		bucket := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cod_bucket"), Count: 1}
		if silent {
			bucket.SetBucketEntityData(nbt.Compound{"Silent": nbt.Byte(1)})
		}
		srv.GiveSlot("Striker", player.HotbarSlot, bucket)
		c.MoveTo(playerX, 64, playerZ, 180, 60)
		c.Settle()
		servertest.UseHeld(c, 180, 60)
		c.Settle()
		srv.TickMobs(1)
		id := int32(0)
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:cod" {
				id = m.ID
			}
		}
		if id == 0 {
			t.Fatal("the bucket let no cod out")
		}
		collectSounds(c)
		hotbarFish(t, srv, "minecraft:water_bucket")
		c.UseOn(id)
		got := false
		for _, h := range collectSounds(c) {
			got = got || h.name == pickup
		}
		if got == silent {
			t.Errorf("silent %v: pickup sound heard = %v", silent, got)
		}
	}
}

// TestAFlopUsesTheVoicePitch: the flop is Mob.makeSound, so its pitch is the
// voice's roll and not a fixed 1.
// vanilla: AbstractFish.aiStep, Mob.makeSound
func TestAFlopUsesTheVoicePitch(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	for i := range 6 {
		c.ChatCommand(fmt.Sprintf("summon minecraft:cod %d.5 64 2.5", 12+i))
		servertest.AwaitSummonFeedback(t, c, "minecraft:cod", "entity.minecraft.cod")
		c.ExpectMobSpawn(fishType(t, "minecraft:cod"))
	}
	pitches := map[float32]bool{}
	for range 120 {
		srv.TickMobs(1)
		for _, h := range collectSounds(c) {
			if h.name == "minecraft:entity.cod.flop" {
				pitches[h.pitch] = true
			}
		}
	}
	if len(pitches) < 2 {
		t.Errorf("the flops all had pitches %v, want the voice's varying roll", pitches)
	}
}

// TestPouringAFishBucketPlaysTheNeutralEmptySound: the sound the other
// players hear is in the neutral category.
// vanilla: MobBucketItem.playEmptySound
func TestPouringAFishBucketPlaysTheNeutralEmptySound(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	watcher := servertest.DialServer(t, srv).Join("Watcher")
	watcher.MoveToGround(playerX, 64, playerZ, true)
	watcher.Settle()
	c.Settle()

	hotbarFish(t, srv, "minecraft:cod_bucket")
	c.MoveTo(playerX, 64, playerZ, 0, 60)
	c.Settle()
	servertest.UseHeld(c, 0, 60)
	c.Settle()
	var found bool
	for _, h := range collectSounds(watcher) {
		if h.name == "minecraft:item.bucket.empty_fish" {
			found = true
			if h.category != protocol.SoundCategoryNeutral {
				t.Errorf("the empty sound's category = %d, want neutral (%d)", h.category, protocol.SoundCategoryNeutral)
			}
		}
	}
	if !found {
		t.Error("the watcher never heard the fish bucket empty")
	}
}

// TestASalmonBucketWithoutASizeReleasesAMediumSalmon: the bucket spawn skips
// finalizeSpawn, so no size is rolled.
// vanilla: MobBucketItem.spawn
func TestASalmonBucketWithoutASizeReleasesAMediumSalmon(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	for i := range 12 {
		srv.GiveSlot("Striker", player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:salmon_bucket"), Count: 1})
		c.MoveTo(playerX, 64, playerZ, 180, 60)
		c.Settle()
		servertest.UseHeld(c, 180, 60)
		c.Settle()
		srv.TickMobs(1)
		var out server.TestMob
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:salmon" {
				out = m
				srv.KillMob(m.ID)
			}
		}
		if out.ID == 0 {
			t.Fatalf("pour %d let no salmon out", i)
		}
		if st, _ := srv.MobFishState(out.ID); st.SalmonSize != 1 {
			t.Fatalf("pour %d released size id %d, want medium (1)", i, st.SalmonSize)
		}
		srv.TickMobs(1)
	}
}

// TestAFishBucketStillReleasesItsFishWhereWaterEvaporates: the nether
// evaporates the water but MobBucketItem.checkExtraContent runs regardless.
// The column above the netherrack is cleared: the generated nether is solid
// at this height, and a pour into rock places and releases nothing.
// vanilla: BucketItem.use, BucketItem.emptyContents
func TestAFishBucketStillReleasesItsFishWhereWaterEvaporates(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	servertest.CarryIntoNether(t, srv, c, "Striker", 8, 8)
	stone, ok := registry.FindState("minecraft:netherrack", nil)
	if !ok {
		t.Fatal("no netherrack state")
	}
	for y := int32(40); y <= 43; y++ {
		srv.SetBlockInDimension(world.NetherName, 8, y, 8, 0)
	}
	srv.SetBlockInDimension(world.NetherName, 8, 39, 8, stone)
	srv.GiveSlot("Striker", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cod_bucket"), Count: 1})
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	c.Settle()
	srv.TickMobs(1)
	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.Kind == "minecraft:cod" {
			return
		}
	}
	t.Error("the cod bucket let no cod out in the nether")
}

// vanilla: TropicalFish.COMMON_VARIANTS, finalizeSpawn
func TestTropicalFishCommonVariants(t *testing.T) {
	t.Parallel()
	_, c := squidTests(t)
	common := map[int32]bool{}
	for _, v := range []int32{
		117506305, 117899265, 185008129, 117441793, 118161664, 65536,
		50726144, 67764993, 234882305, 67110144, 117441025, 16778497,
		101253888, 50660352, 918529, 235340288, 918273, 67108865,
		917504, 459008, 67699456, 67371009,
	} {
		common[v] = true
	}
	matches := 0
	for i := range 40 {
		id := summonTropicalFish(t, c, "")
		// Variant zero is the client's default and may be omitted.
		v := int32(0)
		if entry := servertest.MetaOf(c.AwaitMetadataFor(id), 17); entry != nil {
			if entry.Typ != protocol.MetadataVarInt {
				t.Fatalf("variant serializer = %d", entry.Typ)
			}
			v = entry.VarintVal
		}
		if common[v] {
			matches++
		}
		if v&0xff > 1 || v>>8&0xff > 5 || v>>16&0xff > 15 || v>>24&0xff > 15 {
			t.Fatalf("fish %d has invalid variant %x", i, v)
		}
	}
	if matches <= 20 {
		t.Fatalf("%d/40 variants are common, want most", matches)
	}
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
	srv.TickMobs(1)
	var stung bool
	var poison servertest.MobEffect
	c.SettleAndCollect(func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlayGameEvent:
			if protocol.NewReader(data).ReadU8() == protocol.GameEventPufferFishSting {
				stung = true
			}
		case protocol.IDPlayUpdateMobEffect:
			r := protocol.NewReader(data)
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
}

// TestAPufferfishBucketKeepsThePuffAndReleasesFromBucket: a water bucket
// scoops a puffed fish into a pufferfish bucket, and pouring it lets out a
// from-bucket pufferfish in the same state.
// vanilla: Pufferfish.saveToBucketTag, Bucketable
func TestAPufferfishBucketKeepsThePuffAndReleasesFromBucket(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	kind := fishType(t, "minecraft:pufferfish")
	id := summonSquidTagged(t, c, "minecraft:pufferfish", kind, pondCentreX, pondNorth, "{PuffState:1}")
	c.Settle()
	hotbarFish(t, srv, "minecraft:water_bucket")
	c.UseOn(id)
	c.Settle()
	srv.TickMobs(1)
	if _, ok := fishAt(srv, id); ok {
		t.Fatal("the scooped pufferfish is still in the world")
	}
	bucket := srv.InventorySlot("Striker", player.HotbarSlot)
	if bucket.ItemID != servertest.MustItemID(t, "minecraft:pufferfish_bucket") || bucket.Count != 1 {
		t.Fatalf("the hand holds %+v, want one pufferfish bucket", bucket)
	}
	c.MoveTo(playerX, 64, playerZ, 180, 60)
	c.Settle()
	servertest.UseHeld(c, 180, 60)
	c.Settle()
	srv.TickMobs(1)
	var out server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:pufferfish" {
			out = m
		}
	}
	if out.ID == 0 {
		t.Fatal("pouring the bucket let no pufferfish out")
	}
	if st, _ := srv.MobFishState(out.ID); !st.FromBucket || st.PuffState != 1 {
		t.Errorf("the released pufferfish = %+v, want from-bucket at puff state 1", st)
	}
}

// TestAKilledPufferfishDropsOnePufferfish: the loot is exactly one pufferfish
// (the bone meal chance is 5%, so only the fish is counted).
// vanilla: entities/pufferfish
func TestAKilledPufferfishDropsOnePufferfish(t *testing.T) {
	t.Parallel()
	n := 0
	for _, it := range killFish(t, "minecraft:pufferfish") {
		if it.Item == "minecraft:pufferfish" {
			n += int(it.Count)
		}
	}
	if n != 1 {
		t.Errorf("the dead pufferfish dropped %d pufferfish, want 1", n)
	}
}
