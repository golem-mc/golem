package server

import "github.com/golem-mc/golem/internal/server/world"

// syncPendingPackets pins the entire pending view, including chunks still
// waiting for a generation slot, before any batch can populate their caches.
// The sender owns these holds under chunkMu, like pendingSend.
func (c *client) syncPendingPackets() {
	next := make(map[world.ChunkPos]struct{}, len(c.pendingSend)+len(c.pendingGeneration)+len(c.generating))
	for _, pos := range c.pendingSend {
		next[pos] = struct{}{}
	}
	for _, pos := range c.pendingGeneration {
		next[pos] = struct{}{}
	}
	for pos := range c.generating {
		if world.InViewOf(pos, c.center, c.radius) {
			next[pos] = struct{}{}
		}
	}
	for pos := range next {
		if _, held := c.pendingPackets[pos]; !held {
			c.loadedWorld.HoldChunkPacket(pos)
		}
	}
	for pos := range c.pendingPackets {
		if _, keep := next[pos]; !keep {
			c.loadedWorld.ReleaseChunkPacket(pos)
		}
	}
	c.pendingPackets = next
}

func (c *client) releasePendingPacket(pos world.ChunkPos) {
	if _, held := c.pendingPackets[pos]; held {
		c.loadedWorld.ReleaseChunkPacket(pos)
		delete(c.pendingPackets, pos)
	}
}

func (c *client) releasePendingPackets() {
	for pos := range c.pendingPackets {
		c.releasePendingPacket(pos)
	}
	c.pendingPackets = nil
}
