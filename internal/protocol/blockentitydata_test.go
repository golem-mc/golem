package protocol

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestBlockEntityDataRoundTrips checks that minecraft:block_entity_data is
// written and read as the block entity type and its network NBT, through
// both the trusted slot and the untrusted one a creative client sends.
func TestBlockEntityDataRoundTrips(t *testing.T) {
	tag := nbt.Compound{
		"id":        nbt.String("minecraft:mob_spawner"),
		"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:skeleton")}},
	}
	stack := ItemStack{ItemID: 1, Count: 1}
	if !stack.SetBlockEntityData(9, tag) {
		t.Fatal("SetBlockEntityData refused the spawner")
	}
	entityType, gotTag, ok := stack.BlockEntityDataOf()
	if !ok || entityType != 9 {
		t.Fatalf("BlockEntityDataOf = %d, %v, want the spawner", entityType, ok)
	}
	if _, hasID := gotTag["id"]; hasID {
		t.Error("typed payload retained id beside the registry type")
	}

	var trusted Buffer
	trusted.PutSlot(stack)
	read, ok := NewReader(trusted.Bytes()).ReadSlot()
	if !ok {
		t.Fatal("ReadSlot refused the block entity data")
	}
	if entityType, _, ok := read.BlockEntityDataOf(); !ok || entityType != 9 {
		t.Errorf("the trusted slot's block_entity_data = %d, %v, want the spawner", entityType, ok)
	}

	untrusted := untrustedSlotOf(stack.ItemID, DataComponentBlockEntityData, stack.Raw[0].Data)
	read, ok = NewReader(untrusted).ReadUntrustedSlot()
	if !ok {
		t.Fatal("ReadUntrustedSlot refused the block entity data")
	}
	if entityType, _, ok := read.BlockEntityDataOf(); !ok || entityType != 9 {
		t.Errorf("the untrusted slot's block_entity_data = %d, %v, want the spawner", entityType, ok)
	}

	if stack.SetBlockEntityData(RegistrySizeBlockEntityType, tag) {
		t.Error("SetBlockEntityData accepted a type past the registry")
	}
}

func TestTypedPayloadHistorySurvivesWire(t *testing.T) {
	tag, err := nbt.ParseOrderedSNBT(`{id:"ignored",BB:1,Aa:2,nested:{BB:3,Aa:4}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []int32{DataComponentEntityData, DataComponentBlockEntityData, DataComponentBucketEntityData, DataComponentBees} {
		t.Run(fmt.Sprint(component), func(t *testing.T) {
			stack := ItemStack{ItemID: 1, Count: 1}
			set := func(stack *ItemStack, tag nbt.CompoundTag) {
				switch component {
				case DataComponentEntityData:
					if !stack.SetEntityData(1, tag) {
						t.Fatal("entity data refused")
					}
				case DataComponentBlockEntityData:
					if !stack.SetBlockEntityData(1, tag) {
						t.Fatal("block data refused")
					}
				case DataComponentBucketEntityData:
					stack.SetBucketEntityData(tag)
				case DataComponentBees:
					if !stack.SetBees([]Bee{{EntityType: 1, Data: tag}}) {
						t.Fatal("bee refused")
					}
				}
			}
			payload := func(stack ItemStack) nbt.CompoundTag {
				switch component {
				case DataComponentEntityData:
					_, tag, _ := stack.EntityDataTag()
					return tag
				case DataComponentBlockEntityData:
					_, tag, _ := stack.BlockEntityDataTag()
					return tag
				case DataComponentBucketEntityData:
					return stack.BucketEntityDataTag()
				default:
					bees, ok := stack.BeesOf()
					if !ok || len(bees) != 1 {
						t.Fatal("bee missing")
					}
					return bees[0].Data
				}
			}
			set(&stack, tag)
			copy := stack
			other, _ := nbt.ParseOrderedSNBT(`{BB:5,Aa:6}`)
			set(&copy, other)
			for _, untrusted := range []bool{false, true} {
				var received ItemStack
				if untrusted {
					received, _ = NewReader(untrustedSlotOf(stack.ItemID, component, stack.Raw[0].Data)).ReadUntrustedSlot()
				} else {
					var wire Buffer
					wire.PutSlot(stack)
					received, _ = NewReader(wire.Bytes()).ReadSlot()
				}
				data := payload(received)
				if data == nil {
					t.Fatal("payload missing")
				}
				nested, ok := data.Entries()["nested"].(nbt.CompoundTag)
				if !ok || !slices.Equal(nbt.HashOrderedKeys(nested), []string{"BB", "Aa"}) {
					t.Fatalf("nested history lost: %v", data)
				}
				keys := nbt.HashOrderedKeys(data)
				if len(keys) < 2 || keys[0] != "BB" || keys[1] != "Aa" || data.Entries()["BB"] != nbt.Int(1) {
					t.Fatalf("payload history or copy isolation lost: %v", keys)
				}
				_, hasID := data.Entries()["id"]
				if hasID != (component == DataComponentBucketEntityData) {
					t.Fatalf("typed id stripping: %v", hasID)
				}
			}
		})
	}
	if tag.Entries()["id"] != nbt.String("ignored") {
		t.Fatal("setting typed data mutated the input")
	}
}
