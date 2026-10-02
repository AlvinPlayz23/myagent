package tui

import (
	"reflect"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/llm"
)

func TestConfigCallbacksPreserveExternalUpdates(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	snapshot := &config.Config{DefaultModel: "openai/old-model", WelcomeStyle: "rain"}
	if err := config.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	m := &model{}
	m.configurePersistedSettings(snapshot)
	// A web client changes tools after the TUI has captured its snapshot.
	if err := config.Update(func(cfg *config.Config) error {
		cfg.DisabledTools = []string{"bash"}
		cfg.DefaultModel = "openai/new-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, save := range []func() error{
		func() error { return m.saveWelcomeStyle(welcomeStyle("default")) },
		func() error { return m.savePromptStyle(promptStyle("default")) },
		func() error { return m.saveDefaultEffort(llm.Effort("high")) },
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.DisabledTools, []string{"bash"}) || got.DefaultModel != "openai/new-model" {
		t.Fatal("TUI save clobbered an external update")
	}
	if got.WelcomeStyle != "default" || got.PromptStyle != "default" || got.DefaultEffort != "high" {
		t.Fatal("TUI callbacks did not persist their settings")
	}
	if err := m.saveDisabledTools([]string{"write"}); err != nil {
		t.Fatal(err)
	}
	got, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.DisabledTools, []string{"write"}) || got.WelcomeStyle != "default" || got.DefaultEffort != "high" {
		t.Fatal("tools save clobbered another setting")
	}
	if snapshot.DefaultModel != "openai/old-model" || snapshot.WelcomeStyle != "rain" || snapshot.DisabledTools != nil {
		t.Fatal("save mutated the startup snapshot")
	}
}
