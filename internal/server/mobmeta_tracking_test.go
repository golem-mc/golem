package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func mobMetadataTracking(kind entity.MobType) (*mobEntity, *mobDimTick, *mobTickInputs) {
	w := new(world.World)
	s := new(Server)
	s.entityBroadcastRange.Store(100)
	s.viewDistance.Store(10)
	dim := &dimension{w: w}
	m := entity.NewMob(kind, 7, 0.5, 64, 0.5, 11)
	m.SetMetadataNoAI(true)
	ent := &mobEntity{mob: m, w: w}
	herd := newMobHerd(w)
	herd.add(m)
	herd.remember(ent)
	tick := &mobDimTick{s: s, dim: dim, h: herd, w: w}
	in := &mobTickInputs{inWorld: map[*world.World][]*client{}, players: map[*world.World][]entity.Player{}}
	for i := range 2 {
		c := &client{srv: s, player: &player.Player{UUID: uuid.UUID{byte(i + 1)}}}
		s.assignPlayerSlot(c)
		c.dim.Store(dim)
		in.inWorld[w] = append(in.inWorld[w], c)
		in.players[w] = append(in.players[w], entity.Player{})
		body := mobTracker.body(ent)
		body.mark(c)
	}
	return ent, tick, in
}

func metadataTrackingSends(tick *mobDimTick, in *mobTickInputs) []mobSend {
	var fx mobEffects
	tick.scheduleMovement(&fx, in)
	var packets []mobSend
	for _, p := range fx.sends {
		if p.id == protocol.IDPlayEntityMetadata || p.id == protocol.IDPlayEntityEquipment || p.id == protocol.IDPlayUpdateAttributes {
			packets = append(packets, p)
		}
	}
	return packets
}

func TestMobMetadataRevisionsAreSharedAndImmutable(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	first := ent.metaSeen
	for _, tr := range ent.tracked.all() {
		if tr.own.meta != first {
			t.Fatal("pairing did not share its revision")
		}
	}
	if packets := metadataTrackingSends(tick, in); len(packets) != 0 {
		t.Fatalf("unchanged mob sent %d packets", len(packets))
	}
	if ent.metaSeen != first {
		t.Fatal("unchanged mob replaced its revision")
	}
	ent.mob.SetMetadataHealth(ent.mob.Health - 1)
	packets := metadataTrackingSends(tick, in)
	if len(packets) != 2 {
		t.Fatalf("health update sent %d packets, want two", len(packets))
	}
	if first.health != float32(ent.mob.MaxHealth) {
		t.Fatal("health update mutated the previous revision")
	}
	for _, tr := range ent.tracked.all() {
		if tr.own.meta != ent.metaSeen || tr.own.meta.health != float32(ent.mob.MaxHealth)-1 {
			t.Fatal("watcher did not advance to the shared revision")
		}
	}
	if packets := metadataTrackingSends(tick, in); len(packets) != 0 {
		t.Fatalf("idle tick repeated %d packets", len(packets))
	}
}

func TestMobMetadataImmediateBroadcastKeepsPendingFields(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	original := ent.metaSeen
	ent.mob.SetMetadataHealth(7)
	ent.syncHealth()
	if original.health != float32(ent.mob.MaxHealth) {
		t.Fatal("immediate health broadcast mutated the shared revision")
	}
	ent.mob.SetMetadataAggressive(true)
	var want protocol.Buffer
	want.PutVarInt(ent.mob.ID)
	want.PutMetadata(protocol.MetadataIndexMobFlags, protocol.MetadataByte)
	want.PutU8(protocol.EntityFlagNoAI | protocol.EntityFlagAggressive)
	want.EndMetadata()
	packets := metadataTrackingSends(tick, in)
	if len(packets) != 2 {
		t.Fatalf("pending aggression sent %d packets, want two", len(packets))
	}
	for _, p := range packets {
		if p.id != protocol.IDPlayEntityMetadata || !bytes.Equal(p.data, want.Bytes()) {
			t.Fatalf("pending update = %x/%x, want metadata %x", p.id, p.data, want.Bytes())
		}
	}
	ent.mob.SetMetadataHealth(6)
	ent.syncHealth()
	if packets := metadataTrackingSends(tick, in); len(packets) != 0 {
		t.Fatalf("health was repeated in %d packets", len(packets))
	}
	for _, tr := range ent.tracked.all() {
		if tr.own.meta != ent.metaSeen {
			t.Fatal("equal broadcast baseline was not folded into the shared revision")
		}
	}
}

func TestMobMetadataNewWatcherKeepsItsPairingBaseline(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	ent.mob.SetMetadataHealth(ent.mob.Health - 1)
	c := &client{srv: tick.s, player: &player.Player{UUID: uuid.UUID{3}}}
	tick.s.assignPlayerSlot(c)
	c.dim.Store(tick.dim)
	in.inWorld[tick.w] = append(in.inWorld[tick.w], c)
	in.players[tick.w] = append(in.players[tick.w], entity.Player{})
	body := mobTracker.body(ent)
	body.mark(c)
	packets := metadataTrackingSends(tick, in)
	if len(packets) != 2 {
		t.Fatalf("update sent %d packets, want two old watchers", len(packets))
	}
	for _, p := range packets {
		if p.c == c {
			t.Fatal("new watcher was sent metadata already in its pairing")
		}
	}
}

func TestMobMetadataRevisionsKeepDependentPacketOrder(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   entity.MobType
		turn   func(*entity.Mob)
		first  int32
		second int32
	}{
		{"saddle", entity.TypePig, func(m *entity.Mob) {
			m.SetMetadataHorseSaddle(protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1})
		}, protocol.IDPlayEntityEquipment, protocol.IDPlayEntityMetadata},
		{"baby", entity.TypeZombie, func(m *entity.Mob) { m.SetBaby(true) }, protocol.IDPlayEntityMetadata, protocol.IDPlayUpdateAttributes},
	} {
		t.Run(test.name, func(t *testing.T) {
			ent, tick, in := mobMetadataTracking(test.kind)
			test.turn(ent.mob)
			packets := metadataTrackingSends(tick, in)
			if len(packets) != 4 {
				t.Fatalf("update sent %d packets, want four", len(packets))
			}
			for i := 0; i < len(packets); i += 2 {
				if packets[i].c != packets[i+1].c || packets[i].id != test.first || packets[i+1].id != test.second {
					t.Fatalf("packet order = %x, %x", packets[i].id, packets[i+1].id)
				}
			}
		})
	}
}

func TestMobMetadataDirtyCaptureSurvivesNoWatchersAndImmediateSends(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	old, read := ent.metaSeen, ent.metaRead
	watchers := in.inWorld[tick.w]
	in.inWorld[tick.w] = nil
	ent.mob.SetMetadataAggressive(true)
	ent.mob.SetMetadataHealth(7)
	ent.syncHealth()
	if packets := metadataTrackingSends(tick, in); len(packets) != 0 {
		t.Fatal("sent without watchers")
	}
	if ent.metaRead != read || ent.metaSeen != old || !ent.mob.MetadataDirty().Has(entity.MobMetadataMobFlags) {
		t.Fatal("unwatched or immediate send consumed pending capture")
	}
	in.inWorld[tick.w] = watchers
	packets := metadataTrackingSends(tick, in)
	if len(packets) != 2 {
		t.Fatalf("pending flags sent %d packets", len(packets))
	}
	if ent.metaRead != ent.mob.MetadataRevision() || ent.mob.MetadataDirty() != (entity.MobMetadataMask{}) {
		t.Fatal("tracking did not consume the revision and mask together")
	}
	for _, p := range packets {
		var want protocol.Buffer
		want.PutVarInt(ent.mob.ID)
		want.PutMetadata(protocol.MetadataIndexMobFlags, protocol.MetadataByte)
		want.PutU8(protocol.EntityFlagNoAI | protocol.EntityFlagAggressive)
		want.EndMetadata()
		if !bytes.Equal(p.data, want.Bytes()) {
			t.Fatalf("pending packet = %x, want %x", p.data, want.Bytes())
		}
	}
}

func TestMobMetadataDeltaEncodingsAreSharedAcrossBaselines(t *testing.T) {
	ent, _, _ := mobMetadataTracking(entity.TypeZombie)
	first := ent.metaSeen
	ent.mob.SetBaby(true)
	second := ent.metadataState()
	ent.mob.SetMetadataHealth(7)
	now := ent.metadataState()
	cache := mobMetadataDeltas{ent: ent, now: now}
	a := cache.from(first)
	if len(a) != 2 || cache.others != nil {
		t.Fatal("common baseline allocated a map or lost dependent attributes")
	}
	again := cache.from(first)
	if &a[0] != &again[0] {
		t.Fatal("common baseline re-encoded its packets")
	}
	b := cache.from(second)
	if len(b) != 1 {
		t.Fatal("late baseline repeated the baby attributes")
	}
	for range 3 {
		again := cache.from(first)
		if &a[0] != &again[0] {
			t.Fatal("staggered baseline re-encoded its packets")
		}
		again = cache.from(second)
		if &b[0] != &again[0] {
			t.Fatal("late baseline re-encoded its packets")
		}
	}
}

func TestMobMetadataEffectCaptureKeepsImmutableParticleStorage(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	ent.mob.LivingEffects.AddEffect(effect.New(registry.MobEffectSpeed, 10, 0, false, true, true))
	if packets := metadataTrackingSends(tick, in); len(packets) != 2 {
		t.Fatal("embedded effect revision did not reach both watchers")
	}
	before := ent.metaSeen
	ent.mob.SetMetadataGlowing(true)
	metadataTrackingSends(tick, in)
	if ent.metaSeen == before || &ent.metaSeen.particles[0] != &before.particles[0] || before.baseFlags != 0 {
		t.Fatal("flag capture mutated particles or its old snapshot")
	}
	ent.mob.LivingEffects.ClearEffects()
	if packets := metadataTrackingSends(tick, in); len(packets) != 2 {
		t.Fatal("embedded effect removal did not reach both watchers")
	}
	if len(ent.metaSeen.particles) != 0 || len(before.particles) == 0 {
		t.Fatal("removal retained particles or changed the old snapshot")
	}
}
