package server

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestPluginEntityHandleCacheMatchesUncached(t *testing.T) {
	s, pw := newPluginHandleTestWorld(t)
	for i := range registry.EntityTypeCount {
		kind := registry.EntityType(i)
		if kind == registry.EntityTypePlayer {
			continue
		}
		t.Run(kind.Name(), func(t *testing.T) {
			extra := ""
			if kind == registry.EntityTypeItem {
				extra = ",Item:{id:\"minecraft:stone\",count:1}"
			}
			id := uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012x", i+1))
			out := s.RunLine(fmt.Sprintf("summon %s 0 -60 0 {UUID:[I;0,0,0,%d]%s}", kind, i+1, extra))
			if len(out) == 1 && len(out[0].Extra) == 1 {
				switch out[0].Extra[0].Translate {
				case "commands.summon.failed", "entity.not_summonable":
					t.Skip(out[0].Extra[0].Translate)
				}
			}
			if len(out) != 1 || out[0].Translate != "commands.summon.success" {
				t.Fatalf("summon feedback: %+v", out)
			}
			body, ok := pw.host.entityByUUID(id, pw.dim.name)
			if !ok || pluginEntityType(body) != kind {
				t.Fatalf("summon did not make a live %v with UUID %v", kind, id)
			}

			slot := &handleShapes[kind]
			slot.Store(0)
			base := pluginEntity{host: pw.host, id: id}
			uncached := fixedPluginHandle(base, body, kind)
			if uncached != nil {
				checkPluginHandleTypes(t, pw.host, body, reflect.TypeOf(uncached))
				if slot.Load() != 0 {
					t.Fatal("fixed handle populated the cache")
				}
				return
			}

			shape := uncachedHandleShape(body, kind)
			counted := &countedHandleBody{entitySnapshot: body.(*entitySnapshot)}
			checkPluginHandleTypes(t, pw.host, counted, reflect.TypeOf(handleOf(base, shape)))
			if got := handleShape(slot.Load()); got != shape {
				t.Errorf("cached shape = %v, want %v", got, shape)
			}
			if counted.saves != 1 {
				t.Errorf("building cold and warm handles took %d saves, want 1", counted.saves)
			}
		})
	}
}

func checkPluginHandleTypes(t *testing.T, host *pluginHost, body command.Entity, want reflect.Type) {
	t.Helper()
	for i := range 2 {
		if got := host.pluginEntityOf(body); reflect.TypeOf(got) != want {
			t.Errorf("handle %d = %T, want %v", i+1, got, want)
		}
	}
}

type countedHandleBody struct {
	*entitySnapshot
	saves int
}

func (b *countedHandleBody) DataTag() (nbt.CompoundTag, bool) {
	b.saves++
	return b.entitySnapshot.DataTag()
}

func TestPluginEntityHandleIgnoresUnmodelledProjectileFields(t *testing.T) {
	s, pw := newPluginHandleTestWorld(t)
	handleShapes[registry.EntityTypeZombie].Store(0)
	tagged := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	plain := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	s.RunLine("summon zombie 0 -60 0 {UUID:[I;0,0,0,1],HasBeenShot:1b,inGround:1b}")
	s.RunLine("summon zombie 0 -60 0 {UUID:[I;0,0,0,2]}")
	for _, id := range []uuid.UUID{tagged, plain} {
		handle, ok := pw.Entity(plugin.UUID(id))
		if want := reflect.TypeFor[pluginAgeableEntity](); !ok || reflect.TypeOf(handle) != want {
			t.Errorf("zombie %v has handle %T, want %v", id, handle, want)
		}
	}
}

func newPluginHandleTestWorld(t *testing.T) (*Server, pluginWorld) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	dim := s.allDims()[0]
	pos := world.ChunkPos{}
	dim.w.Acquire(pos)
	t.Cleanup(func() {
		dim.w.Release(pos)
		s.Shutdown()
	})
	return s, pluginWorld{srv: s, dim: dim, host: &pluginHost{srv: s}}
}
