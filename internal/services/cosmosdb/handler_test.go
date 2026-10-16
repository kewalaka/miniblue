package cosmosdb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/store"
)

const (
	testSub  = "sub1"
	testRG   = "rg1"
	testAcct = "mycosmosacct"
)

func setupCosmosHandlerServer() *httptest.Server {
	r := chi.NewRouter()
	NewHandler(store.New()).Register(r)
	return httptest.NewServer(r)
}

func cosmosAccountBase(ts *httptest.Server, account string) string {
	return ts.URL + "/subscriptions/" + testSub + "/resourceGroups/" + testRG + "/providers/Microsoft.DocumentDB/databaseAccounts/" + account
}

func doCosmosRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, url, bytes.NewBufferString(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeCosmosJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAccountOperations(t *testing.T) {
	ts := setupCosmosHandlerServer()
	defer ts.Close()
	base := cosmosAccountBase(ts, testAcct)

	resp := doCosmosRequest(t, http.MethodPut, base, `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected account create status 200, got %d", resp.StatusCode)
	}

	for _, op := range []string{"listKeys", "readonlykeys", "listConnectionStrings", "regenerateKey"} {
		resp := doCosmosRequest(t, http.MethodPost, base+"/"+op, `{"keyKind":"primary"}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for %s, got %d", op, resp.StatusCode)
		}
		body := decodeCosmosJSON(t, resp)
		resp.Body.Close()
		if len(body) == 0 {
			t.Fatalf("expected non-empty response for %s", op)
		}
	}

	resp = doCosmosRequest(t, http.MethodPost, base+"/listKeys", "")
	keys := decodeCosmosJSON(t, resp)
	resp.Body.Close()
	for _, field := range []string{"primaryMasterKey", "secondaryMasterKey", "primaryReadonlyMasterKey", "secondaryReadonlyMasterKey"} {
		if keys[field] == "" {
			t.Fatalf("expected %s in listKeys response", field)
		}
	}

	resp = doCosmosRequest(t, http.MethodPost, base+"/listConnectionStrings", "")
	body := decodeCosmosJSON(t, resp)
	resp.Body.Close()
	strings, ok := body["connectionStrings"].([]interface{})
	if !ok || len(strings) == 0 {
		t.Fatalf("expected non-empty connectionStrings array, got %v", body["connectionStrings"])
	}
	first := strings[0].(map[string]interface{})
	if first["connectionString"] == "" || first["description"] == "" {
		t.Fatalf("expected connectionString and description, got %v", first)
	}
}

func TestAccountOperationsNotFound(t *testing.T) {
	ts := setupCosmosHandlerServer()
	defer ts.Close()
	base := cosmosAccountBase(ts, "nonexistent")

	for _, op := range []string{"listKeys", "readonlykeys", "listConnectionStrings", "regenerateKey"} {
		resp := doCosmosRequest(t, http.MethodPost, base+"/"+op, `{"keyKind":"primary"}`)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status 404 for %s, got %d", op, resp.StatusCode)
		}
		body := decodeCosmosJSON(t, resp)
		resp.Body.Close()
		errBody := body["error"].(map[string]interface{})
		if errBody["code"] != "ResourceNotFound" {
			t.Fatalf("expected ResourceNotFound for %s, got %v", op, errBody["code"])
		}
	}
}
