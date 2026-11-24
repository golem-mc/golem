package server

import (
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/server/world"
)

// viewerKey is a block container position; the same coordinates in another
// world are another container.
type viewerKey struct {
	w       *world.World
	x, y, z int32
}

// containerViewer is a client and the window it had open when booked.
type containerViewer struct {
	c      *client
	window *openWindow
}

// containerViewerIndex maps each container position to the windows showing
// it, both halves of a double chest included. Its lock is a leaf, taken under
// a client's container lock; readers copy out and recheck the window.
type containerViewerIndex struct {
	mu   sync.Mutex
	byAt map[viewerKey][]containerViewer
}

// move books c's window change from old to now; either may be nil.
func (i *containerViewerIndex) move(c *client, old, now *openWindow) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if old != nil && old.shows() {
		for _, h := range old.halves() {
			i.removeLocked(viewerKey{old.w, h[0], h[1], h[2]}, c)
		}
	}
	if now != nil && now.shows() {
		if i.byAt == nil {
			i.byAt = map[viewerKey][]containerViewer{}
		}
		for _, h := range now.halves() {
			k := viewerKey{now.w, h[0], h[1], h[2]}
			i.byAt[k] = append(i.byAt[k], containerViewer{c, now})
		}
	}
}

func (i *containerViewerIndex) removeLocked(k viewerKey, c *client) {
	list := i.byAt[k]
	for n, v := range list {
		if v.c == c {
			list = slices.Delete(list, n, n+1)
			break
		}
	}
	if len(list) == 0 {
		delete(i.byAt, k)
	} else {
		i.byAt[k] = list
	}
}

// at appends the windows showing the container at the position to dst.
func (i *containerViewerIndex) at(dst []containerViewer, w *world.World, x, y, z int32) []containerViewer {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append(dst, i.byAt[viewerKey{w, x, y, z}]...)
}
