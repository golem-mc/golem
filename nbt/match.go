package nbt

// Matches reports whether actual contains expected's compound entries and list
// elements, recursively; scalar and array tags must match exactly.
// vanilla: NbtUtils.compareNbt (partialListMatches=true)
func Matches(expected, actual Tag) bool {
	if expected == nil {
		return true
	}
	if actual == nil || expected.Type() != actual.Type() {
		return false
	}
	switch e := expected.(type) {
	case CompoundTag:
		a := actual.(CompoundTag)
		if len(a.Entries()) < len(e.Entries()) {
			return false
		}
		for name, child := range e.Entries() {
			if !Matches(child, a.Entries()[name]) {
				return false
			}
		}
		return true
	case List:
		a := actual.(List)
		if len(e.Items) == 0 {
			return len(a.Items) == 0
		}
		if len(a.Items) < len(e.Items) {
			return false
		}
		for _, item := range e.Items {
			found := false
			for _, value := range a.Items {
				if Matches(item, value) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return Equal(expected, actual)
}
