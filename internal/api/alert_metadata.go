package api

func peerSnapshotRestoreAlertCodes() []string {
	return []string{
		"peer_snapshot_restored",
		"peer_snapshot_restore_divergence",
		"peer_snapshot_restore_import_repair",
		"peer_snapshot_restore_fetch_fallback",
	}
}

func peerIncidentAlertCodes() []string {
	return append([]string{
		"peer_import_blocked",
		"peer_admission_blocked",
		"peer_replication_blocked",
	}, peerSnapshotRestoreAlertCodes()...)
}

func peerIncidentContinuityAlertCodes() []string {
	return append([]string{
		"peer_sync_degraded",
		"peer_sync_unavailable",
	}, peerIncidentAlertCodes()...)
}
