package server

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// The schema supplies the cases, so adding a source also adds an invalidation check.
func TestMobMetadataSourceSettersCoverProjection(t *testing.T) {
	covered := map[string]bool{}
	for _, group := range mobMetadataSchema {
		for _, source := range group.sources {
			if strings.HasSuffix(source.path, "()") {
				continue
			}
			for kind := range entity.MobType(registry.EntityTypeCount) {
				if !entity.KnownMobKind(kind) {
					continue
				}
				m := entity.NewMob(kind, 7, .5, 64, .5, 11)
				// Enable branch-dependent reads without changing the source being tested.
				m.SetMetadataSleeping(true)
				m.SetMetadataFrogPhase(2)
				m.SetMetadataFrogTarget(17)
				m.SetMetadataAngerTicks(30)
				m.SetMetadataGameTime(40)
				storage := reflect.ValueOf(m).Elem()
				for part := range strings.SplitSeq(strings.TrimPrefix(source.path, "m."), ".") {
					if storage.Kind() == reflect.Pointer {
						if storage.IsNil() {
							storage = reflect.Value{}
							break
						}
						storage = storage.Elem()
					}
					storage = storage.FieldByName(part)
				}
				if !storage.IsValid() {
					continue
				} // Dragon storage exists only on dragons.
				old := copyMetadataSource(storage)
				methodName := "SetMetadata"
				for part := range strings.SplitSeq(strings.TrimPrefix(source.path, "m."), ".") {
					methodName += strings.ToUpper(part[:1]) + part[1:]
				}
				setter := reflect.ValueOf(m).MethodByName(methodName)
				if !setter.IsValid() {
					t.Fatalf("missing setter for %s", source.path)
				}
				candidates := []reflect.Value{copyMetadataSource(old)}
				next := copyMetadataSource(old)
				switch next.Kind() {
				case reflect.Bool:
					next.SetBool(!old.Bool())
				case reflect.Int, reflect.Int32, reflect.Int64:
					next.SetInt(old.Int() + 1)
				case reflect.Uint8, reflect.Uint16:
					next.SetUint(old.Uint() + 1)
				case reflect.Float64:
					next.SetFloat(old.Float() + 1)
				case reflect.String:
					next.SetString("brown")
				case reflect.Array:
					if next.Index(0).Kind() == reflect.Uint8 {
						next.Index(0).SetUint(1)
					} else {
						next.Index(0).SetInt(1)
					}
				case reflect.Struct:
					next = reflect.ValueOf(protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1})
				default:
					t.Fatalf("unhandled source %s (%v)", source.path, next.Type())
				}
				candidates = append(candidates, next, reflect.New(old.Type()).Elem())
				if next.Kind() >= reflect.Int && next.Kind() <= reflect.Int64 {
					negative := copyMetadataSource(old)
					negative.SetInt(-1)
					candidates = append(candidates, negative)
				}
				before := mobMetadataStateOf(m)
				for candidate, value := range candidates {
					ent := &mobEntity{mob: m}
					baseline := ent.metadataState()
					revision := m.MetadataRevision()
					setter.Call([]reflect.Value{value})
					now := before
					captureMobMetadata(m, &now, m.MetadataDirty())
					full := mobMetadataStateOf(m)
					if mobMetadataTurned(now, full) {
						t.Fatalf("%s kind %d: invalidation missed a projection dependency", source.path, kind)
					}
					if mobMetadataTurned(before, full) {
						covered[source.path] = true
					}
					if candidate == 0 && (m.MetadataRevision() != revision || m.MetadataDirty() != (entity.MobMetadataMask{})) {
						t.Fatalf("%s dirtied an equal write", source.path)
					}
					tracked := ent.metadataState()
					if mobMetadataTurned(*tracked, full) {
						t.Fatalf("%s shared snapshot missed a source", source.path)
					}
					if candidate == 0 && tracked != baseline {
						t.Fatalf("%s replaced an unchanged snapshot", source.path)
					}
					if !bytes.Equal(mobMetadataDeltaFrom(ent, tracked, baseline), mobMetadataDeltaFrom(ent, &full, baseline)) {
						t.Fatalf("%s changed source-to-packet bytes", source.path)
					}
					if !bytes.Equal(mobMetadataDeltaFrom(ent, &now, &before), mobMetadataDeltaFrom(ent, &full, &before)) {
						t.Fatalf("%s partial capture changed delta bytes", source.path)
					}
					// Reverting before a send restores the full projection through the same mask.
					setter.Call([]reflect.Value{old})
					captureMobMetadata(m, &now, m.MetadataDirty())
					if mobMetadataTurned(now, before) {
						t.Fatalf("%s did not restore the previous state", source.path)
					}
					restored := ent.metadataState()
					if mobMetadataTurned(*restored, before) || !bytes.Equal(mobMetadataDeltaFrom(ent, restored, tracked), mobMetadataDeltaFrom(ent, &before, &full)) {
						t.Fatalf("%s did not send its return to default", source.path)
					}
					setter.Call([]reflect.Value{value})
					setter.Call([]reflect.Value{old})
					if ent.metadataState() != restored {
						t.Fatalf("%s changed a reverted snapshot", source.path)
					}
				}
			}
			if !covered[source.path] {
				t.Errorf("source %s never changed its projection", source.path)
			}
		}
	}
}

// Reflection cannot Interface an unexported leaf; copy its scalar value instead.
func copyMetadataSource(value reflect.Value) reflect.Value {
	out := reflect.New(value.Type()).Elem()
	switch value.Kind() {
	case reflect.Bool:
		out.SetBool(value.Bool())
	case reflect.Int, reflect.Int32, reflect.Int64:
		out.SetInt(value.Int())
	case reflect.Uint8, reflect.Uint16:
		out.SetUint(value.Uint())
	case reflect.Float64:
		out.SetFloat(value.Float())
	case reflect.String:
		out.SetString(value.String())
	case reflect.Array:
		for i := range value.Len() {
			out.Index(i).Set(copyMetadataSource(value.Index(i)))
		}
	case reflect.Struct:
		out.Set(value)
	default:
		panic("unsupported metadata source")
	}
	return out
}

func TestMobMetadataAncestorInvalidationUsesSchemaUnion(t *testing.T) {
	masks := map[string]entity.MobMetadataMask{}
	bit := 0
	for _, group := range mobMetadataSchema {
		if group.fields == "" {
			continue
		}
		for _, source := range group.sources {
			if strings.HasSuffix(source.path, "()") {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(source.path, "m."), ".")
			name := "InvalidateMetadata"
			for _, part := range parts[:len(parts)-1] {
				name += strings.ToUpper(part[:1]) + part[1:]
				mask := masks[name]
				mask[bit/64] |= 1 << (bit % 64)
				masks[name] = mask
			}
		}
		bit++
	}
	for name, want := range masks {
		m := entity.NewMob(entity.TypeCow, 7, .5, 64, .5, 11)
		before := m.MetadataRevision()
		reflect.ValueOf(m).MethodByName(name).Call(nil)
		if m.MetadataRevision() != before+1 || m.MetadataDirty() != want {
			t.Fatalf("%s did not invalidate exactly its descendant sources", name)
		}
	}
}

func TestMobMetadataClockAndCountdownKeepEndSnapshot(t *testing.T) {
	m := entity.NewMob(entity.TypeWolf, 7, .5, 64, .5, 11)
	m.SetMetadataGameTime(100)
	m.SetMetadataAngerTicks(20)
	before := mobMetadataStateOf(m)
	m.ClearMetadataDirty()
	m.SetMetadataGameTime(101)
	m.SetMetadataAngerTicks(19)
	now := before
	captureMobMetadata(m, &now, m.MetadataDirty())
	if mobMetadataTurned(now, before) || now.angerEnd != 120 {
		t.Fatal("normal clock/countdown changed the projected end time")
	}
}
