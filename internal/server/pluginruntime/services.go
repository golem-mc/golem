package pluginruntime

import (
	"github.com/golem-mc/golem/plugin"
	"reflect"
	"slices"
	"sync"
)

type pluginServices struct{ host *Host }
type pluginService struct {
	host     *Host
	impl     any
	priority int
}
type pluginServiceRegistry struct {
	mu        sync.Mutex
	providers map[reflect.Type][]pluginService
}

func (s *pluginServices) Provide(t reflect.Type, impl any, priority int) {
	if t == nil || t.Kind() != reflect.Interface || impl == nil || !reflect.TypeOf(impl).Implements(t) {
		panic("plugin: service must implement an interface")
	}
	r := &s.host.runtime.services
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.host.closed.Load() {
		return
	}
	if r.providers == nil {
		r.providers = make(map[reflect.Type][]pluginService)
	}
	r.providers[t] = append(r.providers[t], pluginService{s.host, impl, priority})
	slices.SortStableFunc(r.providers[t], func(a, b pluginService) int {
		if a.priority > b.priority {
			return -1
		}
		if a.priority < b.priority {
			return 1
		}
		return 0
	})
}
func (s *pluginServices) Lookup(t reflect.Type) (any, bool) {
	return s.host.runtime.lookupService(t)
}
func (runtime *Runtime) lookupService(t reflect.Type) (any, bool) {
	r := &runtime.services
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.providers[t]; len(p) > 0 {
		return p[0].impl, true
	}
	return nil, false
}
func (s *pluginServices) Providers(t reflect.Type) []plugin.ServiceProvider {
	r := &s.host.runtime.services
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]plugin.ServiceProvider, 0, len(r.providers[t]))
	for _, p := range r.providers[t] {
		out = append(out, plugin.ServiceProvider{Plugin: p.host.registration.Name, Priority: p.priority, Impl: p.impl})
	}
	return out
}
func (s *pluginServices) remove() {
	r := &s.host.runtime.services
	r.mu.Lock()
	defer r.mu.Unlock()
	for t, ps := range r.providers {
		r.providers[t] = slices.DeleteFunc(ps, func(p pluginService) bool { return p.host == s.host })
	}
}
