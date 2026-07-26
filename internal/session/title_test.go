package session

import (
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestExplicitTitlePersistsAndOverridesPrompt(t *testing.T) {
	t.Setenv(SessionsDirEnv, t.TempDir())
	s, err := Create("/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(types.Message{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("original prompt")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle("  Renamed session  "); err != nil {
		t.Fatal(err)
	}
	if got, want := s.Title(), "Renamed session"; got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, want := reopened.Title(), "Renamed session"; got != want {
		t.Fatalf("reopened Title() = %q, want %q", got, want)
	}
	infos, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Title != "Renamed session" {
		t.Fatalf("List() = %#v, want persisted title", infos)
	}
}

func TestTitleUsesFirstUserPrompt(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{types.TextBlock("hello")}},
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("First prompt")}},
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("Later prompt")}},
	}
	if got, want := Title(messages), "First prompt"; got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}

func TestTitleForEmptyConversation(t *testing.T) {
	if got, want := Title(nil), "new"; got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}
