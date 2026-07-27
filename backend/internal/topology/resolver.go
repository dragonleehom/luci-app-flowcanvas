package topology

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type Neighbor struct {
	IP        string
	MAC       string
	Interface string
}

type Lease struct {
	IP       string
	MAC      string
	Hostname string
	Expires  time.Time
}

func ReadARP(path string) ([]Neighbor, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open arp table: %w", err)
	}
	defer file.Close()

	var neighbors []Neighbor
	scanner := bufio.NewScanner(file)
	firstLine := true
	for scanner.Scan() {
		if firstLine {
			firstLine = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}
		ip, err := netip.ParseAddr(fields[0])
		if err != nil || !ip.IsValid() || strings.EqualFold(fields[3], "00:00:00:00:00:00") {
			continue
		}
		neighbors = append(neighbors, Neighbor{
			IP:        ip.String(),
			MAC:       strings.ToLower(fields[3]),
			Interface: fields[5],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan arp table: %w", err)
	}
	return neighbors, nil
}

func ReadDnsmasqLeases(path string, now time.Time) ([]Lease, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open dnsmasq lease file: %w", err)
	}
	defer file.Close()

	leasesByIP := make(map[string]Lease)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		ip, err := netip.ParseAddr(fields[2])
		if err != nil || !ip.IsValid() {
			continue
		}
		expires, err := parseLeaseExpiry(fields[0], now)
		if err != nil || (!expires.IsZero() && expires.Before(now)) {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		leasesByIP[ip.String()] = Lease{
			IP:       ip.String(),
			MAC:      strings.ToLower(fields[1]),
			Hostname: hostname,
			Expires:  expires,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan dnsmasq lease file: %w", err)
	}
	leases := make([]Lease, 0, len(leasesByIP))
	for _, lease := range leasesByIP {
		leases = append(leases, lease)
	}
	return leases, nil
}

func BuildDevices(neighbors []Neighbor, leases []Lease, observedAt time.Time) []domain.Device {
	leasesByIP := make(map[string]Lease, len(leases))
	for _, lease := range leases {
		leasesByIP[lease.IP] = lease
	}
	devices := make([]domain.Device, 0, len(neighbors))
	for _, neighbor := range neighbors {
		lease := leasesByIP[neighbor.IP]
		mac := neighbor.MAC
		if lease.MAC != "" {
			mac = lease.MAC
		}
		name := lease.Hostname
		if name == "" {
			name = "设备 " + neighbor.IP
		}
		devices = append(devices, domain.Device{
			ID:        domain.StableID("dev", neighbor.IP+"|"+mac),
			IPAddress: neighbor.IP,
			MAC:       mac,
			Name:      name,
			Hostname:  lease.Hostname,
			State:     domain.StateActive,
			FirstSeen: observedAt.UTC(),
			LastSeen:  observedAt.UTC(),
		})
	}
	return devices
}

func parseLeaseExpiry(raw string, now time.Time) (time.Time, error) {
	if raw == "0" {
		return time.Time{}, nil
	}
	var unix int64
	if _, err := fmt.Sscan(raw, &unix); err != nil {
		return time.Time{}, err
	}
	return time.Unix(unix, 0).UTC(), nil
}
