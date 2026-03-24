package api

import (
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type structuredEventLogger struct {
	enabled          bool
	logger           *log.Logger
	nodeID           string
	validatorAddress string
}

func newStructuredEventLogger(config Config) *structuredEventLogger {
	if !config.EnableStructuredLogs {
		return &structuredEventLogger{}
	}

	writer := config.StructuredLogWriter
	if writer == nil {
		writer = log.Writer()
	}
	if writer == nil {
		writer = io.Discard
	}

	return &structuredEventLogger{
		enabled:          true,
		logger:           log.New(writer, "", 0),
		nodeID:           config.NodeID,
		validatorAddress: config.ValidatorAddress,
	}
}

func (l *structuredEventLogger) logConsensusDiagnostic(diagnostic ledger.ConsensusDiagnostic) {
	l.logEvent("consensus", "diagnostic", diagnostic.ObservedAt, map[string]any{
		"kind":       diagnostic.Kind,
		"code":       diagnostic.Code,
		"message":    diagnostic.Message,
		"height":     diagnostic.Height,
		"round":      diagnostic.Round,
		"blockHash":  diagnostic.BlockHash,
		"validator":  diagnostic.Validator,
		"source":     diagnostic.Source,
		"observedAt": diagnostic.ObservedAt,
	})
}

func (l *structuredEventLogger) logPeerIncident(incident ledger.PeerSyncIncident) {
	timestamp := incident.LastObservedAt
	if timestamp.IsZero() {
		timestamp = incident.FirstObservedAt
	}
	l.logEvent("peer_sync", "incident", timestamp, map[string]any{
		"peerUrl":         incident.PeerURL,
		"state":           incident.State,
		"reason":          incident.Reason,
		"localHeight":     incident.LocalHeight,
		"peerHeight":      incident.PeerHeight,
		"heightDelta":     incident.HeightDelta,
		"blockHash":       incident.BlockHash,
		"errorCode":       incident.ErrorCode,
		"errorMessage":    incident.ErrorMessage,
		"firstObservedAt": incident.FirstObservedAt,
		"lastObservedAt":  incident.LastObservedAt,
		"occurrences":     incident.Occurrences,
		"peerUrlKey":      incident.PeerURL,
	})
}

func (l *structuredEventLogger) logSnapshotRestore(peerLabel string, height uint64, blockHash string, restoredAt time.Time) {
	l.logEvent("recovery", "snapshot_restore", restoredAt, map[string]any{
		"peer":       peerLabel,
		"height":     height,
		"blockHash":  blockHash,
		"restoredAt": restoredAt,
	})
}

func (l *structuredEventLogger) logEvent(component string, event string, timestamp time.Time, fields map[string]any) {
	if l == nil || !l.enabled || l.logger == nil {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	} else {
		timestamp = timestamp.UTC()
	}

	entry := map[string]any{
		"timestamp": timestamp,
		"level":     "info",
		"component": component,
		"event":     event,
		"nodeId":    l.nodeID,
	}
	if l.validatorAddress != "" {
		entry["validatorAddress"] = l.validatorAddress
	}
	for key, value := range fields {
		switch typed := value.(type) {
		case string:
			if typed == "" {
				continue
			}
			entry[key] = typed
		case time.Time:
			if typed.IsZero() {
				continue
			}
			entry[key] = typed.UTC()
		default:
			entry[key] = value
		}
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		l.logger.Printf("zephyr structured-log marshal error: %v", err)
		return
	}
	l.logger.Print(string(payload))
}
