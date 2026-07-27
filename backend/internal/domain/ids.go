package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

func StableID(prefix, raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return prefix + "-" + hex.EncodeToString(sum[:8])
}

func SourceNodeID(deviceID string) string {
	return "source:" + deviceID
}

func FilterNodeID(deviceApplicationID string) string {
	return "filter:" + deviceApplicationID
}

func TargetNodeID(proxyName string) string {
	return "target:" + StableID("proxy", proxyName)
}
