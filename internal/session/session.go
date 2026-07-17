// Package session provides append-only JSONL session persistence.
//
// Ported from pi packages/coding-agent/src/core/session-manager.ts. On-disk
// format is version 3: line 1 is a "session" header, each subsequent line is a
// tree entry (we persist "message" entries). Entries form a parent/child chain
// via id/parentId so a linear conversation can be reconstructed.
package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/types"
)

// currentVersion is the on-disk session format version. Matches pi's
// CURRENT_SESSION_VERSION. Bumped 3 → 4 to add the "compaction" entry type.
// v3 files (no compaction entries) are still read correctly by Open().
const currentVersion = 4

// header is line 1 of a session file.
type header struct {
	Type      string `json:"type"` // "session"
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"` // ISO 8601
	Cwd       string `json:"cwd"`
}

// entry is a tree entry (lines 2..N). The "message" type carries a single
// message; the "compaction" type carries a compaction summary and the id of
// the first entry kept verbatim after compaction.
type entry struct {
	Type      string         `json:"type"` // "message" | "compaction"
	ID        string         `json:"id"`
	ParentID  *string        `json:"parentId"`
	Timestamp string         `json:"timestamp"`
	Message   *types.Message `json:"message,omitempty"` // type == "message"

	// type == "compaction"
	Summary          string          `json:"summary,omitempty"`
	FirstKeptEntryID string          `json:"firstKeptEntryId,omitempty"`
	TokensBefore     int             `json:"tokensBefore,omitempty"`
	Details          json.RawMessage `json:"details,omitempty"`
}

// Session is an open, append-only session file.
type Session struct {
	id       string
	path     string
	cwd      string
	lastID   *string // id of the most recent entry (parent for the next append)
	f        *os.File
	w        *bufio.Writer
	messages []types.Message
	entryIDs []string // entry id parallel to messages; "" for synthetic summary
}

// ID returns the session id.
func (s *Session) ID() string { return s.id }

// Path returns the on-disk path of the session file.
func (s *Session) Path() string { return s.path }

// Messages returns the reconstructed conversation loaded from disk.
func (s *Session) Messages() []types.Message { return s.messages }

// Dir returns the sessions directory (~/.myagent/sessions), creating it.
func Dir() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Create starts a fresh session file and writes its header.
func Create(cwd string) (*Session, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	id := newID()
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s := &Session{id: id, path: path, cwd: cwd, f: f, w: bufio.NewWriter(f)}
	h := header{Type: "session", Version: currentVersion, ID: id, Timestamp: nowISO(), Cwd: cwd}
	if err := s.writeLine(h); err != nil {
		_ = f.Close()
		return nil, err
	}
	return s, nil
}

// Open loads an existing session file for appending, reconstructing its
// message history and the id/parentId chain.
//
// When the file contains one or more "compaction" entries, only the LATEST
// compaction is applied: the reconstructed message history is
// [summary-as-user-message] + [messages from the compaction's
// firstKeptEntryId onwards, in chain order]. Messages before the kept
// boundary remain in the file for audit but are NOT loaded into memory.
func Open(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &Session{path: path}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	entriesByID := map[string]entry{}
	var order []entry
	var latestCompaction *entry
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			var h header
			if err := json.Unmarshal(line, &h); err != nil || h.Type != "session" || h.ID == "" {
				return nil, fmt.Errorf("session: invalid header in %s", path)
			}
			s.id = h.ID
			s.cwd = h.Cwd
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerate unknown/other entry types
		}
		switch e.Type {
		case "message":
			entriesByID[e.ID] = e
			order = append(order, e)
		case "compaction":
			entriesByID[e.ID] = e
			order = append(order, e)
			// Track the latest compaction in file order. Take a copy so the
			// map entry isn't aliased.
			c := e
			latestCompaction = &c
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reconstruct the message history.
	if len(order) > 0 {
		last := order[len(order)-1]
		id := last.ID
		s.lastID = &id

		if latestCompaction != nil {
			s.messages, s.entryIDs = buildCompactedMessages(entriesByID, last.ID, latestCompaction)
		} else {
			chain := buildChain(entriesByID, last.ID, "")
			for _, e := range chain {
				if e.Type == "message" && e.Message != nil {
					s.messages = append(s.messages, *e.Message)
					s.entryIDs = append(s.entryIDs, e.ID)
				}
			}
		}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	s.w = bufio.NewWriter(f)
	return s, nil
}

// buildCompactedMessages reconstructs [summaryMessage] + [kept messages] from
// a session that has been compacted. The kept region starts at the
// compaction entry's FirstKeptEntryID and runs to the leaf along the
// parentId chain; the chain walk stops at (and includes) FirstKeptEntryID so
// compacted-away messages are excluded. Returns the messages and a parallel
// slice of entry ids ("" for the synthetic summary message).
func buildCompactedMessages(byID map[string]entry, leafID string, cmp *entry) ([]types.Message, []string) {
	var msgs []types.Message
	var ids []string
	// Synthesize the summary as a user message wrapped with pi's prefix/suffix.
	msgs = append(msgs, compaction.BuildSummaryMessage(cmp.Summary, parseISOmillis(cmp.Timestamp)))
	ids = append(ids, "") // synthetic — no on-disk entry

	chain := buildChain(byID, leafID, cmp.FirstKeptEntryID)
	for _, e := range chain {
		if e.Type == "message" && e.Message != nil {
			msgs = append(msgs, *e.Message)
			ids = append(ids, e.ID)
		}
	}
	return msgs, ids
}

// parseISOmillis parses an ISO 8601 timestamp into Unix millis, returning 0
// on failure.
func parseISOmillis(ts string) int64 {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", ts)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// buildChain walks parentId links from leaf back to root, returning entries in
// root->leaf order. If stopID is non-empty, the walk includes the entry with
// that id and stops there (used for compaction: only messages at or after the
// first-kept entry are loaded).
func buildChain(byID map[string]entry, leafID, stopID string) []entry {
	var reversed []entry
	cur := &leafID
	seen := map[string]bool{}
	for cur != nil {
		e, ok := byID[*cur]
		if !ok || seen[*cur] {
			break
		}
		seen[*cur] = true
		reversed = append(reversed, e)
		if stopID != "" && e.ID == stopID {
			break
		}
		cur = e.ParentID
	}
	// reverse into root->leaf order
	out := make([]entry, len(reversed))
	for i, e := range reversed {
		out[len(reversed)-1-i] = e
	}
	return out
}

// AppendMessage appends a message entry, chaining it to the previous entry.
func (s *Session) AppendMessage(m types.Message) error {
	id := shortID()
	e := entry{Type: "message", ID: id, ParentID: s.lastID, Timestamp: nowISO(), Message: &m}
	if err := s.writeLine(e); err != nil {
		return err
	}
	s.lastID = &id
	s.messages = append(s.messages, m)
	s.entryIDs = append(s.entryIDs, id)
	return s.w.Flush()
}

// AppendCompaction appends a "compaction" entry recording that the session
// history was compacted. The first-kept entry id identifies the oldest
// in-memory message still represented verbatim after this compaction;
// messages before it remain on disk for audit but are not loaded by Open.
//
// The in-memory message history MUST already have been replaced by the caller
// with [summary-as-user-message] + [kept messages]; this method only persists
// the compaction record. tokensBefore is the estimated context-token count
// that triggered compaction. details is an optional JSON blob (e.g. file
// lists accumulated across the summarized span); pass nil to omit it.
func (s *Session) AppendCompaction(summary, firstKeptEntryID string, tokensBefore int, details json.RawMessage) error {
	if summary == "" {
		return fmt.Errorf("session: AppendCompaction requires a summary")
	}
	id := shortID()
	e := entry{
		Type:             "compaction",
		ID:               id,
		ParentID:         s.lastID,
		Timestamp:        nowISO(),
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		Details:          details,
	}
	if err := s.writeLine(e); err != nil {
		return err
	}
	s.lastID = &id
	return s.w.Flush()
}

// LastEntryID returns the id of the most recent entry (message or compaction),
// or "" if the session has no entries yet. This is the parent id for the next
// append and is also the id that a compaction's firstKeptEntryID should point
// at when the kept region begins at the current tail.
func (s *Session) LastEntryID() string {
	if s.lastID == nil {
		return ""
	}
	return *s.lastID
}

// compactionDetailsJSON is the JSON shape persisted in a compaction entry's
// details field. Matches compaction.CompactionDetails.
type compactionDetailsJSON struct {
	ReadFiles     []string `json:"readFiles,omitempty"`
	ModifiedFiles []string `json:"modifiedFiles,omitempty"`
}

// ApplyCompaction replaces the in-memory message history with
// [summaryMsg] + [messages from info.FirstKeptIndex onwards] and persists a
// "compaction" entry recording the boundary. The caller (agent loop) has
// already generated the summary and computed the cut point; this method
// performs the session-side replacement and persistence.
//
// info.FirstKeptIndex is an index into the session's current in-memory
// messages (which are kept in sync with the loop's message list via
// per-message AppendMessage calls). The entry id at that index becomes the
// compaction entry's firstKeptEntryId, so a subsequent Open() reconstructs
// only [summary] + [kept messages].
func (s *Session) ApplyCompaction(info types.CompactionInfo, summaryMsg types.Message) error {
	if info.FirstKeptIndex < 0 || info.FirstKeptIndex > len(s.messages) {
		return fmt.Errorf("session: ApplyCompaction index %d out of range (messages: %d)", info.FirstKeptIndex, len(s.messages))
	}

	firstKeptEntryID := ""
	if info.FirstKeptIndex < len(s.entryIDs) {
		firstKeptEntryID = s.entryIDs[info.FirstKeptIndex]
	}

	// Replace in-memory state: [summary] + [kept messages].
	keptMsgs := append([]types.Message(nil), s.messages[info.FirstKeptIndex:]...)
	keptIDs := append([]string{}, s.entryIDs[info.FirstKeptIndex:]...)
	s.messages = append([]types.Message{summaryMsg}, keptMsgs...)
	s.entryIDs = append([]string{""}, keptIDs...)

	details, _ := json.Marshal(compactionDetailsJSON{
		ReadFiles:     info.ReadFiles,
		ModifiedFiles: info.ModifiedFiles,
	})
	return s.AppendCompaction(info.Summary, firstKeptEntryID, info.TokensBefore, details)
}

// Close flushes and closes the underlying file.
func (s *Session) Close() error {
	if s.w != nil {
		_ = s.w.Flush()
	}
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}

func (s *Session) writeLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	if err := s.w.WriteByte('\n'); err != nil {
		return err
	}
	return s.w.Flush()
}

// Info is lightweight metadata about a session on disk, used for listing.
type Info struct {
	ID           string    // session id (from the header)
	Path         string    // absolute path to the .jsonl file
	Cwd          string    // working directory the session was created in
	Created      time.Time // header timestamp
	Modified     time.Time // file mtime
	MessageCount int       // number of persisted message entries
	Preview      string    // first user message text, truncated
}

// previewMaxLen bounds the Preview string length.
const previewMaxLen = 80

// List enumerates all sessions under the sessions directory, newest first
// (by file mtime). Malformed files are skipped.
func List() ([]Info, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var infos []Info
	for _, p := range matches {
		info, err := readInfo(p)
		if err != nil {
			continue // skip malformed/unreadable files
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Modified.After(infos[j].Modified) })
	return infos, nil
}

// readInfo reads a session file's header and counts message entries without
// reconstructing the full conversation.
func readInfo(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return Info{}, err
	}

	info := Info{Path: path, Modified: stat.ModTime()}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			var h header
			if err := json.Unmarshal(line, &h); err != nil || h.Type != "session" || h.ID == "" {
				return Info{}, fmt.Errorf("session: invalid header in %s", path)
			}
			info.ID = h.ID
			info.Cwd = h.Cwd
			if t, perr := time.Parse("2006-01-02T15:04:05.000Z", h.Timestamp); perr == nil {
				info.Created = t
			}
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil || e.Type != "message" {
			continue
		}
		info.MessageCount++
		if info.Preview == "" && e.Message != nil && e.Message.Role == types.RoleUser {
			info.Preview = previewText(*e.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return Info{}, err
	}
	if first {
		return Info{}, fmt.Errorf("session: empty file %s", path)
	}
	return info, nil
}

// previewText extracts and truncates the first text block of a message.
func previewText(m types.Message) string {
	for _, c := range m.Content {
		if c.Type == types.ContentText && c.Text != "" {
			text := strings.ReplaceAll(c.Text, "\n", " ")
			// Truncate on a rune boundary so we never split a multibyte rune.
			if r := []rune(text); len(r) > previewMaxLen {
				return string(r[:previewMaxLen-1]) + "\u2026"
			}
			return text
		}
	}
	return ""
}

// ResumeByID opens the session whose header id matches the given id (or whose
// filename stem matches). Returns an error if no such session exists.
func ResumeByID(id string) (*Session, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	// Fast path: id matches the filename stem.
	candidate := filepath.Join(dir, id+".jsonl")
	if _, statErr := os.Stat(candidate); statErr == nil {
		return Open(candidate)
	}
	// Fall back to scanning headers (handles ids that differ from filenames).
	infos, err := List()
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		if info.ID == id {
			return Open(info.Path)
		}
	}
	return nil, fmt.Errorf("session: no session with id %q", id)
}

// MostRecent returns the path of the most recently modified session file, or
// "" if none exist. Used to implement --continue.
func MostRecent() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	type fileMod struct {
		path string
		mod  time.Time
	}
	var files []fileMod
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		files = append(files, fileMod{path: p, mod: info.ModTime()})
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	return files[0].path, nil
}

func nowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// newID generates a uuidv7-style session id (time-ordered). Ported in spirit
// from pi's uuidv7 usage.
func newID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	_, _ = rand.Read(b[6:])
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// shortID generates a short random hex id for tree entries.
func shortID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
	}
	return hex.EncodeToString(b[:])
}
