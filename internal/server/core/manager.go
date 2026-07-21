package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/tools"
)

// ResolveFunc resolves a (provider, model) pair — either may be empty for the
// configured default — into a ready llm.Provider/Model. Wired to
// config.ResolveWithAuth in serve mode; replaceable in tests.
type ResolveFunc func(providerName, modelID string) (llm.Provider, llm.Model, error)

// Options configures a Manager.
type Options struct {
	// Resolve maps provider/model names to live provider instances. Required.
	Resolve ResolveFunc
	// DefaultCwd is used for sessions created without an explicit cwd.
	DefaultCwd string
	// CompactionSettings for all sessions; zero value disables auto-compaction.
	CompactionSettings compaction.Settings
}

// Manager owns the set of live server sessions. All methods are safe for
// concurrent use by multiple transport connections.
type Manager struct {
	ctx  context.Context
	opts Options

	mu       sync.Mutex
	sessions map[string]*ServerSession
}

// NewManager builds a Manager. ctx bounds every run; cancel it on server
// shutdown, then call Shutdown to close session files.
func NewManager(ctx context.Context, opts Options) *Manager {
	return &Manager{ctx: ctx, opts: opts, sessions: map[string]*ServerSession{}}
}

// CreateParams are the options for Create.
type CreateParams struct {
	Cwd      string
	Provider string
	Model    string
}

// Create starts a fresh persisted session owned by connID.
func (m *Manager) Create(connID string, p CreateParams) (*ServerSession, error) {
	cwd := p.Cwd
	if cwd == "" {
		cwd = m.opts.DefaultCwd
	}
	provider, model, err := m.opts.Resolve(p.Provider, p.Model)
	if err != nil {
		return nil, err
	}
	sess, err := session.Create(cwd)
	if err != nil {
		return nil, err
	}
	ss := m.wrap(sess, provider, model, cwd)
	if err := ss.claim(connID); err != nil { // cannot fail on a fresh session
		ss.close()
		return nil, err
	}
	m.mu.Lock()
	m.sessions[ss.ID()] = ss
	m.mu.Unlock()
	return ss, nil
}

// Resume returns the live session with the given id, or opens it from disk.
// The session is claimed for connID; a session owned by another live
// connection returns ErrNotOwner.
func (m *Manager) Resume(connID, sessionID string) (*ServerSession, error) {
	m.mu.Lock()
	if ss, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		if err := ss.claim(connID); err != nil {
			return nil, err
		}
		return ss, nil
	}
	m.mu.Unlock()

	sess, err := session.ResumeByID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	provider, model, rerr := m.opts.Resolve("", "")
	if rerr != nil {
		_ = sess.Close()
		return nil, rerr
	}
	cwd := sess.Cwd()
	if cwd == "" {
		cwd = m.opts.DefaultCwd
	}
	ss := m.wrap(sess, provider, model, cwd)

	m.mu.Lock()
	// Another connection may have opened the same session concurrently; keep
	// the first registration and discard ours.
	if existing, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		ss.close()
		if err := existing.claim(connID); err != nil {
			return nil, err
		}
		return existing, nil
	}
	m.sessions[sessionID] = ss
	m.mu.Unlock()

	if err := ss.claim(connID); err != nil {
		return nil, err
	}
	return ss, nil
}

// wrap builds the ServerSession over an open session file.
func (m *Manager) wrap(sess *session.Session, provider llm.Provider, model llm.Model, cwd string) *ServerSession {
	registry := tools.DefaultRegistry(cwd)
	cfg := agent.Config{
		Provider:           provider,
		Model:              model,
		Registry:           registry,
		SystemPrompt:       agent.BuildSystemPrompt(registry, cwd),
		CompactionSettings: m.opts.CompactionSettings,
	}
	return newServerSession(m.ctx, sess, cfg, model.Provider+"/"+model.ID, cwd)
}

// Get returns the live session with the given id if connID may act on it.
func (m *Manager) Get(connID, sessionID string) (*ServerSession, error) {
	m.mu.Lock()
	ss, ok := m.sessions[sessionID]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	if err := ss.claim(connID); err != nil {
		return nil, err
	}
	return ss, nil
}

// SetModel resolves and applies a model switch on an owned session.
func (m *Manager) SetModel(connID, sessionID, providerName, modelID string) error {
	ss, err := m.Get(connID, sessionID)
	if err != nil {
		return err
	}
	provider, model, err := m.opts.Resolve(providerName, modelID)
	if err != nil {
		return err
	}
	return ss.SetModel(provider, model)
}

// Close removes the session from the manager and closes its file. The JSONL
// file remains on disk and can be resumed later.
func (m *Manager) Close(connID, sessionID string) error {
	ss, err := m.Get(connID, sessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	ss.close()
	return nil
}

// List returns metadata for all persisted sessions, newest first.
func (m *Manager) List() ([]session.Info, error) { return session.List() }

// ReleaseOwner is called when a transport connection closes: every session
// owned by connID has its active run aborted and its ownership cleared, but
// stays registered so the client can Resume after reconnecting.
func (m *Manager) ReleaseOwner(connID string) {
	m.mu.Lock()
	owned := make([]*ServerSession, 0, len(m.sessions))
	for _, ss := range m.sessions {
		owned = append(owned, ss)
	}
	m.mu.Unlock()
	for _, ss := range owned {
		ss.release(connID)
	}
}

// Shutdown aborts all runs and closes every session file. Call after
// canceling the manager context.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	all := make([]*ServerSession, 0, len(m.sessions))
	for _, ss := range m.sessions {
		all = append(all, ss)
	}
	m.sessions = map[string]*ServerSession{}
	m.mu.Unlock()
	for _, ss := range all {
		ss.close()
	}
}
