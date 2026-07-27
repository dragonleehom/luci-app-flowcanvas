package domain

import "time"

type NodeKind string

const (
	NodeKindSource NodeKind = "source"
	NodeKindFilter NodeKind = "filter"
	NodeKindTarget NodeKind = "target"
)

type ResourceState string

const (
	StateActive   ResourceState = "active"
	StateInactive ResourceState = "inactive"
	StateUnknown  ResourceState = "unknown"
)

type Network string

const (
	NetworkTCP     Network = "tcp"
	NetworkUDP     Network = "udp"
	NetworkQUIC    Network = "quic"
	NetworkUnknown Network = "unknown"
)

type MatchKind string

const (
	MatchDomain  MatchKind = "domain"
	MatchSuffix  MatchKind = "suffix"
	MatchKeyword MatchKind = "keyword"
)

type MatchSpec struct {
	Kind  MatchKind `json:"kind"`
	Value string    `json:"value"`
}

type Device struct {
	ID        string        `json:"id"`
	IPAddress string        `json:"ipAddress"`
	MAC       string        `json:"mac,omitempty"`
	Name      string        `json:"name"`
	Hostname  string        `json:"hostname,omitempty"`
	State     ResourceState `json:"state"`
	FirstSeen time.Time     `json:"firstSeenAt"`
	LastSeen  time.Time     `json:"lastSeenAt"`
}

type Application struct {
	ID           string        `json:"id"`
	ObservedHost string        `json:"observedHost"`
	Match        MatchSpec     `json:"match"`
	State        ResourceState `json:"state"`
	FirstSeen    time.Time     `json:"firstSeenAt"`
	LastSeen     time.Time     `json:"lastSeenAt"`
}

type DeviceApplication struct {
	ID                string        `json:"id"`
	DeviceID          string        `json:"deviceId"`
	ApplicationID     string        `json:"applicationId"`
	ObservedHost      string        `json:"observedHost"`
	Network           Network       `json:"network"`
	TransportHint     string        `json:"transportHint,omitempty"`
	DestinationIP     string        `json:"destinationIP,omitempty"`
	DestinationPort   uint16        `json:"destinationPort,omitempty"`
	State             ResourceState `json:"state"`
	ActiveConnections int           `json:"activeConnections"`
	Match             MatchSpec     `json:"match"`
	FirstSeen         time.Time     `json:"firstSeenAt"`
	LastSeen          time.Time     `json:"lastSeenAt"`
}

type Target struct {
	ID        string        `json:"id"`
	ProxyName string        `json:"proxyName"`
	ProxyType string        `json:"proxyType"`
	Alive     bool          `json:"alive"`
	UDP       bool          `json:"udp"`
	State     ResourceState `json:"state"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type SourceNodeData struct {
	DeviceID   string        `json:"deviceId"`
	Label      string        `json:"label"`
	IP         string        `json:"ip"`
	MAC        string        `json:"mac,omitempty"`
	State      ResourceState `json:"state"`
	LastSeenAt time.Time     `json:"lastSeenAt"`
}

type FilterNodeData struct {
	DeviceApplicationID string        `json:"deviceApplicationId"`
	DeviceID            string        `json:"deviceId"`
	ObservedHost        string        `json:"observedHost"`
	Network             Network       `json:"network"`
	TransportHint       string        `json:"transportHint,omitempty"`
	State               ResourceState `json:"state"`
	ActiveConnections   int           `json:"activeConnections"`
	Match               MatchSpec     `json:"match"`
	FirstSeenAt         time.Time     `json:"firstSeenAt"`
	LastSeenAt          time.Time     `json:"lastSeenAt"`
}

type TargetNodeData struct {
	ProxyName string        `json:"proxyName"`
	ProxyType string        `json:"proxyType"`
	Alive     bool          `json:"alive"`
	UDP       bool          `json:"udp"`
	State     ResourceState `json:"state"`
}

type CanvasNode struct {
	ID       string   `json:"id"`
	Kind     NodeKind `json:"kind"`
	Position Position `json:"position"`
	Data     any      `json:"data"`
}

type EdgeKind string

const (
	EdgeSourceToFilter EdgeKind = "source_to_filter"
	EdgeFilterToTarget EdgeKind = "filter_to_target"
)

type CanvasEdge struct {
	ID     string   `json:"id"`
	Source string   `json:"source"`
	Target string   `json:"target"`
	Kind   EdgeKind `json:"kind"`
}

type CanvasMetadata struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Revision  int64     `json:"revision"`
	ETag      string    `json:"etag"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DiscoveryStatus struct {
	ConnectionsUpdatedAt time.Time `json:"connectionsUpdatedAt,omitempty"`
	DevicesUpdatedAt     time.Time `json:"devicesUpdatedAt,omitempty"`
	TargetsUpdatedAt     time.Time `json:"targetsUpdatedAt,omitempty"`
}

type CanvasSnapshot struct {
	Canvas    CanvasMetadata  `json:"canvas"`
	Nodes     []CanvasNode    `json:"nodes"`
	Edges     []CanvasEdge    `json:"edges"`
	Discovery DiscoveryStatus `json:"discovery"`
}

type NodePosition struct {
	ID       string   `json:"id"`
	Position Position `json:"position"`
}

type GraphSaveRequest struct {
	NodePositions []NodePosition `json:"nodePositions"`
	Edges         []CanvasEdge   `json:"edges"`
}

type ObservedFeature struct {
	ConnectionID       string
	SourceIP           string
	DestinationIP      string
	DestinationPort    uint16
	ObservedHost       string
	Network            Network
	TransportHint      string
	OpenedAt           time.Time
	ObservedAt         time.Time
	UploadBytes        int64
	DownloadBytes      int64
	ProxyChain         []string
	MatchedRule        string
	MatchedRulePayload string
}

type FeatureEventKind string

const (
	FeatureObserved FeatureEventKind = "observed"
	FeatureClosed   FeatureEventKind = "closed"
)

type FeatureEvent struct {
	Kind    FeatureEventKind
	Feature ObservedFeature
}

type CompilationStatus string

const (
	CompilationDraft      CompilationStatus = "draft"
	CompilationValidated  CompilationStatus = "validated"
	CompilationApplied    CompilationStatus = "applied"
	CompilationFailed     CompilationStatus = "failed"
	CompilationRolledBack CompilationStatus = "rolled_back"
)

type RollbackStatus string

const (
	RollbackNotNeeded RollbackStatus = "not_needed"
	RollbackRestored  RollbackStatus = "restored"
	RollbackFailed    RollbackStatus = "rollback_failed"
)

type CompiledProvider struct {
	Name       string   `json:"name"`
	TargetName string   `json:"targetName"`
	Payload    []string `json:"payload"`
}

type CompilationPreview struct {
	CanvasID       string             `json:"canvasId"`
	CanvasRevision int64              `json:"canvasRevision"`
	Providers      []CompiledProvider `json:"providers"`
	Rules          []string           `json:"rules"`
	ManagedYAML    string             `json:"managedYaml"`
	ContentHash    string             `json:"contentHash"`
	Warnings       []string           `json:"warnings"`
}

type CompilationRecord struct {
	ID             string            `json:"id"`
	CanvasID       string            `json:"canvasId"`
	CanvasRevision int64             `json:"canvasRevision"`
	Status         CompilationStatus `json:"status"`
	ManagedYAML    string            `json:"managedYaml,omitempty"`
	ConfigHash     string            `json:"configHash,omitempty"`
	ErrorMessage   string            `json:"errorMessage,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	AppliedAt      *time.Time        `json:"appliedAt,omitempty"`
}

type CompilationRollback struct {
	ID                  string         `json:"id"`
	CompilationID       string         `json:"compilationId"`
	PriorConfigHash     string         `json:"priorConfigHash"`
	CandidateConfigHash string         `json:"candidateConfigHash"`
	BackupPath          string         `json:"backupPath"`
	Status              RollbackStatus `json:"status"`
	ErrorMessage        string         `json:"errorMessage,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	RestoredAt          *time.Time     `json:"restoredAt,omitempty"`
}

type CompilationResult struct {
	Compilation CompilationRecord    `json:"compilation"`
	Preview     CompilationPreview   `json:"preview"`
	Rollback    *CompilationRollback `json:"rollback,omitempty"`
}
