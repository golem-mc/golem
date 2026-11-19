package protocol

import "strings"

// CompostableComponent is a stack's compostable layers: a constant or a
// context_int_provider key. An empty Provider selects Layers.
// vanilla: Compostable, ResolvableInt
type CompostableComponent struct {
	Layers   int32
	Provider string
}

func readCompostable(r *Reader) (CompostableComponent, bool) {
	var c CompostableComponent
	if r.ReadBool() {
		c.Layers = r.ReadI32()
	} else {
		c.Provider = r.ReadString(32767)
		if !ValidIdentifier(c.Provider) {
			return c, false
		}
		if !strings.ContainsRune(c.Provider, ':') {
			c.Provider = "minecraft:" + c.Provider
		}
	}
	return c, r.Err() == nil
}

// CompostableOf returns the stack's own compostable component.
func (s ItemStack) CompostableOf() (CompostableComponent, bool) {
	for _, raw := range s.Raw {
		if raw.Type == DataComponentCompostable {
			r := NewReader(raw.Data)
			c, ok := readCompostable(r)
			return c, ok && r.Remaining() == 0
		}
	}
	return CompostableComponent{}, false
}

// SetCompostable replaces the stack's compostable component.
func (s *ItemStack) SetCompostable(c CompostableComponent) {
	var b Buffer
	b.PutBool(c.Provider == "")
	if c.Provider == "" {
		b.PutI32(c.Layers)
	} else {
		b.PutString(c.Provider)
	}
	s.putRaw(RawComponent{Type: DataComponentCompostable, Data: b.Bytes()})
}
