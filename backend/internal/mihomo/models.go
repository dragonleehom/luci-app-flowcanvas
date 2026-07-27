package mihomo

import (
	"net/netip"
	"strings"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type ConnectionSnapshot struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Memory        int64        `json:"memory"`
	Connections   []Connection `json:"connections"`
}

type Connection struct {
	ID             string    `json:"id"`
	Metadata       Metadata  `json:"metadata"`
	Upload         int64     `json:"upload"`
	Download       int64     `json:"download"`
	Start          time.Time `json:"start"`
	Chains         []string  `json:"chains"`
	ProviderChains []string  `json:"providerChains"`
	Rule           string    `json:"rule"`
	RulePayload    string    `json:"rulePayload"`
}

type Metadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	SourcePort        uint16 `json:"sourcePort"`
	DestinationIP     string `json:"destinationIP"`
	DestinationPort   uint16 `json:"destinationPort"`
	Host              string `json:"host"`
	SniffHost         string `json:"sniffHost"`
	RemoteDestination string `json:"remoteDestination"`
	SniffType         string `json:"sniffType"`
}

type ProxyCatalogResponse struct {
	Proxies map[string]Proxy `json:"proxies"`
}

type Proxy struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	UDP   bool   `json:"udp"`
	Alive bool   `json:"alive"`
}

func NormalizeConnection(connection Connection, observedAt time.Time) (domain.ObservedFeature, bool) {
	if connection.ID == "" {
		return domain.ObservedFeature{}, false
	}
	sourceIP, err := netip.ParseAddr(connection.Metadata.SourceIP)
	if err != nil || !sourceIP.IsValid() {
		return domain.ObservedFeature{}, false
	}
	host := canonicalHost(connection.Metadata.SniffHost)
	if host == "" {
		host = canonicalHost(connection.Metadata.Host)
	}
	if host == "" {
		return domain.ObservedFeature{}, false
	}

	network, transportHint := normalizeNetwork(connection.Metadata.Network, connection.Metadata.SniffType)
	if network == domain.NetworkUnknown {
		return domain.ObservedFeature{}, false
	}

	destinationIP := ""
	if parsed, err := netip.ParseAddr(connection.Metadata.DestinationIP); err == nil && parsed.IsValid() {
		destinationIP = parsed.String()
	}
	openedAt := connection.Start.UTC()
	if openedAt.IsZero() {
		openedAt = observedAt.UTC()
	}
	proxyChain := connection.Chains
	if len(proxyChain) == 0 {
		proxyChain = connection.ProviderChains
	}

	return domain.ObservedFeature{
		ConnectionID:       connection.ID,
		SourceIP:           sourceIP.String(),
		DestinationIP:      destinationIP,
		DestinationPort:    connection.Metadata.DestinationPort,
		ObservedHost:       host,
		Network:            network,
		TransportHint:      transportHint,
		OpenedAt:           openedAt,
		ObservedAt:         observedAt.UTC(),
		UploadBytes:        connection.Upload,
		DownloadBytes:      connection.Download,
		ProxyChain:         proxyChain,
		MatchedRule:        connection.Rule,
		MatchedRulePayload: connection.RulePayload,
	}, true
}

func TargetsFromCatalog(catalog ProxyCatalogResponse) []domain.Target {
	targets := make([]domain.Target, 0, len(catalog.Proxies))
	for fallbackName, proxy := range catalog.Proxies {
		name := proxy.Name
		if name == "" {
			name = fallbackName
		}
		if name == "" {
			continue
		}
		state := domain.StateInactive
		if proxy.Alive {
			state = domain.StateActive
		}
		targets = append(targets, domain.Target{
			ID:        domain.TargetNodeID(name),
			ProxyName: name,
			ProxyType: proxy.Type,
			Alive:     proxy.Alive,
			UDP:       proxy.UDP,
			State:     state,
		})
	}
	return targets
}

func canonicalHost(raw string) string {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " /\\@") {
		return ""
	}
	if addr, err := netip.ParseAddr(host); err == nil && addr.IsValid() {
		return ""
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ""
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return ""
		}
	}
	return host
}

func normalizeNetwork(rawNetwork, rawSniffType string) (domain.Network, string) {
	network := strings.ToLower(strings.TrimSpace(rawNetwork))
	sniffType := strings.ToLower(strings.TrimSpace(rawSniffType))
	switch network {
	case "tcp":
		if sniffType == "tls" || sniffType == "http" {
			return domain.NetworkTCP, sniffType
		}
		return domain.NetworkTCP, ""
	case "udp":
		if sniffType == "quic" {
			return domain.NetworkQUIC, "quic"
		}
		return domain.NetworkUDP, ""
	case "quic":
		return domain.NetworkQUIC, "quic"
	default:
		return domain.NetworkUnknown, ""
	}
}
