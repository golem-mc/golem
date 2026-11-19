package protocol

import (
	"slices"
	"sync"
)

// CachedPacket owns an immutable payload and shares its framed form between
// connections. Only the most recent threshold is retained, bounding memory
// even when connections use many different compression settings.
type CachedPacket struct {
	id   int32
	data []byte

	mu        sync.Mutex
	threshold int
	framed    []byte
}

// NewCachedPacket takes ownership of data; callers must not change it.
func NewCachedPacket(id int32, data []byte) *CachedPacket {
	return &CachedPacket{id: id, data: data}
}

// Data returns the shared payload, which callers must not change.
func (p *CachedPacket) Data() []byte { return p.data }

// frame returns immutable bytes before encryption, which remains per connection.
func (p *CachedPacket) frame(c *Conn) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.framed != nil && p.threshold == c.threshold {
		return p.framed, nil
	}
	f := frames.Get().(*frame)
	defer frames.Put(f)
	packet, err := c.framePacket(f, p.id, p.data, false)
	if err != nil {
		return nil, err
	}
	p.framed, p.threshold = slices.Clone(packet), c.threshold
	return p.framed, nil
}

// WriteCachedPacket writes a shared packet using this connection's compression
// and encryption settings.
func (c *Conn) WriteCachedPacket(p *CachedPacket) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	packet, err := p.frame(c)
	if err != nil {
		return err
	}
	if c.encrypter != nil {
		// The cached frame is shared; only a private copy may be encrypted.
		f := frames.Get().(*frame)
		defer frames.Put(f)
		f.out.Reset()
		f.out.PutRaw(packet)
		packet = f.out.Bytes()
	}
	if err := c.writeFrames(packet); err != nil {
		return err
	}
	c.traceOut(p.id, len(p.data))
	return nil
}
