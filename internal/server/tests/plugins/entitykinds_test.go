package plugins_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func kindsFixture(t *testing.T) (*server.Server, plugin.World, func(func()), func(string), func()) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	save := func() {
		servertest.ReleaseTicks(t, srv)
		srv.SaveAll(true)
		servertest.HoldTicks(t, srv)
	}
	return srv, h.Overworld(), run, func(line string) { srv.RunLine(line) }, save
}

func spawnAt(t *testing.T, w plugin.World, k registry.EntityType, configure func(plugin.Entity)) plugin.Entity {
	t.Helper()
	e, err := w.Spawn(k, plugin.Location{World: w, Pos: plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}}, configure)
	if err != nil || e == nil {
		t.Fatalf("spawn %s: %v", k.Name(), err)
	}
	return e
}

func TestPluginSpawnConfigureRemoveAndData(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	sw := srv.OverworldWorld()
	sw.Acquire(world.ChunkPos{X: 0, Z: 0})
	sw.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	run(func() {
		w := h.Overworld()
		name := plugin.Plain("Bessie")
		var configured plugin.Entity
		cow := spawnAt(t, w, registry.EntityTypeCow, func(e plugin.Entity) {
			configured = e
			e.SetCustomName(&name)
			e.(plugin.Mob).SetPersistent(true)
		})
		if configured == nil || configured.UUID() != cow.UUID() || !cow.IsValid() {
			t.Fatal("configure did not see the spawned entity")
		}
		if n := cow.CustomName(); n == nil || n.PlainString() != "Bessie" || !cow.(plugin.Mob).Persistent() {
			t.Errorf("configured state lost: %v", cow.CustomName())
		}
		if _, ok := w.Entity(cow.UUID()); !ok {
			t.Error("spawned entity not in the world")
		}
		cow.SetCustomName(nil)
		if cow.CustomName() != nil {
			t.Error("name not cleared")
		}
		if _, err := w.Spawn(registry.EntityTypePlayer, plugin.Location{World: w}, nil); err == nil {
			t.Error("a player was spawned")
		}

		key := plugin.Key{Namespace: "test", Path: "mark"}
		d := cow.Data()
		d.Set(key, int32(7))
		d.Set(plugin.Key{Namespace: "test", Path: "name"}, "x")
		if v, ok := plugin.GetData[int32](cow.Data(), key); !ok || v != 7 || len(d.Keys()) != 2 {
			t.Errorf("data=%v keys=%v", v, d.Keys())
		}
		d.Remove(plugin.Key{Namespace: "test", Path: "name"})
		if len(d.Keys()) != 1 {
			t.Errorf("keys=%v", d.Keys())
		}

	})
}

func TestPluginEntityKindInterfaces(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		var (
			kinds = map[registry.EntityType]func(plugin.Entity) bool{
				registry.EntityTypeArmorStand:   func(e plugin.Entity) bool { _, ok := e.(plugin.ArmorStand); return ok },
				registry.EntityTypeItemFrame:    func(e plugin.Entity) bool { _, ok := e.(plugin.ItemFrame); return ok },
				registry.EntityTypeTnt:          func(e plugin.Entity) bool { _, ok := e.(plugin.TNT); return ok },
				registry.EntityTypeFallingBlock: func(e plugin.Entity) bool { _, ok := e.(plugin.FallingBlock); return ok },
				registry.EntityTypeBlockDisplay: func(e plugin.Entity) bool { _, ok := e.(plugin.BlockDisplay); return ok },
				registry.EntityTypeItemDisplay:  func(e plugin.Entity) bool { _, ok := e.(plugin.ItemDisplay); return ok },
				registry.EntityTypeTextDisplay:  func(e plugin.Entity) bool { _, ok := e.(plugin.TextDisplay); return ok },
				registry.EntityTypeInteraction:  func(e plugin.Entity) bool { _, ok := e.(plugin.Interaction); return ok },
				registry.EntityTypeArrow:        func(e plugin.Entity) bool { _, ok := e.(plugin.Arrow); return ok },
				registry.EntityTypeSnowball:     func(e plugin.Entity) bool { _, ok := e.(plugin.Projectile); return ok },
			}
		)
		for k, is := range kinds {
			e := spawnAt(t, w, k, nil)
			if !is(e) {
				t.Errorf("%s lacks its interface", k.Name())
			}
			if _, isMob := e.(plugin.Mob); isMob {
				t.Errorf("%s is a Mob", k.Name())
			}
			if _, isArrow := e.(plugin.Arrow); isArrow && k != registry.EntityTypeArrow {
				t.Errorf("%s is an Arrow", k.Name())
			}
			if _, isDisplay := e.(plugin.Display); isDisplay != slices.Contains([]registry.EntityType{registry.EntityTypeBlockDisplay, registry.EntityTypeItemDisplay, registry.EntityTypeTextDisplay}, k) {
				t.Errorf("%s Display=%v", k.Name(), isDisplay)
			}
		}
	})
}

func TestPluginEntityKindMethods(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		stand := spawnAt(t, w, registry.EntityTypeArmorStand, nil).(plugin.ArmorStand)
		stand.SetSmall(true)
		stand.SetArms(true)
		stand.SetBasePlate(false)
		stand.SetMarker(true)
		stand.SetVisible(false)
		stand.SetPose(plugin.PartHead, plugin.Vec3{X: 10, Y: 20, Z: 30})
		if !stand.Small() || !stand.Arms() || stand.BasePlate() || !stand.Marker() || stand.Visible() ||
			stand.Pose(plugin.PartHead) != (plugin.Vec3{X: 10, Y: 20, Z: 30}) || stand.Pose(plugin.PartRightArm) != (plugin.Vec3{X: -15, Z: 10}) {
			t.Errorf("stand small=%v arms=%v base=%v marker=%v visible=%v head=%v", stand.Small(), stand.Arms(), stand.BasePlate(), stand.Marker(), stand.Visible(), stand.Pose(plugin.PartHead))
		}

		tnt := spawnAt(t, w, registry.EntityTypeTnt, nil).(plugin.TNT)
		tnt.SetFuse(500)
		tnt.SetYield(6)
		if tnt.Fuse() != 500 || tnt.Yield() != 6 {
			t.Errorf("tnt fuse=%d yield=%v", tnt.Fuse(), tnt.Yield())
		}

		fall := spawnAt(t, w, registry.EntityTypeFallingBlock, nil).(plugin.FallingBlock)
		fall.SetHurtEntities(true)
		fall.SetDropItem(false)
		if fall.DropItem() || !fall.HurtEntities() || fall.State() == 0 {
			t.Errorf("fall drop=%v hurt=%v state=%d", fall.DropItem(), fall.HurtEntities(), fall.State())
		}

		frame := spawnAt(t, w, registry.EntityTypeItemFrame, nil).(plugin.ItemFrame)
		frame.SetRotation(9)
		frame.SetFixed(true)
		frame.SetVisible(false)
		if f := frame.Facing(); f > plugin.East || frame.Rotation() != 1 || !frame.Fixed() || frame.Visible() {
			t.Errorf("frame facing=%d rot=%d fixed=%v visible=%v", frame.Facing(), frame.Rotation(), frame.Fixed(), frame.Visible())
		}

		display := spawnAt(t, w, registry.EntityTypeTextDisplay, nil).(plugin.TextDisplay)
		tr := plugin.Transform{Translation: [3]float32{1, 2, 3}, Scale: [3]float32{2, 2, 2}, LeftRotation: [4]float32{0, 0, 0, 1}, RightRotation: [4]float32{0, 0, 0, 1}}
		display.SetTransform(tr)
		display.SetBillboard(plugin.BillboardCenter)
		display.SetBrightness(&[2]int{4, 9})
		display.SetViewRange(2.5)
		display.SetShadow(1, .5)
		display.SetGlowColor(0xff00ff)
		display.SetText(plugin.Plain("hello"))
		display.SetLineWidth(120)
		display.SetBackground(0x80112233)
		display.SetSeeThrough(true)
		if display.Transform() != tr || display.Billboard() != plugin.BillboardCenter || *display.Brightness() != [2]int{4, 9} ||
			display.ViewRange() != 2.5 || display.GlowColor() != 0xff00ff || display.Text().PlainString() != "hello" ||
			display.LineWidth() != 120 || display.Background() != 0x80112233 || !display.SeeThrough() {
			t.Errorf("text display: %+v %v", display.Transform(), display.Text())
		}
		if r, s := display.Shadow(); r != 1 || s != .5 {
			t.Errorf("shadow %v %v", r, s)
		}
		display.SetBrightness(nil)
		if display.Brightness() != nil {
			t.Error("brightness not cleared")
		}

		item := spawnAt(t, w, registry.EntityTypeItemDisplay, nil).(plugin.ItemDisplay)
		if !item.Item().Empty() {
			t.Error("new item display holds an item")
		}

		inter := spawnAt(t, w, registry.EntityTypeInteraction, nil).(plugin.Interaction)
		inter.SetSize(2, 3)
		inter.SetResponsive(true)
		if wd, ht := inter.Size(); wd != 2 || ht != 3 || !inter.Responsive() {
			t.Errorf("interaction %v %v %v", wd, ht, inter.Responsive())
		}

		arrow := spawnAt(t, w, registry.EntityTypeArrow, nil).(plugin.Arrow)
		arrow.SetBaseDamage(5)
		arrow.SetCritical(true)
		arrow.SetPierceLevel(2)
		arrow.SetPickup(plugin.PickupCreativeOnly)
		cow := spawnAt(t, w, registry.EntityTypeCow, nil)
		arrow.SetShooter(cow)
		if sh, ok := arrow.Shooter(); arrow.BaseDamage() != 5 || !arrow.Critical() || arrow.PierceLevel() != 2 || arrow.Pickup() != plugin.PickupCreativeOnly || !ok || sh.UUID() != cow.UUID() {
			t.Errorf("arrow %v %v %d %v", arrow.BaseDamage(), arrow.Critical(), arrow.PierceLevel(), arrow.Pickup())
		}
		arrow.SetShooter(nil)
		if _, ok := arrow.Shooter(); ok {
			t.Error("shooter kept")
		}
	})
}

func TestPluginLivingTraceAndLaunch(t *testing.T) {
	_, w, run, cmd, _ := kindsFixture(t)
	cmd("setblock 8 80 12 minecraft:stone")
	run(func() {
		cow := spawnAt(t, w, registry.EntityTypeCow, func(e plugin.Entity) { e.(plugin.Mob).SetAI(false) }).(plugin.LivingEntity)
		cow.Equipment().SetDropChance(plugin.SlotHead, 1)
		if cow.Equipment().DropChance(plugin.SlotHead) != 1 || cow.Equipment().DropChance(plugin.SlotFeet) != .085 {
			t.Error("drop chances")
		}
		cow.SetGliding(true)
		if !cow.Gliding() || cow.Sleeping() {
			t.Error("gliding or sleeping")
		}
		if _, ok := cow.Killer(); ok {
			t.Error("a cow with no attacker has a killer")
		}
		cow.(plugin.Entity).Teleport(plugin.Location{World: w, Pos: plugin.Vec3{X: 8.5, Y: 79, Z: 8.5}, Yaw: 0, Pitch: 0}, plugin.TeleportPlugin)
		hit, ok := cow.RayTrace(10, false)
		if !ok || hit.Block == nil || hit.Block.Pos != (plugin.BlockPos{X: 8, Y: 80, Z: 12}) || hit.Face != plugin.North {
			t.Fatalf("ray hit=%+v ok=%v", hit, ok)
		}
		if _, ok := cow.RayTrace(2, false); ok {
			t.Error("a short ray hit the stone")
		}
		other := spawnAt(t, w, registry.EntityTypeCow, nil)
		other.Teleport(plugin.Location{World: w, Pos: plugin.Vec3{X: 8.5, Y: 79, Z: 10.5}}, plugin.TeleportPlugin)
		if hit, ok := cow.RayTrace(10, false); !ok || hit.Entity == nil || hit.Entity.UUID() != other.UUID() {
			t.Errorf("entity ray %+v", hit)
		}
		if !cow.HasLineOfSight(other) {
			t.Error("clear line of sight refused")
		}
		p, err := cow.LaunchProjectile(registry.EntityTypeSnowball, plugin.Vec3{})
		if err != nil || p == nil {
			t.Fatalf("launch: %v", err)
		}
		if sh, ok := p.Shooter(); !ok || sh.UUID() != cow.UUID() || p.Velocity().Z <= 0 {
			t.Errorf("projectile shooter=%v v=%v", ok, p.Velocity())
		}
		if _, err := cow.LaunchProjectile(registry.EntityTypeCow, plugin.Vec3{}); err == nil {
			t.Error("a cow was launched")
		}
		other.(plugin.Entity).Remove()
		if other.IsValid() {
			t.Error("removed entity is valid")
		}
	})
}

func TestPluginEntityDataSaveReload(t *testing.T) {
	srv, w, run, _, save := kindsFixture(t)
	key := plugin.Key{Namespace: "test", Path: "mark"}
	var id plugin.UUID
	run(func() {
		e := spawnAt(t, w, registry.EntityTypeArmorStand, nil)
		id = e.UUID()
		e.Data().Set(key, "kept")
	})
	save()
	// A storage opened on the saved files stands for a restarted server.
	disk := world.NewCommandStorages(filepath.Join(srv.Config.LevelName, "plugin_entity_data"))
	var got nbt.Tag
	disk.Read("plugin:entity/"+uuid.UUID(id).String(), func(c nbt.CompoundTag) { got = c.Entries()["test:mark"] })
	if s, ok := got.(nbt.String); !ok || s != "kept" {
		t.Errorf("saved data=%v", got)
	}
	run(func() {
		e, ok := w.Entity(id)
		if !ok {
			t.Fatal("entity lost")
		}
		if v, ok := plugin.GetData[string](e.Data(), key); !ok || v != "kept" {
			t.Errorf("data=%q %v", v, ok)
		}
		e.Remove()
		if e.IsValid() || len(e.Data().Keys()) != 0 {
			t.Error("removed entity still valid")
		}
	})
}

// A mob killed by something other than Remove loses its stored Data too.
func TestPluginEntityDataDroppedOnDeath(t *testing.T) {
	srv, w, run, cmd, save := kindsFixture(t)
	key := plugin.Key{Namespace: "test", Path: "mark"}
	var id plugin.UUID
	run(func() {
		e := spawnAt(t, w, registry.EntityTypeCow, nil)
		id = e.UUID()
		e.Data().Set(key, "x")
	})
	cmd("kill @e[type=cow]")
	servertest.ReleaseTicks(t, srv)
	servertest.RunTicks(t, srv, 30)
	servertest.HoldTicks(t, srv)
	save()
	disk := world.NewCommandStorages(filepath.Join(srv.Config.LevelName, "plugin_entity_data"))
	disk.Read("plugin:entity/"+uuid.UUID(id).String(), func(c nbt.CompoundTag) {
		if len(c.Entries()) != 0 {
			t.Errorf("data of a dead mob kept: %v", c.Entries())
		}
	})
}

// A client sees a spawned entity with its configured state, and its removal.
func TestPluginSpawnAndRemoveReachTheClient(t *testing.T) {
	srv, h, run, c := worldEffectsPlugin(t)
	p := srv.PlayerPosition("Effects")
	var e plugin.Entity
	run(func() {
		w := h.Overworld()
		var err error
		e, err = w.Spawn(registry.EntityTypeTextDisplay, plugin.Location{World: w, Pos: plugin.Vec3{X: p.X + 2, Y: p.Y + 1, Z: p.Z}}, func(e plugin.Entity) {
			e.(plugin.TextDisplay).SetText(plugin.Plain("holo"))
		})
		if err != nil {
			t.Error(err)
		}
	})
	if e == nil {
		return
	}
	c.ExpectWithinTicks(srv, 5, protocol.IDPlaySpawnEntity)
	run(func() { e.Remove() })
	c.ExpectWithinTicks(srv, 5, protocol.IDPlayRemoveEntities)
	if e.IsValid() {
		t.Error("removed entity is valid")
	}
}
