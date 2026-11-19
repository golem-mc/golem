package terrain

import "testing"

func TestContextRecycle(t *testing.T) {
	// Not NewContext: a free list borrowed from the shared pool may already
	// hold a smaller buffer that fits, which acquire would pick instead.
	c := &Context{caches: []cacheCell{}}
	b := c.acquire(64)
	c.release(b)
	if again := c.acquire(32); &again[:1][0] != &b[:1][0] {
		t.Fatal("acquire did not reuse a released buffer that fits")
	}
	c.recycle()
	if c.scratch != nil || c.caches != nil {
		t.Fatal("recycle kept the context's buffers")
	}
	// A recycled context still hands out buffers; it allocates them.
	if got := c.acquire(16); len(got) != 16 {
		t.Fatalf("acquire after recycle: len %d, want 16", len(got))
	}
}
