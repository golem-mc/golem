package server

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestWakingEntityJoinsItsTagTeam checks that a body read from a chunk's
// entity file joins the team its Team string names once registered.
// vanilla: Entity.load
func TestWakingEntityJoinsItsTagTeam(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.allDims()[0].w
	pos := world.ChunkPos{X: 2, Z: 2}
	w.Acquire(pos)
	defer w.Release(pos)

	m := entity.NewMob(entity.TypeZombie, s.nextMobEntity(entity.TypeZombie), 40.5, -60, 40.5, 0)
	identity := uuid.New()
	tag := mobTag(&mobEntity{mob: m, w: w, uuid: identity})
	tag["Team"] = nbt.String("red")
	if err := w.SaveChunkEntities(pos, []nbt.Tag{tag}); err != nil {
		t.Fatal(err)
	}
	s.scoreboard.AddTeam("red", nil)

	woken, fresh := s.wakeChunkOnce(s.allDims()[0], pos)
	if !fresh || len(woken.mobs) != 1 {
		t.Fatalf("wake stood %d mobs (fresh %v), want the saved zombie", len(woken.mobs), fresh)
	}
	s.registerWake(woken)
	if team, ok := s.scoreboard.PlayerTeam(identity.String()); !ok || team != "red" {
		t.Fatalf("woken zombie's team = %q, %v; want red", team, ok)
	}
}

// TestCarriedTagJoinsItsTeamOnceAndWritesBackWithoutIt checks that a write
// wakes a chunk's mob and carries its other tags with their Team, and that
// the next tick's wake joins both and carries the tag without the key.
// vanilla: Entity.load, Entity.saveWithoutId
func TestCarriedTagJoinsItsTeamOnceAndWritesBackWithoutIt(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	dim := s.allDims()[0]
	h, w := dim.mobs, dim.w
	pos := world.ChunkPos{X: 2, Z: 2}
	w.Acquire(pos)
	defer w.Release(pos)
	zombie := uuid.New()
	mob := mobTag(&mobEntity{mob: entity.NewMob(entity.TypeZombie, s.nextMobEntity(entity.TypeZombie), 40.5, -60, 40.5, 0), w: w, uuid: zombie})
	mob["Team"] = nbt.String("red")
	carried := uuid.New()
	tag := nbt.Compound{
		"id":   nbt.String("minecraft:not_an_entity"),
		"UUID": nbt.UUIDToIntArray(carried),
		"Team": nbt.String("red"),
	}
	if err := w.SaveChunkEntities(pos, []nbt.Tag{mob, tag}); err != nil {
		t.Fatal(err)
	}
	s.scoreboard.AddTeam("red", nil)

	s.mobsMu.Lock()
	h.add(h.wakeForWrite(pos, s, h.liveWith(map[uuid.UUID]bool{}))...)
	fx := newMobEffects(s)
	tick := mobDimTick{s: s, dim: dim, h: h, w: w, known: map[uuid.UUID]bool{}, prepared: true}
	tick.wakeLoadedChunks(&fx, nil)
	s.mobsMu.Unlock()
	s.registerWake(fx.loaded)

	for name, identity := range map[string]uuid.UUID{"zombie": zombie, "carried tag": carried} {
		if team, ok := s.scoreboard.PlayerTeam(identity.String()); !ok || team != "red" {
			t.Errorf("%s's team = %q, %v; want red", name, team, ok)
		}
	}
	if len(fx.loaded.teams) != 2 {
		t.Errorf("tick's wake joined %d teams, want 2", len(fx.loaded.teams))
	}
	if len(h.carried[pos]) != 1 {
		t.Fatalf("carried %d tags, want 1", len(h.carried[pos]))
	}
	carriedTag, ok := nbt.CompoundView(h.carried[pos][0])
	if !ok {
		t.Fatalf("carried tag is %T, want compound", h.carried[pos][0])
	}
	if _, has := carriedTag["Team"]; has {
		t.Error("carried tag still has Team")
	}
}

// TestSpawnsFromOneRiderTagSeatEachRider checks that two summons from one
// tag whose rider has no UUID each seat a rider of their own, and leave the
// tag's rider without a UUID, as a spawner's SpawnData must stay.
// vanilla: EntityType.loadEntityRecursive
func TestSpawnsFromOneRiderTagSeatEachRider(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	dim := s.allDims()[0]
	rider := nbt.Compound{"id": nbt.String("minecraft:zombie")}
	tag := nbt.Compound{"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{rider}}}
	for range 2 {
		if _, err := s.spawnSummonedMobInDim(dim, entity.TypeZombie, 40.5, -60, 40.5, maps.Clone(tag)); err != nil {
			t.Fatal(err)
		}
	}
	if _, has := rider["UUID"]; has {
		t.Error("the tag's rider gained a UUID")
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var mobs, seated int
	for _, ent := range dim.mobs.book {
		mobs++
		if ent.mob.Vehicle != nil {
			seated++
		}
	}
	if mobs != 4 || seated != 2 {
		t.Errorf("%d mobs with %d seated, want 4 and 2", mobs, seated)
	}
}

// TestSpawnEggJoinsItsEntityDataTeam checks that a spawn egg's entity_data
// Team puts the spawned mob on that team.
// vanilla: EntityType.updateCustomEntityTag, Entity.load
func TestSpawnEggJoinsItsEntityDataTeam(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	s.scoreboard.AddTeam("red", nil)
	typ, _ := registry.EntityTypeID("minecraft:zombie")
	data, err := nbt.EncodeNetwork(nbt.Compound{"Team": nbt.String("red")})
	if err != nil {
		t.Fatal(err)
	}
	var component protocol.Buffer
	component.PutVarInt(typ)
	component.PutRaw(data)
	egg := protocol.ItemStack{ItemID: int32(registry.ItemZombieSpawnEgg), Count: 1,
		Raw: []protocol.RawComponent{{Type: protocol.DataComponentEntityData, Data: component.Bytes()}}}
	s.standEggMob(s.allDims()[0], egg, entity.TypeZombie, 40.5, -60, 40.5)
	s.mobsMu.Lock()
	var identity uuid.UUID
	for _, m := range s.allDims()[0].mobs.book {
		identity = m.uuid
	}
	s.mobsMu.Unlock()
	if team, ok := s.scoreboard.PlayerTeam(identity.String()); !ok || team != "red" {
		t.Fatalf("egg mob's team = %q, %v; want red", team, ok)
	}
}
