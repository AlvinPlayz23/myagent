package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListOpenAIModels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"z"},{"id":"a"},{"id":"z"},{"id":" "},{}]}`))
	}))
	defer srv.Close()

	models, err := ListOpenAIModels(context.Background(), srv.Client(), "secret", srv.URL+"/v1/")
	if err != nil {
		t.Fatalf("ListOpenAIModels: %v", err)
	}
	if got, want := models, []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	if gotPath != "/v1/models" || gotAuth != "Bearer secret" {
		t.Fatalf("path/auth = %q/%q", gotPath, gotAuth)
	}
}

func TestListOpenAIModelsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no access", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := ListOpenAIModels(context.Background(), srv.Client(), "", srv.URL); err == nil {
		t.Fatal("expected status error")
	}
	if _, err := ListOpenAIModels(context.Background(), srv.Client(), "", ""); err == nil {
		t.Fatal("expected base URL error")
	}
}
