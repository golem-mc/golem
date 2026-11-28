package command

import (
	"slices"
	"strconv"

	"github.com/golem-mc/golem/nbt"
)

// The path's reads and writes. The reads walk the tags its elements
// name; the writes go through places, which hold each tag together with
// the way to write it back where it lives — the Go value types need the
// write-back when a list changes length, where the vanilla node writes
// through an object it holds a reference to.

// maxNesting is the depth the game allows an NBT tag; a write past it,
// counting a node per path element below the value, is refused.
// vanilla: NbtPathArgument.ERROR_DATA_TOO_DEEP
const maxNesting = 512

// nbtPlace is where one tag the path names sits: the tag and the way to
// put a changed one back where it lives. The root sits in no place, so
// its write is nil.
type nbtPlace struct {
	get nbt.Tag
	set func(nbt.Tag)
}

// hasRootMatch reports a leading {...} filter, which tests the whole root.
func (p nbtPath) hasRootMatch() bool {
	if len(p.nodes) == 0 {
		return false
	}
	_, ok := p.nodes[0].(matchRootObjectNode)
	return ok
}

// isKeyChain reports whether every node names a compound child, the paths
// whose set changes nothing when it refuses.
func (p nbtPath) isKeyChain() bool {
	for _, n := range p.nodes {
		if _, ok := n.(compoundChildNode); !ok {
			return false
		}
	}
	return len(p.nodes) > 0
}

// rootKey is the only root entry this path can edit, if it names one.
// setScalarInKeyChain is set for an isKeyChain path and a scalar value, which
// the stored tag needs no copy of: it creates the missing parents as
// compounds and reports whether the entry changed.
func (p nbtPath) setScalarInKeyChain(root nbt.CompoundTag, value nbt.Tag) bool {
	if nbtTooDeep(value, len(p.nodes)) {
		return false
	}
	c := root
	for _, node := range p.nodes[:len(p.nodes)-1] {
		name := node.(compoundChildNode).name
		child, ok := c.Entries()[name]
		if !ok {
			child = &nbt.OrderedCompound{}
			c.Set(name, child)
		}
		if c, ok = child.(nbt.CompoundTag); !ok {
			return false
		}
	}
	name := p.nodes[len(p.nodes)-1].(compoundChildNode).name
	if existing, ok := c.Entries()[name]; ok && nbt.Equal(existing, value) {
		return false
	}
	c.Set(name, value)
	return true
}

func (p nbtPath) rootKey() (string, bool) {
	nodes := p.nodes
	if len(nodes) > 0 {
		if _, ok := nodes[0].(matchRootObjectNode); ok {
			nodes = nodes[1:]
		}
	}
	if len(nodes) > 0 {
		switch node := nodes[0].(type) {
		case compoundChildNode:
			return node.name, true
		case matchObjectNode:
			return node.name, true
		}
	}
	return "", false
}

// replace writes the tag back to its place, so a growth or the shrink
// of a list the caller worked on reaches the place the list lives in.
func (p nbtPlace) replace(tag nbt.Tag) {
	if p.set != nil {
		p.set(tag)
	}
}

// get answers the tags the path names in root; a step that names nothing
// is the vanilla not-found refusal, spelled with the path's own text up
// to where the failing element ended.
// vanilla: NbtPathArgument$NbtPath.get
func (p nbtPath) get(root nbt.Tag) ([]nbt.Tag, *pathError) {
	current := []nbt.Tag{root}
	for i, node := range p.nodes {
		var next []nbt.Tag
		for _, tag := range current {
			next = node.getTag(tag, next)
		}
		if len(next) == 0 {
			return nil, pathRefusal(pathNothingFoundKey, p.original[:p.elementEnd(i)])
		}
		current = next
	}
	return current, nil
}

// countMatching answers how many tags the path names, none being none
// rather than a refusal, which is what the /execute data test asks by.
// vanilla: NbtPathArgument$NbtPath.countMatching
func (p nbtPath) countMatching(root nbt.Tag) int {
	current := []nbt.Tag{root}
	for _, node := range p.nodes {
		var next []nbt.Tag
		for _, tag := range current {
			next = node.getTag(tag, next)
		}
		if len(next) == 0 {
			return 0
		}
		current = next
	}
	return len(current)
}

// elementEnd is where the element at index ended in the text the path
// was typed as, which is how far its refusal spells the path with.
func (p nbtPath) elementEnd(index int) int {
	if index < len(p.ends) {
		return p.ends[index]
	}
	return len(p.original)
}

// placesCreated walks the places the path's elements name, each missing
// one created as the tag the next element prefers — the last one as the
// default given — the shape the vanilla get-or-create composes, turned
// into places so the writes that follow write back.
// vanilla: NbtPathArgument$NbtPath.getOrCreate
func (p nbtPath) placesCreated(root nbt.Tag, last func() nbt.Tag) ([]nbtPlace, *pathError) {
	current := []nbtPlace{{get: root}}
	for i, node := range p.nodes {
		var preferred func() nbt.Tag
		if i+1 < len(p.nodes) {
			preferred = p.nodes[i+1].preferredParent
		} else {
			preferred = last
		}
		var next []nbtPlace
		for _, place := range current {
			next = node.places(place, preferred, next)
		}
		if len(next) == 0 {
			return nil, pathRefusal(pathNothingFoundKey, p.original[:p.elementEnd(i)])
		}
		current = next
	}
	return current, nil
}

// placesFound walks the places the path's elements name, creating
// nothing: the parents a removal walks, which names only what is there.
func (p nbtPath) placesFound(root nbt.Tag) []nbtPlace {
	if len(p.nodes) == 0 {
		return []nbtPlace{{get: root}}
	}
	current := []nbtPlace{{get: root}}
	for _, node := range p.nodes[:len(p.nodes)-1] {
		var next []nbtPlace
		for _, place := range current {
			next = node.found(place, next)
		}
		current = next
	}
	return current
}

// set puts value at every place the path names, the missing parents
// along it created, and answers how many of the places changed. The
// first write takes a copy of the value and the rest take copies of that copy.
// vanilla: NbtPathArgument$NbtPath.set
func (p nbtPath) set(root nbt.Tag, value nbt.Tag) (int, *pathError) {
	if nbtTooDeep(value, len(p.nodes)) {
		return 0, pathRefusal(pathTooDeepKey)
	}
	if len(p.nodes) == 0 {
		return 0, nil
	}
	firstCopy := nbt.OrderedCopy(value)
	current := []nbtPlace{{get: root}}
	for i, node := range p.nodes[:len(p.nodes)-1] {
		var next []nbtPlace
		for _, place := range current {
			next = node.places(place, p.nodes[i+1].preferredParent, next)
		}
		if len(next) == 0 {
			return 0, pathRefusal(pathNothingFoundKey, p.original[:p.elementEnd(i)])
		}
		current = next
	}
	changed, usedFirst := 0, false
	nextValue := func() nbt.Tag {
		if !usedFirst {
			usedFirst = true
			return firstCopy
		}
		return nbt.OrderedCopy(firstCopy)
	}
	last := p.nodes[len(p.nodes)-1]
	for _, place := range current {
		changed += last.setTagAt(place, nextValue)
	}
	return changed, nil
}

// insert puts values into each collection the path names at index (negative
// from the end, -1 appending), answering how many changed. A created place
// is an empty list; a typed array grows the same, skipping non-numbers.
// vanilla: NbtPathArgument$NbtPath.insert
func (p nbtPath) insert(index int, root nbt.Tag, values []nbt.Tag) (int, *pathError) {
	ins := make([]nbt.Tag, len(values))
	for i, value := range values {
		if nbtTooDeep(value, len(p.nodes)) {
			return 0, pathRefusal(pathTooDeepKey)
		}
		ins[i] = nbt.OrderedCopy(value)
	}
	places, err := p.placesCreated(root, func() nbt.Tag { return nbt.List{} })
	if err != nil {
		return 0, err
	}
	changed, shared := 0, false
	for _, place := range places {
		if list, ok := place.get.(nbt.List); ok {
			pos := index
			if index < 0 {
				pos = len(list.Items) + index + 1
			}
			moved := false
			for _, value := range ins {
				v := value
				if shared {
					v = nbt.OrderedCopy(value)
				}
				if pos < 0 || pos > len(list.Items) {
					return 0, pathRefusal(dataInvalidIndexKey, strconv.Itoa(pos))
				}
				insertListTag(&list, pos, v)
				pos++
				moved = true
			}
			shared = true
			place.replace(list)
			if moved {
				changed++
			}
			continue
		}
		if elements, kind, isArray := arrayElements(place.get); isArray {
			pos := index
			if index < 0 {
				pos = len(elements) + index + 1
			}
			moved := false
			for _, value := range ins {
				narrowed, ok := arrayNarrow(kind, value)
				if !ok {
					continue
				}
				if pos < 0 || pos > len(elements) {
					return 0, pathRefusal(dataInvalidIndexKey, strconv.Itoa(pos))
				}
				elements = slices.Insert(elements, pos, narrowed)
				pos++
				moved = true
			}
			if moved {
				if tag, ok := arrayOfKind(kind, elements); ok {
					place.replace(tag)
					changed++
				}
			}
			continue
		}
		return 0, pathRefusal(dataExpectedListKey, nbt.Stringify(place.get))
	}
	return changed, nil
}

// insertListTag puts value at the position of the list given, keeping a
// mixed list's entries unwrapped as the SNBT reader does.
// vanilla: ListTag.addTag
func insertListTag(list *nbt.List, pos int, value nbt.Tag) {
	whole := nbt.ListOf(slices.Insert(list.Items, pos, value))
	list.Elem, list.Items = whole.Elem, whole.Items
}

// remove takes the places the path names out and answers how many went;
// a path that names nothing removes none, which the caller reports as its
// own no-change refusal.
// vanilla: NbtPathArgument$NbtPath.remove
func (p nbtPath) remove(root nbt.Tag) int {
	if len(p.nodes) == 0 {
		return 0
	}
	removed := 0
	last := p.nodes[len(p.nodes)-1]
	for _, place := range p.placesFound(root) {
		removed += last.removeAt(place)
	}
	return removed
}

// nbtTooDeep reports whether a tag below the depth given would nest the
// whole write past the depth the game allows.
// vanilla: NbtPathArgument$NbtPath.isTooDeep
func nbtTooDeep(tag nbt.Tag, depth int) bool {
	if depth >= maxNesting {
		return true
	}
	switch v := tag.(type) {
	case nbt.CompoundTag:
		for _, child := range v.Entries() {
			if nbtTooDeep(child, depth+1) {
				return true
			}
		}
	case nbt.List:
		for _, child := range v.Items {
			if nbtTooDeep(child, depth+1) {
				return true
			}
		}
	}
	return false
}

// nbtPathNode is one element of a path: the tags it names, the places
// they write back through, what a value set at them changes and what
// taking them out removes. The lists and the typed arrays read as the
// collections the game counts them among; because a Go slice is a value,
// the array's write-back rebuilds it where its container lives.
type nbtPathNode interface {
	// getTag appends the tags the node names in tag to results.
	getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag
	// places appends the places of the tags the node names in the parent,
	// creating them where nothing is as the preferred answers.
	places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace
	// found is places creating nothing: the tags that are there only.
	found(parent nbtPlace, out []nbtPlace) []nbtPlace
	// preferredParent is what a missing parent of the node's place is
	// created as.
	preferredParent() nbt.Tag
	// setTagAt puts value at the node's place in the parent, answering
	// whether anything changed.
	setTagAt(parent nbtPlace, value func() nbt.Tag) int
	// removeAt takes the node's place out of the parent, answering
	// whether anything was there.
	removeAt(parent nbtPlace) int
}

// compoundChildNode is a compound's entry, by its name.
type compoundChildNode struct{ name string }

func (n compoundChildNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	if c, ok := tag.(nbt.CompoundTag); ok {
		if child, ok := c.Entries()[n.name]; ok {
			return append(results, child)
		}
	}
	return results
}

func (n compoundChildNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return out
	}
	child, ok := c.Entries()[n.name]
	if !ok {
		child = preferred()
		c.Set(n.name, child)
	}
	return append(out, nbtPlace{get: child, set: func(t nbt.Tag) { c.Set(n.name, t) }})
}

func (n compoundChildNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return out
	}
	if child, ok := c.Entries()[n.name]; ok {
		out = append(out, nbtPlace{get: child, set: func(t nbt.Tag) { c.Set(n.name, t) }})
	}
	return out
}

func (compoundChildNode) preferredParent() nbt.Tag { return &nbt.OrderedCompound{} }

func (n compoundChildNode) setTagAt(parent nbtPlace, value func() nbt.Tag) int {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return 0
	}
	next := value()
	if existing, ok := c.Entries()[n.name]; ok && nbt.Equal(existing, next) {
		return 0
	}
	c.Set(n.name, next)
	return 1
}

func (n compoundChildNode) removeAt(parent nbtPlace) int {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return 0
	}
	if _, ok := c.Entries()[n.name]; !ok {
		return 0
	}
	c.Delete(n.name)
	return 1
}

// indexedElementNode is one element of a collection by its index; a
// negative index counts from the end.
type indexedElementNode struct{ index int }

// resolveIndex is where the index falls in a collection of n elements,
// reporting whether an element lies there.
func (n indexedElementNode) resolveIndex(size int) (int, bool) {
	index := n.index
	if index < 0 {
		index += size
	}
	return index, index >= 0 && index < size
}

func (n indexedElementNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	elements, ok := collectionElements(tag)
	if !ok {
		return results
	}
	if index, ok := n.resolveIndex(len(elements)); ok {
		return append(results, elements[index])
	}
	return results
}

// arrayElementSetter is the write of one element of a typed array: the
// value narrowed to the array's kind, the whole array rebuilt and put back
// where it lives. A value no number narrows leaves the array alone.
func arrayElementSetter(parent nbtPlace, kind nbt.Type, elements []nbt.Tag, index int) func(nbt.Tag) {
	return func(t nbt.Tag) {
		narrowed, ok := arrayNarrow(kind, t)
		if !ok {
			return
		}
		replaced := slices.Clone(elements)
		replaced[index] = narrowed
		if tag, ok := arrayOfKind(kind, replaced); ok {
			parent.replace(tag)
		}
	}
}

func (n indexedElementNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	// An index never creates an element, so the preferred goes unread.
	return n.found(parent, out)
}

func (n indexedElementNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	if list, ok := parent.get.(nbt.List); ok {
		if index, in := n.resolveIndex(len(list.Items)); in {
			items := list.Items
			out = append(out, nbtPlace{get: items[index], set: func(t nbt.Tag) { items[index] = t }})
		}
		return out
	}
	// A typed array keeps its numbers; an element written through one is
	// narrowed and the whole array put back where it lives.
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		if index, in := n.resolveIndex(len(elements)); in {
			out = append(out, nbtPlace{get: elements[index], set: arrayElementSetter(parent, kind, elements, index)})
		}
	}
	return out
}

func (indexedElementNode) preferredParent() nbt.Tag { return nbt.List{} }

func (n indexedElementNode) setTagAt(parent nbtPlace, value func() nbt.Tag) int {
	if list, ok := parent.get.(nbt.List); ok {
		index, in := n.resolveIndex(len(list.Items))
		if !in {
			return 0
		}
		next := value()
		if nbt.Equal(list.Items[index], next) {
			return 0
		}
		list.Items[index] = next
		parent.replace(list)
		return 1
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		index, in := n.resolveIndex(len(elements))
		if !in || nbt.Equal(elements[index], value()) {
			return 0
		}
		narrowed, ok := arrayNarrow(kind, value())
		if !ok {
			return 0
		}
		replaced := slices.Clone(elements)
		replaced[index] = narrowed
		if tag, ok := arrayOfKind(kind, replaced); ok {
			parent.replace(tag)
			return 1
		}
	}
	return 0
}

func (n indexedElementNode) removeAt(parent nbtPlace) int {
	if list, ok := parent.get.(nbt.List); ok {
		index, in := n.resolveIndex(len(list.Items))
		if !in {
			return 0
		}
		list.Items = slices.Delete(list.Items, index, index+1)
		parent.replace(list)
		return 1
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		index, in := n.resolveIndex(len(elements))
		if !in {
			return 0
		}
		if tag, ok := arrayOfKind(kind, slices.Delete(slices.Clone(elements), index, index+1)); ok {
			parent.replace(tag)
			return 1
		}
	}
	return 0
}

// allElementsNode is every element of a collection.
type allElementsNode struct{}

func (allElementsNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	elements, ok := collectionElements(tag)
	if !ok {
		return results
	}
	return append(results, elements...)
}

func (allElementsNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	if list, ok := parent.get.(nbt.List); ok {
		if len(list.Items) == 0 {
			// The empty list keeps one element of the kind the next element
			// prefers, the create the vanilla get-or-else stands in.
			created := preferred()
			insertListTag(&list, 0, created)
			parent.replace(list)
			return append(out, nbtPlace{get: created, set: func(t nbt.Tag) {
				list.Items[0] = t
				parent.replace(list)
			}})
		}
		items := list.Items
		for i := range items {
			index := i
			out = append(out, nbtPlace{get: items[index], set: func(t nbt.Tag) { items[index] = t }})
		}
		return out
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		if len(elements) == 0 {
			narrowed, ok := arrayNarrow(kind, preferred())
			if !ok {
				return out
			}
			if tag, ok := arrayOfKind(kind, []nbt.Tag{narrowed}); ok {
				parent.replace(tag)
			}
			return append(out, nbtPlace{get: narrowed, set: func(t nbt.Tag) {
				next, ok := arrayNarrow(kind, t)
				if !ok {
					return
				}
				if tag, ok := arrayOfKind(kind, []nbt.Tag{next}); ok {
					parent.replace(tag)
				}
			}})
		}
		for i := range elements {
			index := i
			out = append(out, nbtPlace{get: elements[index], set: arrayElementSetter(parent, kind, elements, index)})
		}
	}
	return out
}

func (allElementsNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	if list, ok := parent.get.(nbt.List); ok {
		items := list.Items
		for i := range items {
			index := i
			out = append(out, nbtPlace{get: items[index], set: func(t nbt.Tag) { items[index] = t }})
		}
		return out
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		for i := range elements {
			index := i
			out = append(out, nbtPlace{get: elements[index], set: arrayElementSetter(parent, kind, elements, index)})
		}
	}
	return out
}

func (allElementsNode) preferredParent() nbt.Tag { return nbt.List{} }

func (allElementsNode) setTagAt(parent nbtPlace, value func() nbt.Tag) int {
	if list, ok := parent.get.(nbt.List); ok {
		if len(list.Items) == 0 {
			insertListTag(&list, 0, value())
			parent.replace(list)
			return 1
		}
		// The one value replaces every element; a list already holding it
		// everywhere answers nothing changed.
		v := value()
		changed := len(list.Items)
		for _, element := range list.Items {
			if nbt.Equal(element, v) {
				changed--
			}
		}
		if changed == 0 {
			return 0
		}
		list.Items[0] = v
		for i := 1; i < len(list.Items); i++ {
			list.Items[i] = value()
		}
		list.Elem = v.Type()
		parent.replace(list)
		return changed
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray {
		if len(elements) == 0 {
			// Vanilla adds the one element and counts the write whatever
			// the add answers.
			if narrowed, ok := arrayNarrow(kind, value()); ok {
				if tag, ok := arrayOfKind(kind, []nbt.Tag{narrowed}); ok {
					parent.replace(tag)
				}
			}
			return 1
		}
		v := value()
		changed := len(elements)
		for _, element := range elements {
			if nbt.Equal(element, v) {
				changed--
			}
		}
		if changed == 0 {
			return 0
		}
		narrowed, ok := arrayNarrow(kind, v)
		if !ok {
			// An element no number reads clears the array, which the throw
			// discards with the rest of the uncommitted write.
			return 0
		}
		replaced := make([]nbt.Tag, len(elements))
		for i := range replaced {
			replaced[i] = narrowed
		}
		if tag, ok := arrayOfKind(kind, replaced); ok {
			parent.replace(tag)
			return changed
		}
	}
	return 0
}

func (allElementsNode) removeAt(parent nbtPlace) int {
	if list, ok := parent.get.(nbt.List); ok {
		if len(list.Items) == 0 {
			return 0
		}
		removed := len(list.Items)
		list.Items, list.Elem = nil, nbt.TypeEnd
		parent.replace(list)
		return removed
	}
	if elements, kind, isArray := arrayElements(parent.get); isArray && len(elements) > 0 {
		if tag, ok := arrayOfKind(kind, nil); ok {
			parent.replace(tag)
			return len(elements)
		}
	}
	return 0
}

// matchElementNode is the elements of a list that carry a compound's
// entries.
type matchElementNode struct{ pattern nbt.CompoundTag }

func (n matchElementNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	elements, ok := collectionElements(tag)
	if !ok {
		return results
	}
	for _, element := range elements {
		if tagsMatchPattern(n.pattern, element) {
			results = append(results, element)
		}
	}
	return results
}

func (n matchElementNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	// vanilla: NbtPathArgument.MatchElementNode.getOrCreateTag
	found := n.found(parent, out)
	if len(found) > len(out) {
		return found
	}
	list, ok := parent.get.(nbt.List)
	if !ok {
		return out
	}
	created := nbt.OrderedCopy(n.pattern)
	index := len(list.Items)
	insertListTag(&list, index, created)
	parent.replace(list)
	return append(out, nbtPlace{get: created, set: func(t nbt.Tag) {
		list.Items[index] = t
		parent.replace(list)
	}})
}

func (n matchElementNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	list, ok := parent.get.(nbt.List)
	if !ok {
		return out
	}
	items := list.Items
	for i := range items {
		index := i
		if !tagsMatchPattern(n.pattern, items[index]) {
			continue
		}
		out = append(out, nbtPlace{get: items[index], set: func(t nbt.Tag) { items[index] = t }})
	}
	return out
}

func (matchElementNode) preferredParent() nbt.Tag { return nbt.List{} }

func (n matchElementNode) setTagAt(parent nbtPlace, value func() nbt.Tag) int {
	list, ok := parent.get.(nbt.List)
	if !ok {
		return 0
	}
	if len(list.Items) == 0 {
		insertListTag(&list, 0, value())
		parent.replace(list)
		return 1
	}
	changed := 0
	for i, element := range list.Items {
		if !tagsMatchPattern(n.pattern, element) {
			continue
		}
		next := value()
		if nbt.Equal(element, next) {
			continue
		}
		list.Items[i] = next
		changed++
	}
	if changed > 0 {
		parent.replace(list)
	}
	return changed
}

func (n matchElementNode) removeAt(parent nbtPlace) int {
	list, ok := parent.get.(nbt.List)
	if !ok {
		return 0
	}
	removed := 0
	for i, v := range slices.Backward(list.Items) {
		if !tagsMatchPattern(n.pattern, v) {
			continue
		}
		list.Items = slices.Delete(list.Items, i, i+1)
		removed++
	}
	if removed > 0 {
		parent.replace(list)
	}
	return removed
}

// matchObjectNode is a compound's entry that carries a compound's
// entries, by its name.
type matchObjectNode struct {
	name    string
	pattern nbt.CompoundTag
}

func (n matchObjectNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	c, ok := tag.(nbt.CompoundTag)
	if !ok {
		return results
	}
	if child, ok := c.Entries()[n.name]; ok && tagsMatchPattern(n.pattern, child) {
		return append(results, child)
	}
	return results
}

func (n matchObjectNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return out
	}
	child, ok := c.Entries()[n.name]
	if !ok {
		child = nbt.OrderedCopy(n.pattern)
		c.Set(n.name, child)
	} else if !tagsMatchPattern(n.pattern, child) {
		return out
	}
	return append(out, nbtPlace{get: child, set: func(t nbt.Tag) { c.Set(n.name, t) }})
}

func (n matchObjectNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return out
	}
	if child, ok := c.Entries()[n.name]; ok && tagsMatchPattern(n.pattern, child) {
		out = append(out, nbtPlace{get: child, set: func(t nbt.Tag) { c.Set(n.name, t) }})
	}
	return out
}

func (matchObjectNode) preferredParent() nbt.Tag { return &nbt.OrderedCompound{} }

func (n matchObjectNode) setTagAt(parent nbtPlace, value func() nbt.Tag) int {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return 0
	}
	if child, ok := c.Entries()[n.name]; ok && tagsMatchPattern(n.pattern, child) {
		next := value()
		if nbt.Equal(child, next) {
			return 0
		}
		c.Set(n.name, next)
		return 1
	}
	return 0
}

func (n matchObjectNode) removeAt(parent nbtPlace) int {
	c, ok := parent.get.(nbt.CompoundTag)
	if !ok {
		return 0
	}
	if child, ok := c.Entries()[n.name]; ok && tagsMatchPattern(n.pattern, child) {
		c.Delete(n.name)
		return 1
	}
	return 0
}

// matchRootObjectNode is the whole root, when it carries a compound's
// entries; it reads the root alone and writes nothing.
// vanilla: NbtPathArgument$MatchRootObjectNode
type matchRootObjectNode struct{ pattern nbt.CompoundTag }

func (n matchRootObjectNode) getTag(tag nbt.Tag, results []nbt.Tag) []nbt.Tag {
	if tagsMatchPattern(n.pattern, tag) {
		return append(results, tag)
	}
	return results
}

func (n matchRootObjectNode) places(parent nbtPlace, preferred func() nbt.Tag, out []nbtPlace) []nbtPlace {
	return n.found(parent, out)
}

func (n matchRootObjectNode) found(parent nbtPlace, out []nbtPlace) []nbtPlace {
	if tagsMatchPattern(n.pattern, parent.get) {
		out = append(out, parent)
	}
	return out
}

func (matchRootObjectNode) preferredParent() nbt.Tag { return &nbt.OrderedCompound{} }

func (matchRootObjectNode) setTagAt(nbtPlace, func() nbt.Tag) int { return 0 }

func (matchRootObjectNode) removeAt(nbtPlace) int { return 0 }
