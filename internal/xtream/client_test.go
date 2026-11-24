package xtream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestClient_UsesUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "p", "LimePlayer")
	_, err := c.GetVODCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "LimePlayer" {
		t.Errorf("UA = %q, want LimePlayer", gotUA)
	}
}

func TestClient_WaitsForAccessGate(t *testing.T) {
	requestReceived := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived <- struct{}{}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	allow := make(chan struct{})
	client := NewClient(srv.URL, "u", "p", "LimePlayer")
	client.SetAccessGate(func(ctx context.Context) error {
		select {
		case <-allow:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := client.GetVODCategories(context.Background())
		done <- err
	}()

	select {
	case <-requestReceived:
		t.Fatal("provider request passed the closed access gate")
	case <-time.After(100 * time.Millisecond):
	}
	close(allow)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not continue after gate opened")
	}
}

func TestClient_GetVODCategories_Fixture(t *testing.T) {
	body, err := os.ReadFile("../../testdata/xtream/vod_categories.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("action"); got != "get_vod_categories" {
			t.Errorf("action = %q", got)
		}
		w.Write(body)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "p", "LimePlayer")
	cats, err := c.GetVODCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) == 0 {
		t.Fatal("no categories parsed")
	}
	if cats[0].CategoryName == "" {
		t.Error("first category name is empty")
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "u", "p", "LimePlayer")
	_, err := c.GetVODCategories(context.Background())
	if err == nil {
		t.Fatal("expected error on 503")
	}
}
