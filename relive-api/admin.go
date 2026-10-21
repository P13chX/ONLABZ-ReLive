package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ownerRecord struct {
	ID          int64     `json:"id"`
	OwnerKey    string    `json:"owner_key"`
	Name        string    `json:"name"`
	Active      bool      `json:"active"`
	ChannelCount int      `json:"channel_count"`
	UserCount   int       `json:"user_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ownerInput struct {
	OwnerKey string `json:"owner_key"`
	Name     string `json:"name"`
	Active   *bool  `json:"active,omitempty"`
}

type userUpdateInput struct {
	DisplayName *string `json:"display_name,omitempty"`
	Role        *string `json:"role,omitempty"`
	Active      *bool   `json:"active,omitempty"`
	Password    *string `json:"password,omitempty"`
	ChannelIDs  []int64 `json:"channel_ids,omitempty"`
}

type channelUpdateInput struct {
	Name             *string `json:"name,omitempty"`
	IngestHost       *string `json:"ingest_host,omitempty"`
	IngestProtocol   *string `json:"ingest_protocol,omitempty"`
	IngestPort       *int    `json:"ingest_port,omitempty"`
	IngestApp        *string `json:"ingest_app,omitempty"`
	Resolution       *string `json:"resolution,omitempty"`
	FPS              *int    `json:"fps,omitempty"`
	VideoBitrateKbps *int    `json:"video_bitrate_kbps,omitempty"`
	AudioBitrateKbps *int    `json:"audio_bitrate_kbps,omitempty"`
	SRTLatencyMs     *int    `json:"srt_latency_ms,omitempty"`
}

type systemHealth struct {
	Status              string         `json:"status"`
	Database            string         `json:"database"`
	Core                string         `json:"core"`
	Channels            int            `json:"channels"`
	Users               int            `json:"users"`
	Owners              int            `json:"owners"`
	Destinations        int            `json:"destinations"`
	LiveDestinations    int            `json:"live_destinations"`
	FailedDestinations  int            `json:"failed_destinations"`
	RTMPPublishers       int            `json:"rtmp_publishers"`
	SRTPublishers        int            `json:"srt_publishers"`
	PreviewProcesses     int            `json:"preview_processes"`
	RecentCritical      int            `json:"recent_critical_incidents"`
	GeneratedAt          time.Time      `json:"generated_at"`
}

func (a *app) listOwners(w http.ResponseWriter,r *http.Request) {
	if _,ok:=a.requireRole(r,roleSuperAdministrator);!ok {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"super administrator role required"});return
	}
	_,_ = a.db.ExecContext(r.Context(),`
		INSERT INTO owners(owner_key,name,active)
		SELECT owner_id,owner_id,true FROM (
			SELECT DISTINCT owner_id FROM channels
			UNION SELECT DISTINCT owner_id FROM users
		) x WHERE owner_id<>'' ON CONFLICT(owner_key) DO NOTHING`)
	rows,err:=a.db.QueryContext(r.Context(),`
		SELECT o.id,o.owner_key,o.name,o.active,
			(SELECT COUNT(*) FROM channels c WHERE c.owner_id=o.owner_key),
			(SELECT COUNT(*) FROM users u WHERE u.owner_id=o.owner_key),
			o.created_at,o.updated_at
		FROM owners o ORDER BY o.name,o.owner_key`)
	if err!=nil { serverError(w,err);return }
	defer rows.Close()
	out:=[]ownerRecord{}
	for rows.Next() {
		var x ownerRecord
		if err:=rows.Scan(&x.ID,&x.OwnerKey,&x.Name,&x.Active,&x.ChannelCount,&x.UserCount,&x.CreatedAt,&x.UpdatedAt);err!=nil { serverError(w,err);return }
		out=append(out,x)
	}
	writeJSON(w,http.StatusOK,out)
}

func (a *app) createOwner(w http.ResponseWriter,r *http.Request) {
	if _,ok:=a.requireRole(r,roleSuperAdministrator);!ok {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"super administrator role required"});return
	}
	var in ownerInput
	if err:=decodeJSON(r,&in);err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return }
	in.OwnerKey=strings.TrimSpace(in.OwnerKey);in.Name=strings.TrimSpace(in.Name)
	if in.OwnerKey=="" || in.Name=="" { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"owner_key and name are required"});return }
	active:=true
	if in.Active!=nil { active=*in.Active }
	var x ownerRecord
	err:=a.db.QueryRowContext(r.Context(),`
		INSERT INTO owners(owner_key,name,active) VALUES($1,$2,$3)
		RETURNING id,owner_key,name,active,created_at,updated_at`,in.OwnerKey,in.Name,active).
		Scan(&x.ID,&x.OwnerKey,&x.Name,&x.Active,&x.CreatedAt,&x.UpdatedAt)
	if err!=nil { serverError(w,err);return }
	writeJSON(w,http.StatusCreated,x)
}

func (a *app) updateOwner(w http.ResponseWriter,r *http.Request) {
	if _,ok:=a.requireRole(r,roleSuperAdministrator);!ok {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"super administrator role required"});return
	}
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid owner id"});return }
	var in ownerInput
	if err:=decodeJSON(r,&in);err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return }
	var current ownerRecord
	err=a.db.QueryRowContext(r.Context(),"SELECT id,owner_key,name,active,created_at,updated_at FROM owners WHERE id=$1",id).
		Scan(&current.ID,&current.OwnerKey,&current.Name,&current.Active,&current.CreatedAt,&current.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows) { writeJSON(w,http.StatusNotFound,map[string]string{"error":"owner not found"});return }
	if err!=nil { serverError(w,err);return }
	name:=current.Name;if strings.TrimSpace(in.Name)!="" { name=strings.TrimSpace(in.Name) }
	active:=current.Active;if in.Active!=nil { active=*in.Active }
	_,err=a.db.ExecContext(r.Context(),"UPDATE owners SET name=$1,active=$2,updated_at=now() WHERE id=$3",name,active,id)
	if err!=nil { serverError(w,err);return }
	current.Name=name;current.Active=active;current.UpdatedAt=time.Now().UTC()
	writeJSON(w,http.StatusOK,current)
}

func (a *app) updateUser(w http.ResponseWriter,r *http.Request) {
	actor,ok:=a.requireRole(r,roleAdministrator)
	if !ok { writeJSON(w,http.StatusForbidden,map[string]string{"error":"administrator role required"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid user id"});return }
	var target authUser
	err=a.db.QueryRowContext(r.Context(),"SELECT id,email,display_name,owner_id,role,active FROM users WHERE id=$1",id).
		Scan(&target.ID,&target.Email,&target.DisplayName,&target.OwnerID,&target.Role,&target.Active)
	if errors.Is(err,sql.ErrNoRows) { writeJSON(w,http.StatusNotFound,map[string]string{"error":"user not found"});return }
	if err!=nil { serverError(w,err);return }
	if actor.Role!=roleSuperAdministrator && (target.OwnerID!=actor.OwnerID || target.Role==roleSuperAdministrator) {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return
	}
	var in userUpdateInput
	if err:=decodeJSON(r,&in);err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return }
	if in.DisplayName!=nil { target.DisplayName=strings.TrimSpace(*in.DisplayName) }
	if in.Active!=nil { target.Active=*in.Active }
	if in.Role!=nil {
		role:=strings.TrimSpace(*in.Role)
		if !validRole(role) { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid role"});return }
		if actor.Role!=roleSuperAdministrator && role==roleSuperAdministrator { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return }
		target.Role=role
	}
	tx,err:=a.db.BeginTx(r.Context(),nil);if err!=nil { serverError(w,err);return };defer tx.Rollback()
	_,err=tx.ExecContext(r.Context(),"UPDATE users SET display_name=$1,role=$2,active=$3,updated_at=now() WHERE id=$4",target.DisplayName,target.Role,target.Active,id)
	if err!=nil { serverError(w,err);return }
	if in.Password!=nil {
		if len(*in.Password)<10 { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"password must be at least 10 characters"});return }
		salt,hash,err:=hashPassword(*in.Password);if err!=nil { serverError(w,err);return }
		if _,err=tx.ExecContext(r.Context(),"UPDATE users SET password_salt=$1,password_hash=$2,updated_at=now() WHERE id=$3",salt,hash,id);err!=nil { serverError(w,err);return }
	}
	if in.ChannelIDs!=nil && target.Role==roleUser {
		if _,err=tx.ExecContext(r.Context(),"DELETE FROM user_channels WHERE user_id=$1",id);err!=nil { serverError(w,err);return }
		for _,cid:=range in.ChannelIDs {
			var owner string
			if err=tx.QueryRowContext(r.Context(),"SELECT owner_id FROM channels WHERE id=$1",cid).Scan(&owner);err!=nil || owner!=target.OwnerID {
				writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel assignment"});return
			}
			if _,err=tx.ExecContext(r.Context(),"INSERT INTO user_channels(user_id,channel_id) VALUES($1,$2) ON CONFLICT DO NOTHING",id,cid);err!=nil { serverError(w,err);return }
		}
	}
	if err=tx.Commit();err!=nil { serverError(w,err);return }
	writeJSON(w,http.StatusOK,target)
}

func (a *app) updateChannel(w http.ResponseWriter,r *http.Request) {
	actor,ok:=a.requireRole(r,roleAdministrator)
	if !ok { writeJSON(w,http.StatusForbidden,map[string]string{"error":"administrator role required"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"});return }
	if !a.canAccessChannel(r.Context(),actor,id) { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return }
	ch,err:=a.channelByID(r.Context(),r.PathValue("id"));if err!=nil { serverError(w,err);return }
	var in channelUpdateInput
	if err:=decodeJSON(r,&in);err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return }
	if in.Name!=nil && strings.TrimSpace(*in.Name)!="" { ch.Name=strings.TrimSpace(*in.Name) }
	if in.IngestHost!=nil && strings.TrimSpace(*in.IngestHost)!="" { ch.IngestHost=strings.TrimSpace(*in.IngestHost) }
	if in.IngestProtocol!=nil {
		p:=strings.ToLower(strings.TrimSpace(*in.IngestProtocol));if p!="rtmp"&&p!="srt" { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid ingest protocol"});return };ch.IngestProtocol=p
	}
	if in.IngestPort!=nil && *in.IngestPort>0 { ch.IngestPort=*in.IngestPort }
	if in.IngestApp!=nil { ch.IngestApp=normalizeIngestApp(*in.IngestApp) }
	if in.Resolution!=nil && strings.TrimSpace(*in.Resolution)!="" { ch.Resolution=strings.TrimSpace(*in.Resolution) }
	if in.FPS!=nil && *in.FPS>0 { ch.FPS=*in.FPS }
	if in.VideoBitrateKbps!=nil && *in.VideoBitrateKbps>0 { ch.VideoBitrateKbps=*in.VideoBitrateKbps }
	if in.AudioBitrateKbps!=nil && *in.AudioBitrateKbps>0 { ch.AudioBitrateKbps=*in.AudioBitrateKbps }
	if in.SRTLatencyMs!=nil && *in.SRTLatencyMs>0 { ch.SRTLatencyMs=*in.SRTLatencyMs }
	_,err=a.db.ExecContext(r.Context(),`
		UPDATE channels SET name=$1,ingest_protocol=$2,ingest_host=$3,ingest_port=$4,ingest_app=$5,
			resolution=$6,fps=$7,video_bitrate_kbps=$8,audio_bitrate_kbps=$9,srt_latency_ms=$10,updated_at=now()
		WHERE id=$11`,ch.Name,ch.IngestProtocol,ch.IngestHost,ch.IngestPort,ch.IngestApp,ch.Resolution,ch.FPS,ch.VideoBitrateKbps,ch.AudioBitrateKbps,ch.SRTLatencyMs,id)
	if err!=nil { serverError(w,err);return }
	var previewPID string
	_ = a.db.QueryRowContext(r.Context(),"SELECT COALESCE(preview_process_id,'') FROM channels WHERE id=$1",id).Scan(&previewPID)
	if previewPID!="" {
		ctx,cancel:=context.WithTimeout(r.Context(),4*time.Second);_ = a.core.deleteProcess(ctx,previewPID);cancel()
		_,_=a.db.ExecContext(r.Context(),"UPDATE channels SET preview_process_id='' WHERE id=$1",id)
	}
	writeJSON(w,http.StatusOK,ch)
}

func (a *app) deleteChannel(w http.ResponseWriter,r *http.Request) {
	actor,ok:=a.requireRole(r,roleSuperAdministrator)
	if !ok { writeJSON(w,http.StatusForbidden,map[string]string{"error":"super administrator role required"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"});return }
	if !a.canAccessChannel(r.Context(),actor,id) { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return }
	var pid string
	_ = a.db.QueryRowContext(r.Context(),"SELECT COALESCE(preview_process_id,'') FROM channels WHERE id=$1",id).Scan(&pid)
	if pid!="" { ctx,cancel:=context.WithTimeout(r.Context(),4*time.Second);_ = a.core.deleteProcess(ctx,pid);cancel() }
	res,err:=a.db.ExecContext(r.Context(),"DELETE FROM channels WHERE id=$1",id)
	if err!=nil { serverError(w,err);return }
	n,_:=res.RowsAffected();if n==0 { writeJSON(w,http.StatusNotFound,map[string]string{"error":"channel not found"});return }
	writeJSON(w,http.StatusOK,map[string]bool{"ok":true})
}

func (a *app) systemHealth(w http.ResponseWriter,r *http.Request) {
	if _,ok:=a.requireRole(r,roleSuperAdministrator);!ok {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"super administrator role required"});return
	}
	h:=systemHealth{Status:"ok",Database:"up",Core:"up",GeneratedAt:time.Now().UTC()}
	if err:=a.db.PingContext(r.Context());err!=nil { h.Database="down";h.Status="degraded" }
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM channels").Scan(&h.Channels)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM users").Scan(&h.Users)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM owners").Scan(&h.Owners)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM destinations").Scan(&h.Destinations)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM destinations WHERE status='live'").Scan(&h.LiveDestinations)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM destinations WHERE status='failed'").Scan(&h.FailedDestinations)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM channels WHERE preview_process_id<>''").Scan(&h.PreviewProcesses)
	_ = a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM incident_events WHERE severity='critical' AND created_at>now()-interval '1 hour'").Scan(&h.RecentCritical)
	rtmp,err:=a.core.rtmp(r.Context())
	if err!=nil { h.Core="degraded";h.Status="degraded" } else { h.RTMPPublishers=len(rtmp) }
	srt,err:=a.core.srt(r.Context())
	if err!=nil { h.Core="degraded";h.Status="degraded" } else { h.SRTPublishers=len(srt.Publisher) }
	writeJSON(w,http.StatusOK,h)
}
