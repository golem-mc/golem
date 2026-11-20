package pluginruntime

import (
	"errors"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/plugin"
)

type enablePlugin struct {
	enable  func() error
	disable func()
}

func (p enablePlugin) Enable(plugin.Server) error {
	if p.enable == nil {
		return nil
	}
	return p.enable()
}
func (p enablePlugin) Disable() {
	if p.disable != nil {
		p.disable()
	}
}

// Every entry of pluginHookTypes has a mask bit.
func TestListenerMaskCoversEveryHook(t *testing.T) {
	var b pluginBus
	b.listeners = map[reflect.Type][]*pluginListener{}
	for _, typ := range pluginHookTypes {
		b.listeners[typ] = []*pluginListener{{}}
		b.updateMask(typ)
	}
	for i := range pluginHookTypes {
		if !b.has(i) {
			t.Fatalf("hook %d (%v) has no mask bit", i, pluginHookTypes[i])
		}
	}
}

// An Enable that errors while a release panics leaves the plugin disabled and
// returns.
func TestEnableErrorWithPanickingCleanup(t *testing.T) {
	var host *Host
	r := New([]plugin.Registration{{Name: "p", Plugin: enablePlugin{enable: func() error {
		host.Own(func() { panic("release") })
		return errors.New("enable failed")
	}}}}, func(h *Host) plugin.Server { host = h; return nil })
	r.Load()
	r.Enable()
	if host.enabled.Load() {
		t.Fatal("plugin stayed enabled")
	}
	if !host.closed.Load() {
		t.Fatal("plugin was not cleaned up")
	}
}

// Calling Disable more than once runs each plugin's Disable once and does not
// panic on the second call.
func TestDisableIsIdempotent(t *testing.T) {
	var calls int
	r := New([]plugin.Registration{{Name: "p", Plugin: enablePlugin{disable: func() { calls++ }}}}, func(*Host) plugin.Server { return nil })
	r.Load()
	r.Enable()
	r.Disable()
	r.Disable()
	if calls != 1 {
		t.Fatalf("Disable called %d times, want 1", calls)
	}
}
