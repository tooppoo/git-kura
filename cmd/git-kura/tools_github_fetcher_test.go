package main

import (
	"bytes"
	"net/http"
	"testing"
)

func TestGithubReleaseFetcher(t *testing.T) {
	body := []byte("payload")
	g := githubReleaseFetcher{client: &http.Client{Transport: stubTransport{status: http.StatusOK, body: body}}}
	if got, err := g.fetchSidecar("v1.2.3", "1.2.3"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("fetchSidecar = %q, %v", got, err)
	}
	if got, err := g.fetchArchive("v1.2.3", "git-kura-tools_1.2.3.tar.gz"); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("fetchArchive = %q, %v", got, err)
	}
	if url := g.downloadURL("v1.2.3", "asset.tar.gz"); url != "https://github.com/tooppoo/git-kura/releases/download/v1.2.3/asset.tar.gz" {
		t.Fatalf("downloadURL = %q", url)
	}

	bad := githubReleaseFetcher{client: &http.Client{Transport: stubTransport{status: http.StatusNotFound, body: nil}}}
	if _, err := bad.fetchSidecar("v9.9.9", "9.9.9"); err == nil {
		t.Fatal("expected error on 404")
	}
	if newGithubReleaseFetcher().client == nil {
		t.Fatal("newGithubReleaseFetcher should set a client")
	}
}

func TestGithubFetcherTransportError(t *testing.T) {
	g := githubReleaseFetcher{client: &http.Client{Transport: errTransport{}}}
	if _, err := g.get("https://example.invalid/x"); err == nil {
		t.Fatal("transport error should propagate")
	}
	// A zero-value fetcher falls back to http.DefaultClient; a refused
	// connection still surfaces as an error.
	if _, err := (githubReleaseFetcher{}).get("http://127.0.0.1:1/nope"); err == nil {
		t.Fatal("connection error should propagate")
	}
}
