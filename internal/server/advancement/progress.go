package advancement

import (
	"slices"
	"sync"
	"time"
)

// Progress is one player's advancements: criteria obtained, what the screen
// shows, and changes since the last Flush. Safe for concurrent use: the tick,
// the connection's handlers and a save all reach it.
// vanilla: PlayerAdvancements
type Progress struct {
	mu   sync.Mutex
	tree *Tree
	// obtained holds, by node index, each criterion's time; a nil map is a
	// node with nothing obtained.
	obtained []map[string]time.Time
	visible  []bool
	// changed is the nodes whose progress the next Flush carries, if shown.
	changed map[*Node]struct{}
	// rootsToUpdate is the trees whose visibility the next Flush
	// re-evaluates.
	rootsToUpdate map[*Node]struct{}
	// first is whether the next Update Advancements resets the client's
	// tree; it is cleared by the first Flush, sent or not.
	first bool
	// selectedTab is the root of the tab the client was last told to show.
	selectedTab *Node
	// unknown keeps the saved entries of advancements the tree lacks.
	unknown map[string][]byte
	// completed is the nodes that became done since the last TakeCompleted,
	// which the server rewards and announces in chat.
	completed []*Node
	// held counts open Hold calls; the tick's FlushTick sends nothing while
	// it is above zero.
	held int
	// veto, if set, is asked before a criterion is obtained and refuses it
	// by returning true. It runs under mu and must not call back into p.
	veto func(n *Node, criterion string) bool
}

// SetVeto installs the callback that may refuse a criterion before it is
// obtained; it runs with p's lock held, so it must not reach p.
func (p *Progress) SetVeto(veto func(n *Node, criterion string) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.veto = veto
}

// NewProgress returns a player's progress with nothing obtained.
func NewProgress(tree *Tree) *Progress {
	return &Progress{
		tree:          tree,
		obtained:      make([]map[string]time.Time, len(tree.nodes)),
		visible:       make([]bool, len(tree.nodes)),
		changed:       map[*Node]struct{}{},
		rootsToUpdate: map[*Node]struct{}{},
		first:         true,
	}
}

// isDone reports whether every requirements group of n has a criterion
// obtained; no groups is never done.
// vanilla: AdvancementRequirements.test
func (p *Progress) isDone(n *Node) bool {
	if len(n.Requirements) == 0 {
		return false
	}
	got := p.obtained[n.index]
	for _, group := range n.Requirements {
		found := false
		for _, name := range group {
			if _, ok := got[name]; ok {
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

// hasCriterion reports whether name is one of n's requirement criteria.
func hasCriterion(n *Node, name string) bool {
	for _, group := range n.Requirements {
		if slices.Contains(group, name) {
			return true
		}
	}
	return false
}

// Award obtains one criterion of n at the given time, reporting whether it
// was newly obtained. vanilla: PlayerAdvancements.award
func (p *Progress) Award(n *Node, criterion string, at time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.award(n, criterion, at)
}

func (p *Progress) award(n *Node, criterion string, at time.Time) bool {
	if !hasCriterion(n, criterion) {
		return false
	}
	got := p.obtained[n.index]
	if _, ok := got[criterion]; ok {
		return false
	}
	if p.veto != nil && p.veto(n, criterion) {
		return false
	}
	wasDone := p.isDone(n)
	if got == nil {
		got = map[string]time.Time{}
		p.obtained[n.index] = got
	}
	got[criterion] = at
	p.changed[n] = struct{}{}
	if !wasDone && p.isDone(n) {
		p.rootsToUpdate[n.Root()] = struct{}{}
		p.completed = append(p.completed, n)
	}
	return true
}

// Revoke takes one criterion of n back, reporting whether it was obtained.
// vanilla: PlayerAdvancements.revoke
func (p *Progress) Revoke(n *Node, criterion string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	got := p.obtained[n.index]
	if _, ok := got[criterion]; !ok {
		return false
	}
	wasDone := p.isDone(n)
	delete(got, criterion)
	p.changed[n] = struct{}{}
	if wasDone && !p.isDone(n) {
		p.rootsToUpdate[n.Root()] = struct{}{}
	}
	return true
}

// SetAwarded makes exactly the named criteria of n obtained, at the given time
// for the new ones. It queues no completion and asks no veto, so nothing is
// announced or rewarded; unknown names are ignored.
func (p *Progress) SetAwarded(n *Node, criteria []string, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(criteria) == 0 && len(p.obtained[n.index]) == 0 {
		return
	}
	wasDone := p.isDone(n)
	want := map[string]time.Time{}
	for _, c := range criteria {
		if !hasCriterion(n, c) {
			continue
		}
		t, ok := p.obtained[n.index][c]
		if !ok {
			t = at
		}
		want[c] = t
	}
	if len(want) == len(p.obtained[n.index]) {
		same := true
		for c := range want {
			if _, ok := p.obtained[n.index][c]; !ok {
				same = false
			}
		}
		if same {
			return
		}
	}
	if len(want) == 0 {
		p.obtained[n.index] = nil
	} else {
		p.obtained[n.index] = want
	}
	p.changed[n] = struct{}{}
	if wasDone != p.isDone(n) {
		p.rootsToUpdate[n.Root()] = struct{}{}
	}
}

// TakeCompleted returns the nodes done since the last call, in order.
// vanilla: PlayerAdvancements.award
func (p *Progress) TakeCompleted() []*Node {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.completed
	p.completed = nil
	return out
}

// Obtained reports whether one criterion of n is obtained.
func (p *Progress) Obtained(n *Node, criterion string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.obtained[n.index][criterion]
	return ok
}

// Remaining returns n's criteria not yet obtained, in requirement order.
// vanilla: AdvancementProgress.getRemainingCriteria
func (p *Progress) Remaining(n *Node) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, group := range n.Requirements {
		for _, name := range group {
			if _, ok := p.obtained[n.index][name]; !ok {
				out = append(out, name)
			}
		}
	}
	return out
}

// HasProgress reports whether any criterion of n is obtained.
// vanilla: AdvancementProgress.hasProgress
func (p *Progress) HasProgress(n *Node) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.obtained[n.index]) > 0
}

// Completed returns n's obtained criteria in requirement order.
// vanilla: AdvancementProgress.getCompletedCriteria
func (p *Progress) Completed(n *Node) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, group := range n.Requirements {
		for _, name := range group {
			if _, ok := p.obtained[n.index][name]; ok {
				out = append(out, name)
			}
		}
	}
	return out
}

// Done reports whether the advancement is done.
func (p *Progress) Done(n *Node) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isDone(n)
}

// CriterionState is one criterion of an advancement's progress; a zero
// Obtained is one not obtained.
type CriterionState struct {
	Name     string
	Obtained time.Time
}

// NodeProgress is one advancement's progress as the packet carries it:
// every requirement criterion, obtained or not.
type NodeProgress struct {
	Node     *Node
	Criteria []CriterionState
}

// Update is what one Update Advancements carries.
type Update struct {
	Reset bool
	// Show is the packet's show advancements flag: false suppresses the
	// toasts of what it completes.
	Show     bool
	Added    []*Node
	Removed  []string
	Progress []NodeProgress
}

// Sends reports whether the update has anything for the packet.
func (u Update) Sends() bool {
	return len(u.Added) > 0 || len(u.Removed) > 0 || len(u.Progress) > 0
}

// Flush returns what changed since the last call, and whether there is
// anything to send. vanilla: PlayerAdvancements.flushDirty
func (p *Progress) Flush() (Update, bool) { return p.FlushShow(true) }

// FlushShow is Flush with the packet's show advancements flag given.
func (p *Progress) FlushShow(show bool) (Update, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.flushLocked(show)
}

// Hold stops FlushTick sending until release, so a command on another
// goroutine sends its changes with its own show flag, not the tick's.
func (p *Progress) Hold() (release func()) {
	p.mu.Lock()
	p.held++
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.held--
		p.mu.Unlock()
	}
}

// FlushTick is Flush for the tick: nothing is sent, and the changes stay
// pending, while the progress is held.
func (p *Progress) FlushTick() (Update, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held > 0 {
		return Update{}, false
	}
	return p.flushLocked(true)
}

func (p *Progress) flushLocked(show bool) (Update, bool) {
	if !p.first && len(p.rootsToUpdate) == 0 && len(p.changed) == 0 {
		return Update{}, false
	}
	u := Update{Reset: p.first, Show: show}
	p.first = false
	// The roots in load order, each tree children first: the client's
	// AdvancementTree.addAll takes the list in any order.
	for _, root := range p.tree.roots {
		if _, ok := p.rootsToUpdate[root]; !ok {
			continue
		}
		p.evaluateVisibility(root, func(n *Node, show bool) {
			switch {
			case show && !p.visible[n.index]:
				p.visible[n.index] = true
				u.Added = append(u.Added, n)
				p.changed[n] = struct{}{}
			case !show && p.visible[n.index]:
				p.visible[n.index] = false
				u.Removed = append(u.Removed, n.ID)
			}
		})
	}
	clear(p.rootsToUpdate)
	for _, n := range p.tree.nodes {
		if _, ok := p.changed[n]; !ok || !p.visible[n.index] {
			continue
		}
		np := NodeProgress{Node: n}
		for _, group := range n.Requirements {
			for _, name := range group {
				np.Criteria = append(np.Criteria, CriterionState{Name: name, Obtained: p.obtained[n.index][name]})
			}
		}
		u.Progress = append(u.Progress, np)
	}
	clear(p.changed)
	return u, u.Sends()
}

// SelectTab records the client's open tab (any of its advancements) and
// returns the tab's root when it changed; a node that is not a displayed
// root clears it. ok is false when nothing changed.
// vanilla: PlayerAdvancements.setSelectedTab
func (p *Progress) SelectTab(n *Node) (tab *Node, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n != nil && (n.ParentNode != nil || n.Display == nil) {
		n = nil
	}
	if n == p.selectedTab {
		return nil, false
	}
	p.selectedTab = n
	return n, true
}

// The visibility rules of one node for its descendants.
// vanilla: AdvancementVisibilityEvaluator.VisibilityRule
type visibilityRule int8

const (
	ruleNoChange visibilityRule = iota
	ruleShow
	ruleHide
)

// visibilityDepth is how many ancestors' rules an unfinished node looks
// through. vanilla: AdvancementVisibilityEvaluator.VISIBILITY_DEPTH
const visibilityDepth = 2

func (p *Progress) ruleOf(n *Node, done bool) visibilityRule {
	switch {
	case n.Display == nil:
		return ruleHide
	case done:
		return ruleShow
	case n.Display.Hidden:
		return ruleHide
	}
	return ruleNoChange
}

// evaluateVisibility tells out whether each node of root's tree shows: when
// it or a descendant is done, or when the nearest rule within visibilityDepth
// of its ancestors (itself first) says show.
// vanilla: AdvancementVisibilityEvaluator.evaluateVisibility
func (p *Progress) evaluateVisibility(root *Node, out func(n *Node, show bool)) {
	stack := make([]visibilityRule, visibilityDepth+1, 16)
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		done := p.isDone(n)
		stack = append(stack, p.ruleOf(n, done))
		doneBelow := done
		for _, child := range n.Children {
			if walk(child) {
				doneBelow = true
			}
		}
		show := doneBelow
		if !show {
			for i := 0; i <= visibilityDepth; i++ {
				if r := stack[len(stack)-1-i]; r != ruleNoChange {
					show = r == ruleShow
					break
				}
			}
		}
		stack = stack[:len(stack)-1]
		out(n, show)
		return doneBelow
	}
	walk(root)
}
