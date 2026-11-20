package pluginruntime

import (
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/golem-mc/golem/plugin"
)

type pluginPermissions struct{ host *Host }
type ownedPermission struct {
	host       *Host
	permission plugin.Permission
}
type permissionParent struct {
	name  string
	grant bool
}

// permissionIndex is immutable once published, so lookups hold no lock during callbacks.
type permissionIndex struct {
	names   map[string]plugin.Permission
	parents map[string][]permissionParent
}

type permissionRegistry struct {
	mu      sync.Mutex
	entries []ownedPermission
	index   *permissionIndex
}

func clonePermission(p plugin.Permission) plugin.Permission {
	p.Children = maps.Clone(p.Children)
	return p
}

func (p *pluginPermissions) Register(permission plugin.Permission) {
	r := &p.host.runtime.permissions
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.host.closed.Load() {
		return
	}
	r.entries = slices.DeleteFunc(r.entries, func(e ownedPermission) bool {
		return e.host == p.host && e.permission.Name == permission.Name
	})
	r.entries = append(r.entries, ownedPermission{p.host, clonePermission(permission)})
	r.reindex()
}

func (p *pluginPermissions) Get(name string) (plugin.Permission, bool) {
	r := &p.host.runtime.permissions
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.index != nil {
		if permission, ok := r.index.names[name]; ok {
			return clonePermission(permission), true
		}
	}
	return plugin.Permission{}, false
}

func (p *pluginPermissions) remove() {
	r := &p.host.runtime.permissions
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = slices.DeleteFunc(r.entries, func(e ownedPermission) bool { return e.host == p.host })
	r.reindex()
}

// reindex runs under mu, keeping the last registration's precedence for each node.
func (r *permissionRegistry) reindex() {
	index := &permissionIndex{names: make(map[string]plugin.Permission), parents: make(map[string][]permissionParent)}
	for _, e := range slices.Backward(r.entries) {
		p := e.permission
		if _, exists := index.names[p.Name]; exists {
			continue
		}
		index.names[p.Name] = p
		for child, grant := range p.Children {
			index.parents[child] = append(index.parents[child], permissionParent{p.Name, grant})
		}
	}
	r.index = index
}

// AttachmentSource is implemented by a CommandSender that carries per-player
// transient permission attachments.
type AttachmentSource interface {
	AttachedPermission(node string) (value, set bool)
}

// HasPermission consults the provider outside runtime locks, then attachments,
// then defaults and children.
func (r *Runtime) HasPermission(sender plugin.CommandSender, node string) bool {
	if r == nil {
		return sender.OpLevel() > 0
	}
	var provider plugin.PermissionProvider
	if v, ok := r.lookupService(reflect.TypeFor[plugin.PermissionProvider]()); ok {
		provider = v.(plugin.PermissionProvider)
		if allowed, decided := provider.Has(sender, node); decided {
			return allowed
		}
	}
	attachments, _ := sender.(AttachmentSource)
	if attachments != nil {
		if allowed, set := attachments.AttachedPermission(node); set {
			return allowed
		}
	}
	r.permissions.mu.Lock()
	index := r.permissions.index
	r.permissions.mu.Unlock()
	memo := make(map[string]bool)
	visiting := make(map[string]bool)
	var has func(string) bool
	has = func(name string) (allowed bool) {
		if value, ok := memo[name]; ok {
			return value
		}
		if visiting[name] {
			return false
		}
		visiting[name] = true
		defer func() { delete(visiting, name); memo[name] = allowed }()
		// The requested node's provider and attachments were consulted before taking the snapshot.
		if name != node {
			if provider != nil {
				if value, decided := provider.Has(sender, name); decided {
					return value
				}
			}
			if attachments != nil {
				if value, set := attachments.AttachedPermission(name); set {
					return value
				}
			}
		}
		if index != nil {
			for _, parent := range index.parents[name] {
				if has(parent.name) {
					return parent.grant
				}
			}
			if p, ok := index.names[name]; ok {
				return sender.OpLevel() >= p.Default.OpLevel
			}
		}
		return sender.OpLevel() > 0
	}
	return has(node)
}

// PermissionSet answers whether a provider or attachment decided the node for sender.
func (r *Runtime) PermissionSet(sender plugin.CommandSender, node string) bool {
	if r == nil {
		return false
	}
	if v, ok := r.lookupService(reflect.TypeFor[plugin.PermissionProvider]()); ok {
		if _, decided := v.(plugin.PermissionProvider).Has(sender, node); decided {
			return true
		}
	}
	if attachments, ok := sender.(AttachmentSource); ok {
		if _, set := attachments.AttachedPermission(node); set {
			return true
		}
	}
	return false
}

// Permissions answers this plugin's permission registrations.
func (h *Host) Permissions() plugin.Permissions { return &h.permissions }
