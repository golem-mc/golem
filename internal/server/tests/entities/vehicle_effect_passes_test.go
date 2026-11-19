package entities

import (
	"fmt"
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
