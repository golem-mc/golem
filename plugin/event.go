package plugin

import "reflect"

// Event is an event a plugin can listen to: a pointer to one of this
// package's *Event structs. Only this package's types are events.
//
// Each event's doc comment says when it fires and on which goroutine
// (the tick goroutine unless it says otherwise). Fields a listener may
// change say so; the rest are for reading, and changing them does nothing.
// A cancellable event embeds Cancellable: cancelling it stops the action,
// and the server sends the client whatever undoes its prediction (the block
// back, the slot back), as vanilla does when it refuses the same action.
type Event interface{ event() }

// Base is embedded in every event.
type Base struct{}

func (Base) event() {}

// Cancellable is embedded in every event that can be cancelled.
// Bukkit: Cancellable
type Cancellable struct {
	Base
	cancelled bool
}

func (c *Cancellable) Cancelled() bool     { return c.cancelled }
func (c *Cancellable) SetCancelled(v bool) { c.cancelled = v }

// Priority orders a type's listeners: Lowest runs first, Monitor last.
// A Monitor listener only observes the outcome and must change nothing.
// Bukkit: EventPriority
type Priority int8

const (
	Lowest Priority = iota - 2
	Low
	Normal
	High
	Highest
	Monitor
)

// ListenOptions are a listener's priority and filter.
type ListenOptions struct {
	Priority Priority
	// IgnoreCancelled skips the listener for an event a listener before it
	// cancelled.
	IgnoreCancelled bool
}

// Listener is a registered listener.
type Listener interface {
	// Unregister removes it; it is removed anyway when the plugin is
	// disabled.
	Unregister()
}

// Listen registers fn for events of type E, at Normal priority unless opts
// say otherwise:
//
//	plugin.Listen(s, func(e *plugin.BlockBreakEvent) {
//		if e.Player.GameMode() != plugin.Creative { e.SetCancelled(true) }
//	})
//
// Bukkit: @EventHandler
func Listen[E any, P interface {
	*E
	Event
}](s Server, fn func(P), opts ...ListenOptions) Listener {
	o := ListenOptions{Priority: Normal}
	if len(opts) > 0 {
		o = opts[0]
	}
	return s.Events().Register(reflect.TypeFor[P](), func(e Event) { fn(e.(P)) }, o)
}

// DefineEvent makes E, a struct of the plugin's own that embeds Base or
// Cancellable and is named ...Event, an event type that plugins can fire
// (Events.Fire) and listen to (Listen):
//
//	plugin.DefineEvent[BalanceChangeEvent](s)
func DefineEvent[E any, P interface {
	*E
	Event
}](s Server) {
	s.Events().Define(reflect.TypeFor[P]())
}
