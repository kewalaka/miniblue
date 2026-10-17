package keyvault

import (
	"testing"

	"github.com/moabukar/miniblue/internal/store"
)

func TestCleanupRGMatchesVaultNameCaseInsensitively(t *testing.T) {
	s := store.NewWithBackend(store.NewMemoryBackend())
	h := NewHandler(s)

	// The vault record is keyed lower-case; the data plane keeps whatever
	// spelling the client used.
	s.Set(h.vaultKey("sub1", "rg1", "MyVault"), map[string]interface{}{"name": "myvault"})
	s.Set(h.vaultKey("sub1", "rg2", "kvother"), map[string]interface{}{"name": "kvother"})

	doomed := []string{
		h.key("MyVault", "a"), h.key("myvault", "a"), h.key("MYVAULT", "b"),
		h.versionKey("MyVault", "a", "v1"), h.versionKey("myvault", "a", "v2"),
		h.deletedKey("MyVault", "old"), h.deletedKey("myvault", "old"),
	}
	kept := []string{
		h.key("kvother", "a"),
		h.versionKey("kvother", "a", "v1"),
		h.deletedKey("kvother", "old"),
		// A vault whose name merely starts with a doomed one is not ours.
		h.key("myvault2", "a"),
		h.versionKey("myvault2", "a", "v1"),
		h.deletedKey("myvault2", "old"),
	}
	for _, k := range append(append([]string{}, doomed...), kept...) {
		s.Set(k, "x")
	}

	CleanupRG(s, "sub1", "rg1")

	for _, k := range doomed {
		if s.Exists(k) {
			t.Errorf("%s should have been removed", k)
		}
	}
	for _, k := range kept {
		if !s.Exists(k) {
			t.Errorf("%s should have been kept", k)
		}
	}
	if s.Exists(h.vaultKey("sub1", "rg1", "myvault")) {
		t.Error("vault record should have been removed")
	}
	if !s.Exists(h.vaultKey("sub1", "rg2", "kvother")) {
		t.Error("vault in another resource group should have been kept")
	}
}

func TestCleanupRGWithoutVaultsKeepsSecrets(t *testing.T) {
	s := store.NewWithBackend(store.NewMemoryBackend())
	h := NewHandler(s)
	s.Set(h.key("orphan", "a"), "x")

	CleanupRG(s, "sub1", "empty")

	if !s.Exists(h.key("orphan", "a")) {
		t.Error("secrets of vaults outside the resource group should be kept")
	}
}
