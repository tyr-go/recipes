package main

import (
	"bytes"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/tyr-go/recipes/tasks/service"
	"github.com/tyr-go/tyr/jsonrpc"
	"github.com/tyr-go/tyr/rest"
)

var update = flag.Bool("update", false, "rewrite the documents in api/ from the API")

// TestDocumentFiles checks that the documents in api/, which the clients
// of the service are generated from and reviews see change, are those of
// the API. After a change to the contract, rewrite them:
//
//	go test -run TestDocumentFiles -update .
func TestDocumentFiles(t *testing.T) {
	api := newAPI(service.New(nil), slog.New(slog.DiscardHandler))
	routes := rest.Mount(http.NewServeMux(), api, mountOptions...)
	docs := []struct {
		file string
		doc  []byte
	}{
		{"api/openapi.json", routes.OpenAPIJSON(info)},
		{"api/openrpc.json", jsonrpc.OpenRPCJSON(api, info)},
	}
	for _, d := range docs {
		if *update {
			if err := os.WriteFile(d.file, d.doc, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(d.file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, d.doc) {
			t.Errorf("%s isn't the document of the API: rewrite it with go test -run TestDocumentFiles -update .", d.file)
		}
	}
}
