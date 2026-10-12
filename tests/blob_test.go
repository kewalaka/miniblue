package tests

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBlobStorageCRUD(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount"

	// Create container
	resp := doRequest(t, "PUT", base+"/mycontainer", "")
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Upload blob
	resp = doRequest(t, "PUT", base+"/mycontainer/hello.txt", "Hello World!")
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Download
	resp = doRequest(t, "GET", base+"/mycontainer/hello.txt", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if buf.String() != "Hello World!" {
		t.Fatalf("expected 'Hello World!', got '%s'", buf.String())
	}

	// Verify content-length header
	if resp.Header.Get("Content-Length") != "12" {
		t.Fatalf("expected Content-Length=12, got %s", resp.Header.Get("Content-Length"))
	}
}

func TestBlobListContentLength(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"

	doRequest(t, "PUT", base, "").Body.Close()
	doRequest(t, "PUT", base+"/test.txt", "abcdef").Body.Close()

	resp := doRequest(t, "GET", base, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Fatalf("expected XML Content-Type, got %s", ct)
	}

	var result struct {
		XMLName         xml.Name `xml:"EnumerationResults"`
		ServiceEndpoint string   `xml:"ServiceEndpoint,attr"`
		ContainerName   string   `xml:"ContainerName,attr"`
		Blobs           struct {
			Items []struct {
				Name       string `xml:"Name"`
				Properties struct {
					ContentLength int64  `xml:"Content-Length"`
					ContentType   string `xml:"Content-Type"`
					BlobType      string `xml:"BlobType"`
					LeaseStatus   string `xml:"LeaseStatus"`
					LeaseState    string `xml:"LeaseState"`
					Etag          string `xml:"Etag"`
					LastModified  string `xml:"Last-Modified"`
				} `xml:"Properties"`
			} `xml:"Blob"`
		} `xml:"Blobs"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode XML response: %v", err)
	}

	if result.ContainerName != "mycontainer" {
		t.Fatalf("expected ContainerName=mycontainer, got %q", result.ContainerName)
	}
	if result.ServiceEndpoint == "" {
		t.Fatalf("expected ServiceEndpoint attribute to be set")
	}
	if len(result.Blobs.Items) != 1 {
		t.Fatalf("expected 1 blob, got %d", len(result.Blobs.Items))
	}
	b := result.Blobs.Items[0]
	if b.Name != "test.txt" {
		t.Fatalf("expected blob name test.txt, got %q", b.Name)
	}
	if b.Properties.ContentLength != 6 {
		t.Fatalf("expected Content-Length=6, got %d", b.Properties.ContentLength)
	}
	if b.Properties.BlobType != "BlockBlob" {
		t.Fatalf("expected BlobType=BlockBlob, got %q", b.Properties.BlobType)
	}
	if b.Properties.LeaseStatus != "unlocked" || b.Properties.LeaseState != "available" {
		t.Fatalf("expected unlocked/available lease, got %q/%q", b.Properties.LeaseStatus, b.Properties.LeaseState)
	}
}

func TestBlobListDelimiterReturnsBlobPrefixForPulumiProjectStacks(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"

	doRequest(t, "PUT", base, "").Body.Close()
	doRequest(t, "PUT", base+"/.pulumi/stacks/state-backends-azure/dev.json", "{}").Body.Close()
	doRequest(t, "PUT", base+"/.pulumi/stacks/state-backends-azure/dev.json.bak", "{}").Body.Close()

	resp := doRequest(t, "GET", base+"?restype=container&comp=list&prefix=.pulumi/stacks/&delimiter=/", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	var result struct {
		Blobs struct {
			Items []struct {
				Name string `xml:"Name"`
			} `xml:"Blob"`
			Prefixes []struct {
				Name string `xml:"Name"`
			} `xml:"BlobPrefix"`
		} `xml:"Blobs"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode XML response: %v", err)
	}
	if len(result.Blobs.Items) != 0 {
		t.Fatalf("expected no blob entries at delimiter level, got %+v", result.Blobs.Items)
	}
	if len(result.Blobs.Prefixes) != 1 {
		t.Fatalf("expected one BlobPrefix, got %d", len(result.Blobs.Prefixes))
	}
	if got, want := result.Blobs.Prefixes[0].Name, ".pulumi/stacks/state-backends-azure/"; got != want {
		t.Fatalf("expected BlobPrefix %q, got %q", want, got)
	}
}

func TestBlobNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/blob/acct/container/nope.txt", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestBlobDelete(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"

	doRequest(t, "PUT", base, "").Body.Close()
	doRequest(t, "PUT", base+"/file.txt", "data").Body.Close()

	resp := doRequest(t, "DELETE", base+"/file.txt", "")
	resp.Body.Close()
	expectStatus(t, resp, 202)

	resp = doRequest(t, "GET", base+"/file.txt", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

// TestBlobNestedPath verifies that blob paths containing forward slashes
// (e.g. Terraform's "env:/prod/terraform.tfstate" layout) are routed to the
// blob service handlers rather than falling through chi's route tree to the
// parent ARM mux's NotFound (which would emit an "InvalidResourceType" JSON
// error and break Terraform's azurerm backend — see issue #14).
func TestBlobNestedPath(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"
	nested := base + "/demo/terraform.tfstate"

	doRequest(t, "PUT", base, "").Body.Close()

	// HEAD on a nested, non-existent blob must reach writeBlobNotFound
	// (XML body, x-ms-error-code: BlobNotFound) — not the ARM 404.
	resp := doRequest(t, "HEAD", nested, "")
	expectStatus(t, resp, 404)
	if got := resp.Header.Get("x-ms-error-code"); got != "BlobNotFound" {
		t.Errorf("HEAD nested: expected x-ms-error-code=BlobNotFound, got %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("HEAD nested: expected XML Content-Type, got %q", ct)
	}
	resp.Body.Close()

	// PUT a nested blob, then GET it back to confirm the wildcard route
	// preserves the full path including slashes.
	resp = doRequest(t, "PUT", nested, "state-payload")
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doRequest(t, "GET", nested, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if buf.String() != "state-payload" {
		t.Fatalf("expected 'state-payload', got %q", buf.String())
	}
}

func TestBlobHeadEncodedPathCreationTime(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"
	encoded := base + "/.pulumi%2Fmeta.yaml"

	doRequest(t, "PUT", base, "").Body.Close()

	resp := doRequest(t, "PUT", encoded, "hello")
	resp.Body.Close()
	expectStatus(t, resp, 201)

	resp = doRequest(t, "HEAD", encoded, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	if got := resp.Header.Get("Content-Length"); got != "5" {
		t.Fatalf("expected Content-Length=5, got %q", got)
	}
	if got := resp.Header.Get("ETag"); got == "" {
		t.Fatal("expected ETag header")
	}
	if got := resp.Header.Get("Last-Modified"); got == "" {
		t.Fatal("expected Last-Modified header")
	} else if _, err := http.ParseTime(got); err != nil {
		t.Fatalf("expected parseable Last-Modified header, got %q: %v", got, err)
	}
	if got := resp.Header.Get("x-ms-creation-time"); got == "" {
		t.Fatal("expected x-ms-creation-time header")
	} else if _, err := http.ParseTime(got); err != nil {
		t.Fatalf("expected parseable x-ms-creation-time header, got %q: %v", got, err)
	}
	if got := resp.Header.Get("x-ms-blob-type"); got != "BlockBlob" {
		t.Fatalf("expected x-ms-blob-type=BlockBlob, got %q", got)
	}
	if got := resp.Header.Get("x-ms-request-id"); got == "" {
		t.Fatal("expected x-ms-request-id header")
	}
	if !resp.Close {
		t.Fatal("expected HEAD response to close cleanly")
	}
}

// TestBlobNestedPathLease verifies that ?comp=lease on a nested blob path is
// also routed to the blob service handler (handleLease) rather than the ARM
// fallback. This is exactly the third request Terraform's azurerm backend
// issues during Lock().
func TestBlobNestedPathLease(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/blob/myaccount/mycontainer"
	nested := base + "/demo/terraform.tfstate"

	doRequest(t, "PUT", base, "").Body.Close()
	doRequest(t, "PUT", nested, "").Body.Close()

	req, _ := http.NewRequest("PUT", nested+"?comp=lease", nil)
	req.Header.Set("x-ms-lease-action", "acquire")
	req.Header.Set("x-ms-lease-duration", "60")
	req.Header.Set("x-ms-proposed-lease-id", "11111111-1111-1111-1111-111111111111")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Real blob service returns 201 Created with x-ms-lease-id; ARM
	// fallback would return JSON 404 InvalidResourceType.
	expectStatus(t, resp, 201)
	if got := resp.Header.Get("x-ms-lease-id"); got == "" {
		t.Errorf("expected x-ms-lease-id header, got empty")
	}
}

// TestBlobNotFoundTerminatesResponse verifies that HEAD/GET against a
// non-existent blob returns a *fully terminated* 404 response — i.e. the
// client is not left hanging waiting for an unfinished chunked body. This
// regression broke Terraform's azurerm remote-state backend, whose Lock()
// path issues GetProperties (HEAD) on the state blob and treats a clean 404
// as "blob doesn't exist yet, create it".
func TestBlobNotFoundTerminatesResponse(t *testing.T) {
ts := setupServer()
defer ts.Close()
base := ts.URL + "/blob/myaccount/mycontainer"

doRequest(t, "PUT", base, "").Body.Close()

for _, method := range []string{"HEAD", "GET"} {
resp := doRequest(t, method, base+"/missing.txt", "")
expectStatus(t, resp, 404)

if got := resp.Header.Get("x-ms-error-code"); got != "BlobNotFound" {
t.Errorf("%s: expected x-ms-error-code=BlobNotFound, got %q", method, got)
}
if resp.Header.Get("x-ms-request-id") == "" {
t.Errorf("%s: missing x-ms-request-id header", method)
}
// The response MUST be terminated. Either via Content-Length, or — if
// chunked — via the zero-length terminator (which makes io.ReadAll
// return without error). Without termination, this read would block
// until the test timeout.
if _, err := io.ReadAll(resp.Body); err != nil {
t.Errorf("%s: reading body: %v", method, err)
}
resp.Body.Close()

// Explicit Content-Length avoids Go's chunked fallback (which
// silently truncates HEAD responses without a terminator).
if cl := resp.Header.Get("Content-Length"); cl == "" {
t.Errorf("%s: missing Content-Length on 404 response", method)
}
}
}
