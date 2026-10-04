package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaMTXSupervisorUsesExistingExternalInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/paths/list" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"itemCount":0,"pageCount":0,"items":[]}`))
	}))
	defer server.Close()

	settings := Settings{
		MTXAPI:          server.URL,
		MediaMTXManaged: true,

		// Ce binaire ne doit jamais être lancé puisque l'API
		// MediaMTX est déjà disponible.
		MediaMTXBin: "/this/binary/must/not/be/launched",
	}

	supervisor := newMediaMTXSupervisor(settings)

	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf(
			"existing MediaMTX API should be reused: %v",
			err,
		)
	}

	supervisor.mu.Lock()
	cmd := supervisor.cmd
	supervisor.mu.Unlock()

	if cmd != nil {
		t.Fatal(
			"supervisor unexpectedly owns a MediaMTX process",
		)
	}

	if err := supervisor.Stop(); err != nil {
		t.Fatalf(
			"Stop() with external MediaMTX: %v",
			err,
		)
	}

	// Stop() ne doit surtout pas affecter l'instance externe.
	resp, err := http.Get(
		server.URL + "/v3/paths/list",
	)
	if err != nil {
		t.Fatalf(
			"external MediaMTX disappeared after Stop(): %v",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf(
			"unexpected external MediaMTX status: %s",
			resp.Status,
		)
	}
}
