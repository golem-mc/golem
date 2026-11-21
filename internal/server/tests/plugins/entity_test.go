package plugins_test

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginEntityHandles(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.Acquire(world.ChunkPos{X: 5, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}, {X: 5, Z: 0}})
	srv.RunLine("summon minecraft:armor_stand 8.5 80 8.5 {Tags:[\"a\"]}")
	srv.RunLine("summon minecraft:item 9.5 80 8.5 {Item:{id:\"minecraft:stone\",count:1}}")
	srv.RunLine("summon minecraft:armor_stand 88.5 80 8.5")

	var stand plugin.Entity
	run(func() {
		pw := h.Overworld()
		var kinds []registry.EntityType
		for e := range pw.Entities() {
			kinds = append(kinds, e.Type())
			if e.Type() == registry.EntityTypeArmorStand && slices.Contains(e.Tags(), "a") {
				stand = e
			}
		}
		if len(kinds) != 3 || stand == nil {
			t.Fatalf("kinds=%v stand=%v", kinds, stand)
		}
		if !stand.IsValid() || stand.ID() == 0 || stand.World() != pw {
			t.Errorf("valid=%v id=%d", stand.IsValid(), stand.ID())
		}
		if p := stand.Pos(); p.X != 8.5 || p.Y != 80 || p.Z != 8.5 {
			t.Errorf("pos=%v", p)
		}
		if got, ok := pw.Entity(stand.UUID()); !ok || got.UUID() != stand.UUID() {
			t.Error("Entity lookup failed")
		}
		if n := len(pw.EntitiesIn(plugin.Box{Min: plugin.Vec3{X: 8, Y: 79, Z: 8}, Max: plugin.Vec3{X: 10, Y: 82, Z: 9}})); n != 2 {
			t.Errorf("EntitiesIn=%d, want 2", n)
		}
		c, _ := pw.Chunk(plugin.ChunkPos{X: 0, Z: 0})
		if n := len(c.Entities()); n != 2 {
			t.Errorf("chunk entities=%d, want 2", n)
		}
		if n := len(stand.Nearby(2)); n != 1 {
			t.Errorf("nearby=%d, want 1 (the item)", n)
		}
		if !stand.AddTag("b") || !slices.Contains(stand.Tags(), "b") || !stand.RemoveTag("b") {
			t.Errorf("tags=%v", stand.Tags())
		}
		if !stand.Teleport(plugin.Location{World: pw, Pos: plugin.Vec3{X: 88.5, Y: 80, Z: 8.5}}, plugin.TeleportPlugin) {
			t.Error("teleport refused")
		}
	})
	servertest.RunTicks(t, srv, 1)
	run(func() {
		if p := stand.Pos(); p.X != 88.5 {
			t.Errorf("after teleport pos=%v", p)
		}
	})
	srv.RunLine("kill @e[type=minecraft:armor_stand,tag=a]")
	run(func() {
		if stand.IsValid() || stand.ID() != 0 || stand.World() != nil || stand.Tags() != nil || stand.AddTag("x") {
			t.Error("stale handle is not inert")
		}
		if _, ok := h.Overworld().Entity(stand.UUID()); ok {
			t.Error("removed entity still found")
		}
	})
}

func TestPluginEntityPlayerHandle(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "entities", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	id := c.JoinAs("Walker")
	servertest.HoldTicks(t, srv)
	p, ok := host.PlayerNamed("Walker")
	if !ok {
		t.Fatal("player missing")
	}
	if p.ID() != id || p.Type() != registry.EntityTypePlayer || p.World() != host.Overworld() {
		t.Errorf("id=%d want %d, type=%v", p.ID(), id, p.Type())
	}
	var found plugin.Entity
	for e := range host.Overworld().Entities() {
		if e.UUID() == p.UUID() {
			found = e
		}
	}
	if found == nil || found.ID() != id {
		t.Fatalf("player not among the world's entities: %v", found)
	}
	if !p.AddTag("walker") || !slices.Contains(p.Tags(), "walker") {
		t.Errorf("tags=%v", p.Tags())
	}
	if p.BoundingBox().Max.Y <= p.BoundingBox().Min.Y {
		t.Errorf("box=%v", p.BoundingBox())
	}
}

func TestPluginLivingEntityHandle(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	srv.RunLine("summon minecraft:cow 8.5 80 8.5 {NoAI:1b}")
	srv.RunLine("summon minecraft:item 9.5 80 8.5 {Item:{id:\"minecraft:stone\",count:1}}")
	var cow plugin.LivingEntity
	run(func() {
		for e := range h.Overworld().Entities() {
			l, living := e.(plugin.LivingEntity)
			switch e.Type() {
			case registry.EntityTypeCow:
				if !living {
					t.Fatal("cow is not a LivingEntity")
				}
				cow = l
			case registry.EntityTypeItem:
				if living {
					t.Error("item is a LivingEntity")
				}
			}
		}
		if cow == nil {
			t.Fatal("cow missing")
		}
		if cow.MaxHealth() != 10 || cow.Health() != 10 {
			t.Errorf("health %v/%v", cow.Health(), cow.MaxHealth())
		}
		cow.SetHealth(4)
		if cow.Health() != 4 {
			t.Errorf("SetHealth: %v", cow.Health())
		}
		cow.Heal(100, plugin.RegainPlugin)
		if cow.Health() != 10 {
			t.Errorf("Heal: %v", cow.Health())
		}
		attr, ok := cow.Attribute(registry.AttributeMaxHealth)
		if !ok {
			t.Fatal("no max_health attribute")
		}
		attr.SetBase(30)
		attr.AddModifier(plugin.AttributeModifier{ID: plugin.Key{Namespace: "test", Path: "bonus"}, Amount: 2, Operation: registry.AttributeOperation(0)})
		if cow.MaxHealth() != 32 || len(attr.Modifiers()) != 1 {
			t.Errorf("max=%v modifiers=%v", cow.MaxHealth(), attr.Modifiers())
		}
		attr.RemoveModifier(plugin.Key{Namespace: "test", Path: "bonus"})
		if cow.MaxHealth() != 30 {
			t.Errorf("max after remove=%v", cow.MaxHealth())
		}
		speed, _ := h.Registries().Effect(plugin.MinecraftKey("speed"))
		if !cow.AddEffect(plugin.EffectInstance{Effect: speed, Duration: 200, Amplifier: 1, Particles: true, Icon: true}) {
			t.Error("AddEffect refused")
		}
		if in, ok := cow.Effect(speed); !ok || in.Amplifier != 1 || in.Duration <= 0 || in.Duration > 200 {
			t.Errorf("effect=%+v,%v", in, ok)
		}
		if !cow.RemoveEffect(speed) || len(cow.Effects()) != 0 {
			t.Errorf("effects=%v", cow.Effects())
		}
		saddle := plugin.ItemStack{Item: registry.ItemSaddle, Count: 1}
		cow.Equipment().Set(plugin.SlotMainHand, plugin.ItemStack{Item: registry.ItemStick, Count: 1})
		if got := cow.Equipment().Get(plugin.SlotMainHand); got.Item != registry.ItemStick {
			t.Errorf("main hand=%v", got)
		}
		_ = saddle
		cow.SetGlowing(true)
		cow.SetNoGravity(true)
		cow.SetFireTicks(100)
		cow.SetVelocity(plugin.Vec3{X: 0.5})
		if !cow.Glowing() || !cow.NoGravity() || cow.FireTicks() != 100 || cow.Velocity().X != 0.5 || cow.Silent() {
			t.Errorf("flags glow=%v grav=%v fire=%d vel=%v", cow.Glowing(), cow.NoGravity(), cow.FireTicks(), cow.Velocity())
		}
		generic, _ := h.Registries().DamageType(plugin.MinecraftKey("generic"))
		before := cow.Health()
		if !cow.Damage(3, plugin.DamageSource{Type: generic}) || cow.Health() != before-3 {
			t.Errorf("Damage: health %v -> %v", before, cow.Health())
		}
	})
	servertest.RunTicks(t, srv, 1)
	run(func() {
		cow.Kill()
	})
	servertest.RunTicks(t, srv, 30)
	run(func() {
		if cow.IsValid() {
			t.Error("killed cow still valid")
		}
	})
}

func TestPluginDropItem(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	run(func() {
		pw := h.Overworld()
		at := plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}
		it := pw.DropItem(at, plugin.ItemStack{Item: registry.ItemStone, Count: 3}, false)
		if it == nil || !it.IsValid() {
			t.Fatal("DropItem failed")
		}
		if got := it.Stack(); got.Item != registry.ItemStone || got.Count != 3 {
			t.Errorf("stack=%v", got)
		}
		if p := it.Pos(); p.X != 8.5 || p.Z != 8.5 {
			t.Errorf("pos=%v", p)
		}
		it.SetStack(plugin.ItemStack{Item: registry.ItemDirt, Count: 2})
		it.SetPickupDelay(77)
		owner := plugin.UUID{1, 2, 3}
		it.SetOwner(owner)
		if got := it.Stack(); got.Item != registry.ItemDirt || got.Count != 2 || it.PickupDelay() != 77 {
			t.Errorf("after set: %v delay=%d", got, it.PickupDelay())
		}
		if o, ok := it.Owner(); !ok || o != owner {
			t.Errorf("owner=%v,%v", o, ok)
		}
		it.SetOwner(plugin.UUID{})
		if _, ok := it.Owner(); ok {
			t.Error("owner not cleared")
		}
		n := pw.DropItem(at, plugin.ItemStack{Item: registry.ItemStone, Count: 1}, true)
		if p := n.Pos(); math.Abs(p.X-8.5) > .25 || math.Abs(p.Z-8.5) > .25 || p.Y > 80 {
			t.Errorf("natural pos=%v", p)
		}
		if v := n.Velocity(); v.Y != .2 {
			t.Errorf("natural velocity=%v", v)
		}
		if pw.DropItem(at, plugin.ItemStack{}, false) != nil {
			t.Error("empty stack dropped")
		}
	})
}

func TestPluginChunkEntitiesFollowMobs(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	chunks := []world.ChunkPos{{X: 0, Z: 0}, {X: 1, Z: 0}}
	for _, p := range chunks {
		w.Acquire(p)
	}
	w.InhabitTime(chunks)
	srv.RunLine("summon minecraft:cow 15.99 80 8.5 {NoAI:1b,Tags:[\"a\"]}")
	srv.RunLine("summon minecraft:cow 16.0 80 9.5 {NoAI:1b,Tags:[\"b\"]}")
	// tags lists the cows in chunk (x, 0), in order.
	tags := func(x int) []string {
		c, _ := h.Overworld().Chunk(plugin.ChunkPos{X: x, Z: 0})
		var out []string
		for _, e := range c.Entities() {
			out = append(out, e.Tags()...)
		}
		return out
	}
	check := func(want0, want1 []string) {
		t.Helper()
		if got := tags(0); !slices.Equal(got, want0) {
			t.Errorf("chunk 0 = %v, want %v", got, want0)
		}
		if got := tags(1); !slices.Equal(got, want1) {
			t.Errorf("chunk 1 = %v, want %v", got, want1)
		}
	}
	run(func() { check([]string{"a"}, []string{"b"}) })
	srv.RunLine("tp @e[tag=a] 16.5 80 8.5")
	run(func() { check(nil, []string{"a", "b"}) })
	srv.RunLine("data merge entity @e[tag=a,limit=1] {Pos:[3.5d,80d,8.5d]}")
	run(func() { check([]string{"a"}, []string{"b"}) })
}
