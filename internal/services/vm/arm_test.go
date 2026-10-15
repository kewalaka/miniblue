package vm

import "testing"

func TestImageOSAndDefaultDiskSize(t *testing.T) {
	tests := []struct {
		name         string
		publisher    string
		offer        string
		sku          string
		wantOS       string
		wantDiskSize float64
	}{
		{"catalog linux", "Canonical", "0001-com-ubuntu-server-jammy", "22_04-lts", "Linux", 30},
		{"unknown linux", "RedHat", "RHEL", "9-lvm-gen2", "Linux", 30},
		{"empty reference", "", "", "", "Linux", 30},
		{"catalog windows", "MicrosoftWindowsServer", "WindowsServer", "2019-Datacenter", "Windows", 127},
		{"windows server outside catalog", "MicrosoftWindowsServer", "WindowsServer", "2022-datacenter", "Windows", 127},
		{"windows server smalldisk", "MicrosoftWindowsServer", "WindowsServer", "2022-datacenter-smalldisk", "Windows", 30},
		{"windows desktop", "MicrosoftWindowsDesktop", "windows-11", "win11-23h2-pro", "Windows", 127},
		{"publisher is case-insensitive", "microsoftwindowsserver", "windowsserver", "2025-datacenter", "Windows", 127},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOS, gotSize := imageOSAndDefaultDiskSize(map[string]interface{}{
				"publisher": tt.publisher, "offer": tt.offer, "sku": tt.sku,
			})
			if gotOS != tt.wantOS || gotSize != tt.wantDiskSize {
				t.Fatalf("got (%s, %v), want (%s, %v)", gotOS, gotSize, tt.wantOS, tt.wantDiskSize)
			}
		})
	}
}
