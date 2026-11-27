package networkinterfaces

import (
	"errors"
	"strings"
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

type failPrefixScanBackend struct {
	*store.MemoryBackend
	failPrefix string
	err        error
}

func (b *failPrefixScanBackend) Scan(prefix string) ([]store.KV, error) {
	if strings.HasPrefix(prefix, b.failPrefix) {
		return nil, b.err
	}
	return b.MemoryBackend.Scan(prefix)
}

func TestRefreshReferencesAbortsOnScanError(t *testing.T) {
	mem := store.NewMemoryBackend()
	scanErr := errors.New("nic scan failed")
	s := store.NewWithBackend(&failPrefixScanBackend{
		MemoryBackend: mem,
		failPrefix:    "nic:",
		err:           scanErr,
	})

	subnetKey := "subnet:sub1:rg1:vnet1:web"
	if err := s.Set(subnetKey, map[string]interface{}{
		"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/web",
		"properties": map[string]interface{}{
			"ipConfigurations": []interface{}{
				map[string]interface{}{"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1/ipConfigurations/ipconfig1"},
			},
		},
	}); err != nil {
		t.Fatalf("seed subnet: %v", err)
	}

	h := NewHandler(s)
	if err := h.refreshReferences("sub1", "rg1"); !errors.Is(err, scanErr) {
		t.Fatalf("refreshReferences err = %v, want %v", err, scanErr)
	}

	raw, ok := s.Get(subnetKey)
	if !ok {
		t.Fatalf("subnet disappeared")
	}
	configs := raw.(map[string]interface{})["properties"].(map[string]interface{})["ipConfigurations"].([]interface{})
	if len(configs) != 1 {
		t.Fatalf("subnet ipConfigurations cleared on scan error: got %d, want 1", len(configs))
	}
}
