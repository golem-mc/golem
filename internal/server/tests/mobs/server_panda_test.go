package mobs

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const panda = "minecraft:panda"

func pandaType() int32 { id, _ := registry.EntityTypeID(panda); return int32(id) }

func pandaBabies(srv *server.Server) (babies int) {
	for _, m := range srv.Mobs() {
		if m.Kind == panda && m.Baby {
			babies++
		}
	}
	return babies
}

func TestAPandaSpawnsWithAGene(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	id := srv.SpawnMob(panda, 3.5, 64, 0.5)
	if m := c.ExpectMobSpawn(pandaType()); m.EntityID != id || m.Health != 20 {
		t.Fatalf("spawn = %d, health %v; want %d, 20", m.EntityID, m.Health, id)
	}
	data := entityData(t, c, "@e[type=panda,limit=1]")
	genes := []string{"normal", "lazy", "worried", "playful", "brown", "weak", "aggressive"}
	for _, key := range []string{"MainGene", "HiddenGene"} {
		found := false
		for _, g := range genes {
			found = found || strings.Contains(data, key+": \""+g+"\"")
		}
		if !found {
			t.Errorf("%s is not a gene name in %s", key, data)
		}
	}
}

// mobAttribute reads the attribute value of the first mob of the kind
// through /attribute.
func mobAttribute(t *testing.T, c *servertest.Client, kind, attribute string) float64 {
	t.Helper()
	c.ChatCommand("attribute @e[type=" + kind + ",limit=1] " + attribute + " get")
	var value string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
			value = text.With[len(text.With)-1].Text
		}
	})
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("%s = %q, not a number", attribute, value)
	}
	return v
}

func TestAWeakPandaHasTenHealth(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.ChatCommand("summon minecraft:panda 1.5 64 0.5 {MainGene:\"weak\",HiddenGene:\"weak\"}")
	c.ExpectMobSpawn(pandaType())
	if v := mobAttribute(t, c, "panda", "minecraft:max_health"); v != 10 {
		t.Fatalf("max_health = %v, want 10", v)
	}
}

// TestPandaGenesSizeTheBasesASavedAttributeLoadsOnto: an unrelated saved
// attribute leaves the weak and lazy bases in place.
func TestPandaGenesSizeTheBasesASavedAttributeLoadsOnto(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	armor := `attributes:[{id:"minecraft:armor",base:2.0d}]`
	c.ChatCommand(`summon minecraft:panda 1.5 64 0.5 {MainGene:"weak",HiddenGene:"weak",` + armor + `}`)
	c.ExpectMobSpawn(pandaType())
	if v := mobAttribute(t, c, "panda", "minecraft:max_health"); v != 10 {
		t.Errorf("weak max_health = %v, want 10", v)
	}
	// The killed panda's bamboo would otherwise spawn as an item that can
	// reach the client ahead of the next panda.
	c.ChatCommand("gamerule mob_drops false")
	c.ChatCommand("kill @e[type=panda]")
	c.ChatCommand(`summon minecraft:panda 1.5 64 0.5 {MainGene:"lazy",HiddenGene:"lazy",` + armor + `}`)
	c.ExpectMobSpawn(pandaType())
	if v := mobAttribute(t, c, "panda", "minecraft:movement_speed"); math.Abs(v-0.07) > 1e-6 {
		t.Errorf("lazy movement_speed = %v, want 0.07", v)
	}
}

// TestAWeakPandaKeepsItsSavedMaxHealthAndHealth: a saved max_health base
// outranks the weak gene's 10, and the saved Health is capped by it alone.
func TestAWeakPandaKeepsItsSavedMaxHealthAndHealth(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.ChatCommand(`summon minecraft:panda 1.5 64 0.5 {MainGene:"weak",HiddenGene:"weak",Health:30f,` +
		`attributes:[{id:"minecraft:max_health",base:40.0d}]}`)
	c.ExpectMobSpawn(pandaType())
	if v := mobAttribute(t, c, "panda", "minecraft:max_health"); v != 40 {
		t.Errorf("max_health = %v, want the saved 40", v)
	}
	if data := entityData(t, c, "@e[type=panda,limit=1]"); !strings.Contains(data, "Health: 30.0f") {
		t.Errorf("data = %s, want Health 30.0f", data)
	}
}

// TestAPandaRefusesALead: vanilla's Panda.canBeLeashed is false, so the lead
// neither ties nor is spent, where a cow beside it ties.
func TestAPandaRefusesALead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()
	giveLead(t, srv, "Keeper", 2)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	pandaID := srv.SpawnMob(panda, 2.5, 64, 7.5)
	c.ExpectMobSpawn(pandaType())

	c.UseOn(cowID)
	if attached, _ := c.ExpectLinkPacket(); attached != cowID {
		t.Fatalf("the cow's link attaches %d, want %d", attached, cowID)
	}
	c.Settle()

	c.UseOn(pandaID)
	srv.TickMobs(3)
	linked := false
	c.SettleAndCollect(func(id int32, _ []byte) { linked = linked || id == protocol.IDPlaySetEntityLink })
	if linked {
		t.Error("a lead on a panda tied a link")
	}
	if got := srv.InventorySlot("Keeper", player.HotbarSlot); got.Count != 1 {
		t.Errorf("the hand after the refused lead = %+v, want the one lead the cow left", got)
	}
}

// feedPanda feeds the panda bamboo and reads, in ticks, the hearts that follow.
func feedPanda(t *testing.T, srv *server.Server, c *servertest.Client, id int32) {
	t.Helper()
	c.UseOn(id)
	if !c.ReadTicks(srv, 20, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == id && r.ReadU8() == loveHearts
	}) {
		t.Fatalf("panda %d showed no hearts within 20 ticks", id)
	}
}

// plantPandas joins with the world frozen, holds the loop before anything
// spawns or is fed, and leaves two fed pandas whose bamboo is spent.
func plantPandas(t *testing.T, bamboo bool) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:bamboo", 64,
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)
	if bamboo {
		srv.SetBlock(4, 64, 4, blockNamed(t, "minecraft:bamboo"))
	}
	p1 := srv.SpawnMob(panda, 1.5, 64, 2.5)
	p2 := srv.SpawnMob(panda, 2.5, 64, 2.5)
	feedPanda(t, srv, c, p1)
	feedPanda(t, srv, c, p2)
	if n := srv.InventorySlot("Rancher", player.HotbarSlot).Count; n != 62 {
		t.Fatalf("bamboo left = %d, want 62", n)
	}
	return srv, c
}

func TestTwoFedPandasNextToBambooBreed(t *testing.T) {
	t.Parallel()
	srv, c := plantPandas(t, true)
	if !servertest.ASpawnOfWithinTicks(c, srv, 300, pandaType()) {
		t.Fatal("no panda cub spawned within 300 ticks")
	}
	if n := pandaBabies(srv); n != 1 {
		t.Fatalf("%d cubs, want 1", n)
	}
}

func TestPandasWithoutBambooAreUnhappy(t *testing.T) {
	t.Parallel()
	srv, c := plantPandas(t, false)
	sounded := false
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			sounded = sounded || name == "minecraft:entity.panda.cant_breed"
		}
		return false
	})
	if n := pandaBabies(srv); n != 0 {
		t.Fatalf("%d cubs without bamboo", n)
	}
	if !sounded {
		t.Fatal("no cant_breed sound within 200 ticks")
	}
}

// TestAPandaSpawnEggOnAWeakPandaMakesAWeakCub: the egg's offspring runs the
// breeding gene inheritance, so a weak/weak parent's cub carries weak genes.
func TestAPandaSpawnEggOnAWeakPandaMakesAWeakCub(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"),
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:panda_spawn_egg"), Count: 1})
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:panda 1.5 64 0.5 {MainGene:\"weak\",HiddenGene:\"weak\",Tags:[\"parent\"]}")
	// The command runs when the connection delivers it, not on a fixed tick.
	if !c.ReadTicks(srv, 200, func(id int32, _ []byte) bool { return id == protocol.IDPlaySystemChat }) {
		t.Fatal("no summon feedback within 200 ticks")
	}
	c.UseOn(srv.Mobs()[0].ID)
	if !c.ReadTicks(srv, 200, func(id int32, _ []byte) bool { return id == protocol.IDPlaySpawnEntity }) {
		t.Fatal("no cub spawned within 200 ticks")
	}
	if pandaBabies(srv) != 1 {
		t.Fatalf("%d cubs, want 1", pandaBabies(srv))
	}
	data := entityData(t, c, "@e[type=panda,tag=!parent,limit=1]")
	if !strings.Contains(data, "MainGene: \"weak\"") || !strings.Contains(data, "HiddenGene: \"weak\"") {
		t.Fatalf("cub data = %s, want weak genes", data)
	}
}

// pandaFlagsWithin reports whether metadata sets one of the flag bits within n ticks.
func pandaFlagsWithin(c *servertest.Client, srv *server.Server, n int, id int32, want uint8) bool {
	found := false
	c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		entries, ok := servertest.ReadMetadata(r)
		if e := servertest.MetaOf(entries, protocol.MetadataIndexPandaFlags); ok && e != nil && e.ByteVal&want != 0 {
			found = true
		}
		return found
	})
	return found
}

func TestAFedPandaThatCannotBreedSitsAndHoldsTheBamboo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:bamboo", 64,
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(panda, 1.5, 64, 2.5)
	feedPanda(t, srv, c, id)
	c.UseOn(id)
	if !pandaFlagsWithin(c, srv, 5, id, 8) {
		t.Fatal("the fed panda showed no sit flag within 5 ticks")
	}
	if got := srv.MobMainHand(id); got.ItemID != servertest.MustItemID(t, "minecraft:bamboo") {
		t.Fatalf("main hand = %+v, want bamboo", got)
	}
}

// TestABabyWeakPandaSneezesAndDropsASlimeBall: the gift is 1 in 700, so the
// seed is one whose first sneeze rolls the slime ball.
func TestABabyWeakPandaSneezesAndDropsASlimeBall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper")
	srv.StopTick()
	id := srv.SpawnMobSeeded(panda, 1.5, 64, 0.5, 517)
	srv.SetMobBaby(id, true)
	srv.ForcePandaSneeze(id)
	srv.TickMobs(25)
	sneezed := false
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			sneezed = sneezed || name == "minecraft:entity.panda.sneeze"
		}
	})
	if !sneezed {
		t.Fatal("no sneeze sound within 25 ticks")
	}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:slime_ball" {
			return
		}
	}
	t.Fatal("the sneeze dropped no slime ball")
}

func TestAWorriedPandaSitsInThunder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.ChatCommand(`summon minecraft:panda 1.5 64 0.5 {MainGene:"worried",HiddenGene:"worried"}`)
	c.ExpectMobSpawn(pandaType())
	id := srv.Mobs()[0].ID
	c.ChatCommand("weather thunder")
	// The thunder level climbs 0.01 a tick, so isThundering turns true after about 100.
	if !pandaFlagsWithin(c, srv, 150, id, 8) {
		t.Fatal("the worried panda did not sit within 150 ticks of thunder")
	}
}

func TestAPandaPicksUpBambooLyingNextToIt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.MobTestConfig(), "Keeper", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(panda, 1.5, 64, 0.5)
	srv.SpawnItemEntityDelay(1.5, 64, 0.5, "minecraft:bamboo", 1, 0)
	servertest.UntilTicks(t, srv, 20, "the panda picking up the bamboo", func() bool {
		return !srv.MobMainHand(id).Empty()
	})
	if got := srv.MobMainHand(id); got.ItemID != servertest.MustItemID(t, "minecraft:bamboo") {
		t.Fatalf("main hand = %+v, want bamboo", got)
	}
}

func TestAScaredPandaRefusesBamboo(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bamboo"), Count: 4})
	c.ChatCommand(`summon minecraft:panda 1.5 64 0.5 {MainGene:"worried",HiddenGene:"worried",Age:-24000}`)
	c.ExpectMobSpawn(pandaType())
	id := srv.Mobs()[0].ID
	c.ChatCommand("weather thunder")
	if !pandaFlagsWithin(c, srv, 150, id, 8) {
		t.Fatal("the worried panda did not sit within 150 ticks of thunder")
	}
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	c.UseOn(id)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	if !srv.MobIsBaby(id) {
		t.Fatal("a scared baby panda grew up from bamboo")
	}
	if n := srv.InventorySlot("Keeper", player.HotbarSlot).Count; n != 4 {
		t.Fatalf("bamboo count = %d, want 4", n)
	}
}
