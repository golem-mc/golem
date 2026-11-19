package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// AddPostEffect adds id to the player's post effects and resends them; false
// when it was already there.
// vanilla: ServerPlayer.addPostEffect
func (c *client) AddPostEffect(id string) bool {
	c.postMu.Lock()
	if slices.Contains(c.postEffects, id) {
		c.postMu.Unlock()
		return false
	}
	c.postEffects = append(c.postEffects, id)
	c.postMu.Unlock()
	c.logPostEffects(c.sendPostEffects())
	return true
}

// RemovePostEffect removes id and resends the rest; false when it was not set.
// vanilla: ServerPlayer.removePostEffect
func (c *client) RemovePostEffect(id string) bool {
	c.postMu.Lock()
	i := slices.Index(c.postEffects, id)
	if i < 0 {
		c.postMu.Unlock()
		return false
	}
	c.postEffects = slices.Delete(c.postEffects, i, i+1)
	c.postMu.Unlock()
	c.logPostEffects(c.sendPostEffects())
	return true
}

// ClearPostEffects empties the set and resends it; false when it was empty.
// vanilla: ServerPlayer.clearPostEffects
func (c *client) ClearPostEffects() bool {
	if !c.clearPostEffects() {
		return false
	}
	c.logPostEffects(c.sendPostEffects())
	return true
}

func (c *client) clearPostEffects() bool {
	c.postMu.Lock()
	defer c.postMu.Unlock()
	had := len(c.postEffects) > 0
	c.postEffects = nil
	return had
}

// PostEffects returns a copy of the player's post effect ids.
func (c *client) PostEffects() []string {
	c.postMu.Lock()
	defer c.postMu.Unlock()
	return slices.Clone(c.postEffects)
}

// sendPostEffects sends the set. Vanilla sends it on every join and
// dimension change too; an empty set is skipped unless the client holds a
// non-empty one, since it has the same effect as no packet.
// vanilla: ServerPlayer.sendPostEffects
func (c *client) sendPostEffects() error {
	c.postMu.Lock()
	ids := slices.Clone(c.postEffects)
	if len(ids) == 0 && !c.postSent {
		c.postMu.Unlock()
		return nil
	}
	c.postSent = len(ids) > 0
	c.postMu.Unlock()
	return c.write(protocol.IDPlayPostEffects, "post effects", func(b *protocol.Buffer) {
		b.PutVarInt(int32(len(ids)))
		for _, id := range ids {
			b.PutString(id)
		}
	})
}

func (c *client) logPostEffects(err error) {
	if err != nil {
		c.log.Debug("sending post effects failed", "error", err)
	}
}

// loadPostEffects reads the saved set.
// vanilla: ServerPlayer.readAdditionalSaveData (post_effects)
func (c *client) loadPostEffects(root nbt.Compound) {
	list, ok := nbt.Get[nbt.List](root, "post_effects")
	if !ok {
		return
	}
	var ids []string
	for _, item := range list.Items {
		if s, ok := item.(nbt.String); ok && !slices.Contains(ids, string(s)) {
			ids = append(ids, string(s))
		}
	}
	c.postMu.Lock()
	c.postEffects = ids
	c.postMu.Unlock()
}

// storePostEffects writes the set into a save compound.
// vanilla: ServerPlayer.addAdditionalSaveData (post_effects)
func (c *client) storePostEffects(state nbt.Compound) {
	ids := c.PostEffects()
	items := make([]nbt.Tag, len(ids))
	for i, id := range ids {
		items[i] = nbt.String(id)
	}
	state["post_effects"] = nbt.List{Elem: nbt.TypeString, Items: items}
}
