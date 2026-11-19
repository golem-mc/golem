package entities

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractBoat.tick, AbstractMinecart.move, OldMinecartBehavior.tick,
// AbstractMinecart.tick, InsideBlockEffectApplier.StepBasedCollector
func TestVehicleBlockEffectPasses(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart", "chest_minecart", "hopper_minecart", "furnace_minecart", "command_block_minecart", "spawner_minecart"} {
		for _, block := range []string{"lava", "fire", "soul_fire"} {
			if kind != "oak_boat" && kind != "minecart" && block != "lava" {
				continue
			}
			for _, entering := range []bool{false, true} {
				if kind != "oak_boat" && kind != "minecart" && entering {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/entering=%v", kind, block, entering), func(t *testing.T) {
					t.Parallel()
					srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
					servertest.HoldTicks(t, srv)
					c.ChatCommand(fmt.Sprintf(`summon minecraft:%s 4.5 64.5 4.5 {NoGravity:1b,CustomName:"Bob"}`, kind))
					typ, _ := registry.EntityTypeID("minecraft:" + kind)
					id := c.AwaitSpawn(typ).EntityID
					c.AwaitMetadataFor(id)
					c.Settle()
					if entering {
						servertest.RunTicks(t, srv, 1)
						c.Settle()
					}
					srv.SetBlock(4, 64, 4, registry.MustState("minecraft:"+block, nil))
					damage := float32(0)
					dir := int32(1)
					breakTick := 3
					if kind == "oak_boat" && block == "fire" {
						breakTick = 2
					}
					perTick := float32(20)
					if block == "soul_fire" {
						breakTick = 2
						perTick = 40
					}
					if block == "lava" {
						breakTick = 1
						perTick = 80
					}
					for tick := 1; tick <= breakTick; tick++ {
						removed, drops, burns := 0, 0, 0
						dropIDs := map[int32]bool{}
						var stack protocol.ItemStack
						servertest.RunTicks(t, srv, 1)
						if kind != "oak_boat" {
							// Cart metadata is tracked before physics, even on a frozen loop iteration.
							srv.SetTickFrozen(true)
							servertest.RunTicks(t, srv, 1)
							srv.SetTickFrozen(false)
						}
						c.SettleAndCollect(func(packetID int32, data []byte) {
							r := protocol.NewReader(data)
							switch packetID {
							case protocol.IDPlayRemoveEntities:
								for n := r.ReadVarInt(); n > 0; n-- {
									if r.ReadVarInt() == id {
										removed++
									}
								}
							case protocol.IDPlaySpawnEntity:
								spawned := r.ReadVarInt()
								r.ReadUUID()
								if r.ReadVarInt() == protocol.EntityTypeItem {
									drops++
									dropIDs[spawned] = true
								}
							case protocol.IDPlaySoundEffect:
								sound := r.ReadVarInt()
								name, _ := servertest.SoundEventName(sound - 1)
								if name == "minecraft:entity.generic.burn" {
									burns++
								}
							case protocol.IDPlayEntityMetadata:
								about := r.ReadVarInt()
								if dropIDs[about] {
									for r.Err() == nil {
										index := r.ReadU8()
										if index == protocol.MetadataEnd {
											break
										}
										typ := r.ReadVarInt()
										if index == protocol.MetadataIndexItemSlot {
											stack, _ = r.ReadSlot()
										} else {
											servertest.StepOverMetadataValue(r, typ)
										}
									}
									return
								}
								if about != id {
									return
								}
								for r.Err() == nil {
									index := r.ReadU8()
									if index == protocol.MetadataEnd {
										break
									}
									fieldType := r.ReadVarInt()
									if index == protocol.MetadataIndexVehicleShakeDamage {
										damage = r.ReadF32()
									} else if index == protocol.MetadataIndexVehicleShakeDirection {
										dir = r.ReadVarInt()
									} else {
										servertest.StepOverMetadataValue(r, fieldType)
									}
								}
							}
						})
						wantBurns := 0
						if block == "lava" {
							wantBurns = 2
							if kind != "oak_boat" {
								wantBurns = 3
							}
						}
						if burns != wantBurns {
							t.Errorf("tick %d: burn sounds=%d, want %d", tick, burns, wantBurns)
						}
						if tick == breakTick {
							if removed != 1 || drops != 1 {
								t.Fatalf("tick %d: removals=%d drops=%d, want 1 each", tick, removed, drops)
							}
							if stack.Count != 1 || stack.CustomName == nil || stack.CustomName.Text != "Bob" {
								t.Fatalf("tick %d: drop=%+v, want one named vehicle item", tick, stack)
							}
						} else {
							want := perTick*float32(tick) - float32(tick-1)
							if removed != 0 || drops != 0 || damage != want || dir != 1 {
								t.Fatalf("tick %d: removals=%d drops=%d damage=%v dir=%d, want 0,0,%v,1", tick, removed, drops, damage, dir, want)
							}
						}
					}
				})
			}
		}
	}
}

// vanilla: AbstractBoat.tick, Entity.applyEffectsFromBlocks
func TestRiddenBoatBurnPasses(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"lava", "fire", "soul_fire"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon oak_boat 4.5 64.5 4.5 {NoGravity:1b}`)
			id := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
			c.AwaitMetadataFor(id)
			c.ChatCommand(`ride Rider mount @e[type=oak_boat,limit=1]`)
			c.Settle()
			srv.SetBlock(4, 64, 4, registry.MustState("minecraft:"+block, nil))
			ticks := 2
			if block == "lava" {
				ticks = 1
			}
			for tick := 1; tick <= ticks; tick++ {
				servertest.RunTicks(t, srv, 1)
				_, _, _, _, _, alive := srv.BoatState(id)
				if alive != (tick < ticks) {
					t.Fatalf("tick %d: boat alive=%v, want %v", tick, alive, tick < ticks)
				}
			}
		})
	}
}

// vanilla: BaseFireBlock.entityInside, LavaFluid.entityInside,
// InsideBlockEffectApplier.StepBasedCollector.APPLY_ORDER
func TestVehicleBurnCellsKeepEveryHurt(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		for _, blocks := range [][2]string{{"fire", "fire"}, {"fire", "soul_fire"}, {"fire", "lava"}, {"lava", "fire"}} {
			t.Run(kind+"/"+blocks[0]+"-"+blocks[1], func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
				servertest.HoldTicks(t, srv)
				c.ChatCommand(fmt.Sprintf(`summon %s 5.0 64.5 4.5 {NoGravity:1b}`, kind))
				typ, _ := registry.EntityTypeID("minecraft:" + kind)
				id := c.AwaitSpawn(typ).EntityID
				c.AwaitMetadataFor(id)
				c.Settle()
				for i, block := range blocks {
					srv.SetBlock(4+int32(i), 64, 4, registry.MustState("minecraft:"+block, nil))
				}
				// FIRE_IGNITE hurts run before LAVA_IGNITE hurts, even if lava was visited first.
				heard, _, _ := servertest.CollectSoundsTicks(c, srv, 1)
				wantSounds := 0
				if blocks[0] == "lava" || blocks[1] == "lava" {
					wantSounds = 1
					if kind == "minecart" {
						wantSounds = 2
					}
				}
				if got := servertest.CountSounds(heard, "minecraft:entity.generic.burn", protocol.SoundCategoryNeutral); got != wantSounds {
					t.Errorf("burn sounds=%d, want %d", got, wantSounds)
				}
				alive := false
				if kind == "oak_boat" {
					_, _, _, _, _, alive = srv.BoatState(id)
				} else {
					_, _, _, _, alive = srv.MinecartState(id)
				}
				wantAlive := blocks[0] == "fire" && blocks[1] == "fire"
				if alive != wantAlive {
					t.Fatalf("alive after tick 1=%v, want %v", alive, wantAlive)
				}
			})
		}
	}
}

// vanilla: MinecartTNT.hurtServer, MinecartTNT.destroy, MinecartTNT.tick
func TestRepeatedLavaHurtsPrimeOnlyTheTNTCart(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
	servertest.HoldTicks(t, srv)
	id, _ := tntCartAt(t, srv, 4.5, 64.5, 4.5, `{NoGravity:1b}`, 9)
	c.AwaitSpawn(protocol.EntityTypeTntMinecart)
	c.Settle()
	srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
	heard, counts, _ := servertest.CollectSoundsTicks(c, srv, 1)
	r := rand.New(rand.NewSource(9))
	want := int32(r.Intn(20)+r.Intn(20)) - 1
	if fuse, alive := srv.MinecartFuse(id); !alive || fuse != want {
		t.Fatalf("fuse=%d alive=%v, want %d,true", fuse, alive, want)
	}
	if counts[protocol.IDPlayEntityEvent] != 1 || counts[protocol.IDPlayRemoveEntities] != 0 {
		t.Errorf("prime/removal packets=%v, want one prime and no removal", counts)
	}
	if got := servertest.CountSounds(heard, "minecraft:entity.generic.burn", protocol.SoundCategoryNeutral); got != 3 {
		t.Errorf("burn sounds=%d, want 3", got)
	}
}

// vanilla: AbstractBoat.tick, AbstractMinecart.move, AbstractMinecart.tick
func TestVehiclesEnterLavaDuringTheirMove(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stoker", servertest.OpsListing(t, "Stoker"))
			servertest.HoldTicks(t, srv)
			x := 3.2
			if kind == "minecart" {
				x = 3.49
			}
			c.ChatCommand(fmt.Sprintf(`summon %s %g 64.5 4.5 {NoGravity:1b,Motion:[0.4d,0d,0d]}`, kind, x))
			typ, _ := registry.EntityTypeID("minecraft:" + kind)
			id := c.AwaitSpawn(typ).EntityID
			c.AwaitMetadataFor(id)
			c.Settle()
			srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil))
			heard, counts, _ := servertest.CollectSoundsTicks(c, srv, 1)
			want := 2
			if kind == "minecart" {
				want = 3
			}
			if got := servertest.CountSounds(heard, "minecraft:entity.generic.burn", protocol.SoundCategoryNeutral); got != want {
				t.Errorf("burn sounds=%d, want %d", got, want)
			}
			if counts[protocol.IDPlayRemoveEntities] != 1 || counts[protocol.IDPlaySpawnEntity] != 1 {
				t.Fatalf("removal/drop traffic=%v, want one removal and one item spawn", counts)
			}
		})
	}
}
