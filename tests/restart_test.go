package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moabukar/miniblue/internal/server"
	"github.com/moabukar/miniblue/internal/store"
)

// A restart must not change what clients read back. The in-memory store keeps
// live Go structs, so only a save and reload through the file backend shows
// handlers that type-assert structs, or fields that never reach the snapshot.
//
// Each read is captured before the restart and compared after it. A read with
// blocked set fails today and names the PLAN row that fixes it; delete the
// field when that row lands.

const (
	restartSub = "sub1"
	restartRG  = "rg-restart"
	restartARM = "/subscriptions/" + restartSub + "/resourceGroups/" + restartRG + "/providers/"
	restartAV  = "?api-version=2023-07-01"
)

type restartRead struct {
	name    string
	method  string // GET when empty
	path    string
	body    string
	blocked string
}

type restartCase struct {
	name  string
	setup func(t *testing.T, base string)
	reads []restartRead
}

type restartResponse struct {
	status int
	etag   string
	body   []byte
}

func restartRequest(t *testing.T, base string, r restartRead) restartResponse {
	t.Helper()
	method := r.method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequest(method, base+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return restartResponse{status: resp.StatusCode, etag: resp.Header.Get("ETag"), body: body}
}

// sameBody compares JSON semantically: struct field order before the restart
// differs from the sorted map keys after it.
func sameBody(a, b []byte) bool {
	var x, y interface{}
	if json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil {
		return reflect.DeepEqual(x, y)
	}
	return bytes.Equal(a, b)
}

func startFileServer(t *testing.T, path string) (*httptest.Server, *store.Store) {
	t.Helper()
	fb, err := store.NewFileBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewWithBackend(fb)
	ts := httptest.NewServer(server.NewWithStore(st).Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func restartPut(t *testing.T, url, body string, want int, headers map[string]string) {
	t.Helper()
	resp := doBlobRequest(t, http.MethodPut, url, body, headers)
	resp.Body.Close()
	expectStatus(t, resp, want)
}

const (
	restartBlobBase    = "/blob/restartacct/restartcont"
	restartBlobUpload  = restartBlobBase + "/upload.txt"
	restartBlobCopy    = restartBlobBase + "/copy.txt"
	restartBlobCommit  = restartBlobBase + "/blocks.txt"
	restartBlobPayload = "hello blob"
)

var restartCases = []restartCase{
	{
		name: "resourcegroup",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+"/subscriptions/"+restartSub+"/resourcegroups/"+restartRG+restartAV,
				`{"location":"eastus","tags":{"env":"dev"}}`, 201, map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "get", path: "/subscriptions/" + restartSub + "/resourcegroups/" + restartRG + restartAV},
			{name: "list", path: "/subscriptions/" + restartSub + "/resourcegroups" + restartAV},
		},
	},
	{
		name: "blob",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+restartBlobBase, "", 201, nil)
			restartPut(t, base+restartBlobUpload, restartBlobPayload, 201, map[string]string{"x-ms-blob-type": "BlockBlob"})
			restartPut(t, base+restartBlobCopy, "", 202, map[string]string{"x-ms-copy-source": base + restartBlobUpload})
			restartPut(t, base+restartBlobCommit+"?comp=block&blockid=YQ==", "block a", 201, nil)
			restartPut(t, base+restartBlobCommit+"?comp=blocklist", `<BlockList><Latest>YQ==</Latest></BlockList>`, 201, nil)
		},
		reads: []restartRead{
			{name: "upload", path: restartBlobUpload, blocked: "0.3"},
			{name: "copy", path: restartBlobCopy, blocked: "0.3"},
			{name: "block commit", path: restartBlobCommit, blocked: "0.3"},
			{name: "list", path: restartBlobBase + "?restype=container&comp=list", blocked: "0.3"},
		},
	},
	{
		name: "keyvault vault",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+restartARM+"Microsoft.KeyVault/vaults/restartkv"+restartAV,
				`{"location":"eastus","properties":{"tenantId":"00000000-0000-0000-0000-000000000000","sku":{"family":"A","name":"standard"}}}`,
				201, map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "get", path: restartARM + "Microsoft.KeyVault/vaults/restartkv" + restartAV},
			{name: "list", path: restartARM + "Microsoft.KeyVault/vaults" + restartAV},
		},
	},
	{
		name: "keyvault secret",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+"/keyvault/restartkv/secrets/db-pass", `{"value":"supersecret"}`, 200,
				map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "get", path: "/keyvault/restartkv/secrets/db-pass"},
			{name: "list", path: "/keyvault/restartkv/secrets", blocked: "0.3"},
		},
	},
	{
		name: "acr",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+restartARM+"Microsoft.ContainerRegistry/registries/restartreg"+restartAV,
				`{"location":"eastus","sku":{"name":"Premium","tier":"Premium"}}`, 201,
				map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "get", path: restartARM + "Microsoft.ContainerRegistry/registries/restartreg" + restartAV},
			{name: "list", path: restartARM + "Microsoft.ContainerRegistry/registries" + restartAV},
			{
				name: "name check", method: http.MethodPost,
				path: "/subscriptions/" + restartSub + "/providers/Microsoft.ContainerRegistry/checkNameAvailability" + restartAV,
				body: `{"name":"restartreg","type":"Microsoft.ContainerRegistry/registries"}`,
			},
		},
	},
	{
		name: "appconfig",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+restartARM+"Microsoft.AppConfiguration/configurationStores/restartconfig"+restartAV,
				`{"location":"eastus","sku":{"name":"standard"}}`, 201,
				map[string]string{"Content-Type": "application/json"})
			restartPut(t, base+"/appconfig/restartconfig/kv/db-host", `{"value":"localhost:5432"}`, 200,
				map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "store", path: restartARM + "Microsoft.AppConfiguration/configurationStores/restartconfig" + restartAV},
			{name: "store list", path: restartARM + "Microsoft.AppConfiguration/configurationStores" + restartAV},
			{name: "key value", path: "/appconfig/restartconfig/kv/db-host", blocked: "0.3"},
			{name: "key value list", path: "/appconfig/restartconfig/kv"},
		},
	},
	{
		name: "postgres server",
		setup: func(t *testing.T, base string) {
			restartPut(t, base+restartARM+"Microsoft.DBforPostgreSQL/flexibleServers/restartpg"+restartAV,
				`{"location":"eastus"}`, 201, map[string]string{"Content-Type": "application/json"})
		},
		reads: []restartRead{
			{name: "get", path: restartARM + "Microsoft.DBforPostgreSQL/flexibleServers/restartpg" + restartAV},
			{name: "list", path: restartARM + "Microsoft.DBforPostgreSQL/flexibleServers" + restartAV},
		},
	},
}

func TestRestartRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	ts, st := startFileServer(t, path)
	before := map[string]restartResponse{}
	for _, c := range restartCases {
		c.setup(t, ts.URL)
		for _, r := range c.reads {
			got := restartRequest(t, ts.URL, r)
			if got.status != http.StatusOK {
				t.Fatalf("%s/%s before restart: status %d, body %s", c.name, r.name, got.status, got.body)
			}
			before[c.name+"/"+r.name] = got
		}
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	ts.Close()

	ts, st = startFileServer(t, path)
	for _, c := range restartCases {
		t.Run(c.name, func(t *testing.T) {
			for _, r := range c.reads {
				t.Run(r.name, func(t *testing.T) {
					if r.blocked != "" {
						t.Skip("blocked by PLAN row " + r.blocked)
					}
					want := before[c.name+"/"+r.name]
					got := restartRequest(t, ts.URL, r)
					if got.status != want.status || got.etag != want.etag || !sameBody(got.body, want.body) {
						t.Fatalf("changed after restart\nbefore: %d %q %s\nafter:  %d %q %s",
							want.status, want.etag, want.body, got.status, got.etag, got.body)
					}
				})
			}
		})
	}

	// The handlers cannot read blobs back until row 0.3, so check the
	// reloaded values directly: every payload must have reached the snapshot.
	t.Run("blob content", func(t *testing.T) {
		want := map[string]bool{}
		for _, name := range []string{"upload", "copy", "block commit"} {
			want[string(before["blob/"+name].body)] = true
		}
		blobs := st.ListByPrefix("blob:blob:")
		if len(blobs) != 3 {
			t.Fatalf("expected 3 blobs after restart, got %d", len(blobs))
		}
		for _, v := range blobs {
			stored, _ := v.(map[string]interface{})["content"].(string)
			content, err := base64.StdEncoding.DecodeString(stored)
			if err != nil || len(content) == 0 || !want[string(content)] {
				t.Errorf("blob content after restart = %q (%v), want one of %v", content, err, want)
			}
		}
	})
}
