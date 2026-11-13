package pads

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/helviojunior/nullops/pkg/log"
)

const (
	// MaxLen caps a pad at 1 Mi UTF-16 units.
	MaxLen = 1 << 20
	// historyKeep is how many operations are kept to transform late edits.
	historyKeep = 5000
	// PollWait is how long a poll waits for a change before returning empty.
	PollWait = 25 * time.Second
	// presenceTTL: an editor counts as present while it polled recently.
	presenceTTL = 40 * time.Second
	// flushEvery: dirty pads are encrypted and written to disk this often.
	flushEvery = 5 * time.Second
	// unloadAfter: idle pads leave memory after this long.
	unloadAfter = 10 * time.Minute
)

var (
	// ErrResync tells the client its revision is too old: reload the text.
	ErrResync = errors.New("resync")
	// ErrTooLarge is returned when an edit exceeds MaxLen.
	ErrTooLarge = errors.New("Pad is too large")
	// ErrBadRev is returned for a revision from the future.
	ErrBadRev = errors.New("Invalid revision")
)

type entry struct {
	op     *Operation
	client string
	seq    int
}

type presence struct {
	user string
	at   time.Time
}

type doc struct {
	mu       sync.Mutex
	meta     *Meta
	text     Text
	rev      int // revision of text
	base     int // revision before history[0]
	history  []entry
	changed  chan struct{}
	dirty    bool
	deleted  bool
	lastSeq  map[string]int
	seen     map[string]presence
	lastUsed time.Time
}

func (d *doc) notify() {
	close(d.changed)
	d.changed = make(chan struct{})
}

func (d *doc) editors(now time.Time) []string {
	set := map[string]bool{}
	for c, p := range d.seen {
		if now.Sub(p.at) > presenceTTL {
			delete(d.seen, c)
			continue
		}
		set[p.user] = true
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// Manager holds the pads being edited in memory and persists them.
type Manager struct {
	store *Store
	mu    sync.Mutex
	docs  map[string]*doc
}

// NewManager creates a manager on store.
func NewManager(store *Store) *Manager {
	return &Manager{store: store, docs: map[string]*doc{}}
}

func (m *Manager) load(name string) (*doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.docs[name]; ok {
		return d, nil
	}
	meta, err := m.store.Get(name)
	if err != nil {
		return nil, err
	}
	text, rev, err := m.store.LoadContent(meta)
	if err != nil {
		return nil, err
	}
	d := &doc{meta: meta, text: text, rev: rev, base: rev, changed: make(chan struct{}),
		lastSeq: map[string]int{}, seen: map[string]presence{}, lastUsed: time.Now()}
	m.docs[name] = d
	return d, nil
}

// Info is a pad in the list.
type Info struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
	CreatedBy string    `json:"created_by"`
	Size      int       `json:"size"`
	Editors   []string  `json:"editors"`
}

// List returns every pad, with live data for the ones in memory.
func (m *Manager) List() ([]Info, error) {
	metas, err := m.store.List()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]Info, 0, len(metas))
	for _, meta := range metas {
		in := Info{Name: meta.Name, UpdatedAt: meta.UpdatedAt, UpdatedBy: meta.UpdatedBy,
			CreatedBy: meta.CreatedBy, Size: meta.Size, Editors: []string{}}
		m.mu.Lock()
		d := m.docs[meta.Name]
		m.mu.Unlock()
		if d != nil {
			d.mu.Lock()
			in.UpdatedAt, in.UpdatedBy, in.Size = d.meta.UpdatedAt, d.meta.UpdatedBy, len(d.text)
			in.Editors = d.editors(now)
			d.mu.Unlock()
		}
		out = append(out, in)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Create makes a new pad.
func (m *Manager) Create(name, user string) error {
	_, err := m.store.Create(name, user)
	return err
}

// Delete removes a pad; editors waiting on it are woken up.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	d := m.docs[name]
	delete(m.docs, name)
	m.mu.Unlock()
	if d != nil {
		d.mu.Lock()
		d.deleted, d.dirty = true, false
		d.notify()
		d.mu.Unlock()
	}
	return m.store.Delete(name)
}

// Snapshot is the full state sent when a pad is opened or must be resynced.
type Snapshot struct {
	Text    string   `json:"text"`
	Rev     int      `json:"rev"`
	Editors []string `json:"editors"`
}

// Open returns the current text and revision.
func (m *Manager) Open(name, user, client string) (*Snapshot, error) {
	d, err := m.load(name)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	d.lastUsed = now
	if client != "" {
		d.seen[client] = presence{user: user, at: now}
	}
	return &Snapshot{Text: d.text.String(), Rev: d.rev, Editors: d.editors(now)}, nil
}

// Edit applies op, made by client on revision rev. Operations from the same
// client carry an increasing seq: a retried request already applied is
// accepted without being applied twice.
func (m *Manager) Edit(name, user, client string, seq, rev int, op *Operation) (int, error) {
	d, err := m.load(name)
	if err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.deleted {
		return 0, ErrNotFound
	}
	if seq <= d.lastSeq[client] {
		return d.rev, nil
	}
	if rev > d.rev {
		return 0, ErrBadRev
	}
	if rev < d.base {
		return 0, ErrResync
	}
	for _, e := range d.history[rev-d.base:] {
		if op, _, err = Transform(op, e.op); err != nil {
			return 0, err
		}
	}
	if op.TargetLen > MaxLen {
		return 0, ErrTooLarge
	}
	text, err := op.Apply(d.text)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	d.text = text
	d.rev++
	d.history = append(d.history, entry{op: op, client: client, seq: seq})
	if over := len(d.history) - historyKeep; over > 0 {
		d.history = append([]entry(nil), d.history[over:]...)
		d.base += over
	}
	d.lastSeq[client] = seq
	d.seen[client] = presence{user: user, at: now}
	d.meta.UpdatedAt, d.meta.UpdatedBy = now, user
	d.dirty, d.lastUsed = true, now
	d.notify()
	return d.rev, nil
}

// Change is one operation in a poll result.
type Change struct {
	Op     *Operation `json:"op"`
	Client string     `json:"client"`
	Seq    int        `json:"seq"`
}

// PollResult carries the operations after the client's revision, in order
// (including the client's own, which acknowledge its pending edit), or a
// full snapshot when the client is too far behind.
type PollResult struct {
	Rev      int       `json:"rev"`
	Changes  []Change  `json:"changes"`
	Editors  []string  `json:"editors"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
}

// Poll waits (up to wait) for operations newer than rev.
func (m *Manager) Poll(ctx context.Context, name, user, client string, rev int, wait time.Duration) (*PollResult, error) {
	d, err := m.load(name)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		d.mu.Lock()
		if d.deleted {
			d.mu.Unlock()
			return nil, ErrNotFound
		}
		now := time.Now()
		d.lastUsed = now
		d.seen[client] = presence{user: user, at: now}
		switch {
		case rev > d.rev:
			d.mu.Unlock()
			return nil, ErrBadRev
		case rev < d.base:
			snap := &Snapshot{Text: d.text.String(), Rev: d.rev, Editors: d.editors(now)}
			d.mu.Unlock()
			return &PollResult{Rev: d.rev, Snapshot: snap, Editors: snap.Editors, Changes: []Change{}}, nil
		case rev < d.rev:
			res := &PollResult{Rev: d.rev, Editors: d.editors(now)}
			for _, e := range d.history[rev-d.base:] {
				res.Changes = append(res.Changes, Change{Op: e.op, Client: e.client, Seq: e.seq})
			}
			d.mu.Unlock()
			return res, nil
		}
		ch := d.changed
		d.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			d.mu.Lock()
			res := &PollResult{Rev: d.rev, Changes: []Change{}, Editors: d.editors(time.Now())}
			d.mu.Unlock()
			return res, nil
		}
	}
}

// Flush writes every dirty pad to disk.
func (m *Manager) Flush() {
	m.mu.Lock()
	docs := make([]*doc, 0, len(m.docs))
	for _, d := range m.docs {
		docs = append(docs, d)
	}
	m.mu.Unlock()
	for _, d := range docs {
		d.mu.Lock()
		if d.dirty && !d.deleted {
			meta := *d.meta
			text := append(Text(nil), d.text...)
			rev := d.rev
			d.dirty = false
			d.mu.Unlock()
			if err := m.store.SaveContent(&meta, text, rev); err != nil {
				log.Error("pad save failed", "pad", meta.Name, "err", err)
				d.mu.Lock()
				d.dirty = true
				d.mu.Unlock()
			}
			continue
		}
		d.mu.Unlock()
	}
}

// Run persists dirty pads every few seconds and unloads idle ones, until ctx
// is cancelled; it then flushes one last time.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.Flush()
			return
		case <-t.C:
			m.Flush()
			m.unloadIdle()
		}
	}
}

func (m *Manager) unloadIdle() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, d := range m.docs {
		d.mu.Lock()
		if !d.dirty && now.Sub(d.lastUsed) > unloadAfter {
			delete(m.docs, name)
		}
		d.mu.Unlock()
	}
}
