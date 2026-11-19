package storage

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAMobKeepsItsEntityFieldsAcrossARestart plants a pillager carrying the
// Entity fields every kind shares and the ones the server keeps beside the
// body (its name, scoreboard tags and raid membership), runs it through two
// servers in turn, and finds each field in the file the second one writes;
// a cow beside it, no raider, writes no raid fields.
// vanilla: Entity.saveWithoutId, Entity.load, Raider.addAdditionalSaveData
func TestAMobKeepsItsEntityFieldsAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	pillager := servertest.FixtureUUID(11, 22, 33, 44)
	cow := servertest.FixtureUUID(55, 66, 77, 88)
	name := nbt.Compound{"text": nbt.String("Patrol"), "color": nbt.String("gold")}
	tags := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("raider")}}
	doubles := func(v ...float64) nbt.List {
		l := nbt.List{Elem: nbt.TypeDouble}
		for _, d := range v {
			l.Items = append(l.Items, nbt.Double(d))
		}
		return l
	}
	rotation := nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(90), nbt.Float(-15)}}
	// NoAI and NoGravity hold both still, so their place and turn are the
	// planted ones; Invulnerable keeps the burning pillager from burning to
	// death on a slow join.
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		nbt.Compound{
			"id":                  nbt.String("minecraft:pillager"),
			"UUID":                servertest.UuidWords(pillager),
			"Pos":                 doubles(8.25, 64, 8.75),
			"Rotation":            rotation,
			"OnGround":            nbt.Byte(1),
			"NoGravity":           nbt.Byte(1),
			"NoAI":                nbt.Byte(1),
			"Invulnerable":        nbt.Byte(1),
			"PersistenceRequired": nbt.Byte(1),
			"Fire":                nbt.Short(2000),
			"PortalCooldown":      nbt.Int(2000),
			"CustomName":          name,
			"CustomNameVisible":   nbt.Byte(1),
			"Tags":                tags,
			"Wave":                nbt.Int(3),
			"CanJoinRaid":         nbt.Byte(1),
		},
		nbt.Compound{
			"id":                  nbt.String("minecraft:cow"),
			"UUID":                servertest.UuidWords(cow),
			"Pos":                 doubles(4.5, 64, 4.5),
			"NoAI":                nbt.Byte(1),
			"PersistenceRequired": nbt.Byte(1),
		},
	)

	// The first server reads the planted file and writes its own; the
	// second reads that one back and writes the file checked below.
	for range 2 {
		srv, connect := servertest.Start(t, cfg)
		c := connect()
		c.Login("Keeper")
		c.Configure()
		c.JoinSeeingMobs("Keeper", pillager, cow)
		servertest.HoldTicks(t, srv)
		servertest.RunTicks(t, srv, 20)
		srv.FlushMobs()
		c.Disconnect()
		srv.Shutdown()
	}

	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	got, ok := saved[pillager]
	if !ok {
		t.Fatal("the pillager was not saved back under its UUID")
	}
	for key, want := range map[string]nbt.Tag{
		"Pos":               doubles(8.25, 64, 8.75),
		"Rotation":          rotation,
		"OnGround":          nbt.Byte(1),
		"NoGravity":         nbt.Byte(1),
		"CustomName":        name,
		"CustomNameVisible": nbt.Byte(1),
		"Tags":              tags,
		"Wave":              nbt.Int(3),
		"CanJoinRaid":       nbt.Byte(1),
	} {
		if !reflect.DeepEqual(got[key], want) {
			t.Errorf("the reloaded pillager saved %s = %v, want %v", key, got[key], want)
		}
	}
	// Both count down with the ticks the two servers ran.
	if fire, ok := nbt.Get[nbt.Short](got, "Fire"); !ok || fire <= 0 || fire >= 2000 {
		t.Errorf("the reloaded pillager saved Fire = %v (%v), want the planted 2000 burnt partly down", got["Fire"], ok)
	}
	if cooldown, ok := nbt.Get[nbt.Int](got, "PortalCooldown"); !ok || cooldown <= 0 || cooldown > 2000 {
		t.Errorf("the reloaded pillager saved PortalCooldown = %v (%v), want the planted 2000 or less, above 0", got["PortalCooldown"], ok)
	}

	plain, ok := saved[cow]
	if !ok {
		t.Fatal("the cow was not saved back under its UUID")
	}
	for _, key := range []string{"Wave", "CanJoinRaid", "CustomName", "CustomNameVisible", "Tags", "NoGravity"} {
		if v, ok := plain[key]; ok {
			t.Errorf("the cow saved %s = %v, a field it never had", key, v)
		}
	}
}
