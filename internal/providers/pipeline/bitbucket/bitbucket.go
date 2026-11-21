// Package bitbucket triggers Bitbucket Cloud pipelines.
package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
)

// DefaultBaseURL is the Bitbucket Cloud REST API.
const DefaultBaseURL = "https://api.bitbucket.org/2.0"

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

type selector struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern"`
}

type target struct {
	RefType  string    `json:"ref_type"`
	Type     string    `json:"type"`
	RefName  string    `json:"ref_name"`
	Selector *selector `json:"selector,omitempty"`
}

type variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type payload struct {
	Target    target     `json:"target"`
	Variables []variable `json:"variables,omitempty"`
}

type response struct {
	UUID        string `json:"uuid"`
	BuildNumber int    `json:"build_number"`
}

// Trigger implements pipeline.Provider.
func (p *Provider) Trigger(ctx context.Context, req pipeline.Request) (pipeline.Result, error) {
	workspace, repo, ok := strings.Cut(req.Repository, "/")
	if !ok {
		return pipeline.Result{}, fmt.Errorf("bitbucket: invalid repository %q", req.Repository)
	}
	creds := req.Credentials
	if creds.Token == "" && (creds.Username == "" || creds.Password == "") {
		return pipeline.Result{}, errors.New("bitbucket: credentials secret must contain \"token\" or \"username\" and \"password\"")
	}
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	ref := req.Ref
	if ref == "" {
		ref = "main"
	}

	body := payload{Target: target{RefType: "branch", Type: "pipeline_ref_target", RefName: ref}}
	if req.Pipeline != "" {
		body.Target.Selector = &selector{Type: "custom", Pattern: req.Pipeline}
	}
	keys := make([]string, 0, len(req.Variables))
	for k := range req.Variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		body.Variables = append(body.Variables, variable{Key: k, Value: req.Variables[k]})
	}
	data, err := json.Marshal(body)
	if err != nil {
		return pipeline.Result{}, err
	}

	endpoint := fmt.Sprintf("%s/repositories/%s/%s/pipelines/", base, url.PathEscape(workspace), url.PathEscape(repo))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return pipeline.Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if creds.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+creds.Token)
	} else {
		httpReq.SetBasicAuth(creds.Username, creds.Password)
	}

	resp, err := p.HTTP.Do(httpReq)
	if err != nil {
		return pipeline.Result{}, fmt.Errorf("bitbucket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return pipeline.Result{}, &pipeline.APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	var r response
	_ = json.NewDecoder(resp.Body).Decode(&r)
	res := pipeline.Result{ID: r.UUID}
	if r.BuildNumber > 0 {
		res.URL = fmt.Sprintf("https://bitbucket.org/%s/%s/pipelines/results/%d", workspace, repo, r.BuildNumber)
	}
	return res, nil
}
