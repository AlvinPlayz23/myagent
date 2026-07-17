// Package tools implements the four core tools (read, write, edit, bash).
//
// Ported from pi packages/coding-agent/src/core/tools/. Tool names,
// descriptions, and JSON schemas are copied verbatim because models are
// RL-trained on them.
package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Truncation limits. Ported from pi tools/truncate.ts.
const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024 // 50KB
)

// TruncationResult mirrors pi's TruncationResult.
type TruncationResult struct {
	Content              string
	Truncated            bool
	TruncatedBy          string // "lines" | "bytes" | ""
	TotalLines           int
	TotalBytes           int
	OutputLines          int
	OutputBytes          int
	LastLinePartial      bool
	FirstLineExceedsLimit bool
	MaxLines             int
	MaxBytes             int
}

// FormatSize renders bytes as a human-readable size. Ported from pi formatSize.
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// splitLinesForCounting mirrors pi's splitLinesForCounting: split on "\n" and
// drop a trailing empty produced by a final newline.
func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TruncateHead keeps the first N lines/bytes. Ported from pi truncateHead.
func TruncateHead(content string, maxLines, maxBytes int) TruncationResult {
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content: content, Truncated: false, TruncatedBy: "",
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}

	if len(lines) > 0 && len(lines[0]) > maxBytes {
		return TruncationResult{
			Content: "", Truncated: true, TruncatedBy: "bytes",
			TotalLines: totalLines, TotalBytes: totalBytes,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines, MaxBytes: maxBytes,
		}
	}

	var out []string
	outBytes := 0
	truncatedBy := "lines"
	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len(lines[i])
		if i > 0 {
			lineBytes++ // newline
		}
		if outBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		out = append(out, lines[i])
		outBytes += lineBytes
	}
	if len(out) >= maxLines && outBytes <= maxBytes {
		truncatedBy = "lines"
	}
	outContent := strings.Join(out, "\n")
	return TruncationResult{
		Content: outContent, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(out), OutputBytes: len(outContent),
		MaxLines: maxLines, MaxBytes: maxBytes,
	}
}

// TruncateTail keeps the last N lines/bytes. Ported from pi truncateTail.
func TruncateTail(content string, maxLines, maxBytes int) TruncationResult {
	if maxLines == 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TruncationResult{
			Content: content, Truncated: false, TruncatedBy: "",
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}

	var out []string
	outBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false
	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		lineBytes := len(lines[i])
		if len(out) > 0 {
			lineBytes++ // newline
		}
		if outBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(out) == 0 {
				truncated := truncateStringToBytesFromEnd(lines[i], maxBytes)
				out = append([]string{truncated}, out...)
				outBytes = len(truncated)
				lastLinePartial = true
			}
			break
		}
		out = append([]string{lines[i]}, out...)
		outBytes += lineBytes
	}
	if len(out) >= maxLines && outBytes <= maxBytes {
		truncatedBy = "lines"
	}
	outContent := strings.Join(out, "\n")
	return TruncationResult{
		Content: outContent, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(out), OutputBytes: len(outContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines, MaxBytes: maxBytes,
	}
}

// truncateStringToBytesFromEnd keeps the last maxBytes of s at a valid UTF-8
// boundary. Ported from pi truncateStringToBytesFromEnd.
func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
