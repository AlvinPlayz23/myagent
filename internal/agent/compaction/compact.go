package compaction

import (
	"context"
	"strings"
	"time"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/types"
)

// CompactionDetails is stored alongside a compaction entry. Ported from pi
// CompactionDetails: the file lists accumulated across the summarized history.
type CompactionDetails struct {
	ReadFiles     []string `json:"readFiles,omitempty"`
	ModifiedFiles []string `json:"modifiedFiles,omitempty"`
}

// Result is the output of a successful Compact run. Ported from pi
// CompactionResult (the fields myagent uses).
type Result struct {
	Summary        string            // text that replaces the compacted history
	FirstKeptIndex int               // index (in the original slice) where kept history starts
	TokensBefore   int               // estimated context tokens before compaction
	TokensAfter    int               // estimated context tokens after compaction
	Details        CompactionDetails // file lists from the summarized history
}

// Preparation is the inputs for a compaction run, derived from the current
// message history. Ported from pi CompactionPreparation.
type Preparation struct {
	FirstKeptIndex      int             // index where kept history starts
	MessagesToSummarize []types.Message // messages that will be summarized and discarded
	KeptMessages        []types.Message // messages retained verbatim
	TokensBefore        int             // estimated context tokens before compaction
	PreviousSummary     string          // previous compaction summary, for iterative updates
	FileOps             FileOperations  // file ops accumulated across the summarized history
	Settings            Settings
}

// PrepareCompaction inspects the current message history and decides what to
// summarize. Returns ok=false when compaction is not applicable (nothing to
// compact). Ported from pi prepareCompaction, simplified for v1 (no
// split-turn, no session-tree entry layer).
//
// In-memory representation: a previous compaction's summary is a RoleUser
// message whose text is wrapped in CompactionSummaryPrefix/Suffix. We find
// the latest such message to recover previousSummary and the boundary start.
func PrepareCompaction(msgs []types.Message, s Settings) (Preparation, bool) {
	if len(msgs) == 0 {
		return Preparation{}, false
	}

	// Find the latest compaction-summary user message, if any.
	prevIdx := findLastCompactionSummary(msgs)
	var previousSummary string
	boundaryStart := 0
	if prevIdx >= 0 {
		previousSummary = unwrapSummary(msgs[prevIdx])
		// The kept region from the previous compaction starts right after the
		// summary message.
		boundaryStart = prevIdx + 1
	}

	// If the last message is itself a compaction summary, there's nothing new
	// to summarize.
	if prevIdx == len(msgs)-1 {
		return Preparation{}, false
	}

	tokensBefore := EstimateContextTokens(msgs).Tokens

	cut := FindCutPoint(msgs, s.KeepRecentTokens)
	if cut <= boundaryStart {
		// Nothing safe to summarize.
		return Preparation{}, false
	}

	// Messages from boundaryStart..cut-1 get summarized; cut..end are kept.
	var toSummarize, kept []types.Message
	if boundaryStart < cut && cut <= len(msgs) {
		toSummarize = append(toSummarize, msgs[boundaryStart:cut]...)
		kept = append(kept, msgs[cut:]...)
	} else {
		return Preparation{}, false
	}

	// Accumulate file ops from the messages being summarized, carrying
	// forward any prior file lists.
	ops := NewFileOps()
	if prevIdx >= 0 {
		// We don't carry file lists from the previous summary message itself
		// (the in-memory summary has no structured details); v1 resets file
		// tracking at each compaction. The session-persisted CompactionDetails
		// is recomputed from the summarized span each time. This is a
		// documented v1 simplification.
	}
	for _, m := range toSummarize {
		ExtractFileOpsFromMessage(m, ops)
	}

	return Preparation{
		FirstKeptIndex:      cut,
		MessagesToSummarize: toSummarize,
		KeptMessages:        kept,
		TokensBefore:        tokensBefore,
		PreviousSummary:     previousSummary,
		FileOps:             ops,
		Settings:            s,
	}, true
}

// Compact runs the summarization LLM call and assembles the Result. Ported
// from pi compact.
func Compact(ctx context.Context, provider llm.Provider, model llm.Model, prep Preparation) (Result, error) {
	summary, err := GenerateSummary(ctx, provider, model, prep.MessagesToSummarize, prep.Settings.ReserveTokens, prep.PreviousSummary)
	if err != nil {
		return Result{}, err
	}

	files := ComputeFileLists(prep.FileOps)
	summary += FormatFileOperations(files)

	kept := prep.KeptMessages
	tokensAfter := EstimateContextTokens(append([]types.Message{BuildSummaryMessage(summary, time.Now().UnixMilli())}, kept...)).Tokens

	return Result{
		Summary:        summary,
		FirstKeptIndex: prep.FirstKeptIndex,
		TokensBefore:   prep.TokensBefore,
		TokensAfter:    tokensAfter,
		Details: CompactionDetails{
			ReadFiles:     files.ReadFiles,
			ModifiedFiles: files.ModifiedFiles,
		},
	}, nil
}

// BuildSummaryMessage wraps a compaction summary as a user message ready to
// replace the compacted history in the in-memory conversation. The text is
// wrapped in CompactionSummaryPrefix/Suffix (pi's wrapping). Ported from pi
// createCompactionSummaryMessage + convertToLlm's compactionSummary branch.
func BuildSummaryMessage(summary string, timestampMs int64) types.Message {
	text := CompactionSummaryPrefix + summary + CompactionSummarySuffix
	if timestampMs == 0 {
		timestampMs = time.Now().UnixMilli()
	}
	return types.Message{
		Role:      types.RoleUser,
		Content:   []types.ContentBlock{types.TextBlock(text)},
		Timestamp: timestampMs,
	}
}

// findLastCompactionSummary returns the index of the latest user message
// whose text is wrapped in CompactionSummaryPrefix/Suffix, or -1 if none.
func findLastCompactionSummary(msgs []types.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if isCompactionSummary(msgs[i]) {
			return i
		}
	}
	return -1
}

// isCompactionSummary reports whether a message is a wrapped compaction
// summary.
func isCompactionSummary(m types.Message) bool {
	return IsSummaryMessage(m)
}

// IsSummaryMessage reports whether a message is a wrapped compaction summary
// (a user message whose text contains CompactionSummaryPrefix). Exported so
// UIs can detect and render summary messages differently from ordinary user
// messages.
func IsSummaryMessage(m types.Message) bool {
	if m.Role != types.RoleUser || len(m.Content) == 0 {
		return false
	}
	for _, b := range m.Content {
		if b.Type == types.ContentText && strings.Contains(b.Text, CompactionSummaryPrefix) {
			return true
		}
	}
	return false
}

// unwrapSummary extracts the raw summary text from a wrapped summary message.
func unwrapSummary(m types.Message) string {
	for _, b := range m.Content {
		if b.Type != types.ContentText {
			continue
		}
		s := b.Text
		start := strings.Index(s, CompactionSummaryPrefix)
		if start < 0 {
			continue
		}
		s = s[start+len(CompactionSummaryPrefix):]
		end := strings.Index(s, CompactionSummarySuffix)
		if end < 0 {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(s[:end])
	}
	return ""
}
