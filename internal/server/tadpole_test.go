package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

func TestTadpoleAgeLockMetadata(t *testing.T) {
	m := entity.NewMob(entity.TypeTadpole, 1, 0, 64, 0, 1)
	ent := &mobEntity{mob: m}
	was := mobMetadataStateOf(m)
	m.AgeLocked = true
	field := []byte{protocol.MetadataIndexTadpoleAgeLocked, protocol.MetadataBoolean, 1}
	if !bytes.Contains(mobMetadataPayload(ent), field) || !bytes.Contains(mobMetadataDeltaPayload(ent, was), field) {
		t.Fatal("age lock missing from spawn or delta")
	}
	bucketFlag := []byte{protocol.MetadataIndexFishFromBucket, protocol.MetadataBoolean, 1}
	if bytes.Contains(mobMetadataPayload(ent), bucketFlag) {
		// Tadpole.setFromBucket is a no-op: AbstractFish.FROM_BUCKET stays false.
		t.Fatal("tadpole sent the fish bucket flag")
	}
	was = mobMetadataStateOf(m)
	m.AgeLocked = false
	if !bytes.Contains(mobMetadataDeltaPayload(ent, was), []byte{protocol.MetadataIndexTadpoleAgeLocked, protocol.MetadataBoolean, 0}) {
		t.Fatal("unlock missing from delta")
	}
	if got := mobBucketPour(registry.ItemTadpoleBucket); got.emptySound.Name() != "minecraft:item.bucket.empty_tadpole" || !got.neutral {
		t.Fatal("wrong tadpole pour sound")
	}
}
