package world

import (
	"container/list"
	"encoding/binary"
	"errors"
	"log/slog"
	"sync/atomic"
)

// shapedTerrainColdCap bounds the deflated terrains kept per generator, about
// 1.7 kB each.
const shapedTerrainColdCap = 32768

// coldTerrain is an evicted terrain, deflated.
type coldTerrain struct {
	pos    ChunkPos
	data   []byte
	finals uint32
	elem   *list.Element
}

var errColdTerrain = errors.New("bad cold terrain")

// appendTerrain appends t's saved form to b: per section its fill and, when it
// has storage, bits, used, palette and words; then the fluid updates and the
// two height arrays. The writer-only slots table is rebuilt on decode.
func appendTerrain(b []byte, t *shapedTerrain) []byte {
	for i, w := range t.blocks {
		b = binary.LittleEndian.AppendUint16(b, t.fill[i])
		if w == nil {
			b = append(b, 0)
			continue
		}
		b = append(b, 1, byte(w.bits))
		b = binary.LittleEndian.AppendUint16(b, uint16(w.used))
		for _, p := range w.palette[:w.used] {
			b = binary.LittleEndian.AppendUint32(b, p)
		}
		for _, word := range w.words {
			b = binary.LittleEndian.AppendUint64(b, word)
		}
	}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(t.fluidUpdates)))
	for _, k := range t.fluidUpdates {
		for _, v := range k {
			b = binary.LittleEndian.AppendUint32(b, uint32(v))
		}
	}
	for h := range t.wg {
		for i := range t.wg[h] {
			// Regions still holding t store these concurrently.
			b = binary.LittleEndian.AppendUint32(b, uint32(atomic.LoadInt32(&t.wg[h][i])))
		}
	}
	return b
}

// decodeTerrain undoes appendTerrain.
func decodeTerrain(b []byte) (*shapedTerrain, error) {
	t := &shapedTerrain{}
	// take returns the next n bytes, or nil once b runs out.
	take := func(n int) []byte {
		if len(b) < n {
			b = nil
			return nil
		}
		r := b[:n]
		b = b[n:]
		return r
	}
	for i := range t.blocks {
		h := take(3)
		if h == nil {
			return nil, errColdTerrain
		}
		t.fill[i] = binary.LittleEndian.Uint16(h)
		if h[2] == 0 {
			continue
		}
		h = take(3)
		if h == nil || int(h[0]) < minIndirectBits || int(h[0]) > directBits {
			return nil, errColdTerrain
		}
		w := makeBlockWords(int(h[0]))
		used := int(binary.LittleEndian.Uint16(h[1:]))
		if used > len(w.palette) {
			return nil, errColdTerrain
		}
		w.used = used
		for j := range used {
			p := take(4)
			if p == nil {
				return nil, errColdTerrain
			}
			w.palette[j] = binary.LittleEndian.Uint32(p)
			w.indexSlot(uint16(w.palette[j]), j)
		}
		for j := range w.words {
			p := take(8)
			if p == nil {
				return nil, errColdTerrain
			}
			w.words[j] = binary.LittleEndian.Uint64(p)
		}
		t.blocks[i] = w
	}
	n := take(4)
	if n == nil {
		return nil, errColdTerrain
	}
	if count := int(binary.LittleEndian.Uint32(n)); count > 0 {
		if len(b) < count*12 {
			return nil, errColdTerrain
		}
		t.fluidUpdates = make([]posKey, count)
		for j := range t.fluidUpdates {
			for k := range 3 {
				t.fluidUpdates[j][k] = int32(binary.LittleEndian.Uint32(take(4)))
			}
		}
	}
	for h := range t.wg {
		for i := range t.wg[h] {
			p := take(4)
			if p == nil {
				return nil, errColdTerrain
			}
			t.wg[h][i] = int32(binary.LittleEndian.Uint32(p))
		}
	}
	if len(b) != 0 {
		return nil, errColdTerrain
	}
	return t, nil
}

// spill deflates the victims evict moved to compressing, then files each in
// the cold tier unless a lookup took it back or markFinal finished it.
func (c *shapedTerrainCache) spill(victims []*terrainEntry) {
	if len(victims) == 0 {
		return
	}
	buf := decorProtoBufs.Get().(*decorProtoBuf)
	defer decorProtoBufs.Put(buf)
	for _, e := range victims {
		buf.raw = appendTerrain(buf.raw[:0], e.t)
		buf.out.Reset()
		buf.deflateRaw()
		data := append([]byte(nil), buf.out.Bytes()...)
		c.mu.Lock()
		if c.compressing[e.pos] == e {
			delete(c.compressing, e.pos)
			cold := &coldTerrain{pos: e.pos, data: data, finals: e.finals}
			cold.elem = c.coldLRU.PushFront(cold)
			if c.cold == nil {
				c.cold = map[ChunkPos]*coldTerrain{}
			}
			c.cold[e.pos] = cold
			c.coldBytes += len(data)
			for len(c.cold) > shapedTerrainColdCap {
				c.removeCold(c.coldLRU.Back().Value.(*coldTerrain))
			}
		}
		c.mu.Unlock()
	}
}

func (c *shapedTerrainCache) removeCold(e *coldTerrain) {
	delete(c.cold, e.pos)
	c.coldLRU.Remove(e.elem)
	c.coldBytes -= len(e.data)
}

// inflateTerrain decodes a cold entry's terrain; nil means it is unreadable
// and the caller shapes the chunk again.
func inflateTerrain(cold *coldTerrain) *shapedTerrain {
	buf := decorProtoBufs.Get().(*decorProtoBuf)
	defer decorProtoBufs.Put(buf)
	t, err := decodeTerrain(buf.inflate(cold.data))
	if err != nil {
		slog.Warn("dropping unreadable cold terrain", "chunk", cold.pos, "err", err)
		return nil
	}
	return t
}
