package citizen

import "testing"

func TestPowerAwareMode(t *testing.T) {
	low := SelectMode(PowerState{BatteryPercent: 10, AppActive: true, WiFi: true})
	if !low.VerifyHeaders || low.Relay || low.SampleDA || low.ExecuteRecent {
		t.Fatalf("unexpected low-power mode: %+v", low)
	}
	full := SelectMode(PowerState{BatteryPercent: 80, AppActive: true, WiFi: true, Charging: true})
	if !full.VerifyHeaders || !full.Relay || !full.SampleDA || !full.ExecuteRecent || !full.ServeCache {
		t.Fatalf("unexpected full mode: %+v", full)
	}
}
