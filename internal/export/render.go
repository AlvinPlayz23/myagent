package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// RenderMarkdown keeps tool activity inside the assistant response instead of
// presenting tool calls and results as separate conversation turns.
func RenderMarkdown(doc Document) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", doc.Title)
	fmt.Fprintf(&b, "Session: `%s`  \nWorking directory: `%s`\n\n---\n", doc.SessionID, doc.Cwd)
	results := toolResults(doc.Messages)
	for _, m := range doc.Messages {
		if m.Role == types.RoleToolResult {
			continue
		}
		writeMarkdownMessage(&b, m, results)
	}
	return []byte(b.String()), nil
}

func writeMarkdownMessage(b *strings.Builder, m types.Message, results map[string]types.Message) {
	role := "Assistant"
	if m.Role == types.RoleUser {
		role = "User"
	}
	fmt.Fprintf(b, "\n## %s\n\n", role)
	for _, c := range m.Content {
		switch c.Type {
		case types.ContentText:
			b.WriteString(c.Text + "\n")
		case types.ContentThinking:
			fmt.Fprintf(b, "<details>\n<summary>Thinking</summary>\n\n%s\n\n</details>\n", c.Thinking)
		case types.ContentToolCall:
			fmt.Fprintf(b, "<details>\n<summary>Tool: %s</summary>\n\n", c.Name)
			if result, ok := results[c.ID]; ok {
				b.WriteString(toolResultText(result) + "\n")
			} else if len(c.Arguments) > 0 {
				args, _ := json.MarshalIndent(c.Arguments, "", "  ")
				fmt.Fprintf(b, "```json\n%s\n```\n", args)
			}
			b.WriteString("\n</details>\n")
		case types.ContentImage:
			b.WriteString("*[Image omitted]*\n")
		}
	}
}

func toolResults(messages []types.Message) map[string]types.Message {
	out := make(map[string]types.Message)
	for _, m := range messages {
		if m.Role == types.RoleToolResult && m.ToolCallID != "" {
			out[m.ToolCallID] = m
		}
	}
	return out
}

func toolResultText(m types.Message) string {
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.ContentText {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// RenderHTML creates a self-contained, lightweight session viewer. It uses
// only browser-native HTML/CSS/JS: no remote resources or vendored libraries.
func RenderHTML(doc Document) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>
:root{--bg:#0d1117;--panel:#161b22;--line:#30363d;--text:#e6edf3;--muted:#8b949e;--blue:#58a6ff;--green:#3fb950;--tool:#21262d}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace}#app{display:flex;min-height:100vh}aside{position:sticky;top:0;width:280px;height:100vh;overflow:auto;flex:none;padding:16px 10px;background:var(--panel);border-right:1px solid var(--line)}aside h2{font-size:13px;margin:0 8px 12px;color:var(--muted)}.nav-item{display:block;width:100%%;padding:7px 8px;text-align:left;color:var(--muted);background:none;border:0;border-radius:4px;font:inherit;font-size:12px;cursor:pointer;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.nav-item:hover,.nav-item.active{background:#21262d;color:var(--text)}.nav-user{color:var(--blue)}.nav-assistant{color:var(--green)}main{width:min(900px,100%%);margin:0 auto;padding:32px 28px}.header{padding-bottom:22px;margin-bottom:24px;border-bottom:1px solid var(--line)}h1{font-size:22px;margin:0 0 8px;word-break:break-word}.meta{color:var(--muted);font-size:12px;word-break:break-all}.actions{margin-top:14px}.actions button{font:inherit;font-size:12px;color:var(--text);background:var(--panel);border:1px solid var(--line);border-radius:4px;padding:6px 9px;cursor:pointer}.actions button:hover{border-color:var(--blue)}.message{margin:20px 0}.message-head{font-size:12px;font-weight:bold;margin-bottom:7px}.user .message-head{color:var(--blue)}.assistant .message-head{color:var(--green)}.bubble{padding:14px;border:1px solid var(--line);border-radius:7px;background:var(--panel);white-space:pre-wrap;overflow-wrap:anywhere}.assistant .bubble{background:transparent;border:0;padding:0}.text+ .text{margin-top:12px}details{margin-top:12px;border:1px solid var(--line);border-radius:6px;background:var(--tool)}summary{padding:8px 10px;cursor:pointer;color:var(--muted);list-style:none}summary::-webkit-details-marker{display:none}summary:before{content:'›';display:inline-block;margin-right:8px;color:var(--blue);transition:transform .15s}details[open]>summary:before{transform:rotate(90deg)}.tool-body{padding:0 10px 10px}.tool-preview{color:var(--muted);font-size:12px;margin:0 0 8px;white-space:pre-wrap;overflow:hidden;text-overflow:ellipsis}.tool-output{margin:0;max-height:520px;overflow:auto;padding:10px;background:#0d1117;border-radius:4px;white-space:pre-wrap;overflow-wrap:anywhere}.thinking summary{color:#d2a8ff}.empty{color:var(--muted);font-style:italic}@media(max-width:720px){#app{display:block}aside{position:relative;width:100%%;height:auto;border-right:0;border-bottom:1px solid var(--line);display:flex;gap:2px;overflow-x:auto;padding:8px}.nav-item{width:auto;flex:none;max-width:180px}aside h2{display:none}main{padding:22px 16px}}
</style></head><body><div id="app"><aside><h2>Conversation</h2>`, html.EscapeString(doc.Title))

	visible := 0
	for i, m := range doc.Messages {
		if m.Role == types.RoleToolResult {
			continue
		}
		role := "Assistant"
		class := "nav-assistant"
		if m.Role == types.RoleUser {
			role, class = "User", "nav-user"
		}
		fmt.Fprintf(&b, `<button class="nav-item %s" data-target="message-%d">%d · %s: %s</button>`, class, i, visible+1, role, html.EscapeString(shortText(messageText(m), 52)))
		visible++
	}
	b.WriteString(`</aside><main><header class="header">`)
	fmt.Fprintf(&b, `<h1>%s</h1><div class="meta">Session: %s<br>Working directory: %s</div>`, html.EscapeString(doc.Title), html.EscapeString(doc.SessionID), html.EscapeString(doc.Cwd))
	b.WriteString(`<div class="actions"><button id="toggle-tools">Expand all tools</button></div></header>`)

	results := toolResults(doc.Messages)
	for i, m := range doc.Messages {
		if m.Role == types.RoleToolResult {
			continue
		}
		writeHTMLMessage(&b, m, i, results)
	}
	b.WriteString(`</main></div><script>(function(){const tools=[...document.querySelectorAll('details.tool')],button=document.getElementById('toggle-tools');button.onclick=()=>{const open=tools.some(x=>!x.open);tools.forEach(x=>x.open=open);button.textContent=open?'Collapse all tools':'Expand all tools'};document.querySelectorAll('.nav-item').forEach(x=>x.onclick=()=>{document.getElementById(x.dataset.target)?.scrollIntoView({behavior:'smooth',block:'start'});document.querySelectorAll('.nav-item').forEach(y=>y.classList.remove('active'));x.classList.add('active')})})()</script></body></html>`)
	return b.Bytes(), nil
}

func writeHTMLMessage(b *bytes.Buffer, m types.Message, index int, results map[string]types.Message) {
	role, class := "Assistant", "assistant"
	if m.Role == types.RoleUser {
		role, class = "User", "user"
	}
	fmt.Fprintf(b, `<section class="message %s" id="message-%d"><div class="message-head">%s</div><div class="bubble">`, class, index, role)
	wrote := false
	for _, c := range m.Content {
		switch c.Type {
		case types.ContentText:
			if c.Text != "" {
				fmt.Fprintf(b, `<div class="text">%s</div>`, strings.ReplaceAll(html.EscapeString(c.Text), "\n", "<br>"))
				wrote = true
			}
		case types.ContentThinking:
			fmt.Fprintf(b, `<details class="thinking"><summary>Thinking</summary><div class="tool-body"><pre class="tool-output">%s</pre></div></details>`, html.EscapeString(c.Thinking))
			wrote = true
		case types.ContentToolCall:
			writeHTMLTool(b, c, results[c.ID])
			wrote = true
		case types.ContentImage:
			b.WriteString(`<div class="empty">[Image omitted]</div>`)
			wrote = true
		}
	}
	if !wrote {
		b.WriteString(`<div class="empty">(no content)</div>`)
	}
	b.WriteString(`</div></section>`)
}

func writeHTMLTool(b *bytes.Buffer, call types.ContentBlock, result types.Message) {
	output := toolResultText(result)
	preview := shortLines(output, 3, 300)
	if output == "" && len(call.Arguments) > 0 {
		raw, _ := json.MarshalIndent(call.Arguments, "", "  ")
		output, preview = string(raw), shortLines(string(raw), 3, 300)
	}
	fmt.Fprintf(b, `<details class="tool"><summary>%s</summary><div class="tool-body">`, html.EscapeString(toolSummary(call)))
	if preview != "" {
		fmt.Fprintf(b, `<div class="tool-preview">%s</div>`, html.EscapeString(preview))
	}
	if output != "" {
		fmt.Fprintf(b, `<pre class="tool-output">%s</pre>`, html.EscapeString(output))
	}
	b.WriteString(`</div></details>`)
}

func messageText(m types.Message) string {
	for _, c := range m.Content {
		if c.Type == types.ContentText && c.Text != "" {
			return c.Text
		}
	}
	return "(tool activity)"
}
func shortText(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > max {
		return string([]rune(text)[:max-1]) + "…"
	}
	return text
}
func shortLines(text string, lines, max int) string {
	parts := strings.Split(text, "\n")
	if len(parts) > lines {
		parts = append(parts[:lines], "…")
	}
	return shortText(strings.Join(parts, "\n"), max)
}
func toolSummary(c types.ContentBlock) string {
	if c.Name == "bash" {
		if command, ok := c.Arguments["command"].(string); ok {
			return "bash · " + shortText(command, 70)
		}
	}
	for _, key := range []string{"path", "file_path"} {
		if path, ok := c.Arguments[key].(string); ok && path != "" {
			return c.Name + " · " + path
		}
	}
	return c.Name
}
