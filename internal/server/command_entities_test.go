package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
	"reflect"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestEntitySnapshotDataTagUnlocksAfterPanic(t *testing.T) {
	mu := &sync.Mutex{}
	e := &entitySnapshot{mu: mu, ops: &entityOps{
		tag: func(any) nbt.CompoundTag { panic("tag builder") },
	}}
	func() {
		defer func() {
			if got := recover(); got != "tag builder" {
				t.Errorf("panic = %v, want tag builder", got)
			}
		}()
		e.DataTag()
	}()
	if !mu.TryLock() {
		t.Fatal("tag builder panic left the kind's lock held")
	}
	mu.Unlock()
}

func nameSnapshots() []*entitySnapshot {
	s := withDims(&Server{scoreboard: scoreboard.New(nil)}, []*dimension{{name: world.DimensionName}})
	identity := uuid.MustParse("12345678-1234-5678-90ab-cdef12345678")
	mob := &mobEntity{mob: entity.NewMob(entity.TypePig, 1, 0, 64, 0, 1), uuid: identity}
	mob.name = nbt.Compound{"text": nbt.String("Named pig"), "color": nbt.String("gold"), "insertion": nbt.String("replace me")}
	item := &itemEntity{item: entity.NewItem(2, protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1}, 0, 64, 0, 0, 0, 0, 0, 1), uuid: identity}
	return []*entitySnapshot{
		s.snapshot(nil, nbt.StringSet{}, nil, 3, "minecraft:armor_stand", "Armor Stand", identity, world.DimensionName, player.Position{}, [6]float64{}),
		s.mobSnapshot(mob, world.DimensionName, nil),
		s.itemSnapshot(item, nil),
	}
}

func TestEntitySnapshotDisplayNameConcurrent(t *testing.T) {
	snapshots := nameSnapshots()
	identity := snapshots[0].uuid
	names := []protocol.Text{
		{Translate: "entity.minecraft.armor_stand"},
		{Text: "Named pig", Color: "gold", Insertion: "replace me"},
		{Translate: "item.minecraft.diamond"},
	}
	types := []string{"minecraft:armor_stand", "minecraft:pig", "minecraft:item"}
	plain := []string{"Armor Stand", "Named pig", "Diamond"}
	for i := range snapshots {
		hoverName := names[i]
		names[i].Insertion = identity.String()
		names[i].HoverEntity = &protocol.HoverEntity{Name: hoverName, ID: types[i], UUID: uuidWords(identity)}
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 100 {
				for i, e := range snapshots {
					if got := e.DisplayName(); !reflect.DeepEqual(got, names[i]) {
						t.Errorf("display name %d = %+v, want %+v", i, got, names[i])
						return
					}
					if e.EntityType() != types[i] || e.PlainName() != plain[i] {
						t.Errorf("type or plain name changed for snapshot %d", i)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func TestEntitySnapshotItemName(t *testing.T) {
	s := withDims(&Server{scoreboard: scoreboard.New(nil)}, []*dimension{{name: world.DimensionName}})
	stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	plain := &itemEntity{item: entity.NewItem(2, stack, 0, 64, 0, 0, 0, 0, 0, 1), uuid: uuid.New()}
	e := s.itemSnapshot(plain, nil)
	if got := e.DisplayName().Translate; got != "block.minecraft.stone" {
		t.Errorf("display name = %q", got)
	}
	if got := e.PlainName(); got != "Stone" {
		t.Errorf("plain name = %q", got)
	}
	named := &itemEntity{item: entity.NewItem(3, stack, 0, 64, 0, 0, 0, 0, 0, 1), uuid: uuid.New()}
	named.custom = customNameOfTag(nbt.Compound{"CustomName": nbt.String("Rock")})
	e = s.itemSnapshot(named, nil)
	if got := e.DisplayName(); got.Text != "Rock" || got.Translate != "" {
		t.Errorf("custom display name = %+v", got)
	}
	if got := e.PlainName(); got != "Rock" {
		t.Errorf("custom plain name = %q", got)
	}
}
