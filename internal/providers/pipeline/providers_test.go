package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline/bitbucket"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline/github"
)

func TestGitHubTrigger(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	_, err := github.New(srv.Client()).Trigger(context.Background(), pipeline.Request{
		Repository:  "acme/e2e",
		Ref:         "main",
		Pipeline:    "smoke.yml",
		Variables:   map[string]string{"env": "staging"},
		Credentials: pipeline.Credentials{Token: "t0k"},
		BaseURL:     srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/acme/e2e/actions/workflows/smoke.yml/dispatches" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer t0k" {
		t.Errorf("auth = %s", gotAuth)
	}
	if gotBody["ref"] != "main" || gotBody["inputs"].(map[string]any)["env"] != "staging" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestGitHubRequiresWorkflow(t *testing.T) {
	_, err := github.New(nil).Trigger(context.Background(), pipeline.Request{
		Repository: "acme/e2e", Credentials: pipeline.Credentials{Token: "x"},
	})
	if err == nil {
		t.Fatal("expected error without workflow")
	}
}

func TestBitbucketTrigger(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		user, pass, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uuid":"{abc}","build_number":42}`))
	}))
	defer srv.Close()

	res, err := bitbucket.New(srv.Client()).Trigger(context.Background(), pipeline.Request{
		Repository:  "acme/e2e",
		Ref:         "main",
		Pipeline:    "smoke",
		Variables:   map[string]string{"B": "2", "A": "1"},
		Credentials: pipeline.Credentials{Username: "bot", Password: "secret"},
		BaseURL:     srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repositories/acme/e2e/pipelines/" || user != "bot" || pass != "secret" {
		t.Errorf("path=%s user=%s", gotPath, user)
	}
	target := gotBody["target"].(map[string]any)
	if target["selector"].(map[string]any)["pattern"] != "smoke" {
		t.Errorf("selector = %v", target["selector"])
	}
	vars := gotBody["variables"].([]any)
	if vars[0].(map[string]any)["key"] != "A" {
		t.Errorf("variables not sorted: %v", vars)
	}
	if res.ID != "{abc}" || res.URL != "https://bitbucket.org/acme/e2e/pipelines/results/42" {
		t.Errorf("result = %+v", res)
	}
}

func TestBitbucketAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := bitbucket.New(srv.Client()).Trigger(context.Background(), pipeline.Request{
		Repository: "acme/e2e", Credentials: pipeline.Credentials{Token: "x"}, BaseURL: srv.URL,
	})
	apiErr, ok := err.(*pipeline.APIError)
	if !ok || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v", err)
	}
}
