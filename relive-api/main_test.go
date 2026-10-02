package main

import "testing"

func baseChannel() channel {
	return channel{
		VideoBitrateKbps: 5500,
		AudioBitrateKbps: 192,
		SRTLatencyMs:     750,
		Resolution:       "1920x1080",
		FPS:              50,
	}
}

func TestRecommendKeepKnownGoodProfile(t *testing.T) {
	rec := recommend(baseChannel(), testInput{
		RTTAvgMs: 90, RTTMaxMs: 150,
		PacketLossPct: 0.5,
		RetransmitPct: 0.8,
		BitrateVariancePct: 5,
	})
	if rec.Result != "KEEP" {
		t.Fatalf("expected KEEP, got %s", rec.Result)
	}
	if rec.RecommendedVideoKbps != 5500 || rec.RecommendedSRTLatencyMs != 750 {
		t.Fatalf("known-good profile should remain unchanged: %+v", rec)
	}
}

func TestRecommendIncreaseLatencyBeforeBitrateReduction(t *testing.T) {
	rec := recommend(baseChannel(), testInput{
		RTTAvgMs: 120, RTTMaxMs: 280,
		PacketLossPct: 1.2,
		RetransmitPct: 1.8,
		BitrateVariancePct: 10,
	})
	if rec.Result != "INCREASE_LATENCY" {
		t.Fatalf("expected INCREASE_LATENCY, got %s", rec.Result)
	}
	if rec.RecommendedVideoKbps != 5500 {
		t.Fatalf("bitrate should be preserved before reduction, got %d", rec.RecommendedVideoKbps)
	}
	if rec.RecommendedSRTLatencyMs < 1000 {
		t.Fatalf("expected >=1000ms latency, got %d", rec.RecommendedSRTLatencyMs)
	}
}

func TestRecommendReduceBitrateOnUnstableCellular(t *testing.T) {
	rec := recommend(baseChannel(), testInput{
		RTTAvgMs: 160, RTTMaxMs: 340,
		PacketLossPct: 3.4,
		RetransmitPct: 5.5,
		BitrateVariancePct: 22,
	})
	if rec.Result != "REDUCE_BITRATE" {
		t.Fatalf("expected REDUCE_BITRATE, got %s", rec.Result)
	}
	if rec.RecommendedVideoKbps >= 5500 {
		t.Fatalf("expected reduced bitrate, got %d", rec.RecommendedVideoKbps)
	}
	if rec.RecommendedSRTLatencyMs < 1200 {
		t.Fatalf("expected >=1200ms latency, got %d", rec.RecommendedSRTLatencyMs)
	}
}

func TestRecommendSafeProfileOnPoorNetwork(t *testing.T) {
	rec := recommend(baseChannel(), testInput{
		RTTAvgMs: 250, RTTMaxMs: 500,
		PacketLossPct: 6.0,
		RetransmitPct: 10,
		BitrateVariancePct: 35,
	})
	if rec.Result != "SAFE_PROFILE" {
		t.Fatalf("expected SAFE_PROFILE, got %s", rec.Result)
	}
	if rec.RecommendedSRTLatencyMs < 1500 {
		t.Fatalf("expected >=1500ms latency, got %d", rec.RecommendedSRTLatencyMs)
	}
}

func TestAudioDropFlagsReviewWithoutNetworkDegradation(t *testing.T) {
	rec := recommend(baseChannel(), testInput{
		RTTAvgMs: 80, RTTMaxMs: 140,
		PacketLossPct: 0.2,
		RetransmitPct: 0.3,
		BitrateVariancePct: 4,
		AudioDropCount: 1,
	})
	if rec.Result != "REVIEW" {
		t.Fatalf("expected REVIEW, got %s", rec.Result)
	}
	if rec.NetworkHealth != "UNSTABLE" {
		t.Fatalf("expected UNSTABLE, got %s", rec.NetworkHealth)
	}
}
