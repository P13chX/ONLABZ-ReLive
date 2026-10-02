package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type telemetryCollector struct {
	app       *app
	interval  time.Duration
	retention time.Duration

	mu   sync.Mutex
	last map[int64]telemetryCounter
}

type telemetryCounter struct {
	at    time.Time
	bytes uint64
}

type telemetryHistoryPoint struct {
	ObservedAt        time.Time `json:"observed_at"`
	RTTMs             float64   `json:"rtt_ms"`
	BandwidthMbit     float64   `json:"bandwidth_mbit"`
	ReceiveBitrateMbit float64  `json:"receive_bitrate_mbit"`
	RecvBufferMs      uint64    `json:"recv_buffer_ms"`
	SRTLatencyMs      uint64    `json:"srt_latency_ms"`
	RecvLossPackets   uint64    `json:"recv_loss_packets"`
	RecvRetransPackets uint64   `json:"recv_retrans_packets"`
	RecvDropPackets   uint64    `json:"recv_drop_packets"`
}

func newTelemetryCollector(a *app) *telemetryCollector {
	return &telemetryCollector{
		app:a,
		interval:envDuration("RELIVE_TELEMETRY_INTERVAL",2*time.Second),
		retention:envDuration("RELIVE_TELEMETRY_RETENTION",24*time.Hour),
		last:map[int64]telemetryCounter{},
	}
}

func (c *telemetryCollector) run(ctx context.Context) {
	c.collect(ctx)
	t:=time.NewTicker(c.interval)
	defer t.Stop()
	prune:=time.NewTicker(5*time.Minute)
	defer prune.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect(ctx)
		case <-prune.C:
			c.prune(ctx)
		}
	}
}

func (c *telemetryCollector) collect(ctx context.Context) {
	coreCtx,cancel:=context.WithTimeout(ctx,5*time.Second)
	channels,err:=c.app.core.srt(coreCtx)
	cancel()
	if err!=nil {
		log.Printf("telemetry history poll failed: %v",err)
		return
	}

	rows,err:=c.app.db.QueryContext(ctx,`SELECT id,stream_id FROM channels`)
	if err!=nil {
		log.Printf("telemetry channel query failed: %v",err)
		return
	}
	defer rows.Close()

	type ch struct{ id int64; stream string }
	var list []ch
	for rows.Next() {
		var x ch
		if err:=rows.Scan(&x.id,&x.stream);err!=nil { return }
		list=append(list,x)
	}

	now:=time.Now().UTC()
	for _,x:=range list {
		stats,ok:=findPublisher(channels,x.stream)
		if !ok {
			c.mu.Lock()
			delete(c.last,x.id)
			c.mu.Unlock()
			continue
		}

		receiveMbps:=0.0
		c.mu.Lock()
		prev,hasPrev:=c.last[x.id]
		if hasPrev {
			dt:=now.Sub(prev.at).Seconds()
			if dt>0 && stats.RecvUniqueBytes>=prev.bytes {
				receiveMbps=float64(stats.RecvUniqueBytes-prev.bytes)*8/dt/1_000_000
			}
		}
		c.last[x.id]=telemetryCounter{at:now,bytes:stats.RecvUniqueBytes}
		c.mu.Unlock()

		_,err:=c.app.db.ExecContext(ctx,`
			INSERT INTO telemetry_samples(
				channel_id,observed_at,rtt_ms,bandwidth_mbit,receive_bitrate_mbit,
				recv_buffer_ms,srt_latency_ms,recv_loss_packets,recv_retrans_packets,recv_drop_packets
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			x.id,now,stats.RTTMs,stats.BandwidthMbit,receiveMbps,stats.RecvBufferMs,
			stats.RecvTSBPDDelayMs,stats.RecvLossPkt,stats.RecvRetransPkt,stats.RecvDropPkt)
		if err!=nil {
			log.Printf("telemetry insert channel %d failed: %v",x.id,err)
		}
	}
}

func (c *telemetryCollector) prune(ctx context.Context) {
	cutoff:=time.Now().UTC().Add(-c.retention)
	if _,err:=c.app.db.ExecContext(ctx,`DELETE FROM telemetry_samples WHERE observed_at < $1`,cutoff);err!=nil {
		log.Printf("telemetry prune failed: %v",err)
	}
}

func (a *app) telemetryHistory(w http.ResponseWriter,r *http.Request) {
	channelID,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"})
		return
	}

	minutes:=15
	if raw:=r.URL.Query().Get("minutes");raw!="" {
		if n,e:=strconv.Atoi(raw);e==nil && n>=1 && n<=1440 { minutes=n }
	}
	limit:=1000
	if raw:=r.URL.Query().Get("limit");raw!="" {
		if n,e:=strconv.Atoi(raw);e==nil && n>=10 && n<=5000 { limit=n }
	}

	cutoff:=time.Now().UTC().Add(-time.Duration(minutes)*time.Minute)
	rows,err:=a.db.QueryContext(r.Context(),`
		SELECT observed_at,rtt_ms,bandwidth_mbit,receive_bitrate_mbit,recv_buffer_ms,srt_latency_ms,
			recv_loss_packets,recv_retrans_packets,recv_drop_packets
		FROM telemetry_samples
		WHERE channel_id=$1 AND observed_at >= $2
		ORDER BY observed_at ASC
		LIMIT $3`,channelID,cutoff,limit)
	if err!=nil { serverError(w,err);return }
	defer rows.Close()

	out:=[]telemetryHistoryPoint{}
	for rows.Next() {
		var p telemetryHistoryPoint
		if err:=rows.Scan(&p.ObservedAt,&p.RTTMs,&p.BandwidthMbit,&p.ReceiveBitrateMbit,
			&p.RecvBufferMs,&p.SRTLatencyMs,&p.RecvLossPackets,&p.RecvRetransPackets,&p.RecvDropPackets);err!=nil {
			serverError(w,err);return
		}
		out=append(out,p)
	}
	writeJSON(w,http.StatusOK,out)
}
