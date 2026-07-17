package printmode

import (
	"bytes"
	"context"
	"testing"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/session"
)

// TestRunReturnsSessionAppendFailure ensures a failed JSONL append stops the
// run instead of silently leaving persisted history behind the agent state.
func TestRunReturnsSessionAppendFailure(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	sess, err := session.Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run(context.Background(), agent.Config{}, sess, nil, "hello", &stdout, &stderr)
	if err == nil {
		t.Fatal("Run succeeded after the session file was closed")
	}
}
