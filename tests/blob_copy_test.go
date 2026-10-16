package tests

import (
	"io"
	"testing"
)

func TestBlobCopyFromEncodedSource(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/pulumistate/pulumi-state"
	source := base + "/.pulumi%2Fstacks%2Fstate-backends-azure%2Fdev.json"
	dest := base + "/.pulumi%2Fhistory%2Fstate-backends-azure%2Fdev%2Fdev-1.checkpoint.json"
	body := `{"version":3,"deployment":"ok"}`

	doRequest(t, "PUT", base, "").Body.Close()

	resp := doBlobRequest(t, "PUT", source, body, map[string]string{
		"x-ms-blob-type":         "BlockBlob",
		"x-ms-blob-content-type": "application/json",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doBlobRequest(t, "PUT", dest, "", map[string]string{
		"x-ms-version":     "2023-11-03",
		"x-ms-copy-source": source,
	})
	resp.Body.Close()
	expectStatus(t, resp, 202)
	if got := resp.Header.Get("x-ms-copy-id"); got == "" {
		t.Fatal("expected x-ms-copy-id header")
	}
	if got := resp.Header.Get("x-ms-copy-status"); got != "success" {
		t.Fatalf("expected x-ms-copy-status=success, got %q", got)
	}

	resp = doRequest(t, "GET", base+"/.pulumi/history/state-backends-azure/dev/dev-1.checkpoint.json", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("expected copied body %q, got %q", body, string(got))
	}
}
