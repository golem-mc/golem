package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// benchBobber is a hook just cast.
func benchBobber() *bobberEntity {
	return &bobberEntity{hook: entity.NewBobber(7, 0.5, 65.62, 0.5, 0, 0, 1, 11, 0, 0)}
}

// BenchmarkBobberMetadataDelta builds the metadata delta a tick sends for a
// hook whose bite opened since the watcher was told.
func BenchmarkBobberMetadataDelta(b *testing.B) {
	it := benchBobber()
	was := bobberTold{hooked: it.hook.HookedID, biting: it.hook.Biting}
	it.hook.Biting = true
	b.ReportAllocs()
	for b.Loop() {
		bobberMetadataDeltaPayload(it, was)
	}
}

// BenchmarkBobberMetadataSpawn builds the spawn metadata of a hook caught in
// a body.
func BenchmarkBobberMetadataSpawn(b *testing.B) {
	it := benchBobber()
	it.hook.HookedID = 9
	b.ReportAllocs()
	for b.Loop() {
		bobberMetadataPayload(it)
	}
}
