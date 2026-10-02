package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type app struct {
	db        *sql.DB
	core      *coreClient
	tests     *testManager
	testFor   time.Duration
	sampleFor time.Duration
}

type channel struct {
	ID                 int64     `json:"id"`
	OwnerID            string    `json:"owner_id"`
	Name               string    `json:"name"`
	IngestProtocol     string    `json:"ingest_protocol"`
	IngestHost         string    `json:"ingest_host"`
	IngestPort         int       `json:"ingest_port"`
	StreamID           string    `json:"stream_id"`
	Resolution         string    `json:"resolution"`
	FPS                int       `json:"fps"`
	VideoBitrateKbps   int       `json:"video_bitrate_kbps"`
	AudioBitrateKbps   int       `json:"audio_bitrate_kbps"`
	SRTLatencyMs       int       `json:"srt_latency_ms"`
	Status             string    `json:"status"`
	LastTestAt         *time.Time `json:"last_test_at,omitempty"`
	LastLiveAt         *time.Time `json:"last_live_at,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type testInput struct {
	RTTAvgMs           float64 `json:"rtt_avg_ms"`
	RTTMaxMs           float64 `json:"rtt_max_ms"`
	PacketLossPct      float64 `json:"packet_loss_pct"`
	RetransmitPct      float64 `json:"retransmit_pct"`
	BitrateVariancePct float64 `json:"bitrate_variance_pct"`
	AudioDropCount     int     `json:"audio_drop_count"`
	ReconnectCount     int     `json:"reconnect_count"`
}

type recommendation struct {
	Result                   string   `json:"result"`
	NetworkHealth            string   `json:"network_health"`
	CurrentVideoBitrateKbps  int      `json:"current_video_bitrate_kbps"`
	RecommendedVideoKbps     int      `json:"recommended_video_bitrate_kbps"`
	CurrentSRTLatencyMs      int      `json:"current_srt_latency_ms"`
	RecommendedSRTLatencyMs  int      `json:"recommended_srt_latency_ms"`
	KeepResolution           bool     `json:"keep_resolution"`
	KeepFPS                  bool     `json:"keep_fps"`
	KeepAudio                bool     `json:"keep_audio"`
	Reasons                  []string `json:"reasons"`
}

func main() {
	dsn := env("RELIVE_DATABASE_URL", "postgres://relive:relive@postgres:5432/relive?sslmode=disable")
	addr := env("RELIVE_HTTP_ADDR", ":8090")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := waitForDB(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err := migrate(context.Background(), db); err != nil {
		log.Fatal(err)
	}

	a := &app{
		db:        db,
		core:      newCoreClient(),
		tests:     newTestManager(),
		testFor:   envDuration("RELIVE_TEST_DURATION", 30*time.Second),
		sampleFor: envDuration("RELIVE_SAMPLE_INTERVAL", 2*time.Second),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /api/v1/channels", a.listChannels)
	mux.HandleFunc("POST /api/v1/channels", a.createChannel)
	mux.HandleFunc("GET /api/v1/channels/{id}", a.getChannel)
	mux.HandleFunc("POST /api/v1/channels/{id}/tests", a.createTest)
	mux.HandleFunc("POST /api/v1/channels/{id}/test/start", a.startConnectionTest)
	mux.HandleFunc("POST /api/v1/channels/{id}/test/cancel", a.cancelConnectionTest)
	mux.HandleFunc("GET /api/v1/channels/{id}/recommendation", a.latestRecommendation)
	mux.HandleFunc("GET /api/v1/destination-platforms", a.destinationPlatforms)
	mux.HandleFunc("GET /api/v1/destinations", a.listDestinations)
	mux.HandleFunc("POST /api/v1/destinations", a.createDestination)
	mux.HandleFunc("GET /api/v1/destinations/runtime", a.listDestinationRuntime)
	mux.HandleFunc("POST /api/v1/destinations/{id}/runtime", a.updateDestinationRuntime)
	mux.HandleFunc("GET /api/v1/channels/{id}/telemetry/live", a.channelLiveTelemetry)

	srv := &http.Server{
		Addr:              addr,
		Handler:           logging(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("ReLive Control API listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func envDuration(k string, fallback time.Duration) time.Duration {
	raw := os.Getenv(k)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("invalid %s=%q; using %s", k, raw, fallback)
		return fallback
	}
	return d
}

func waitForDB(ctx context.Context, db *sql.DB) error {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("database unavailable: %w", ctx.Err())
		case <-t.C:
		}
	}
}

func migrate(ctx context.Context, db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS channels (
	id BIGSERIAL PRIMARY KEY,
	owner_id TEXT NOT NULL,
	name TEXT NOT NULL,
	ingest_protocol TEXT NOT NULL DEFAULT 'srt',
	ingest_host TEXT NOT NULL,
	ingest_port INTEGER NOT NULL,
	stream_id TEXT NOT NULL UNIQUE,
	resolution TEXT NOT NULL DEFAULT '1920x1080',
	fps INTEGER NOT NULL DEFAULT 50,
	video_bitrate_kbps INTEGER NOT NULL DEFAULT 5500,
	audio_bitrate_kbps INTEGER NOT NULL DEFAULT 192,
	srt_latency_ms INTEGER NOT NULL DEFAULT 750,
	status TEXT NOT NULL DEFAULT 'offline',
	last_test_at TIMESTAMPTZ,
	last_live_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channels_owner ON channels(owner_id);

CREATE TABLE IF NOT EXISTS network_tests (
	id BIGSERIAL PRIMARY KEY,
	channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	rtt_avg_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
	rtt_max_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
	packet_loss_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	retransmit_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	bitrate_variance_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	audio_drop_count INTEGER NOT NULL DEFAULT 0,
	reconnect_count INTEGER NOT NULL DEFAULT 0,
	result TEXT NOT NULL,
	network_health TEXT NOT NULL,
	recommended_video_bitrate_kbps INTEGER NOT NULL,
	recommended_srt_latency_ms INTEGER NOT NULL,
	reasons JSONB NOT NULL DEFAULT '[]'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_network_tests_channel_created
ON network_tests(channel_id, created_at DESC);

ALTER TABLE network_tests
	ADD COLUMN IF NOT EXISTS receive_bitrate_mbps DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE network_tests
	ADD COLUMN IF NOT EXISTS sample_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS destinations (
	id BIGSERIAL PRIMARY KEY,
	owner_id TEXT NOT NULL,
	name TEXT NOT NULL,
	platform TEXT NOT NULL,
	key_source TEXT NOT NULL DEFAULT 'manual_key',
	server_url TEXT NOT NULL,
	stream_key TEXT NOT NULL DEFAULT '',
	generator_ref TEXT NOT NULL DEFAULT '',
	enabled BOOLEAN NOT NULL DEFAULT true,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_destinations_owner ON destinations(owner_id);
CREATE INDEX IF NOT EXISTS idx_destinations_platform ON destinations(platform);

ALTER TABLE destinations ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE destinations ADD COLUMN IF NOT EXISTS output_bitrate_mbps DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE destinations ADD COLUMN IF NOT EXISTS reconnect_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE destinations ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE destinations ADD COLUMN IF NOT EXISTS last_status_at TIMESTAMPTZ;
`
	_, err := db.ExecContext(ctx, schema)
	return err
}

func (a *app) health(w http.ResponseWriter, r *http.Request) {
	if err := a.db.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "degraded", "database": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "up"})
}

func (a *app) listChannels(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	q := `SELECT id, owner_id, name, ingest_protocol, ingest_host, ingest_port, stream_id,
		resolution, fps, video_bitrate_kbps, audio_bitrate_kbps, srt_latency_ms, status,
		last_test_at, last_live_at, created_at, updated_at FROM channels`
	args := []any{}
	if owner != "" {
		q += " WHERE owner_id=$1"
		args = append(args, owner)
	}
	q += " ORDER BY name"

	rows, err := a.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	out := []channel{}
	for rows.Next() {
		var c channel
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Name, &c.IngestProtocol, &c.IngestHost, &c.IngestPort,
			&c.StreamID, &c.Resolution, &c.FPS, &c.VideoBitrateKbps, &c.AudioBitrateKbps,
			&c.SRTLatencyMs, &c.Status, &c.LastTestAt, &c.LastLiveAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			serverError(w, err)
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createChannel(w http.ResponseWriter, r *http.Request) {
	var c channel
	if err := decodeJSON(r, &c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if c.OwnerID == "" || c.Name == "" || c.IngestHost == "" || c.StreamID == "" || c.IngestPort == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "owner_id, name, ingest_host, ingest_port and stream_id are required"})
		return
	}
	if c.IngestProtocol == "" { c.IngestProtocol = "srt" }
	if c.Resolution == "" { c.Resolution = "1920x1080" }
	if c.FPS == 0 { c.FPS = 50 }
	if c.VideoBitrateKbps == 0 { c.VideoBitrateKbps = 5500 }
	if c.AudioBitrateKbps == 0 { c.AudioBitrateKbps = 192 }
	if c.SRTLatencyMs == 0 { c.SRTLatencyMs = 750 }

	err := a.db.QueryRowContext(r.Context(), `
		INSERT INTO channels(owner_id,name,ingest_protocol,ingest_host,ingest_port,stream_id,resolution,fps,
			video_bitrate_kbps,audio_bitrate_kbps,srt_latency_ms)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id,status,created_at,updated_at`,
		c.OwnerID,c.Name,c.IngestProtocol,c.IngestHost,c.IngestPort,c.StreamID,c.Resolution,c.FPS,
		c.VideoBitrateKbps,c.AudioBitrateKbps,c.SRTLatencyMs,
	).Scan(&c.ID,&c.Status,&c.CreatedAt,&c.UpdatedAt)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *app) getChannel(w http.ResponseWriter, r *http.Request) {
	c, err := a.channelByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *app) createTest(w http.ResponseWriter, r *http.Request) {
	c, err := a.channelByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	var in testInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rec := recommend(c, in)
	reasons, _ := json.Marshal(rec.Reasons)

	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil { serverError(w, err); return }
	defer tx.Rollback()

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO network_tests(channel_id,rtt_avg_ms,rtt_max_ms,packet_loss_pct,retransmit_pct,
			bitrate_variance_pct,audio_drop_count,reconnect_count,result,network_health,
			recommended_video_bitrate_kbps,recommended_srt_latency_ms,reasons)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		c.ID,in.RTTAvgMs,in.RTTMaxMs,in.PacketLossPct,in.RetransmitPct,in.BitrateVariancePct,
		in.AudioDropCount,in.ReconnectCount,rec.Result,rec.NetworkHealth,
		rec.RecommendedVideoKbps,rec.RecommendedSRTLatencyMs,reasons)
	if err != nil { serverError(w, err); return }

	_, err = tx.ExecContext(r.Context(), `UPDATE channels SET last_test_at=now(), updated_at=now() WHERE id=$1`, c.ID)
	if err != nil { serverError(w, err); return }
	if err := tx.Commit(); err != nil { serverError(w, err); return }

	writeJSON(w, http.StatusCreated, rec)
}


func (a *app) startConnectionTest(w http.ResponseWriter, r *http.Request) {
	c, err := a.channelByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	if !a.tests.begin(c.ID, cancel) {
		cancel()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "connection test already running"})
		return
	}

	if _, err := a.db.ExecContext(r.Context(),
		`UPDATE channels SET status='testing', updated_at=now() WHERE id=$1`, c.ID); err != nil {
		a.tests.done(c.ID)
		cancel()
		serverError(w, err)
		return
	}

	go a.runConnectionTest(ctx, c)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"channel_id":       c.ID,
		"status":           "testing",
		"duration_seconds": int(a.testFor.Seconds()),
		"sample_interval":  a.sampleFor.String(),
	})
}

func (a *app) cancelConnectionTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}
	if !a.tests.cancel(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no connection test running"})
		return
	}
	_, _ = a.db.ExecContext(r.Context(),
		`UPDATE channels SET status='offline', updated_at=now() WHERE id=$1`, id)
	writeJSON(w, http.StatusOK, map[string]any{"channel_id": id, "status": "offline"})
}

func (a *app) runConnectionTest(ctx context.Context, c channel) {
	defer a.tests.done(c.ID)

	deadline := time.NewTimer(a.testFor)
	defer deadline.Stop()
	ticker := time.NewTicker(a.sampleFor)
	defer ticker.Stop()

	samples := make([]telemetrySample, 0, int(a.testFor/a.sampleFor)+1)
	seen := false
	missingAfterSeen := false
	reconnects := 0

	sample := func() {
		pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		channels, err := a.core.srt(pollCtx)
		if err != nil {
			log.Printf("channel %d telemetry poll failed: %v", c.ID, err)
			if seen {
				missingAfterSeen = true
			}
			return
		}
		stats, ok := findPublisher(channels, c.StreamID)
		if !ok {
			if seen {
				missingAfterSeen = true
			}
			return
		}
		if missingAfterSeen {
			reconnects++
			missingAfterSeen = false
		}
		seen = true
		samples = append(samples, telemetrySample{at: time.Now(), stats: stats})
	}

	sample()

	for {
		select {
		case <-ctx.Done():
			_, _ = a.db.ExecContext(context.Background(),
				`UPDATE channels SET status='offline', updated_at=now() WHERE id=$1`, c.ID)
			return
		case <-ticker.C:
			sample()
		case <-deadline.C:
			sample()
			a.finishConnectionTest(c, samples, reconnects)
			return
		}
	}
}

func (a *app) finishConnectionTest(c channel, samples []telemetrySample, reconnects int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metrics, err := summarizeSamples(samples, reconnects)
	if err != nil {
		log.Printf("channel %d test failed: %v", c.ID, err)
		_, _ = a.db.ExecContext(ctx,
			`UPDATE channels SET status='degraded', last_test_at=now(), updated_at=now() WHERE id=$1`, c.ID)
		return
	}

	in := testInput{
		RTTAvgMs:           metrics.RTTAvgMs,
		RTTMaxMs:           metrics.RTTMaxMs,
		PacketLossPct:      metrics.PacketLossPct,
		RetransmitPct:      metrics.RetransmitPct,
		BitrateVariancePct: metrics.BitrateVariancePct,
		ReconnectCount:     metrics.ReconnectCount,
	}
	rec := recommend(c, in)
	reasons, _ := json.Marshal(rec.Reasons)

	status := "ready"
	switch rec.Result {
	case "REDUCE_BITRATE", "SAFE_PROFILE", "REVIEW":
		status = "degraded"
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("channel %d test persist failed: %v", c.ID, err)
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO network_tests(
			channel_id,rtt_avg_ms,rtt_max_ms,packet_loss_pct,retransmit_pct,
			bitrate_variance_pct,audio_drop_count,reconnect_count,result,network_health,
			recommended_video_bitrate_kbps,recommended_srt_latency_ms,reasons,
			receive_bitrate_mbps,sample_count
		)
		VALUES($1,$2,$3,$4,$5,$6,0,$7,$8,$9,$10,$11,$12,$13,$14)`,
		c.ID, metrics.RTTAvgMs, metrics.RTTMaxMs, metrics.PacketLossPct,
		metrics.RetransmitPct, metrics.BitrateVariancePct, metrics.ReconnectCount,
		rec.Result, rec.NetworkHealth, rec.RecommendedVideoKbps,
		rec.RecommendedSRTLatencyMs, reasons, metrics.ReceiveBitrateMbps,
		metrics.SampleCount,
	)
	if err != nil {
		log.Printf("channel %d test insert failed: %v", c.ID, err)
		return
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE channels SET status=$1, last_test_at=now(), updated_at=now() WHERE id=$2`,
		status, c.ID)
	if err != nil {
		log.Printf("channel %d status update failed: %v", c.ID, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("channel %d test commit failed: %v", c.ID, err)
		return
	}

	log.Printf(
		"channel %d test complete: status=%s result=%s rtt=%.2fms loss=%.2f%% retrans=%.2f%% receive=%.2fMbps",
		c.ID, status, rec.Result, metrics.RTTAvgMs, metrics.PacketLossPct,
		metrics.RetransmitPct, metrics.ReceiveBitrateMbps,
	)
}

func (a *app) latestRecommendation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}
	var rec recommendation
	var reasons []byte
	err = a.db.QueryRowContext(r.Context(), `
		SELECT nt.result,nt.network_health,c.video_bitrate_kbps,nt.recommended_video_bitrate_kbps,
			c.srt_latency_ms,nt.recommended_srt_latency_ms,nt.reasons
		FROM network_tests nt JOIN channels c ON c.id=nt.channel_id
		WHERE nt.channel_id=$1 ORDER BY nt.created_at DESC LIMIT 1`, id).
		Scan(&rec.Result,&rec.NetworkHealth,&rec.CurrentVideoBitrateKbps,&rec.RecommendedVideoKbps,
			&rec.CurrentSRTLatencyMs,&rec.RecommendedSRTLatencyMs,&reasons)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no test result for channel"})
		return
	}
	if err != nil { serverError(w, err); return }
	_ = json.Unmarshal(reasons, &rec.Reasons)
	rec.KeepResolution = true
	rec.KeepFPS = true
	rec.KeepAudio = true
	writeJSON(w, http.StatusOK, rec)
}

func (a *app) channelByID(ctx context.Context, raw string) (channel, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil { return channel{}, err }
	var c channel
	err = a.db.QueryRowContext(ctx, `SELECT id,owner_id,name,ingest_protocol,ingest_host,ingest_port,stream_id,
		resolution,fps,video_bitrate_kbps,audio_bitrate_kbps,srt_latency_ms,status,last_test_at,last_live_at,
		created_at,updated_at FROM channels WHERE id=$1`, id).
		Scan(&c.ID,&c.OwnerID,&c.Name,&c.IngestProtocol,&c.IngestHost,&c.IngestPort,&c.StreamID,
			&c.Resolution,&c.FPS,&c.VideoBitrateKbps,&c.AudioBitrateKbps,&c.SRTLatencyMs,&c.Status,
			&c.LastTestAt,&c.LastLiveAt,&c.CreatedAt,&c.UpdatedAt)
	return c, err
}

func recommend(c channel, in testInput) recommendation {
	r := recommendation{
		Result: "KEEP",
		NetworkHealth: "GOOD",
		CurrentVideoBitrateKbps: c.VideoBitrateKbps,
		RecommendedVideoKbps: c.VideoBitrateKbps,
		CurrentSRTLatencyMs: c.SRTLatencyMs,
		RecommendedSRTLatencyMs: c.SRTLatencyMs,
		KeepResolution: true, KeepFPS: true, KeepAudio: true,
		Reasons: []string{},
	}

	if in.AudioDropCount > 0 {
		r.NetworkHealth = "UNSTABLE"
		r.Result = "REVIEW"
		r.Reasons = append(r.Reasons, "audio drop detected during test")
	}
	if in.ReconnectCount > 0 {
		r.NetworkHealth = "UNSTABLE"
		r.Result = "REVIEW"
		r.Reasons = append(r.Reasons, "input reconnect detected")
	}
	if in.PacketLossPct >= 5 || in.RetransmitPct >= 8 || in.BitrateVariancePct >= 30 {
		r.NetworkHealth = "POOR"
		r.Result = "SAFE_PROFILE"
		r.RecommendedVideoKbps = stepDown(c.VideoBitrateKbps, 0.70)
		r.RecommendedSRTLatencyMs = maxInt(c.SRTLatencyMs, 1500)
		r.Reasons = append(r.Reasons, "high loss/retransmit/bitrate instability")
	} else if in.PacketLossPct >= 3 || in.RetransmitPct >= 5 || in.BitrateVariancePct >= 20 {
		r.NetworkHealth = "UNSTABLE"
		r.Result = "REDUCE_BITRATE"
		r.RecommendedVideoKbps = stepDown(c.VideoBitrateKbps, 0.82)
		r.RecommendedSRTLatencyMs = maxInt(c.SRTLatencyMs, 1200)
		r.Reasons = append(r.Reasons, "cellular link is unstable")
	} else if in.PacketLossPct >= 1 || in.RetransmitPct >= 2 || in.RTTMaxMs >= 250 {
		r.NetworkHealth = "FAIR"
		r.Result = "INCREASE_LATENCY"
		r.RecommendedSRTLatencyMs = maxInt(c.SRTLatencyMs, 1000)
		r.Reasons = append(r.Reasons, "keep quality but increase SRT recovery window")
	}
	if len(r.Reasons) == 0 {
		r.Reasons = append(r.Reasons, "current known-good profile is suitable")
	}
	return r
}

func stepDown(v int, factor float64) int {
	n := int(math.Round(float64(v)*factor/500.0) * 500)
	if n < 2500 { n = 2500 }
	return n
}

func maxInt(a,b int) int { if a>b { return a }; return b }

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w,r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
