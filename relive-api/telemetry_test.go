package main

import (
	"testing"
	"time"
)

func TestSummarizeSamples(t *testing.T) {
	t0 := time.Unix(1000, 0)
	samples := []telemetrySample{
		{at: t0, stats: srtStats{
			RecvPktUnique: 1000, RecvLossPkt: 10, RecvRetransPkt: 20,
			RecvUniqueBytes: 1_000_000, RTTMs: 80,
		}},
		{at: t0.Add(2 * time.Second), stats: srtStats{
			RecvPktUnique: 2000, RecvLossPkt: 20, RecvRetransPkt: 30,
			RecvUniqueBytes: 2_250_000, RTTMs: 100,
		}},
		{at: t0.Add(4 * time.Second), stats: srtStats{
			RecvPktUnique: 3000, RecvLossPkt: 30, RecvRetransPkt: 40,
			RecvUniqueBytes: 3_500_000, RTTMs: 120,
		}},
	}

	got, err := summarizeSamples(samples, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.RTTAvgMs != 100 || got.RTTMaxMs != 120 {
		t.Fatalf("unexpected RTT summary: %+v", got)
	}
	if got.ReceiveBitrateMbps != 5 {
		t.Fatalf("expected 5 Mbps receive bitrate, got %.2f", got.ReceiveBitrateMbps)
	}
	if got.SampleCount != 3 {
		t.Fatalf("expected 3 samples, got %d", got.SampleCount)
	}
}

func TestFindPublisherExact(t *testing.T) {
	ch := srtChannels{
		Publisher: map[string]uint32{"customer-001-main": 42},
		Connections: map[string]srtConnection{
			"42": {Stats: srtStats{RTTMs: 77}},
		},
	}
	stats, ok := findPublisher(ch, "customer-001-main")
	if !ok || stats.RTTMs != 77 {
		t.Fatalf("publisher lookup failed: ok=%v stats=%+v", ok, stats)
	}
}
