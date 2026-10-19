package export

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func completionMessage() types.Message {
	return types.Message{
		Role:    types.RoleUser,
		Source:  types.SourceSubagentCompletion,
		Content: []types.ContentBlock{types.TextBlock("Subagent task sub_1 finished.")},
	}
}

func TestRenderMarkdownAttributesSubagentCompletion(t *testing.T) {
	doc := Document{Title: "t", SessionID: "s", Cwd: "/w", Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("human question")}},
		completionMessage(),
	}}
	out, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	md := string(out)
	if !strings.Contains(md, "## Subagent") || !strings.Contains(md, "Subagent task sub_1 finished.") {
		t.Fatalf("completion not attributed to Subagent:\n%s", md)
	}
	if !strings.Contains(md, "## User\n\nhuman question") {
		t.Fatalf("real user prompt misrendered:\n%s", md)
	}
	if strings.Contains(md, "## User\n\nSubagent task") {
		t.Fatalf("completion was rendered as human input:\n%s", md)
	}
}

func TestRenderHTMLAttributesSubagentCompletion(t *testing.T) {
	doc := Document{Title: "t", SessionID: "s", Cwd: "/w", Messages: []types.Message{completionMessage()}}
	out, err := RenderHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	if !strings.Contains(html, `class="message subagent"`) || !strings.Contains(html, ">Subagent<") {
		t.Fatalf("completion not attributed to Subagent:\n%s", html)
	}
	if !strings.Contains(html, "nav-subagent") {
		t.Fatalf("nav entry missing subagent class:\n%s", html)
	}
	if strings.Contains(html, `class="message user"`) {
		t.Fatalf("completion was rendered as human input:\n%s", html)
	}
}
