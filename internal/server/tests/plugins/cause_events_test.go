package plugins_test

import (
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func blockPosOf(v plugin.Vec3) plugin.BlockPos {
	return plugin.BlockPos{X: int(math.Floor(v.X)), Y: int(math.Floor(v.Y)), Z: int(math.Floor(v.Z))}
}

func TestPluginBlockIgniteSource(t *testing.T) {
	t.Parallel()
	t.Run("fire_spread", func(t *testing.T) {
		t.Parallel()
		var got []*plugin.BlockIgniteEvent
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.BlockIgniteEvent) {
				got = append(got, e)
			})
		})
		srv.SetBlockAndTell(4, 63, 0, servertest.BlockState(t, "minecraft:netherrack"))
		srv.SetBlockAndTell(5, 64, 0, servertest.BlockState(t, "minecraft:white_wool"))
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:fire"))
		if !tickUntil(srv, 1500, func() bool { return len(got) > 0 }) {
			t.Fatal("no BlockIgniteEvent from fire spread")
		}
		ev := got[0]
		if ev.Cause != plugin.IgniteSpread {
			t.Errorf("cause = %v, want IgniteSpread", ev.Cause)
		}
		wantSrc := plugin.BlockPos{X: 4, Y: 64, Z: 0}
		if ev.Source == nil || ev.Source.Pos != wantSrc {
			t.Errorf("source = %+v, want %v", ev.Source, wantSrc)
		}
	})

	t.Run("lava", func(t *testing.T) {
		t.Parallel()
		var got []*plugin.BlockIgniteEvent
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.BlockIgniteEvent) {
				if e.Cause == plugin.IgniteLava {
					got = append(got, e)
				}
			})
		})
		// Surround lava at (4, 64, 0) with flammable planks so random ticks light one.
		srv.SetBlockAndTell(4, 63, 0, servertest.BlockState(t, "minecraft:stone"))
		srv.SetBlockAndTell(4, 64, 0, servertest.BlockState(t, "minecraft:lava"))
		planks := servertest.BlockState(t, "minecraft:oak_planks")
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				srv.SetBlockAndTell(4+dx, 64, dz, planks)
			}
		}
		for range 200 {
			if len(got) > 0 {
				break
			}
			srv.TickRandomTicks(10, world.ChunkPos{X: 0, Z: 0})
		}
		if len(got) == 0 {
			t.Fatal("no BlockIgniteEvent from lava")
		}
		wantSrc := plugin.BlockPos{X: 4, Y: 64, Z: 0}
		if got[0].Source == nil || got[0].Source.Pos != wantSrc {
			t.Errorf("source = %+v, want %v", got[0].Source, wantSrc)
		}
	})
}

func TestPluginLoginHostname(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
		port int16
		want string
	}{
		{"plain", "play.example.com", 25565, "play.example.com:25565"},
		{"forge_marker", "play.example.com\x00FML\x00", 25566, "play.example.com\x00FML\x00:25566"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got string
			var mu sync.Mutex
			srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "host", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerLoginEvent) {
					mu.Lock()
					got = e.Hostname
					mu.Unlock()
					e.Result, e.Reason = plugin.LoginOther, plugin.Plain("done")
				})
				return nil
			}}}})
			c := servertest.DialServer(t, srv)
			c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
				b.PutVarInt(protocol.Version)
				b.PutString(tc.addr)
				b.PutI16(tc.port)
				b.PutVarInt(protocol.NextStateLogin)
			})
			c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
				b.PutString("HostCheck")
				b.PutUUID(uuid.Nil)
			})
			c.Expect(protocol.IDLoginDisconnect)
			mu.Lock()
			hostname := got
			mu.Unlock()
			if hostname != tc.want {
				t.Fatalf("Hostname = %q, want %q", hostname, tc.want)
			}
			if !strings.HasPrefix(hostname, "play.example.com") {
				t.Errorf("Hostname %q does not start with play.example.com", hostname)
			}
		})
	}
}

func TestPluginEntityCombustSources(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	c := servertest.DialServer(t, srv)
	c.Join("Tester")
	c.Settle()
	victim := servertest.DialServer(t, srv)
	victim.Join("Victim")
	victim.Settle()
	servertest.HoldTicks(t, srv)

	var mu sync.Mutex
	var events []*plugin.EntityCombustEvent
	clearEvents := func() {
		mu.Lock()
		events = nil
		mu.Unlock()
	}
	hasEvent := func(match func(*plugin.EntityCombustEvent) bool) bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.ContainsFunc(events, match)
	}
	snapshotEvents := func() []*plugin.EntityCombustEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*plugin.EntityCombustEvent, len(events))
		copy(out, events)
		return out
	}
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityCombustEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		})
	})

	// 1. Mob in fire -> ByBlock at the fire cell.
	srv.SetBlockAndTell(6, 63, 0, servertest.BlockState(t, "minecraft:netherrack"))
	srv.SetBlockAndTell(6, 64, 0, servertest.BlockState(t, "minecraft:fire"))
	var fireCow plugin.Entity
	var spawnErr error
	run(func() {
		fireCow, spawnErr = h.Overworld().Spawn(registry.EntityTypeCow, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 6.5, Y: 64, Z: 0.5}}, nil)
	})
	if spawnErr != nil {
		t.Fatalf("spawn cow in fire: %v", spawnErr)
	}
	servertest.RunTicks(t, srv, 3)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == fireCow.UUID() && ev.ByBlock != nil && ev.ByBlock.Pos == (plugin.BlockPos{X: 6, Y: 64, Z: 0}) && ev.ByEntity == nil
	}) {
		t.Errorf("mob in fire: no EntityCombustEvent with ByBlock=(6,64,0); got %+v", snapshotEvents())
	}
	run(func() { fireCow.Remove() })
	srv.SetBlockAndTell(6, 64, 0, servertest.BlockState(t, "minecraft:air"))
	clearEvents()

	// 2. Mob in lava -> ByBlock at the lava cell.
	srv.SetBlockAndTell(8, 63, 0, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlockAndTell(8, 64, 0, servertest.BlockState(t, "minecraft:lava"))
	var lavaCow plugin.Entity
	run(func() {
		lavaCow, spawnErr = h.Overworld().Spawn(registry.EntityTypeCow, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 8.5, Y: 64, Z: 0.5}}, nil)
	})
	if spawnErr != nil {
		t.Fatalf("spawn cow in lava: %v", spawnErr)
	}
	servertest.RunTicks(t, srv, 3)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == lavaCow.UUID() && ev.ByBlock != nil && ev.ByBlock.Pos == (plugin.BlockPos{X: 8, Y: 64, Z: 0}) && ev.ByEntity == nil
	}) {
		t.Errorf("mob in lava: no EntityCombustEvent with ByBlock=(8,64,0); got %+v", snapshotEvents())
	}
	run(func() { lavaCow.Remove() })
	srv.SetBlockAndTell(8, 64, 0, servertest.BlockState(t, "minecraft:air"))
	clearEvents()

	// 3. Fire Aspect melee hit -> ByEntity is the attacking player.
	var targetCow plugin.Entity
	var targetID int32
	var attackerUUID plugin.UUID
	run(func() {
		p := testerOf(t, h)
		attackerUUID = p.UUID()
		p.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 0.5, Y: 64, Z: 0.5}}, plugin.TeleportPlugin)
		var sword plugin.ItemStack
		sword, spawnErr = h.Items().Parse("diamond_sword[enchantments={fire_aspect:1}]", 1)
		if spawnErr != nil {
			return
		}
		p.Inventory().SetItem(0, sword)
		targetCow, spawnErr = h.Overworld().Spawn(registry.EntityTypeCow, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 2.0, Y: 64, Z: 0.5}}, nil)
		if spawnErr == nil {
			targetID = targetCow.ID()
		}
	})
	if spawnErr != nil {
		t.Fatalf("setup fire aspect: %v", spawnErr)
	}
	servertest.RunTicks(t, srv, 2)
	servertest.ReleaseTicks(t, srv)
	c.Attack(targetID)
	c.Settle()
	servertest.HoldTicks(t, srv)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == targetCow.UUID() && ev.ByEntity != nil && ev.ByEntity.UUID() == attackerUUID && ev.ByBlock == nil
	}) {
		t.Errorf("fire aspect on cow: no EntityCombustEvent with ByEntity=%s; got %+v", attackerUUID, snapshotEvents())
	}
	run(func() { targetCow.Remove() })
	clearEvents()

	// 3b. Fire Aspect melee hit on a player -> exactly one EntityCombustEvent
	// (no duplicate from pluginVitalsTick in the same tick).
	var victimID int32
	var victimUUID plugin.UUID
	run(func() {
		vp, ok := h.PlayerNamed("Victim")
		if !ok {
			t.Fatal("Victim player not found")
		}
		victimID, victimUUID = vp.ID(), vp.UUID()
		vp.SetGameMode(plugin.Survival)
		vp.SetFireTicks(0)
		vp.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 2.0, Y: 64, Z: 0.5}}, plugin.TeleportPlugin)
	})
	servertest.RunTicks(t, srv, 2)
	servertest.ReleaseTicks(t, srv)
	c.Attack(victimID)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 3)
	var victimEvents []*plugin.EntityCombustEvent
	for _, ev := range snapshotEvents() {
		if ev.Entity != nil && ev.Entity.UUID() == victimUUID {
			victimEvents = append(victimEvents, ev)
		}
	}
	if len(victimEvents) != 1 {
		t.Fatalf("fire aspect on player: got %d EntityCombustEvents, want 1 (%+v)", len(victimEvents), victimEvents)
	}
	if victimEvents[0].ByEntity == nil || victimEvents[0].ByEntity.UUID() != attackerUUID || victimEvents[0].ByBlock != nil {
		t.Errorf("fire aspect on player: event = %+v, want ByEntity=%s ByBlock=nil", victimEvents[0], attackerUUID)
	}
	run(func() {
		if vp, ok := h.PlayerNamed("Victim"); ok {
			vp.SetFireTicks(0)
			vp.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: -10.5, Y: 64, Z: 0.5}}, plugin.TeleportPlugin)
		}
	})
	clearEvents()

	// 4. Burning arrow -> ByEntity is the arrow.
	var arrowTarget plugin.Entity
	var arrowUUID plugin.UUID
	run(func() {
		arrowTarget, spawnErr = h.Overworld().Spawn(registry.EntityTypeCow, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 4.5, Y: 64, Z: 4.5}}, nil)
		if spawnErr != nil {
			return
		}
		var arr plugin.Entity
		arr, spawnErr = h.Overworld().Spawn(registry.EntityTypeArrow, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 4.5, Y: 66, Z: 4.5}}, func(e plugin.Entity) {
			e.SetFireTicks(100)
			e.SetVelocity(plugin.Vec3{Y: -1.5})
		})
		if spawnErr == nil {
			arrowUUID = arr.UUID()
		}
	})
	if spawnErr != nil {
		t.Fatalf("setup burning arrow: %v", spawnErr)
	}
	servertest.RunTicks(t, srv, 5)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == arrowTarget.UUID() && ev.ByEntity != nil && ev.ByEntity.UUID() == arrowUUID && ev.ByBlock == nil
	}) {
		t.Errorf("burning arrow: no EntityCombustEvent with ByEntity=arrow; got %+v", snapshotEvents())
	}
	run(func() { arrowTarget.Remove() })
	clearEvents()

	// 5. Player in fire -> ByBlock at the fire cell (players have a 20-tick fire-immune cooldown).
	srv.SetBlockAndTell(2, 63, 2, servertest.BlockState(t, "minecraft:netherrack"))
	srv.SetBlockAndTell(2, 64, 2, servertest.BlockState(t, "minecraft:fire"))
	run(func() {
		p := testerOf(t, h)
		p.SetGameMode(plugin.Survival)
		p.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 2.5, Y: 64, Z: 2.5}}, plugin.TeleportPlugin)
	})
	servertest.RunTicks(t, srv, 25)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == attackerUUID && ev.ByBlock != nil && ev.ByBlock.Pos == (plugin.BlockPos{X: 2, Y: 64, Z: 2}) && ev.ByEntity == nil
	}) {
		t.Errorf("player in fire: no EntityCombustEvent with ByBlock=(2,64,2); got %+v", snapshotEvents())
	}
	srv.SetBlockAndTell(2, 64, 2, servertest.BlockState(t, "minecraft:air"))
	clearEvents()

	// 6. Player in lava -> ByBlock at the lava cell.
	srv.SetBlockAndTell(2, 63, 4, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlockAndTell(2, 64, 4, servertest.BlockState(t, "minecraft:lava"))
	run(func() {
		p := testerOf(t, h)
		p.SetFireTicks(0)
		p.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 2.5, Y: 64, Z: 4.5}}, plugin.TeleportPlugin)
	})
	servertest.RunTicks(t, srv, 3)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == attackerUUID && ev.ByBlock != nil && ev.ByBlock.Pos == (plugin.BlockPos{X: 2, Y: 64, Z: 4}) && ev.ByEntity == nil
	}) {
		t.Errorf("player in lava: no EntityCombustEvent with ByBlock=(2,64,4); got %+v", snapshotEvents())
	}
	srv.SetBlockAndTell(2, 64, 4, servertest.BlockState(t, "minecraft:air"))
	clearEvents()

	// 7. Mannequin in fire -> ByBlock at the fire cell.
	srv.SetBlockAndTell(6, 63, 4, servertest.BlockState(t, "minecraft:netherrack"))
	srv.SetBlockAndTell(6, 64, 4, servertest.BlockState(t, "minecraft:fire"))
	var fireMannequin plugin.Entity
	run(func() {
		fireMannequin, spawnErr = h.Overworld().Spawn(registry.EntityTypeMannequin, plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 6.5, Y: 64, Z: 4.5}}, nil)
	})
	if spawnErr != nil {
		t.Fatalf("spawn mannequin in fire: %v", spawnErr)
	}
	servertest.RunTicks(t, srv, 3)
	if !hasEvent(func(ev *plugin.EntityCombustEvent) bool {
		return ev.Entity != nil && ev.Entity.UUID() == fireMannequin.UUID() && ev.ByBlock != nil && ev.ByBlock.Pos == (plugin.BlockPos{X: 6, Y: 64, Z: 4}) && ev.ByEntity == nil
	}) {
		t.Errorf("mannequin in fire: no EntityCombustEvent with ByBlock=(6,64,4); got %+v", snapshotEvents())
	}
	run(func() { fireMannequin.Remove() })
}

func TestPluginEntityOrigin(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	c := servertest.DialServer(t, srv)
	c.Join("Tester")
	c.Settle()
	servertest.HoldTicks(t, srv)

	srv.SetBlockAndTell(3, 64, 0, servertest.BlockState(t, "minecraft:tnt"))
	var playerHasOrigin bool
	run(func() {
		p := testerOf(t, h)
		_, playerHasOrigin = p.Origin()
		p.Teleport(plugin.Location{World: h.Overworld(), Pos: plugin.Vec3{X: 1.5, Y: 64, Z: 0.5}}, plugin.TeleportPlugin)
		p.Inventory().SetItem(0, plugin.NewStack(registry.ItemFlintAndSteel, 1))
	})
	if playerHasOrigin {
		t.Errorf("player Origin() = true, want false")
	}
	servertest.RunTicks(t, srv, 2)
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(0) // main hand
		b.PutPosition(3, 64, 0)
		b.PutVarInt(4) // west face
		b.PutF32(0)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
	// Wait for the client packet to prime the TNT, then tick so it hops away.
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "TNT block to be primed", func() bool {
		return srv.BlockAt(3, 64, 0) != servertest.BlockState(t, "minecraft:tnt")
	})
	servertest.RunTicks(t, srv, 10)
	var tntFound, tntOriginOK bool
	var tntOrigin plugin.Location
	var overworldDim plugin.Key
	run(func() {
		overworldDim = h.Overworld().Dimension()
		for e := range h.Overworld().Entities() {
			if e.Type() == registry.EntityTypeTnt {
				tntFound = true
				tntOrigin, tntOriginOK = e.Origin()
				e.Remove()
				break
			}
		}
	})
	if !tntFound {
		t.Fatal("no primed TNT entity found")
	}
	if !tntOriginOK {
		t.Fatal("primed TNT Origin() returned false")
	}
	wantTNT := plugin.BlockPos{X: 3, Y: 64, Z: 0}
	if got := blockPosOf(tntOrigin.Pos); got != wantTNT {
		t.Errorf("primed TNT Origin() block = %v, want %v (pos=%+v)", got, wantTNT, tntOrigin.Pos)
	}
	if tntOrigin.World == nil || tntOrigin.World.Dimension() != overworldDim {
		t.Errorf("primed TNT Origin().World = %v, want overworld", tntOrigin.World)
	}

	// Falling sand block spawned at (5, 70, 0) keeps its origin after falling.
	srv.SetBlockAndTell(5, 70, 0, servertest.BlockState(t, "minecraft:sand"))
	servertest.RunTicks(t, srv, 6)
	var fbFound, fbOriginOK bool
	var fbOrigin plugin.Location
	var fbPos plugin.Vec3
	run(func() {
		for e := range h.Overworld().Entities() {
			if e.Type() == registry.EntityTypeFallingBlock {
				fbFound = true
				fbOrigin, fbOriginOK = e.Origin()
				fbPos = e.Pos()
				e.Remove()
				break
			}
		}
	})
	if !fbFound {
		t.Fatal("no falling block entity found")
	}
	if !fbOriginOK {
		t.Fatal("falling block Origin() returned false")
	}
	wantFB := plugin.BlockPos{X: 5, Y: 70, Z: 0}
	if got := blockPosOf(fbOrigin.Pos); got != wantFB {
		t.Errorf("falling block Origin() block = %v, want %v", got, wantFB)
	}
	if fbPos.Y >= fbOrigin.Pos.Y {
		t.Errorf("falling block did not fall: pos.Y=%v origin.Y=%v", fbPos.Y, fbOrigin.Pos.Y)
	}
}

func TestPluginEntityOriginPersistence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cfg1 := servertest.BlockTestConfig()
	cfg1.LevelName = dir
	srv1, h1, run1 := worldPlugin(t, cfg1)
	c1 := servertest.DialServer(t, srv1)
	c1.Join("Saver")
	c1.Settle()
	servertest.HoldTicks(t, srv1)

	var tntUUID, standUUID plugin.UUID
	var spawnErr error
	run1(func() {
		var tnt, stand plugin.Entity
		tnt, spawnErr = h1.Overworld().Spawn(registry.EntityTypeTnt, plugin.Location{World: h1.Overworld(), Pos: plugin.Vec3{X: 3.5, Y: 64, Z: 0.5}}, nil)
		if spawnErr != nil {
			return
		}
		tntUUID = tnt.UUID()
		tnt.Teleport(plugin.Location{World: h1.Overworld(), Pos: plugin.Vec3{X: 5.5, Y: 66, Z: 2.5}}, plugin.TeleportPlugin)

		stand, spawnErr = h1.Overworld().Spawn(registry.EntityTypeArmorStand, plugin.Location{World: h1.Overworld(), Pos: plugin.Vec3{X: 4.5, Y: 64, Z: 1.5}}, nil)
		if spawnErr != nil {
			return
		}
		standUUID = stand.UUID()
		stand.Teleport(plugin.Location{World: h1.Overworld(), Pos: plugin.Vec3{X: 7.5, Y: 64, Z: 3.5}}, plugin.TeleportPlugin)
	})
	if spawnErr != nil {
		t.Fatalf("spawn entities: %v", spawnErr)
	}
	servertest.ReleaseTicks(t, srv1)
	srv1.Shutdown()

	cfg2 := servertest.BlockTestConfig()
	cfg2.LevelName = dir
	srv2, h2, run2 := worldPlugin(t, cfg2)
	c2 := servertest.DialServer(t, srv2)
	c2.Join("Loader")
	c2.Settle()
	servertest.HoldTicks(t, srv2)
	servertest.RunTicks(t, srv2, 2)

	var gotTNT, gotStand plugin.Location
	var okTNT, okStand bool
	run2(func() {
		if e, ok := h2.Overworld().Entity(tntUUID); ok {
			gotTNT, okTNT = e.Origin()
		}
		if e, ok := h2.Overworld().Entity(standUUID); ok {
			gotStand, okStand = e.Origin()
		}
	})
	if !okTNT {
		t.Fatal("reloaded TNT Origin() returned false")
	}
	if want := (plugin.BlockPos{X: 3, Y: 64, Z: 0}); blockPosOf(gotTNT.Pos) != want {
		t.Errorf("reloaded TNT Origin() = %v, want %v", blockPosOf(gotTNT.Pos), want)
	}
	if !okStand {
		t.Fatal("reloaded armor stand Origin() returned false")
	}
	if want := (plugin.BlockPos{X: 4, Y: 64, Z: 1}); blockPosOf(gotStand.Pos) != want {
		t.Errorf("reloaded armor stand Origin() = %v, want %v", blockPosOf(gotStand.Pos), want)
	}
}
