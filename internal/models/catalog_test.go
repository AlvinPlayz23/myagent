package models

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/llm"
)

func TestNormalizeIncludesOnlyCompatibleToolModels(t *testing.T) {
	compatible := provider{ID: "zenmux", Name: "ZenMux", NPM: "@ai-sdk/openai-compatible", Models: map[string]sourceModel{
		"good":     {ID: "good", Name: "Good", ToolCall: true, Reasoning: true},
		"no-tools": {ID: "no-tools", ToolCall: false},
	}}

	source := map[string]provider{
		"zenmux":     compatible,
		"native":     {ID: "native", NPM: "@ai-sdk/anthropic", Models: map[string]sourceModel{"claude": {ID: "claude", ToolCall: true}}},
		"openrouter": {ID: "openrouter", Name: "OpenRouter", NPM: "@openrouter/ai-sdk-provider", Models: map[string]sourceModel{"auto": {ID: "auto", ToolCall: true}}},
		"aihubmix":   {ID: "aihubmix", Name: "AIHubMix", NPM: "@aihubmix/ai-sdk-provider", Models: map[string]sourceModel{"qwen": {ID: "qwen", ToolCall: true}}},
	}

	got, providers := normalize(source)
	refs := make([]string, len(got))
	for i, model := range got {
		refs[i] = model.Ref()
	}
	if want := []string{"aihubmix/qwen", "openrouter/auto", "zenmux/good"}; !reflect.DeepEqual(refs, want) {
		t.Fatalf("models = %v, want %v", refs, want)
	}
	if got, want := len(providers), 3; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
}

func TestCatalogFiltersConfiguredProvidersAndExpires(t *testing.T) {
	c := &Catalog{data: cache{
		CheckedAt: time.Now().Add(-5 * time.Hour),
		Models:    []Model{{Provider: "openrouter", ID: "a"}, {Provider: "zenmux", ID: "b"}},
	}}
	if !c.NeedsRefresh(time.Now()) {
		t.Fatal("five-hour-old catalog should need refresh")
	}
	got := c.Models(map[string]struct{}{"zenmux": {}})
	if want := []Model{{Provider: "zenmux", ID: "b"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered models = %#v, want %#v", got, want)
	}
}

func TestCatalogModelsDeduplicatesBuiltinAndDiscoveredRefs(t *testing.T) {
	c := &Catalog{data: cache{
		Models: []Model{
			{Provider: "openrouter", ProviderName: "OpenRouter", ID: "catalog", Reasoning: true},
			{Provider: "openrouter", ProviderName: "OpenRouter", ID: "builtin"},
		},
		Custom: []Model{
			{Provider: "openrouter", ProviderName: "Custom", ID: "catalog"},
			{Provider: "openrouter", ProviderName: "Custom", ID: "discovered"},
		},
	}}

	got := c.Models(map[string]struct{}{"openrouter": {}})
	want := []Model{
		{Provider: "openrouter", ProviderName: "OpenRouter", ID: "catalog", Reasoning: true},
		{Provider: "openrouter", ProviderName: "OpenRouter", ID: "builtin"},
		{Provider: "openrouter", ProviderName: "Custom", ID: "discovered"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v, want %#v", got, want)
	}
}

func TestCatalogDerivesProvidersFromLegacyCache(t *testing.T) {
	c := &Catalog{data: cache{Models: []Model{
		{Provider: "openrouter", ProviderName: "OpenRouter", ID: "one"},
		{Provider: "zenmux", ProviderName: "ZenMux", ID: "two"},
	}}}
	if !c.NeedsRefresh(time.Now()) {
		t.Fatal("legacy cache without provider metadata should need refresh")
	}
	got := c.Providers()
	if want := []Provider{{ID: "openrouter", Name: "OpenRouter"}, {ID: "zenmux", Name: "ZenMux"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("derived providers = %#v, want %#v", got, want)
	}
}

func TestCustomModelsSurviveCatalogRefreshState(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	if err := c.SetCustomModels("local", "Local", []string{"two", "one", "two"}); err != nil {
		t.Fatalf("SetCustomModels: %v", err)
	}
	models := c.Models(map[string]struct{}{"local": {}})
	if want := []Model{{Provider: "local", ProviderName: "Local", ID: "one"}, {Provider: "local", ProviderName: "Local", ID: "two"}}; !reflect.DeepEqual(models, want) {
		t.Fatalf("custom models = %#v, want %#v", models, want)
	}
	if err := c.RemoveCustomProvider("local"); err != nil {
		t.Fatalf("RemoveCustomProvider: %v", err)
	}
	if got := c.Models(map[string]struct{}{"local": {}}); len(got) != 0 {
		t.Fatalf("removed custom models = %#v", got)
	}
}

func TestSetCustomModelsUpdatesExistingCacheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cacheFile)
	if err := os.WriteFile(path, []byte(`{"custom":[{"provider":"local","providerName":"Local","id":"old"}]}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c := New(dir)
	if err := c.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := c.SetCustomModels("local", "Local", []string{"new-model-id"}); err != nil {
		t.Fatalf("SetCustomModels: %v", err)
	}

	reloaded := New(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load updated cache: %v", err)
	}
	if got, want := reloaded.Models(map[string]struct{}{"local": {}}), []Model{{Provider: "local", ProviderName: "Local", ID: "new-model-id"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("custom models = %#v, want %#v", got, want)
	}
}

func TestCatalogEnrichUsesOnlyBuiltinCapabilities(t *testing.T) {
	c := &Catalog{data: cache{
		Models: []Model{
			{Provider: "openrouter", ID: "reasoner", Reasoning: true},
			{Provider: "openrouter", ID: "plain"},
		},
		Custom: []Model{{Provider: "local", ID: "unknown"}},
	}}
	reasoner := c.Enrich(llm.Model{Provider: "openrouter", ID: "reasoner"})
	if !reasoner.ReasoningKnown || !reasoner.Reasoning || len(reasoner.SupportedEfforts) != 6 {
		t.Fatalf("reasoning model = %#v", reasoner)
	}
	plain := c.Enrich(llm.Model{Provider: "openrouter", ID: "plain"})
	if !plain.ReasoningKnown || plain.Reasoning || plain.SupportedEfforts != nil {
		t.Fatalf("plain model = %#v", plain)
	}
	custom := c.Enrich(llm.Model{Provider: "local", ID: "unknown"})
	if custom.ReasoningKnown {
		t.Fatalf("custom capability should remain unknown: %#v", custom)
	}
}

func TestCatalogConcurrentReadsAndCustomModelUpdates(t *testing.T) {
	catalog := New(t.TempDir())
	if err := catalog.SetCustomModels("local", "Local", []string{"initial"}); err != nil {
		t.Fatalf("seed custom models: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	reader := func() {
		defer wg.Done()
		<-start
		for range 32 {
			_ = catalog.Models(map[string]struct{}{"local": {}})
			_ = catalog.Providers()
			_ = catalog.NeedsRefresh(time.Now())
			_, _ = catalog.FindModel("local", "initial")
		}
	}
	writer := func() {
		defer wg.Done()
		<-start
		for i := range 16 {
			if err := catalog.SetCustomModels("local", "Local", []string{"model", string(rune('a' + i))}); err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}

	wg.Add(3)
	go reader()
	go reader()
	go writer()
	close(start)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func TestCatalogInstancesPreserveConcurrentCustomModels(t *testing.T) {
	dir := t.TempDir()
	first := New(dir)
	if err := first.SetCustomModels("seed", "Seed", []string{"seed-model"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	second := New(dir)
	if err := second.Load(); err != nil {
		t.Fatalf("load second catalog: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	write := func(c *Catalog, provider string) {
		defer wg.Done()
		<-start
		for range 16 {
			if err := c.SetCustomModels(provider, provider, []string{provider + "-model"}); err != nil {
				errs <- err
				return
			}
		}
	}
	read := func() {
		defer wg.Done()
		<-start
		for range 64 {
			reader := New(dir)
			if err := reader.Load(); err != nil {
				errs <- err
				return
			}
		}
	}

	wg.Add(3)
	go write(first, "alpha")
	go write(second, "beta")
	go read()
	close(start)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}

	reloaded := New(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	for _, provider := range []string{"alpha", "beta", "seed"} {
		if _, ok := reloaded.FindModel(provider, provider+"-model"); provider == "seed" {
			if _, ok := reloaded.FindModel(provider, "seed-model"); !ok {
				t.Fatalf("missing preserved model for %s", provider)
			}
		} else if !ok {
			t.Fatalf("missing preserved model for %s", provider)
		}
	}
}

func TestSaveCacheFallsBackWhenAtomicReplacementFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cacheFile)
	if err := os.WriteFile(path, []byte(`{"custom":[{"provider":"old","id":"old-model"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := cache{Custom: []Model{{Provider: "local", ID: "new-model"}}}
	if err := saveCacheWithReplace(path, want, func(string, string) error {
		return errors.New("sharing violation")
	}); err != nil {
		t.Fatalf("save cache fallback: %v", err)
	}

	got, found, err := loadCache(path)
	if err != nil || !found {
		t.Fatalf("load fallback cache = %#v, found %v, err %v", got, found, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback cache = %#v, want %#v", got, want)
	}
}

func TestCatalogFileLockSerializesProcesses(t *testing.T) {
	const helper = "MYAGENT_CATALOG_LOCK_HELPER"
	const lockPath = "MYAGENT_CATALOG_LOCK_PATH"
	if os.Getenv(helper) == "1" {
		release, err := lockCatalogFile(os.Getenv(lockPath))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		time.Sleep(150 * time.Millisecond)
		release()
		return
	}

	path := filepath.Join(t.TempDir(), cacheFile)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCatalogFileLockSerializesProcesses$")
	cmd.Env = append(os.Environ(), helper+"=1", lockPath+"="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("capture helper output: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lock helper: %v", err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	if !bufio.NewScanner(stdout).Scan() {
		t.Fatalf("lock helper exited before acquiring lock")
	}

	started := time.Now()
	release, err := lockCatalogFile(path)
	if err != nil {
		t.Fatalf("acquire contended lock: %v", err)
	}
	elapsed := time.Since(started)
	release()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("lock helper failed: %v", err)
	}
	finished = true
	if elapsed < 75*time.Millisecond {
		t.Fatalf("second process acquired cache lock after %s, want serialization", elapsed)
	}
}
