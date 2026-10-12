package tests

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// doBlobRequest performs a request supporting custom headers, used heavily in
// the Lease Blob tests below.
func doBlobRequest(t *testing.T, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	var req *http.Request
	if body != "" {
		req, _ = http.NewRequest(method, url, bytes.NewBufferString(body))
	} else {
		req, _ = http.NewRequest(method, url, nil)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func putBlob(t *testing.T, url, body string) {
	t.Helper()
	resp := doRequest(t, "PUT", url, body)
	resp.Body.Close()
	expectStatus(t, resp, 201)
}

func TestLeaseBlobAcquireReleaseLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"

	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/state.tfstate", "data")

	// Acquire
	resp := doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)
	leaseID := resp.Header.Get("x-ms-lease-id")
	if leaseID == "" {
		t.Fatal("expected x-ms-lease-id header on acquire")
	}

	// Acquire again (different proposed id) -> 409 LeaseAlreadyPresent
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 409)

	// Renew
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action": "renew",
		"x-ms-lease-id":     leaseID,
	})
	resp.Body.Close()
	expectStatus(t, resp, 200)

	// Release
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action": "release",
		"x-ms-lease-id":     leaseID,
	})
	resp.Body.Close()
	expectStatus(t, resp, 200)

	// After release, acquire succeeds again
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)
}

func TestLeaseBlobBreakAllowsReacquire(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"

	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/state.tfstate", "x")

	resp := doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Break with period=0 -> immediately broken
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":       "break",
		"x-ms-lease-break-period": "0",
	})
	resp.Body.Close()
	expectStatus(t, resp, 202)

	// Reacquire works (this is what `terraform force-unlock` enables)
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)
}

func TestLeaseBlobEnforcesLeaseIDOnPut(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"

	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/state.tfstate", "v1")

	// Acquire
	resp := doBlobRequest(t, "PUT", base+"/state.tfstate?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)
	leaseID := resp.Header.Get("x-ms-lease-id")

	// PUT without lease id -> 412 LeaseIdMissing
	resp = doRequest(t, "PUT", base+"/state.tfstate", "v2")
	body := new(bytes.Buffer)
	body.ReadFrom(resp.Body)
	resp.Body.Close()
	expectStatus(t, resp, 412)
	if !strings.Contains(body.String(), "LeaseIdMissing") {
		t.Fatalf("expected LeaseIdMissing in body, got %s", body.String())
	}

	// PUT with wrong lease id -> 412 LeaseIdMismatchWithBlobOperation
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate", "v2", map[string]string{
		"x-ms-lease-id": "00000000-0000-0000-0000-000000000000",
	})
	body.Reset()
	body.ReadFrom(resp.Body)
	resp.Body.Close()
	expectStatus(t, resp, 412)
	if !strings.Contains(body.String(), "LeaseIdMismatchWithBlobOperation") {
		t.Fatalf("expected LeaseIdMismatchWithBlobOperation in body, got %s", body.String())
	}

	// PUT with correct lease id succeeds
	resp = doBlobRequest(t, "PUT", base+"/state.tfstate", "v2", map[string]string{
		"x-ms-lease-id": leaseID,
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// DELETE without lease id -> 412
	resp = doRequest(t, "DELETE", base+"/state.tfstate", "")
	resp.Body.Close()
	expectStatus(t, resp, 412)

	// DELETE with lease id succeeds
	resp = doBlobRequest(t, "DELETE", base+"/state.tfstate", "", map[string]string{
		"x-ms-lease-id": leaseID,
	})
	resp.Body.Close()
	expectStatus(t, resp, 202)
}

func TestLeaseBlobChange(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"
	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/b", "x")

	resp := doBlobRequest(t, "PUT", base+"/b?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)
	old := resp.Header.Get("x-ms-lease-id")

	newID := "11111111-2222-3333-4444-555555555555"
	resp = doBlobRequest(t, "PUT", base+"/b?comp=lease", "", map[string]string{
		"x-ms-lease-action":        "change",
		"x-ms-lease-id":            old,
		"x-ms-proposed-lease-id":   newID,
	})
	resp.Body.Close()
	expectStatus(t, resp, 200)
	if got := resp.Header.Get("x-ms-lease-id"); got != newID {
		t.Fatalf("expected lease id %s, got %s", newID, got)
	}

	// Old lease id is no longer valid for renew
	resp = doBlobRequest(t, "PUT", base+"/b?comp=lease", "", map[string]string{
		"x-ms-lease-action": "renew",
		"x-ms-lease-id":     old,
	})
	resp.Body.Close()
	expectStatus(t, resp, 409)
}

func TestLeaseBlobNonexistent(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"
	doRequest(t, "PUT", base, "").Body.Close()

	resp := doBlobRequest(t, "PUT", base+"/missing?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestLeaseBlobInvalidDuration(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"
	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/b", "x")

	for _, d := range []string{"", "0", "10", "61", "abc"} {
		hdrs := map[string]string{"x-ms-lease-action": "acquire"}
		if d != "" {
			hdrs["x-ms-lease-duration"] = d
		}
		resp := doBlobRequest(t, "PUT", base+"/b?comp=lease", "", hdrs)
		resp.Body.Close()
		expectStatus(t, resp, 400)
	}
}

func TestHeadBlobReflectsLeaseState(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/acct/c1"
	doRequest(t, "PUT", base, "").Body.Close()
	putBlob(t, base+"/b", "x")

	// Initially unleased
	resp := doBlobRequest(t, "HEAD", base+"/b", "", nil)
	resp.Body.Close()
	expectStatus(t, resp, 200)
	if got := resp.Header.Get("x-ms-lease-status"); got != "unlocked" {
		t.Fatalf("expected lease-status=unlocked, got %q", got)
	}
	if got := resp.Header.Get("x-ms-lease-state"); got != "available" {
		t.Fatalf("expected lease-state=available, got %q", got)
	}

	// Acquire and re-check
	resp = doBlobRequest(t, "PUT", base+"/b?comp=lease", "", map[string]string{
		"x-ms-lease-action":   "acquire",
		"x-ms-lease-duration": "60",
	})
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doBlobRequest(t, "HEAD", base+"/b", "", nil)
	resp.Body.Close()
	expectStatus(t, resp, 200)
	if got := resp.Header.Get("x-ms-lease-status"); got != "locked" {
		t.Fatalf("expected lease-status=locked, got %q", got)
	}
	if got := resp.Header.Get("x-ms-lease-state"); got != "leased" {
		t.Fatalf("expected lease-state=leased, got %q", got)
	}
	if got := resp.Header.Get("x-ms-lease-duration"); got != "fixed" {
		t.Fatalf("expected lease-duration=fixed, got %q", got)
	}
}
