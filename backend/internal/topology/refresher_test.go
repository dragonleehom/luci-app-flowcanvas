package topology

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestRefresherMergesARPAndDHCPLease(t *testing.T) {
	directory := t.TempDir()
	arpPath := filepath.Join(directory, "arp")
	leasePath := filepath.Join(directory, "dhcp.leases")
	if err := os.WriteFile(arpPath, []byte("IP address       HW type     Flags       HW address            Mask     Device\n192.168.1.50     0x1         0x2         00:11:22:33:44:55     *        br-lan\n"), 0o600); err != nil {
		t.Fatalf("write arp fixture: %v", err)
	}
	if err := os.WriteFile(leasePath, []byte("0 00:11:22:33:44:55 192.168.1.50 LivingRoom-TV *\n"), 0o600); err != nil {
		t.Fatalf("write lease fixture: %v", err)
	}

	store := &recordingDeviceStore{}
	refresher, err := NewRefresher(store, arpPath, leasePath, nil, nil)
	if err != nil {
		t.Fatalf("create topology refresher: %v", err)
	}
	changed, err := refresher.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh topology: %v", err)
	}
	if !changed || len(store.devices) != 1 {
		t.Fatalf("unexpected topology refresh result: changed=%t devices=%+v", changed, store.devices)
	}
	device := store.devices[0]
	if device.Name != "LivingRoom-TV" || device.MAC != "00:11:22:33:44:55" || device.IPAddress != "192.168.1.50" {
		t.Fatalf("unexpected merged device: %+v", device)
	}
}

type recordingDeviceStore struct {
	devices []domain.Device
}

func (s *recordingDeviceStore) UpsertTopologyDevices(_ context.Context, devices []domain.Device, _ time.Time) (bool, error) {
	s.devices = append([]domain.Device(nil), devices...)
	return true, nil
}
