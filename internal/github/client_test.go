package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rewriteTransport redirects requests aimed at the production GitHub API host
// to the test server so NewClient can be exercised without a configurable
// BaseURL field.
type rewriteTransport struct {
	base   *url.URL
	baseRT http.RoundTripper
}

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.github.com" {
		req = req.Clone(req.Context())
		req.URL.Scheme = r.base.Scheme
		req.URL.Host = r.base.Host
	}
	return r.baseRT.RoundTrip(req)
}

func TestCreatePullRequest_Success(t *testing.T) {
	var gotAuth string
	var gotBody PullRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"html_url": "https://github.com/owner/repo/pull/1"})
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := NewClient("gh-token", &http.Client{Transport: &rewriteTransport{base: serverURL, baseRT: http.DefaultTransport}})

	url, err := client.CreatePullRequest(context.Background(), "owner", "repo", PullRequest{
		Title: "Update AUR package foo",
		Head:  "update/aur-foo-20260101-000000-123",
		Base:  "main",
		Body:  "body",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/owner/repo/pull/1" {
		t.Fatalf("unexpected url: %s", url)
	}
	if gotAuth != "Bearer gh-token" {
		t.Fatalf("expected Bearer token, got %q", gotAuth)
	}
	if gotBody.Title != "Update AUR package foo" {
		t.Fatalf("unexpected title: %q", gotBody.Title)
	}
}

func TestCreatePullRequest_ForbiddenMentionsPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	client := NewClient("gh-token", &http.Client{Transport: &rewriteTransport{base: serverURL, baseRT: http.DefaultTransport}})

	_, err := client.CreatePullRequest(context.Background(), "owner", "repo", PullRequest{Title: "t", Head: "h", Base: "b", Body: "b"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pull-requests: write") {
		t.Fatalf("error should mention pull-requests: write, got: %v", err)
	}
}

func TestCreatePullRequest_MissingToken(t *testing.T) {
	client := NewClient("", http.DefaultClient)
	_, err := client.CreatePullRequest(context.Background(), "owner", "repo", PullRequest{Title: "t", Head: "h", Base: "b", Body: "b"})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("error should mention missing token, got: %v", err)
	}
}
