package tui

import (
	"reflect"
	"testing"

	"github.com/myagent/myagent/internal/auth"
	"github.com/myagent/myagent/internal/config"
	modelcatalog "github.com/myagent/myagent/internal/models"
)

func TestAvailableModelCandidatesIncludesConfiguredCustomModels(t *testing.T) {
	dir := t.TempDir()
	catalog := modelcatalog.New(dir)
	if err := catalog.SetCustomModels("custom", "Custom", []string{"discovered", "preferred"}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"custom": {Model: "preferred"},
			"legacy": {},
		},
		DefaultModel: "legacy/vendor/model/with/slashes",
	}

	got := availableModelCandidates(catalog, cfg, &auth.Store{Providers: map[string]auth.Credentials{}})
	refs := make([]string, len(got))
	for i, model := range got {
		refs[i] = model.Ref()
	}
	want := []string{"custom/discovered", "custom/preferred", "legacy/vendor/model/with/slashes"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("model refs = %v, want %v", refs, want)
	}
	if got[1].ProviderName != "Custom" {
		t.Fatalf("discovered metadata was replaced: %#v", got[1])
	}
}
