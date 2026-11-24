package world

import (
	"container/list"
	"errors"
	"fmt"
	"log/slog"
)

// errWorldClosed is returned by region lookups after Close: a late save or load
// fails instead of touching released caches, and the chunk stays dirty.
var errWorldClosed = errors.New("world is closed")

// maxRegionCache is how many region files one store keeps open; past it the
// least recently used closes.
// vanilla: RegionFileStorage.MAX_CACHE_SIZE
const maxRegionCache = 256

// regionCache is a bounded LRU set of open region files, guarded by the world's
// regionMu.
type regionCache struct {
	entries map[[2]int32]*list.Element
	lru     *list.List // front is the most recently used
	open    func(rx, rz int32) (*regionFile, error)
	// refs counts each file's holders; evict leaves a held file open so it is
	// not closed under a running read or write, and a later evict closes it
	// once free.
	refs map[*regionFile]int
}

// regionEntry is one cached file and the key it was opened under.
type regionEntry struct {
	key [2]int32
	rf  *regionFile
}

// newRegionCache returns a cache whose misses are opened by open.
func newRegionCache(open func(rx, rz int32) (*regionFile, error)) *regionCache {
	return &regionCache{
		entries: make(map[[2]int32]*list.Element),
		lru:     list.New(),
		open:    open,
		refs:    make(map[*regionFile]int),
	}
}

// get returns the region file for (rx, rz), opening it on demand and evicting
// the least recently used at the cap. The caller holds it until release. A nil
// cache (after Close) returns errWorldClosed.
func (c *regionCache) get(rx, rz int32) (*regionFile, error) {
	return c.getWith(rx, rz, c.open)
}

// getWith is get that opens a miss with open instead of the cache's opener.
func (c *regionCache) getWith(rx, rz int32, open func(rx, rz int32) (*regionFile, error)) (*regionFile, error) {
	if c == nil {
		return nil, errWorldClosed
	}
	key := [2]int32{rx, rz}
	if el, ok := c.entries[key]; ok {
		c.lru.MoveToFront(el)
		rf := el.Value.(*regionEntry).rf
		c.refs[rf]++
		return rf, nil
	}
	rf, err := open(rx, rz)
	if err != nil {
		return nil, err
	}
	c.entries[key] = c.lru.PushFront(&regionEntry{key: key, rf: rf})
	c.refs[rf] = 1
	c.evict()
	return rf, nil
}

// release drops one hold and trims the cache, closing a file that was in use
// when the cache overflowed.
func (c *regionCache) release(rf *regionFile) {
	if c == nil {
		return
	}
	if n := c.refs[rf] - 1; n > 0 {
		c.refs[rf] = n
	} else {
		delete(c.refs, rf)
	}
	c.evict()
}

// evict closes least recently used files down to the cap, leaving open a file
// still held or one with an unreadable chunk, whose flag must outlive the
// handle so the damaged chunk is not overwritten after a reopen.
func (c *regionCache) evict() {
	for el := c.lru.Back(); c.lru.Len() > maxRegionCache && el != nil; {
		prev := el.Prev()
		entry := el.Value.(*regionEntry)
		if c.refs[entry.rf] == 0 && !entry.rf.hasUnreadable() {
			c.lru.Remove(el)
			delete(c.entries, entry.key)
			// vanilla throws from the save that evicted; the save itself
			// already landed, so the loss is logged rather than failing it.
			if err := entry.rf.close(); err != nil {
				slog.Error("closing region file", "path", entry.rf.path, "err", err)
			}
		}
		el = prev
	}
}

// close closes every file and empties the cache, returning every file's sync or
// close error joined; a nil cache is a no-op.
// vanilla: RegionFileStorage.close
func (c *regionCache) close() error {
	if c == nil {
		return nil
	}
	var errs []error
	for el := c.lru.Front(); el != nil; el = el.Next() {
		rf := el.Value.(*regionEntry).rf
		if err := rf.close(); err != nil {
			errs = append(errs, fmt.Errorf("closing region file %s: %w", rf.path, err))
		}
	}
	c.entries = make(map[[2]int32]*list.Element)
	c.lru.Init()
	c.refs = make(map[*regionFile]int)
	return errors.Join(errs...)
}

// len is how many files the cache holds open.
func (c *regionCache) len() int {
	if c == nil {
		return 0
	}
	return c.lru.Len()
}
