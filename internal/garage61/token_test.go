package garage61

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type tokenRoundTrip func(*http.Request) (*http.Response, error)

func (f tokenRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLocalTokenStorePrefersFileAndFallsBackToEnvironment(t *testing.T) {
	t.Setenv("GARAGE61_TOKEN", "environment-token")
	path := filepath.Join(t.TempDir(), "data", "garage61-token")
	store := NewLocalTokenStore(path)
	ctx := context.Background()
	if got, err := store.Token(ctx); err != nil || got != "environment-token" {
		t.Fatalf("environment token = %q, %v", got, err)
	}
	if got, err := store.Status(); err != nil || !got.Configured || got.Source != "environment" {
		t.Fatalf("environment status = %+v, %v", got, err)
	}
	if err := store.Save("  first-token  "); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Token(ctx); err != nil || got != "first-token" {
		t.Fatalf("saved token = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("token file permissions = %o, want 600", info.Mode().Perm())
	}
	if err := store.Save("replacement-token"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Token(ctx); err != nil || got != "replacement-token" {
		t.Fatalf("replacement token = %q, %v", got, err)
	}
	if got, err := store.Status(); err != nil || !got.Configured || got.Source != "file" {
		t.Fatalf("file status = %+v, %v", got, err)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Token(ctx); err != nil || got != "environment-token" {
		t.Fatalf("fallback token = %q, %v", got, err)
	}
}

func TestLocalTokenStoreRejectsInvalidTokensWithoutLeakingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garage61-token")
	store := NewLocalTokenStore(path)
	for _, token := range []string{"", "two words", "line\nbreak", strings.Repeat("x", maxTokenBytes+1)} {
		if err := store.Save(token); err == nil || strings.Contains(err.Error(), token) && token != "" {
			t.Fatalf("invalid token was accepted or echoed: length=%d, err=%v", len(token), err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid input created a token file: %v", err)
	}
	if err := os.WriteFile(path, []byte("broken token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Token(context.Background()); err == nil || strings.Contains(err.Error(), "broken token") {
		t.Fatalf("invalid saved token error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlinks requires Windows privileges: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := store.Token(context.Background()); err == nil {
		t.Fatal("symlink was accepted as a token file")
	}
}

func TestGarageClientUsesSavedTokenWithoutRestart(t *testing.T) {
	t.Setenv("GARAGE61_TOKEN", "")
	store := NewLocalTokenStore(filepath.Join(t.TempDir(), "garage61-token"))
	if err := store.Save("first-token"); err != nil {
		t.Fatal(err)
	}
	var headers []string
	client := New(store.Token)
	client.http.Transport = tokenRoundTrip(func(r *http.Request) (*http.Response, error) {
		headers = append(headers, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	client.interval = 0
	if _, err := client.get(context.Background(), "/probe", url.Values{}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("replacement-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.get(context.Background(), "/probe", url.Values{}, false); err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 || headers[0] != "Bearer first-token" || headers[1] != "Bearer replacement-token" {
		t.Fatalf("client did not use the latest saved token: %q", headers)
	}
}
