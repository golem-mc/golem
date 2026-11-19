package world

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func joinDragonFight(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	return joinDragonFightWith(t, servertest.CrossingTestConfig(t))
}

// joinDragonFightWith is joinDragonFight on the given config.
func joinDragonFightWith(t *testing.T, cfg *config.Config) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.MovePlayerToDimension("DragonWatcher", world.EndName, 0, 70, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no End view")
	}
	servertest.HoldTicks(t, srv)
	var dragon int32
	// The fight's player scan runs every 20 ticks. Stop on its spawn tick so
	// the movement tick cannot obscure the spawn's y=128.
	for range 40 {
		// The arena generates off the tick, in wall time no tick count bounds.
		servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
		servertest.RunTicks(t, srv, 1)
		for _, m := range srv.MobsInDimension(world.EndName) {
			if m.Kind == "minecraft:ender_dragon" {
				dragon = m.ID
				break
			}
		}
		if dragon != 0 {
			break
		}
	}
	if dragon == 0 {
		t.Fatal("no dragon within 40 ticks")
	}
	// One tick positions the parts, which a NoAI dragon keeps.
	servertest.RunTicks(t, srv, 1)
	srv.SetMobNoAI(dragon)
	return srv, c, dragon
}

func TestEndDragonSpawnAndBossBar(t *testing.T) {
	_, c, id := joinDragonFight(t)
	// The bar is added with the watcher, the dragon once its arena has
	// loaded, so the two arrive in either order.
	var boss []byte
	spawned := false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayBossEvent:
			if boss == nil {
				boss = data
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			got := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() != dragonTypeID() {
				continue
			}
			r.ReadF64()
			if y := r.ReadF64(); got != id || y != 128 {
				t.Fatalf("dragon spawn id=%d y=%v, want %d at 128", got, y, id)
			}
			spawned = true
		}
		if boss != nil && spawned {
			break
		}
	}
	if boss == nil || !spawned {
		t.Fatalf("boss event arrived=%v, dragon spawn arrived=%v", boss != nil, spawned)
	}
	r := protocol.NewReader(boss)
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.BossEventAdd {
		t.Fatalf("boss action=%d", got)
	}
	name, ok := protocol.ReadText(r)
	if !ok || name.Translate != "entity.minecraft.ender_dragon" {
		t.Fatalf("boss title=%+v", name)
	}
	if fill := r.ReadF32(); fill != 1 {
		t.Errorf("fill=%v", fill)
	}
	if color := r.ReadVarInt(); color != 0 {
		t.Errorf("color=%d, want pink (0)", color)
	}
	if overlay := r.ReadVarInt(); overlay != 0 {
		t.Errorf("overlay=%d", overlay)
	}
	if flags := r.ReadU8(); flags != 6 {
		t.Errorf("flags=%d, want music and fog", flags)
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Fatal("malformed boss event")
	}
}

func TestEndDragonBodyDamageAndCrystalHealing(t *testing.T) {
	srv, c, id := joinDragonFight(t)
	// The body's part is dragon+3. Stand beside its 5x3 box, within attack
	// range, with a fully recovered eight-damage netherite sword.
	c.MoveTo(2, 128, 0, 0, 0)
	c.Settle()
	srv.GiveSlot("DragonWatcher", 36, protocol.ItemStack{ItemID: int32(registry.ItemNetheriteSword), Count: 1})
	servertest.RunTicks(t, srv, 21)
	c.Attack(id + 3)
	c.Settle()
	if health, ok := srv.MobHealthInDimension(world.EndName, id); !ok || health != 197 {
		t.Fatalf("body hit health=%v, want 200-(8/4+1)=197", health)
	}
	servertest.RunTicks(t, srv, 1)
	for {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayBossEvent))
		r.ReadUUID()
		if r.ReadVarInt() != protocol.BossEventProgress {
			continue
		}
		if fill := r.ReadF32(); fill != float32(197.0/200) {
			t.Fatalf("body hit bar=%v", fill)
		}
		break
	}
	srv.SpawnEndCrystalInDimension(world.EndName, 0, 128, 10)
	// Selection is a 1/10 roll; wait for the first healed point, then verify
	// the exact ten-tick cadence while the dragon is held in place.
	healed := false
	for range 1000 {
		servertest.RunTicks(t, srv, 1)
		h, _ := srv.MobHealthInDimension(world.EndName, id)
		if h == 198 {
			healed = true
			break
		}
	}
	if !healed {
		t.Fatal("dragon did not select its nearby crystal")
	}
	servertest.RunTicks(t, srv, 9)
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 198 {
		t.Fatalf("healed before ten ticks: %v", h)
	}
	servertest.RunTicks(t, srv, 1)
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 199 {
		t.Fatalf("health after ten ticks=%v, want 199", h)
	}
}

func dragonTypeID() int32 { id, _ := registry.EntityTypeID("minecraft:ender_dragon"); return id }

func TestDestroyingTheHealingCrystalHurtsTheDragonAudibly(t *testing.T) {
	srv, c, dragon := joinDragonFight(t)
	c.ChatCommand("gamemode spectator")
	c.ChatCommand("data merge entity @e[type=ender_dragon,limit=1] {Health:160f}")
	c.MoveTo(0, 138, -24, 0, 15)
	c.Settle()
	// Outside the crystal's 12-block blast radius, the dragon can only be
	// hurt by onCrystalDestroyed's head hit.
	crystal := srv.SpawnEndCrystalInDimension(world.EndName, 0, 128, 20)
	servertest.UntilTicks(t, srv, 100, "dragon healing from the crystal", func() bool {
		h, _ := srv.MobHealthInDimension(world.EndName, dragon)
		return h > 160
	})
	before, _ := srv.MobHealthInDimension(world.EndName, dragon)
	c.Settle()
	c.ChatCommand("damage @e[type=end_crystal,x=0,y=128,z=20,sort=nearest,limit=1] 1 minecraft:player_attack by @s")
	heardHurt, damageEvent, healthMetadata := false, false, false
	c.ReadTicks(srv, 2, func(packet int32, data []byte) bool {
		switch packet {
		case protocol.IDPlaySoundEffect:
			sound, category, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
			if sound == "minecraft:entity.ender_dragon.hurt" {
				heardHurt = true
				if category != protocol.SoundCategoryHostile || volume != 5 {
					t.Errorf("hurt category=%d volume=%v, want hostile at 5", category, volume)
				}
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != dragon {
				break
			}
			damageEvent = true
			if typ := r.ReadVarInt(); typ != registry.MustID("minecraft:damage_type", "minecraft:player_explosion") {
				t.Errorf("damage type=%d, want player_explosion", typ)
			}
			if cause, direct := r.ReadVarInt(), r.ReadVarInt(); cause != c.PlayerEntityID+1 || direct != crystal+1 {
				t.Errorf("damage cause=%d direct=%d, want player=%d crystal=%d", cause, direct, c.PlayerEntityID+1, crystal+1)
			}
			if r.ReadBool() || r.Err() != nil || r.Remaining() != 0 {
				t.Error("malformed crystal damage event")
			}
		case protocol.IDPlayEntityMetadata:
			if m, ok := servertest.ReadBodyMetadata(c, data, map[int32]bool{dragon: true}); ok && m.HasHealth && m.Health == float32(before-10) {
				healthMetadata = true
			}
		}
		return heardHurt && damageEvent && healthMetadata
	})
	if !heardHurt || !damageEvent || !healthMetadata {
		t.Errorf("crystal hurt within 2 ticks: sound=%v damage event=%v health metadata=%v", heardHurt, damageEvent, healthMetadata)
	}
	if h, _ := srv.MobHealthInDimension(world.EndName, dragon); h != before-10 {
		t.Errorf("crystal hurt health=%v, want %v", h, before-10)
	}
}

// TestEndDragonFightScanFindsCarriedPortal checks that the fight's first scan
// reads an end portal entity in the arena as a fight already won: no dragon
// spawns and previously_killed is saved.
// vanilla: EnderDragonFight.scanState, hasActiveExitPortal
func TestEndDragonFightScanFindsCarriedPortal(t *testing.T) {
	srv, c := servertest.Join(t, servertest.CrossingTestConfig(t), "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.MovePlayerToDimension("DragonWatcher", world.EndName, 0, 70, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no End view")
	}
	servertest.HoldTicks(t, srv)
	srv.SetBlockInDimension(world.EndName, 3, 60, 0, registry.BlockStates["minecraft:end_portal"])
	srv.SetBlockEntityTagInDimension(world.EndName, 3, 60, 0, "minecraft:end_portal", nbt.Compound{})
	// The fight admits the watcher and requests the arena on tick 20; the tick
	// after the arena settles completes the scan.
	servertest.RunTicks(t, srv, 20)
	servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
	servertest.RunTicks(t, srv, 1)
	for _, m := range srv.MobsInDimension(world.EndName) {
		if m.Kind == "minecraft:ender_dragon" {
			t.Fatal("a dragon spawned over an existing exit portal")
		}
	}
	saved := srv.SavedDragonFight()
	if nbt.BoolField(saved, "needs_state_scanning") {
		t.Fatal("the fight never scanned")
	}
	if !nbt.BoolField(saved, "previously_killed") {
		t.Fatal("previously_killed not saved")
	}
}
