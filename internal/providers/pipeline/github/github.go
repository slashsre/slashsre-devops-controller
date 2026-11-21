// Package github triggers GitHub Actions workflows through workflow_dispatch.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
)

// DefaultBaseURL is the public GitHub REST API.
const DefaultBaseURL = "https://api.github.com"

// Provider implements pipeline.Provider.
type Provider struct {
	HTTP *http.Client
}

var _ pipeline.Provider = (*Provider)(nil)

// New returns a provider using client (or a default client when nil).
func New(client *http.Client) *Provider {
	if client == nil {
		client = pipeline.DefaultHTTPClient()
	}
	return &Provider{HTTP: client}
}

// Trigger implements pipeline.Provider.
func (p *Provider) Trigger(ctx context.Context, req pipeline.Request) (pipeline.Result, error) {
	if req.Pipeline == "" {
		return pipeline.Result{}, errors.New("github: pipeline (workflow file name or ID) is required")
	}
	if req.Credentials.Token == "" {
		return pipeline.Result{}, errors.New("github: credentials secret must contain key \"token\"")
	}
	owner, repo, ok := strings.Cut(req.Repository, "/")
	if !ok {
		return pipeline.Result{}, fmt.Errorf("github: invalid repository %q", req.Repository)
	}
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	ref := req.Ref
	if ref == "" {
		ref = "main"
	}

	body, err := json.Marshal(map[string]any{"ref": ref, "inputs": nonNil(req.Variables)})
	if err != nil {
		return pipeline.Result{}, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/actions/workflows/%s/dispatches",
		base, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(req.Pipeline))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return pipeline.Result{}, err
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Authorization", "Bearer "+req.Credentials.Token)
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.HTTP.Do(httpReq)
	if err != nil {
		return pipeline.Result{}, fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return pipeline.Result{}, &pipeline.APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	webBase := "https://github.com"
	if base != DefaultBaseURL {
		webBase = strings.TrimSuffix(strings.TrimSuffix(base, "/api/v3"), "/api")
	}
	return pipeline.Result{
		URL: fmt.Sprintf("%s/%s/%s/actions/workflows/%s", webBase, owner, repo, req.Pipeline),
	}, nil
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
