package world

import (
	"bytes"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
)

// chunkPacketCache is guarded by Chunk.packetMu. Its key uses the immutable
// terrain forms and light arrays; update tags are compared by their wire bytes
// because an entity can change under its own lock without a terrain write.
type chunkPacketCache struct {
	packet                               *protocol.CachedPacket
	encoded, heightmaps, light, entities []byte
	sky, block                           [lightSections]*[lightArrayLen]byte
}

func sameForm(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// ChunkPacket returns a shared immutable Chunk Data and Update Light payload.
// Callers must not change the returned bytes.
func (w *World) ChunkPacket(c *Chunk) []byte {
	return w.CachedChunkPacket(c).Data()
}

// CachedChunkPacket returns the shared payload and compression cache for the
// chunk's current terrain, light and block entity update tags. Terrain forms
// built under the read lock are installed only if no write landed meanwhile;
// a second failed try rebuilds under the write lock. Entity locks are taken
// with World.mu released, following its lock order.
func (w *World) CachedChunkPacket(c *Chunk) *protocol.CachedPacket {
	c.packetMu.Lock()
	defer func() {
		c.packetMu.Unlock()
		w.retainChunkPacket(c)
	}()
	for tried := false; ; {
		w.mu.RLock()
		if c.stale {
			writes := c.writes
			encoded, heightmaps := w.encodeChunkWired(c), w.encodeHeightmaps(c)
			w.mu.RUnlock()
			w.mu.Lock()
			switch {
			case c.stale && c.writes == writes:
				c.encoded, c.heightmaps = encoded, heightmaps
				c.stale = false
			case c.stale && tried:
				// Written during both tries: rebuild under the write lock
				// rather than race a chunk that never stops changing.
				w.encodeChunk(c)
				c.stale = false
			}
			tried = true
			w.mu.Unlock()
			continue
		}
		// The cached forms are replaced, never written in place, so they are
		// copied out after the lock is down.
		heightmaps, encoded := c.heightmaps, c.encoded
		next := chunkPacketCache{encoded: encoded, heightmaps: heightmaps, sky: c.skyLight, block: c.blockLight}
		unchanged := c.packet.packet != nil && sameForm(encoded, c.packet.encoded) &&
			sameForm(heightmaps, c.packet.heightmaps) && next.sky == c.packet.sky && next.block == c.packet.block
		light := c.packet.light
		if !unchanged {
			c.lightFormsMu.Lock()
			c.lightFormsRetained = true
			c.lightFormsMu.Unlock()
			light = w.chunkLightWired(c)
		}
		var ownBuf [16]*BlockEntity
		var own []*BlockEntity
		if len(c.entityOrder) <= len(ownBuf) {
			own = c.appendBlockEntities(ownBuf[:0])
		} else {
			own = c.appendBlockEntities(nil)
		}
		rides := c.snapshotPistonRides()
		// Snapshot the carried compounds under the lock: a command setting one
		// replaces the list, and a save reads it. Trial spawners' block state
		// is read under the same lock.
		carried := slices.Clone(c.carriedBlockEntities.Items)
		active := c.activeTrialSpawners(carried)
		w.mu.RUnlock()
		sortPistonRides(rides)

		// The block entities go out packed with their update tags (a sign's
		// text, a banner's patterns, a container only its place). This is the
		// only place a client learns they exist. They are written first, to
		// scratch, so the payload is allocated once at its size.
		entities := scratchBuffer()
		if len(own)+len(rides)+len(carried) == 0 {
			entities.PutU8(0)
		} else {
			putBlockEntities(entities, own, rides, carried, active)
		}
		if unchanged && bytes.Equal(entities.Bytes(), c.packet.entities) {
			scratchBuffers.Put(entities)
			return c.packet.packet
		}
		var b protocol.Buffer
		// x and z, then the sections' length prefix of at most 5 bytes.
		b.Grow(8 + len(heightmaps) + 5 + len(encoded) + entities.Len() + len(light))
		b.PutI32(c.x)
		b.PutI32(c.z)
		b.PutRaw(heightmaps)
		b.PutBytes(encoded)
		b.PutRaw(entities.Bytes())
		b.PutRaw(light)
		next.light, next.entities = light, slices.Clone(entities.Bytes())
		scratchBuffers.Put(entities)
		next.packet = protocol.NewCachedPacket(protocol.IDPlayChunkData, b.Bytes())
		c.packet = next
		return next.packet
	}
}
