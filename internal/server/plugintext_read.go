package server

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// pluginText converts components representable by the API; false refuses
// contents such as selectors rather than silently flattening their behavior.
func pluginText(t protocol.Text) (plugin.Text, bool) {
	return pluginTextOf(protocol.TextTag(t))
}

// vanilla: ComponentSerialization.CODEC, Style.Serializer.MAP_CODEC
func pluginTextOf(tag nbt.Tag) (plugin.Text, bool) {
	if s, ok := tag.(nbt.String); ok {
		return plugin.Plain(string(s)), true
	}
	if list, ok := tag.(nbt.List); ok {
		if len(list.Items) == 0 {
			return plugin.Text{}, false
		}
		head, ok := pluginTextOf(nbt.ListElement(list.Items[0]))
		if !ok {
			return plugin.Text{}, false
		}
		for _, item := range list.Items[1:] {
			child, ok := pluginTextOf(nbt.ListElement(item))
			if !ok {
				return plugin.Text{}, false
			}
			head.Children = append(head.Children, child)
		}
		return head, true
	}
	c, ok := nbt.CompoundView(tag)
	if !ok {
		return plugin.Text{}, false
	}
	var t plugin.Text
	switch {
	case c["text"] != nil:
		t.Literal, ok = pluginString(c["text"])
	case c["translate"] != nil:
		t.Translate, ok = pluginString(c["translate"])
	case c["keybind"] != nil:
		t.Keybind, ok = pluginString(c["keybind"])
	default:
		return plugin.Text{}, false
	}
	if !ok {
		return plugin.Text{}, false
	}
	for key, dest := range map[string]*string{"fallback": &t.Fallback, "color": &t.Style.Color, "insertion": &t.Style.Insertion} {
		if value, present := c[key]; present {
			*dest, ok = pluginString(value)
			if !ok {
				return plugin.Text{}, false
			}
		}
	}
	for key, dest := range map[string]*[]plugin.Text{"with": &t.Args, "extra": &t.Children} {
		if value, present := c[key]; present {
			list, yes := value.(nbt.List)
			if !yes {
				return plugin.Text{}, false
			}
			for _, item := range list.Items {
				child, yes := pluginTextOf(nbt.ListElement(item))
				if !yes {
					return plugin.Text{}, false
				}
				*dest = append(*dest, child)
			}
		}
	}
	for key, dest := range map[string]**bool{"bold": &t.Style.Bold, "italic": &t.Style.Italic, "underlined": &t.Style.Underlined, "strikethrough": &t.Style.Strikethrough, "obfuscated": &t.Style.Obfuscated} {
		if value, present := c[key]; present {
			if _, yes := nbt.NumberValue(value); !yes {
				return plugin.Text{}, false
			}
			*dest = new(protocol.FlagOn(value))
		}
	}
	if value, present := c["font"]; present {
		name, yes := pluginString(value)
		if !yes {
			return plugin.Text{}, false
		}
		key, err := plugin.ParseKey(name)
		if err != nil {
			return plugin.Text{}, false
		}
		t.Style.Font = key
	}
	if value, present := c["shadow_color"]; present {
		color, yes := nbt.IntValue(value)
		if !yes {
			return plugin.Text{}, false
		}
		t.Style.Shadow = new(uint32(color))
	}
	if value, present := c["click_event"]; present {
		event, yes := nbt.CompoundView(value)
		if !yes {
			return plugin.Text{}, false
		}
		action, _ := pluginString(event["action"])
		found := false
		for i, field := range pluginClickFields {
			if action != field.action {
				continue
			}
			click := &plugin.Click{Action: plugin.ClickAction(i)}
			if click.Action == plugin.ChangePage {
				page, yes := nbt.IntValue(event[field.key])
				if !yes || page < 1 {
					return plugin.Text{}, false
				}
				click.Value = strconv.FormatInt(int64(page), 10)
			} else {
				click.Value, ok = pluginString(event[field.key])
				if !ok {
					return plugin.Text{}, false
				}
			}
			if payload, present := event["payload"]; present {
				var err error
				click.Payload, err = nbt.EncodeNetwork(payload)
				if err != nil {
					return plugin.Text{}, false
				}
			}
			t.Style.Click, found = click, true
			break
		}
		if !found {
			return plugin.Text{}, false
		}
	}
	if value, present := c["hover_event"]; present {
		event, yes := nbt.CompoundView(value)
		if !yes {
			return plugin.Text{}, false
		}
		t.Style.Hover = &plugin.Hover{}
		switch event["action"] {
		case nbt.String("show_text"):
			child, yes := pluginTextOf(event["value"])
			if !yes {
				return plugin.Text{}, false
			}
			t.Style.Hover.Text = &child
		case nbt.String("show_entity"):
			id, _ := pluginString(event["id"])
			kind, yes := registry.EntityTypeByName(id)
			identity, valid := pluginUUID(event, "uuid")
			if !yes || !valid {
				return plugin.Text{}, false
			}
			entity := &plugin.HoverEntity{Type: kind, UUID: identity}
			if name, present := event["name"]; present {
				text, yes := pluginTextOf(name)
				if !yes {
					return plugin.Text{}, false
				}
				entity.Name = &text
			}
			t.Style.Hover.Entity = entity
		case nbt.String("show_item"):
			id, _ := pluginString(event["id"])
			kind, yes := registry.ItemByName(id)
			if !yes {
				return plugin.Text{}, false
			}
			stack := &plugin.ItemStack{Item: kind, Count: 1}
			if count, present := event["count"]; present {
				n, yes := nbt.IntValue(count)
				if !yes {
					return plugin.Text{}, false
				}
				stack.Count = int(n)
			}
			if value, present := event["components"]; present {
				components, yes := nbt.CompoundView(value)
				if !yes {
					return plugin.Text{}, false
				}
				stack.Patch, yes = pluginItemPatchOf(components)
				if !yes {
					return plugin.Text{}, false
				}
			}
			t.Style.Hover.Item = stack
		default:
			return plugin.Text{}, false
		}
	}
	return t, true
}

func pluginString(tag nbt.Tag) (string, bool) {
	s, ok := tag.(nbt.String)
	return string(s), ok
}

func pluginUUID(c nbt.Compound, key string) (plugin.UUID, bool) {
	if s, ok := pluginString(c[key]); ok {
		id, err := uuid.Parse(s)
		return id, err == nil
	}
	return nbt.UUIDField(c, key)
}
