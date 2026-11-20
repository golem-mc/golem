package pluginruntime

import (
	"reflect"
	"testing"
)

type cloneInner struct{ N int }

type cloneSample struct {
	Slice []cloneInner
	Ptr   *cloneInner
	Map   map[string][]int
	Any   any
	Flat  [2]int
	hid   []int
}

// A snapshot shares no exported memory with its source.
func TestClonePluginValueIsDeep(t *testing.T) {
	orig := &cloneSample{
		Slice: []cloneInner{{1}, {2}},
		Ptr:   &cloneInner{3},
		Map:   map[string][]int{"a": {4}},
		Any:   []int{5},
	}
	c := clonePluginValue(reflect.ValueOf(orig).Elem()).Interface().(cloneSample)
	c.Slice[0].N = 9
	c.Ptr.N = 9
	c.Map["a"][0] = 9
	c.Map["b"] = nil
	c.Any.([]int)[0] = 9
	if orig.Slice[0].N != 1 || orig.Ptr.N != 3 || orig.Map["a"][0] != 4 || len(orig.Map) != 1 || orig.Any.([]int)[0] != 5 {
		t.Fatalf("original changed: %+v", orig)
	}
}
