package tools

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Edit matching helpers, ported from pi packages/coding-agent/src/core/tools/edit-diff.ts.
// The model's oldText rarely matches the file byte-for-byte: files may use CRLF,
// carry a BOM, or differ in trailing whitespace, Unicode punctuation, or
// compatibility forms. Matching therefore runs on an LF-normalized,
// fuzzy-normalized view, while unchanged lines are copied back verbatim from
// the original so fuzzy normalization never rewrites the rest of the file.

type editEntry struct {
	oldText string
	newText string
}

// detectLineEnding reports the file's line ending from its first newline.
func detectLineEnding(content string) string {
	lf := strings.Index(content, "\n")
	if lf == -1 {
		return "\n"
	}
	if crlf := strings.Index(content, "\r\n"); crlf != -1 && crlf < lf {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// fuzzyReplacer maps punctuation that models commonly emit as ASCII back to
// the Unicode forms found in files, and special spaces to a regular space.
var fuzzyReplacer = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'",
	"\u201C", "\"", "\u201D", "\"", "\u201E", "\"", "\u201F", "\"",
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-",
	"\u2014", "-", "\u2015", "-", "\u2212", "-",
	"\u00A0", " ", "\u2002", " ", "\u2003", " ", "\u2004", " ",
	"\u2005", " ", "\u2006", " ", "\u2007", " ", "\u2008", " ",
	"\u2009", " ", "\u200A", " ", "\u202F", " ", "\u205F", " ",
	"\u3000", " ",
)

// normalizeForFuzzyMatch applies the progressive transformations used by pi:
// NFKC normalization, trailing whitespace stripped per line, smart quotes and
// Unicode dashes folded to ASCII, and special spaces folded to a space.
func normalizeForFuzzyMatch(text string) string {
	lines := strings.Split(norm.NFKC.String(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return fuzzyReplacer.Replace(strings.Join(lines, "\n"))
}

// splitLinesWithEndings keeps each line's terminator so line spans can be
// sliced back out of the original byte-for-byte.
func splitLinesWithEndings(content string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i+1])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

type lineSpan struct {
	start int
	end   int
}

func getLineSpans(content string) []lineSpan {
	lines := splitLinesWithEndings(content)
	spans := make([]lineSpan, 0, len(lines))
	offset := 0
	for _, line := range lines {
		spans = append(spans, lineSpan{start: offset, end: offset + len(line)})
		offset += len(line)
	}
	return spans
}

type textReplacement struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

type fuzzyMatchResult struct {
	found          bool
	index          int
	matchLength    int
	usedFuzzyMatch bool
}

// fuzzyFindText tries an exact match first, then a match in the
// fuzzy-normalized view.
func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	if i := strings.Index(content, oldText); i != -1 {
		return fuzzyMatchResult{
			found:       true,
			index:       i,
			matchLength: len(oldText),
		}
	}
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	if i := strings.Index(fuzzyContent, fuzzyOldText); i != -1 {
		return fuzzyMatchResult{
			found:          true,
			index:          i,
			matchLength:    len(fuzzyOldText),
			usedFuzzyMatch: true,
		}
	}
	return fuzzyMatchResult{}
}

func countOccurrences(content, oldText string) int {
	return strings.Count(normalizeForFuzzyMatch(content), normalizeForFuzzyMatch(oldText))
}

func getReplacementLineRange(lines []lineSpan, r textReplacement) (startLine, endLine int, err error) {
	start := r.matchIndex
	end := r.matchIndex + r.matchLength

	startLine = -1
	for i, line := range lines {
		if start >= line.start && start < line.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		return 0, 0, fmt.Errorf("replacement range is outside the base content")
	}

	endLine = startLine
	for endLine < len(lines) && lines[endLine].end < end {
		endLine++
	}
	if endLine >= len(lines) {
		return 0, 0, fmt.Errorf("replacement range is outside the base content")
	}
	return startLine, endLine + 1, nil
}

func applyReplacements(content string, replacements []textReplacement, offset int) string {
	result := content
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		index := r.matchIndex - offset
		result = result[:index] + r.newText + result[index+r.matchLength:]
	}
	return result
}

// applyReplacementsPreservingUnchangedLines rewrites only the lines touched by
// replacements from the normalized base, copying every other line from the
// original. Without this, a fuzzy match on one line would also strip trailing
// whitespace from every other line in the file.
func applyReplacementsPreservingUnchangedLines(originalContent, baseContent string, replacements []textReplacement) (string, error) {
	originalLines := splitLinesWithEndings(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		return "", fmt.Errorf("cannot preserve unchanged lines because the base content has a different line count")
	}

	type group struct {
		startLine    int
		endLine      int
		replacements []textReplacement
	}

	sorted := append([]textReplacement(nil), replacements...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].matchIndex < sorted[j].matchIndex })

	var groups []group
	for _, r := range sorted {
		startLine, endLine, err := getReplacementLineRange(baseLines, r)
		if err != nil {
			return "", err
		}
		if len(groups) > 0 {
			current := &groups[len(groups)-1]
			if startLine < current.endLine {
				if endLine > current.endLine {
					current.endLine = endLine
				}
				current.replacements = append(current.replacements, r)
				continue
			}
		}
		groups = append(groups, group{
			startLine:    startLine,
			endLine:      endLine,
			replacements: []textReplacement{r},
		})
	}

	var b strings.Builder
	originalLineIndex := 0
	for _, g := range groups {
		for _, line := range originalLines[originalLineIndex:g.startLine] {
			b.WriteString(line)
		}
		groupStart := baseLines[g.startLine].start
		groupEnd := baseLines[g.endLine-1].end
		b.WriteString(applyReplacements(baseContent[groupStart:groupEnd], g.replacements, groupStart))
		originalLineIndex = g.endLine
	}
	for _, line := range originalLines[originalLineIndex:] {
		b.WriteString(line)
	}
	return b.String(), nil
}

func notFoundError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	}
	return fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", editIndex, path)
}

func duplicateError(path string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", occurrences, path)
	}
	return fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", occurrences, editIndex, path)
}

func emptyOldTextError(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s.", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s.", editIndex, path)
}

func noChangeError(path string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)
	}
	return fmt.Errorf("No changes made to %s. The replacements produced identical content.", path)
}

// applyEditsToNormalizedContent matches every edit against the same original
// content, then applies replacements in reverse offset order. If any edit used
// fuzzy matching, replacements are applied in normalized space and overlaid
// onto the original line-by-line so untouched lines keep their bytes.
func applyEditsToNormalizedContent(normalizedContent string, edits []editEntry, path string) (baseContent, newContent string, err error) {
	normalizedEdits := make([]editEntry, len(edits))
	for i, e := range edits {
		normalizedEdits[i] = editEntry{
			oldText: normalizeToLF(e.oldText),
			newText: normalizeToLF(e.newText),
		}
	}

	for i := range normalizedEdits {
		if normalizedEdits[i].oldText == "" {
			return "", "", emptyOldTextError(path, i, len(normalizedEdits))
		}
	}

	usedFuzzyMatch := false
	for _, e := range normalizedEdits {
		if fuzzyFindText(normalizedContent, e.oldText).usedFuzzyMatch {
			usedFuzzyMatch = true
			break
		}
	}
	replacementBaseContent := normalizedContent
	if usedFuzzyMatch {
		replacementBaseContent = normalizeForFuzzyMatch(normalizedContent)
	}

	matchedEdits := make([]textReplacement, 0, len(normalizedEdits))
	for i, e := range normalizedEdits {
		match := fuzzyFindText(replacementBaseContent, e.oldText)
		if !match.found {
			return "", "", notFoundError(path, i, len(normalizedEdits))
		}
		occurrences := countOccurrences(replacementBaseContent, e.oldText)
		if occurrences > 1 {
			return "", "", duplicateError(path, i, len(normalizedEdits), occurrences)
		}
		matchedEdits = append(matchedEdits, textReplacement{
			editIndex:   i,
			matchIndex:  match.index,
			matchLength: match.matchLength,
			newText:     e.newText,
		})
	}

	sort.SliceStable(matchedEdits, func(i, j int) bool {
		return matchedEdits[i].matchIndex < matchedEdits[j].matchIndex
	})
	for i := 1; i < len(matchedEdits); i++ {
		previous := matchedEdits[i-1]
		current := matchedEdits[i]
		if previous.matchIndex+previous.matchLength > current.matchIndex {
			return "", "", fmt.Errorf(
				"edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				previous.editIndex, current.editIndex, path,
			)
		}
	}

	if usedFuzzyMatch {
		newContent, err = applyReplacementsPreservingUnchangedLines(
			normalizedContent, replacementBaseContent, matchedEdits,
		)
		if err != nil {
			return "", "", err
		}
	} else {
		newContent = applyReplacements(replacementBaseContent, matchedEdits, 0)
	}

	if normalizedContent == newContent {
		return "", "", noChangeError(path, len(normalizedEdits))
	}
	return normalizedContent, newContent, nil
}
