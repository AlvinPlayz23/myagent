package core

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/agent/compaction"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/plugin"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/tools"
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
	// DefaultEffort is used for sessions created without an explicit effort.
	DefaultEffort llm.Effort
	// NoPlugins disables plugins.json loading.
	NoPlugins bool
	// DefaultProfile pins a profile for every session (fail-fast unknown).
	DefaultProfile string
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
	Effort   llm.Effort
	Profile  string
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
	effort := p.Effort
	if effort == "" {
		effort = m.opts.DefaultEffort
	}
	effort, err = llm.NormalizeEffort(model, effort)
	if err != nil {
		return nil, err
	}
	prof := p.Profile
	if prof == "" {
		prof = m.opts.DefaultProfile
	}
	if prof != "" {
		b := plugin.Load(cwd, m.opts.NoPlugins)
		if b.Profile(prof) == nil {
			return nil, fmt.Errorf("unknown profile %q (available: %s)", prof, strings.Join(b.ProfileNames(), ", "))
		}
	}
	sess, err := session.Create(cwd)
	if err != nil {
		return nil, err
	}
	ss, err := m.wrap(sess, provider, model, cwd, effort, p.Profile)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
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
	effort, rerr := llm.NormalizeEffort(model, m.opts.DefaultEffort)
	if rerr != nil {
		_ = sess.Close()
		return nil, rerr
	}
	cwd := sess.Cwd()
	if cwd == "" {
		cwd = m.opts.DefaultCwd
	}
	ss, err := m.wrap(sess, provider, model, cwd, effort)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}

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

// wrap builds the ServerSession over an open session file. An Apply failure
// (unknown profile, bad deny regex, invalid effort) is returned so callers
// fail fast instead of silently starting the session without the profile.
func (m *Manager) wrap(sess *session.Session, provider llm.Provider, model llm.Model, cwd string, effort llm.Effort, profile ...string) (*ServerSession, error) {
	registry := tools.DefaultRegistry(cwd)
	bundle := plugin.Load(cwd, m.opts.NoPlugins)
	sessID := ""
	if sess != nil {
		sessID = sess.ID()
	}
	for _, st := range plugin.ShellTools(bundle.Tools, cwd, sessID) {
		registry.Add(st)
	}
	basePrompt := agent.BuildSystemPrompt(registry, cwd)
	systemPrompt := basePrompt
	want := m.opts.DefaultProfile
	if len(profile) > 0 && profile[0] != "" {
		want = profile[0]
	}
	if want != "" {
		applied, err := plugin.Apply(bundle, registry, basePrompt, cwd, want)
		if err != nil {
			return nil, err
		}
		if applied.Registry != nil {
			registry = applied.Registry
			systemPrompt = applied.SystemPrompt
		}
		if applied.Effort != "" {
			normalized, err := llm.NormalizeEffort(model, applied.Effort)
			if err == nil {
				effort = normalized
			} else {
				effort = ""
			}
		}
	}
	if sess != nil {
		model.SessionID = sess.ID()
	}
	cfg := agent.Config{
		Provider:           provider,
		Model:              model,
		Registry:           registry,
		SystemPrompt:       systemPrompt,
		CompactionSettings: m.opts.CompactionSettings,
		Effort:             effort,
	}
	return newServerSession(m.ctx, sess, cfg, model.Provider+"/"+model.ID, cwd), nil
}

// SetEffort applies a reasoning effort to subsequent runs on an owned session.
func (m *Manager) SetEffort(connID, sessionID string, effort llm.Effort) error {
	ss, err := m.Get(connID, sessionID)
	if err != nil {
		return err
	}
	effort, err = llm.NormalizeEffort(ss.Model(), effort)
	if err != nil {
		return err
	}
	return ss.SetEffort(effort)
}

// SetProfile applies a plugin profile (or resets with "" / "reset") on an
// owned session: filtered registry + rebuilt prompt + effort override.
// The switch is atomic (single ServerSession lock hold) so a concurrent
// Prompt cannot observe a half-applied profile. Reset also restores the
// manager's default effort; an effort the current model does not support
// falls back to the provider default ("") like SetModel does.
func (m *Manager) SetProfile(connID, sessionID, profile string) error {
	ss, err := m.Get(connID, sessionID)
	if err != nil {
		return err
	}
	if profile == "" || profile == "reset" {
		bundle := plugin.Load(ss.Cwd(), m.opts.NoPlugins)
		base := tools.DefaultRegistry(ss.Cwd())
		for _, st := range plugin.ShellTools(bundle.Tools, ss.Cwd(), ss.ID()) {
			base.Add(st)
		}
		effort, nerr := llm.NormalizeEffort(ss.Model(), m.opts.DefaultEffort)
		if nerr != nil {
			effort = ""
		}
		return ss.SetProfile(base, agent.BuildSystemPrompt(base, ss.Cwd()), effort)
	}
	bundle := plugin.Load(ss.Cwd(), m.opts.NoPlugins)
	base := tools.DefaultRegistry(ss.Cwd())
	for _, st := range plugin.ShellTools(bundle.Tools, ss.Cwd(), ss.ID()) {
		base.Add(st)
	}
	applied, err := plugin.Apply(bundle, base, agent.BuildSystemPrompt(base, ss.Cwd()), ss.Cwd(), profile)
	if err != nil {
		return err
	}
	effort := ss.Effort()
	if applied.Effort != "" {
		var nerr error
		effort, nerr = llm.NormalizeEffort(ss.Model(), applied.Effort)
		if nerr != nil {
			effort = ""
		}
	}
	return ss.SetProfile(applied.Registry, applied.SystemPrompt, effort)
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

// Rename persists a title for an owned session.
func (m *Manager) Rename(connID, sessionID, title string) error {
	ss, err := m.Get(connID, sessionID)
	if err != nil {
		return err
	}
	return ss.SetTitle(title)
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
