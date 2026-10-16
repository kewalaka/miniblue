package tests

import (
	"testing"
)

// TestPutBlobPersistsMetadataHeaders verifies that a plain Put Block Blob
// (PUT {blob} with no ?comp=) persists x-ms-meta-* request headers onto the
// blob, matching the Azure spec. This regression covers the terraform azurerm
// backend's state-write path where the lock metadata must survive the body
// overwrite.
func TestPutBlobPersistsMetadataHeaders(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"

	doRequest(t, "PUT", base, "").Body.Close()

	// PUT blob with x-ms-meta-* and x-ms-blob-content-* headers.
	resp := doBlobRequest(t, "PUT", base+"/state.tfstate", "newstate", map[string]string{
		"x-ms-blob-type":             "BlockBlob",
		"x-ms-meta-terraformlockid":  "HELLO",
		"x-ms-meta-Other":            "World",
		"x-ms-blob-content-type":     "application/json",
		"x-ms-blob-content-encoding": "gzip",
		"x-ms-blob-cache-control":    "no-cache",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// HEAD must echo the metadata + content properties back.
	resp = doBlobRequest(t, "HEAD", base+"/state.tfstate", "", nil)
	resp.Body.Close()
	expectStatus(t, resp, 200)
	if got := resp.Header.Get("x-ms-meta-terraformlockid"); got != "HELLO" {
		t.Fatalf("expected x-ms-meta-terraformlockid=HELLO, got %q", got)
	}
	if got := resp.Header.Get("x-ms-meta-other"); got != "World" {
		t.Fatalf("expected x-ms-meta-other=World, got %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type=application/json, got %q", got)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding=gzip, got %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("expected Cache-Control=no-cache, got %q", got)
	}

	// A subsequent Put Blob WITHOUT any x-ms-meta-* must replace (clear)
	// the prior metadata, per Azure spec.
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate", "again", map[string]string{
		"x-ms-blob-type": "BlockBlob",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doBlobRequest(t, "HEAD", base+"/state.tfstate", "", nil)
	resp.Body.Close()
	expectStatus(t, resp, 200)
	if got := resp.Header.Get("x-ms-meta-terraformlockid"); got != "" {
		t.Fatalf("expected metadata to be cleared on overwrite, got %q", got)
	}
	if got := resp.Header.Get("x-ms-meta-other"); got != "" {
		t.Fatalf("expected metadata to be cleared on overwrite, got %q", got)
	}
}
