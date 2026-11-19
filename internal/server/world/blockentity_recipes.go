package world

// recipeHistory retains the traversal used to populate the saved recipe map.
// Vanilla iterates a Reference2IntOpenHashMap, whose ties depend on JVM object
// identities; retain the load traversal instead of randomizing it on every save.
type recipeHistory struct {
	counts map[string]int
	keys   []string
}

// Len is the number of recipes owed experience.
func (h recipeHistory) Len() int { return len(h.counts) }

// Count is the number of smelts for a recipe.
func (h recipeHistory) Count(id string) int { return h.counts[id] }

// Counts returns a read-only view, valid while the entity's lock is held.
func (h recipeHistory) Counts() map[string]int { return h.counts }

func (h *recipeHistory) add(id string, count int) {
	if h.counts == nil {
		h.counts = make(map[string]int)
	}
	if _, exists := h.counts[id]; !exists {
		h.keys = append(h.keys, id)
	}
	h.counts[id] += count
}
