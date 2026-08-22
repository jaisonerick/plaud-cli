package catalog

import (
	"os"
	"testing"
)

// TestRoundTripsTheRealCatalog is skipped unless a catalog is named, and is
// how the writer is checked against a file people have been curating.
func TestRoundTripsTheRealCatalog(t *testing.T) {
	hub := os.Getenv("PLAUD_CATALOG_HUB")
	if hub == "" {
		t.Skip("set PLAUD_CATALOG_HUB to a directory holding a catalog.jsonl")
	}
	c, err := Open(hub)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d entries", c.Len())
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}
