package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type coreClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

type srtChannels struct {
	Publisher   map[string]uint32        `json:"publisher"`
	Subscriber  map[string][]uint32      `json:"subscriber"`
	Connections map[string]srtConnection `json:"connections"`
}

type srtConnection struct {
	Stats srtStats `json:"stats"`
}

type srtStats struct {
	TimestampMs        uint64  `json:"timestamp_ms"`
	RecvPktUnique      uint64  `json:"recv_unique_pkt"`
	RecvLossPkt        uint64  `json:"recv_loss_pkt"`
	RecvRetransPkt     uint64  `json:"recv_retran_pkts"`
	RecvDropPkt        uint64  `json:"recv_drop_pkt"`
	RecvUniqueBytes    uint64  `json:"recv_unique_bytes"`
	RTTMs              float64 `json:"rtt_ms"`
	BandwidthMbit      float64 `json:"bandwidth_mbit"`
	RecvBufferMs       uint64  `json:"recv_buf_ms"`
	RecvTSBPDDelayMs   uint64  `json:"recv_tsbpd_delay_ms"`
}

type telemetrySample struct {
	at    time.Time
	stats srtStats
}

type testManager struct {
	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

func newTestManager() *testManager {
	return &testManager{running: make(map[int64]context.CancelFunc)}
}

func (m *testManager) begin(channelID int64, cancel context.CancelFunc) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.running[channelID]; ok {
		return false
	}
	m.running[channelID] = cancel
	return true
}

func (m *testManager) done(channelID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, channelID)
}

func (m *testManager) cancel(channelID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.running[channelID]
	if !ok {
		return false
	}
	cancel()
	delete(m.running, channelID)
	return true
}

func newCoreClient() *coreClient {
	return &coreClient{
		baseURL:  strings.TrimRight(env("RELIVE_CORE_URL", "http://restreamer:8080"), "/"),
		username: env("RELIVE_CORE_USERNAME", ""),
		password: env("RELIVE_CORE_PASSWORD", ""),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *coreClient) srt(ctx context.Context) (srtChannels, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v3/srt", nil)
	if err != nil {
		return srtChannels{}, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return srtChannels{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return srtChannels{}, fmt.Errorf("core SRT API returned %s", res.Status)
	}

	var out srtChannels
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return srtChannels{}, err
	}
	if out.Publisher == nil {
		out.Publisher = map[string]uint32{}
	}
	if out.Connections == nil {
		out.Connections = map[string]srtConnection{}
	}
	return out, nil
}

func findPublisher(channels srtChannels, streamID string) (srtStats, bool) {
	id, ok := channels.Publisher[streamID]
	if !ok {
		// Core/SRT deployments sometimes expose the stream resource as a URL-like
		// string. Match only an exact final path segment to avoid cross-channel leaks.
		for key, candidateID := range channels.Publisher {
			u, err := url.Parse(key)
			if err == nil && strings.Trim(u.Path, "/") != "" {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if parts[len(parts)-1] == streamID {
					id = candidateID
					ok = true
					break
				}
			}
		}
	}
	if !ok {
		return srtStats{}, false
	}
	conn, ok := channels.Connections[fmt.Sprintf("%d", id)]
	return conn.Stats, ok
}

type telemetryResult struct {
	RTTAvgMs           float64
	RTTMaxMs           float64
	PacketLossPct      float64
	RetransmitPct      float64
	BitrateVariancePct float64
	ReceiveBitrateMbps float64
	ReconnectCount     int
	SampleCount        int
}

func summarizeSamples(samples []telemetrySample, reconnects int) (telemetryResult, error) {
	if len(samples) < 2 {
		return telemetryResult{}, errors.New("not enough SRT telemetry samples")
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].at.Before(samples[j].at) })

	first := samples[0]
	last := samples[len(samples)-1]

	unique := delta64(last.stats.RecvPktUnique, first.stats.RecvPktUnique)
	lost := delta64(last.stats.RecvLossPkt, first.stats.RecvLossPkt)
	retrans := delta64(last.stats.RecvRetransPkt, first.stats.RecvRetransPkt)
	bytes := delta64(last.stats.RecvUniqueBytes, first.stats.RecvUniqueBytes)

	var rttSum, rttMax float64
	bitrates := make([]float64, 0, len(samples)-1)
	for i, s := range samples {
		rttSum += s.stats.RTTMs
		if s.stats.RTTMs > rttMax {
			rttMax = s.stats.RTTMs
		}
		if i == 0 {
			continue
		}
		dt := s.at.Sub(samples[i-1].at).Seconds()
		if dt <= 0 {
			continue
		}
		db := delta64(s.stats.RecvUniqueBytes, samples[i-1].stats.RecvUniqueBytes)
		bitrates = append(bitrates, float64(db)*8/dt/1_000_000)
	}

	total := unique + lost
	lossPct := 0.0
	if total > 0 {
		lossPct = float64(lost) / float64(total) * 100
	}
	retransPct := 0.0
	if unique > 0 {
		retransPct = float64(retrans) / float64(unique) * 100
	}

	duration := last.at.Sub(first.at).Seconds()
	receiveMbps := 0.0
	if duration > 0 {
		receiveMbps = float64(bytes) * 8 / duration / 1_000_000
	}

	return telemetryResult{
		RTTAvgMs:           round2(rttSum / float64(len(samples))),
		RTTMaxMs:           round2(rttMax),
		PacketLossPct:      round2(lossPct),
		RetransmitPct:      round2(retransPct),
		BitrateVariancePct: round2(coefficientOfVariation(bitrates)),
		ReceiveBitrateMbps: round2(receiveMbps),
		ReconnectCount:     reconnects,
		SampleCount:        len(samples),
	}, nil
}

func delta64(last, first uint64) uint64 {
	if last < first {
		return 0
	}
	return last - first
}

func coefficientOfVariation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	if mean == 0 {
		return 0
	}
	var sq float64
	for _, v := range values {
		d := v - mean
		sq += d * d
	}
	std := math.Sqrt(sq / float64(len(values)))
	return std / mean * 100
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
