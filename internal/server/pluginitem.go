package server

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginProtocolStack decodes a plugin value through the shared component codec.
func pluginProtocolStack(stack plugin.ItemStack) (protocol.ItemStack, bool) {
	if stack.Empty() {
		return protocol.ItemStack{}, true
	}
	if _, ok := registry.ItemName(int32(stack.Item)); !ok || int(int32(stack.Count)) != stack.Count {
		return protocol.ItemStack{}, false
	}
	for _, value := range stack.Patch.Other {
		if value != "" {
			if _, _, err := nbt.ParseValueTag(value); err != nil {
				return protocol.ItemStack{}, false
			}
		}
	}
	components, err := pluginValueTag(pluginItemComponents(stack.Patch))
	if err != nil {
		return protocol.ItemStack{}, false
	}
	// An oversized count clamps to the codec's 99 instead of reading as 1.
	// vanilla: ItemStack.MAX_STACK_SIZE
	return registry.StoredStack(nbt.Compound{
		"id": nbt.String(stack.Item.Name()), "count": nbt.Int(min(stack.Count, 99)), "components": components,
	})
}

// pluginStack copies a stack through the component codec, retaining unmodelled patch fields as SNBT.
func pluginStack(stack protocol.ItemStack) plugin.ItemStack {
	if stack.Empty() {
		return plugin.ItemStack{}
	}
	patch, _ := pluginItemPatchOf(registry.StackComponents(stack))
	return plugin.ItemStack{Item: registry.Item(stack.ItemID), Count: int(stack.Count), Patch: patch}
}

// pluginItems answers plugin.Items with the server's item rules.
type pluginItems struct {
	mu   sync.Mutex
	rand *rules.JavaRandom
}

func (h *pluginHost) Items() plugin.Items { return h.srv.pluginItemRules() }

func (s *Server) pluginItemRules() *pluginItems {
	s.pluginItemsOnce.Do(func() {
		s.pluginItemsVal = &pluginItems{rand: rules.NewJavaRandom(time.Now().UnixNano())}
	})
	return s.pluginItemsVal
}

func (*pluginItems) Parse(text string, count int) (plugin.ItemStack, error) {
	stack, ok := command.ParseItemStack(strings.TrimSpace(text))
	if !ok {
		return plugin.ItemStack{}, fmt.Errorf("invalid item %q", text)
	}
	stack.Count = int32(min(max(count, 0), math.MaxInt32))
	return pluginStack(stack), nil
}

func (*pluginItems) Format(s plugin.ItemStack) string {
	stack, ok := pluginProtocolStack(s)
	if !ok || stack.Empty() {
		return "minecraft:air"
	}
	name := s.Item.Name()
	components := registry.StackComponents(stack)
	keys := make([]string, 0, len(components))
	for key := range components {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(name)
	for i, key := range keys {
		if i == 0 {
			b.WriteByte('[')
		} else {
			b.WriteByte(',')
		}
		if strings.HasPrefix(key, "!") {
			b.WriteString(key)
			continue
		}
		b.WriteString(key + "=" + nbt.Stringify(components[key]))
	}
	if len(keys) > 0 {
		b.WriteByte(']')
	}
	return b.String()
}

func (*pluginItems) Check(s plugin.ItemStack) error {
	if s.Empty() {
		return nil
	}
	stack, ok := pluginProtocolStack(s)
	if !ok || !registry.StackComponentsValid(stack) {
		return fmt.Errorf("invalid item stack %s", s.Item.Name())
	}
	return nil
}

func (*pluginItems) MaxStackSize(s plugin.ItemStack) int {
	stack, ok := pluginProtocolStack(s)
	if !ok || stack.Empty() {
		return 0
	}
	return int(registry.StackMaxStackSize(stack))
}

func (*pluginItems) Similar(a, b plugin.ItemStack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() && b.Empty()
	}
	x, okA := pluginProtocolStack(a)
	y, okB := pluginProtocolStack(b)
	return okA && okB && x.Same(y)
}

func (*pluginItems) Name(s plugin.ItemStack) plugin.Text {
	stack, ok := pluginProtocolStack(s)
	if !ok {
		return plugin.Text{}
	}
	text, _ := pluginText(rules.ItemText(stack, 1))
	return text
}

func (p *pluginItems) Enchant(s plugin.ItemStack, level int, treasure bool) plugin.ItemStack {
	stack, ok := pluginProtocolStack(s)
	if !ok || stack.Empty() {
		return s
	}
	candidates := registry.EnchantingTableIDs()
	if treasure {
		candidates = registry.EnchantmentTagIDs("minecraft:on_random_loot")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return pluginStack(rules.EnchantWithLevels(p.rand, stack, level, candidates))
}

var _ plugin.Items = (*pluginItems)(nil)
