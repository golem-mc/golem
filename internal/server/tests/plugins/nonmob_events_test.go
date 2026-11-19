package plugins_test

import (
	"encoding/binary"
	"fmt"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/nbt"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// summonedID is the entity ID of the one entity of typ in the overworld.
func summonedID(t *testing.T, host plugin.Server, typ registry.EntityType) int32 {
	t.Helper()
	var id int32
	for e := range host.Overworld().Entities() {
		if e.Type() == typ {
			id = e.ID()
		}
	}
	if id == 0 {
		t.Fatalf("no %v summoned", typ)
	}
	return id
}

// vanilla: ServerGamePacketListenerImpl.handleInteract, ArmorStand.hurtServer, ItemFrame.hurtServer
func TestPluginEventsForNonMobEntities(t *testing.T) {
	var attacks, uses, hurts int
	var cancel, cancelHurt bool
	var attackTypes, useTypes, hurtTypes []registry.EntityType
	srv, host, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerAttackEvent) {
			attacks++
			attackTypes = append(attackTypes, e.Target.Type())
			e.SetCancelled(cancel)
		})
		plugin.Listen(s, func(e *plugin.PlayerInteractEntityEvent) {
			uses++
			useTypes = append(useTypes, e.Target.Type())
			e.SetCancelled(cancel)
		})
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			hurts++
			hurtTypes = append(hurtTypes, e.Entity.Type())
			e.SetCancelled(cancelHurt)
		})
	})
	srv.RunLine("summon minecraft:armor_stand 0.5 64 1.5")
	srv.RunLine("summon minecraft:item_frame 0 64 3 {Facing:3b}")
	var stand, frame int32
	onTick(t, srv, host, func() {
		stand = summonedID(t, host, registry.EntityTypeArmorStand)
		frame = summonedID(t, host, registry.EntityTypeItemFrame)
	})
	cancelHurt = true
	c.Attack(stand)
	// The blow's damage event is asked after its attack event, on a later tick.
	fired(t, srv, c, "stand damage event", &hurts, 1)
	if attacks != 1 {
		t.Fatalf("attack events = %d, want 1", attacks)
	}
	cancel = true
	c.UseOn(stand)
	fired(t, srv, c, "stand interact event", &uses, 1)
	c.Attack(frame)
	fired(t, srv, c, "frame attack event", &attacks, 2)
	c.UseOn(frame)
	fired(t, srv, c, "frame interact event", &uses, 2)
	if attackTypes[0] != registry.EntityTypeArmorStand || attackTypes[1] != registry.EntityTypeItemFrame ||
		useTypes[0] != registry.EntityTypeArmorStand || useTypes[1] != registry.EntityTypeItemFrame ||
		hurtTypes[0] != registry.EntityTypeArmorStand {
		t.Fatalf("types: attack %v use %v hurt %v", attackTypes, useTypes, hurtTypes)
	}
	// A cancelled blow leaves the stand standing.
	servertest.RunTicks(t, srv, 1)
	if got := summonedID(t, host, registry.EntityTypeArmorStand); got != stand {
		t.Fatalf("stand %d replaced by %d", stand, got)
	}
}

// vanilla: ArmorStand.hurtServer, ServerExplosion.hurtEntities
func TestPluginBlastAsksArmorStandDamage(t *testing.T) {
	var hurts int
	var cancel bool
	srv, host, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			if e.Entity.Type() == registry.EntityTypeArmorStand {
				hurts++
				e.SetCancelled(cancel)
			}
		})
	})
	srv.RunLine("summon minecraft:armor_stand 6.5 64 0.5")
	srv.RunLine("summon minecraft:tnt 6.5 64.5 1.5 {fuse:1}")
	cancel = true
	servertest.RunTicks(t, srv, 4)
	if hurts != 1 {
		t.Fatalf("stand damage events = %d, want 1", hurts)
	}
	onTick(t, srv, host, func() { summonedID(t, host, registry.EntityTypeArmorStand) })
}

// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.hurtServer
func TestPluginBlastNamesItsExploder(t *testing.T) {
	var causes []plugin.HangingBreakCause
	var removers []registry.EntityType
	var direct registry.EntityType = -2
	srv, _, c := blockScene(t, "minecraft:dirt", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.HangingBreakEvent) {
			causes = append(causes, e.Cause)
			if e.Remover != nil {
				removers = append(removers, e.Remover.Type())
			}
		})
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			if e.Entity.Type() == registry.EntityTypePlayer && e.Source.Direct != nil {
				direct = e.Source.Direct.Type()
			}
		})
	})
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	c.MoveToGround(4.5, 64, 3.5, true)
	srv.SpawnFrameEntity(4, 64, 4)
	c.Settle()
	srv.RunLine("summon minecraft:tnt 4.5 66 2.5 {fuse:1}")
	servertest.RunTicks(t, srv, 4)
	if len(causes) != 1 || causes[0] != plugin.HangingBreakExplosion || len(removers) != 1 || removers[0] != registry.EntityTypeTnt {
		t.Errorf("hanging break causes %v removers %v, want one explosion by tnt", causes, removers)
	}
	if direct != registry.EntityTypeTnt {
		t.Errorf("blast damage direct = %v, want tnt", direct)
	}
}

// vanilla: ThrownEgg.onHit
func TestPluginEggThrowEventControlsHatching(t *testing.T) {
	var thrown int
	srv, host, c := blockScene(t, "minecraft:egg", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerEggThrowEvent) {
			thrown++
			if e.Player.Name() != "Miner" || e.Egg.Type() != registry.EntityTypeEgg {
				t.Errorf("egg event: player %q egg %v", e.Player.Name(), e.Egg.Type())
			}
			e.Hatching, e.Hatches = true, 3
		})
	})
	c.UseItemRightClicks(protocol.HandMain)
	// The packet may not be read yet, and the egg flies for a while.
	fired(t, srv, c, "egg event", &thrown, 1)
	chicks := 0
	onTick(t, srv, host, func() {
		for e := range host.Overworld().Entities() {
			if e.Type() == registry.EntityTypeChicken {
				chicks++
			}
		}
	})
	if chicks != 3 {
		t.Errorf("chicks = %d, want the 3 the listener set", chicks)
	}
}

// vanilla: ServerLevel.addFreshEntity
func TestPluginEntitySpawnEventForNonMobs(t *testing.T) {
	var seen []registry.EntityType
	var cancel bool
	var asked int
	srv, _, c := blockScene(t, "minecraft:snowball", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			seen = append(seen, e.Entity.Type())
			asked++
			e.SetCancelled(cancel)
		})
	})
	summonTicking(t, srv, "summon minecraft:item 3.5 64 3.5 {Item:{id:\"minecraft:stone\",count:1}}")
	c.UseItemRightClicks(protocol.HandMain)
	fired(t, srv, c, "projectile spawn event", &asked, 2)
	if len(seen) != 2 || seen[0] != registry.EntityTypeItem || seen[1] != registry.EntityTypeSnowball {
		t.Fatalf("spawn events for %v, want an item then a snowball", seen)
	}
	cancel = true
	summonTicking(t, srv, "summon minecraft:item 3.5 64 3.5 {Item:{id:\"minecraft:dirt\",count:1}}")
	servertest.RunTicks(t, srv, 2)
	if n := len(srv.ItemsInDimension("minecraft:overworld")); n != 1 {
		t.Errorf("items after a cancelled spawn = %d, want only the first", n)
	}
}

// summonTicking runs a command whose listeners are asked on a tick, so the
// held loop is stepped until the command returns.
func summonTicking(t *testing.T, srv *server.Server, line string) {
	t.Helper()
	done := make(chan struct{})
	go func() { srv.RunLine(line); close(done) }()
	for i := 0; ; i++ {
		select {
		case <-done:
			return
		default:
		}
		if i == 400 {
			t.Fatalf("%q did not return", line)
		}
		servertest.RunTicks(t, srv, 1)
		time.Sleep(5 * time.Millisecond)
	}
}

// vanilla: Plugin-defined events; Entity.kill, DamageTypes.GENERIC_KILL
func TestPluginDefinesItsOwnEventAndKillSaysWhy(t *testing.T) {
	var host plugin.Server
	var types []registry.DamageType
	var got []string
	srv, _ := entityPlugin(t, func(s plugin.Server) {
		host = s
		plugin.DefineEvent[definedEvent](s)
		plugin.Listen(s, func(e *definedEvent) {
			got = append(got, e.Name)
			e.Balance++
			e.SetCancelled(true)
		})
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) { types = append(types, e.Source.Type) })
	})
	onTick(t, srv, host, func() {
		e := &definedEvent{Name: "Victim"}
		host.Events().Fire(e)
		if !e.Cancelled() || e.Balance != 1 {
			t.Errorf("defined event outcome: cancelled %v balance %d", e.Cancelled(), e.Balance)
		}
	})
	if len(got) != 1 {
		t.Fatalf("defined event listeners ran %d times, want 1", len(got))
	}
	onTick(t, srv, host, func() {
		p, ok := host.Player(protocol.OfflineUUID("Victim"))
		if !ok {
			t.Fatal("no Victim")
		}
		p.Kill()
	})
	if len(types) != 1 || types[0] != registry.DamageTypeGenericKill {
		t.Errorf("kill damage types = %v, want generic_kill", types)
	}
}

// definedEvent is a plugin's own event, named ...Event as a defined one must be.
type definedEvent struct {
	plugin.Cancellable
	Name    string
	Balance int
}

// vanilla: HurtByTargetGoal, Mob.setTarget
func TestPluginTargetEventNamesRetaliation(t *testing.T) {
	var reasons []plugin.TargetReason
	var targets []string
	srv, host, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTargetEvent) {
			reasons = append(reasons, e.Reason)
			if e.Target != nil {
				targets = append(targets, e.Target.Type().String())
			}
		})
	})
	srv.RunLine("summon minecraft:wolf 0.5 64 1.5")
	var wolf int32
	onTick(t, srv, host, func() { wolf = summonedID(t, host, registry.EntityTypeWolf) })
	c.Attack(wolf)
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	for i := 0; i < 20 && len(reasons) == 0; i++ {
		servertest.RunTicks(t, srv, 1)
	}
	if len(reasons) == 0 || reasons[0] != plugin.TargetAttacked {
		t.Fatalf("target reasons %v (%v), want attacked first", reasons, targets)
	}
}

// tamedWolfReasons tames a wolf to the player, lets prod provoke a blow, and
// returns the target reasons the wolf's goals reported.
func tamedWolfReasons(t *testing.T, victim string, want plugin.TargetReason, prod func(c *servertest.Client, victim int32)) []plugin.TargetReason {
	t.Helper()
	var reasons []plugin.TargetReason
	srv, host, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTargetEvent) { reasons = append(reasons, e.Reason) })
	})
	srv.RunLine("summon minecraft:wolf 1.5 64 0.5")
	srv.RunLine("summon " + victim + " 0.5 64 1.5")
	var vid int32
	onTick(t, srv, host, func() {
		for e := range host.Overworld().Entities() {
			if w, ok := e.(plugin.Tameable); ok {
				w.SetOwner(protocol.OfflineUUID("Miner"))
			}
		}
		for e := range host.Overworld().Entities() {
			if e.Type() != registry.EntityTypeWolf && e.Type() != registry.EntityTypePlayer {
				vid = e.ID()
			}
		}
	})
	prod(c, vid)
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	for i := 0; i < 60 && !slices.Contains(reasons, want); i++ {
		servertest.RunTicks(t, srv, 1)
	}
	return reasons
}

// vanilla: OwnerHurtTargetGoal, Mob.setTarget
func TestPluginTargetEventNamesOwnerAttackedTarget(t *testing.T) {
	reasons := tamedWolfReasons(t, "minecraft:cow", plugin.TargetOwnerAttackedTarget, func(c *servertest.Client, id int32) { c.Attack(id) })
	if len(reasons) == 0 || reasons[0] != plugin.TargetOwnerAttackedTarget {
		t.Fatalf("target reasons %v, want owner-attacked-target first", reasons)
	}
}

// vanilla: OwnerHurtByTargetGoal, Mob.setTarget
func TestPluginTargetEventNamesOwnerAttacked(t *testing.T) {
	reasons := tamedWolfReasons(t, "minecraft:zombie", plugin.TargetOwnerAttacked, func(*servertest.Client, int32) {})
	if !slices.Contains(reasons, plugin.TargetOwnerAttacked) {
		t.Fatalf("target reasons %v, want owner-attacked", reasons)
	}
}

// vanilla: SummonCommand.createEntity, ServerLevel.addFreshEntity
func TestPluginEntitySpawnEventForSummonedVehicles(t *testing.T) {
	var reasons []plugin.SpawnReason
	var types []registry.EntityType
	var cancel bool
	srv, host, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			types = append(types, e.Entity.Type())
			reasons = append(reasons, e.Reason)
			e.SetCancelled(cancel)
		})
	})
	summonTicking(t, srv, "summon minecraft:oak_boat 3.5 64 3.5")
	summonTicking(t, srv, "summon minecraft:minecart 5.5 64 3.5")
	if len(types) != 2 || types[0] != registry.EntityTypeOakBoat || types[1] != registry.EntityTypeMinecart ||
		reasons[0] != plugin.SpawnCommand || reasons[1] != plugin.SpawnCommand {
		t.Fatalf("spawn events %v %v, want a boat and a minecart by command", types, reasons)
	}
	cancel = true
	summonTicking(t, srv, "summon minecraft:minecart 7.5 64 3.5")
	servertest.RunTicks(t, srv, 2)
	carts := 0
	for e := range host.Overworld().Entities() {
		if e.Type() == registry.EntityTypeMinecart {
			carts++
		}
	}
	if carts != 1 {
		t.Errorf("minecarts after a cancelled summon = %d, want 1", carts)
	}
}

// vanilla: Fox.DefendTrustedTargetGoal, DefendVillageTargetGoal, Mob.setTarget
func TestPluginTargetEventNamesDefendTrusted(t *testing.T) {
	var reasons []plugin.TargetReason
	srv, host, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTargetEvent) { reasons = append(reasons, e.Reason) })
	})
	u := protocol.OfflineUUID("Miner")
	a := nbt.UUIDToIntArray(u)
	srv.RunLine(fmt.Sprintf("summon minecraft:fox 1.5 64 0.5 {Trusted:[[I;%d,%d,%d,%d]]}", a[0], a[1], a[2], a[3]))
	srv.RunLine("summon minecraft:zombie 0.5 64 1.5")
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	for i := 0; i < 200 && !slices.Contains(reasons, plugin.TargetDefendOwner); i++ {
		servertest.RunTicks(t, srv, 1)
	}
	_ = host
	if !slices.Contains(reasons, plugin.TargetDefendOwner) {
		t.Fatalf("target reasons %v, want defend", reasons)
	}
}

// vanilla: ServerExplosion.hurtEntities, LivingEntity.hurtServer
func TestPluginBlastNamesItsExploderToMobDamage(t *testing.T) {
	var direct registry.EntityType = -2
	srv, _, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityDamageEvent) {
			if e.Entity.Type() == registry.EntityTypePig && e.Source.Direct != nil {
				direct = e.Source.Direct.Type()
			}
		})
	})
	srv.SpawnMob("minecraft:pig", 6.5, 64, 0.5)
	srv.RunLine("summon minecraft:tnt 6.5 64.5 1.5 {fuse:1}")
	servertest.RunTicks(t, srv, 4)
	if direct != registry.EntityTypeTnt {
		t.Errorf("pig blast damage direct = %v, want tnt", direct)
	}
}

// vanilla: ServerLevel.addFreshEntity
func TestPluginSummonedArmorStandFiresSpawn(t *testing.T) {
	var spawns int
	srv, _, _ := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			if e.Entity.Type() == registry.EntityTypeArmorStand {
				spawns++
			}
		})
	})
	summonTicking(t, srv, "summon minecraft:armor_stand 6.5 64 0.5")
	if spawns != 1 {
		t.Errorf("armor stand spawn events = %d, want 1", spawns)
	}
}

// A frame broken by a TNT a player lit names the player as the remover.
// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.hurtServer
func TestPluginBlastRemoverIsTheLighter(t *testing.T) {
	var removers []plugin.Entity
	srv, host, c := blockScene(t, "minecraft:dirt", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.HangingBreakEvent) {
			removers = append(removers, e.Remover)
		})
	})
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	c.MoveToGround(4.5, 64, 3.5, true)
	srv.SpawnFrameEntity(4, 64, 4)
	c.Settle()
	id := host.Players()[0].UUID()
	srv.RunLine(fmt.Sprintf("summon minecraft:tnt 4.5 66 2.5 {fuse:1,owner:[I;%d,%d,%d,%d]}",
		int32(binary.BigEndian.Uint32(id[0:4])), int32(binary.BigEndian.Uint32(id[4:8])),
		int32(binary.BigEndian.Uint32(id[8:12])), int32(binary.BigEndian.Uint32(id[12:16]))))
	servertest.RunTicks(t, srv, 4)
	if len(removers) != 1 || removers[0] == nil || removers[0].Type() != registry.EntityTypePlayer {
		t.Errorf("removers = %v, want the player who lit the TNT", removers)
	}
}

// vanilla: ArmorStandItem.useOn, ServerLevel.addFreshEntity
func TestPluginPlacedArmorStandAsksSpawn(t *testing.T) {
	var spawns int
	srv, host, c := blockScene(t, "minecraft:armor_stand", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			if e.Entity.Type() == registry.EntityTypeArmorStand {
				spawns++
				e.SetCancelled(true)
			}
		})
	})
	d := servertest.DigTarget
	c.PlaceBlock(d[0], d[1]-1, d[2], protocol.FaceTop)
	fired(t, srv, c, "armor stand spawn event", &spawns, 1)
	for e := range host.Overworld().Entities() {
		if e.Type() == registry.EntityTypeArmorStand {
			t.Errorf("cancelled stand still in the world")
		}
	}
}

// vanilla: Projectile.setOwner, ThrownEgg.onHit
func TestPluginSetShooterOnEggFiresEggThrow(t *testing.T) {
	var thrown int
	srv, host, c := blockScene(t, "minecraft:stone", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerEggThrowEvent) { thrown++ })
	})
	onTick(t, srv, host, func() {
		p, _ := host.PlayerNamed("Miner")
		w := host.Overworld()
		e, err := w.Spawn(registry.EntityTypeEgg, plugin.Location{World: w, Pos: plugin.Vec3{X: 6.5, Y: 66, Z: 0.5}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		pr := e.(plugin.Projectile)
		pr.SetShooter(p)
		if s, ok := pr.Shooter(); !ok || s.UUID() != p.UUID() {
			t.Errorf("shooter = %v, %v", s, ok)
		}
	})
	fired(t, srv, c, "egg event", &thrown, 1)
}
