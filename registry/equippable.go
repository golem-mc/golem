package registry

import "strings"

// AllowsEntity reports whether an entity type may wear the item.
// vanilla: Equippable.canBeEquippedBy
func (e EquippableItem) AllowsEntity(entityType string) bool {
	if e.Entities == "" {
		return true
	}
	if tag, isTag := strings.CutPrefix(e.Entities, "#"); isTag {
		id, ok := EntityTypeID(entityType)
		return ok && EntityTypeInTag(id, tag)
	}
	for allowed := range strings.SplitSeq(e.Entities, ",") {
		if allowed == entityType {
			return true
		}
	}
	return false
}
