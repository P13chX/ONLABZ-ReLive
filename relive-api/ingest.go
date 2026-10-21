package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func generateStreamKey() (string,error) {
	raw,err:=randomToken(24)
	if err!=nil { return "",err }
	return "rlv_"+raw,nil
}

func generateStreamID() (string,error) {
	raw,err:=randomToken(9)
	if err!=nil { return "",err }
	return "ch_"+raw,nil
}

func normalizeIngestApp(app string) string {
	app=strings.TrimSpace(app)
	if app=="" { return "/live" }
	if !strings.HasPrefix(app,"/") { app="/"+app }
	return strings.TrimRight(app,"/")
}

func (c channel) publishURL() string {
	scheme:=strings.ToLower(c.IngestProtocol)
	if scheme=="" { scheme="rtmp" }
	app:=normalizeIngestApp(c.IngestApp)
	return fmt.Sprintf("%s://%s:%d%s",scheme,c.IngestHost,c.IngestPort,app)
}

func (a *app) resetChannelStreamKey(w http.ResponseWriter,r *http.Request) {
	u,ok:=currentUser(r.Context())
	if !ok { writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"});return }
	if !a.canAccessChannel(r.Context(),u,id) {
		writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"})
		return
	}
	key,err:=generateStreamKey()
	if err!=nil { serverError(w,err);return }
	var version int
	var updated time.Time
	err=a.db.QueryRowContext(r.Context(),
		"UPDATE channels SET stream_key=$1,stream_key_version=stream_key_version+1,stream_key_updated_at=now(),updated_at=now() WHERE id=$2 RETURNING stream_key_version,stream_key_updated_at",
		key,id).Scan(&version,&updated)
	if errors.Is(err,sql.ErrNoRows) { writeJSON(w,http.StatusNotFound,map[string]string{"error":"channel not found"});return }
	if err!=nil { serverError(w,err);return }
	rows,queryErr:=a.db.QueryContext(r.Context(),"SELECT core_process_id FROM destinations WHERE channel_id=$1 AND core_process_id<>''",id)
	if queryErr==nil {
		var processIDs []string
		for rows.Next() {
			var pid string
			if rows.Scan(&pid)==nil && pid!="" { processIDs=append(processIDs,pid) }
		}
		rows.Close()
		for _,pid:=range processIDs {
			ctx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
			_ = a.core.deleteProcess(ctx,pid)
			cancel()
		}
		_,_ = a.db.ExecContext(r.Context(),
			"UPDATE destinations SET core_process_id='',status=CASE WHEN desired_state='running' THEN 'reconnecting' ELSE 'idle' END,last_error='',updated_at=now() WHERE channel_id=$1",id)
	}
	_ = a.recordIncident(r.Context(),id,0,"warning","stream_key_rotated",
		fmt.Sprintf("RTMP stream key rotated to version %d by %s; relay workers will reconnect using the new key",version,u.Email))
	writeJSON(w,http.StatusOK,map[string]any{
		"channel_id":id,
		"stream_key":key,
		"stream_key_version":version,
		"stream_key_updated_at":updated,
	})
}
