package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userContextKey contextKey = "relive-user"

const (
	roleUser = "user"
	roleAdministrator = "administrator"
	roleSuperAdministrator = "super_administrator"
)

type authUser struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	OwnerID     string `json:"owner_id"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type createUserInput struct {
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	OwnerID     string  `json:"owner_id"`
	Role        string  `json:"role"`
	Password    string  `json:"password"`
	ChannelIDs  []int64 `json:"channel_ids"`
}

func roleRank(role string) int {
	switch role {
	case roleUser:
		return 1
	case roleAdministrator:
		return 2
	case roleSuperAdministrator:
		return 3
	default:
		return 0
	}
}

func currentUser(ctx context.Context) (authUser, bool) {
	u, ok := ctx.Value(userContextKey).(authUser)
	return u, ok
}

func randomToken(bytesN int) (string, error) {
	b := make([]byte, bytesN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pbkdf2SHA256(password string, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, []byte(password))
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func hashPassword(password string) (saltHex, hashHex string, err error) {
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return "", "", err
	}
	hash := pbkdf2SHA256(password, salt, 200000, 32)
	return hex.EncodeToString(salt), hex.EncodeToString(hash), nil
}

func verifyPassword(password, saltHex, hashHex string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil { return false }
	expected, err := hex.DecodeString(hashHex)
	if err != nil { return false }
	got := pbkdf2SHA256(password, salt, 200000, len(expected))
	return subtle.ConstantTimeCompare(got, expected) == 1
}

func (a *app) bootstrapSuperAdmin(ctx context.Context) error {
	email := strings.TrimSpace(strings.ToLower(env("RELIVE_SUPERADMIN_EMAIL", "")))
	password := env("RELIVE_SUPERADMIN_PASSWORD", "")
	if email == "" || password == "" {
		return nil
	}
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role=$1`, roleSuperAdministrator).Scan(&count); err != nil {
		return err
	}
	if count > 0 { return nil }
	salt, hash, err := hashPassword(password)
	if err != nil { return err }
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO users(email,display_name,owner_id,role,password_salt,password_hash,active)
		VALUES($1,'Super Administrator','ONLIVEABLE',$2,$3,$4,true)`,
		email, roleSuperAdministrator, salt, hash)
	return err
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var in loginInput
	if err := decodeJSON(r,&in); err != nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid login payload"})
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	var u authUser
	var salt, hash string
	err := a.db.QueryRowContext(r.Context(), `
		SELECT id,email,display_name,owner_id,role,active,password_salt,password_hash
		FROM users WHERE email=$1`, in.Email).
		Scan(&u.ID,&u.Email,&u.DisplayName,&u.OwnerID,&u.Role,&u.Active,&salt,&hash)
	if err != nil || !u.Active || !verifyPassword(in.Password,salt,hash) {
		writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"invalid email or password"})
		return
	}

	raw, err := randomToken(32)
	if err != nil { serverError(w,err); return }
	sum := sha256.Sum256([]byte(raw))
	expires := time.Now().UTC().Add(12*time.Hour)
	_, err = a.db.ExecContext(r.Context(), `
		INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`,
		hex.EncodeToString(sum[:]),u.ID,expires)
	if err != nil { serverError(w,err); return }

	secure:=strings.EqualFold(env("RELIVE_COOKIE_SECURE","false"),"true")
	http.SetCookie(w,&http.Cookie{
		Name:"relive_session",Value:raw,Path:"/",HttpOnly:true,Secure:secure,
		SameSite:http.SameSiteLaxMode,Expires:expires,
	})
	writeJSON(w,http.StatusOK,u)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if c,err:=r.Cookie("relive_session"); err==nil {
		sum:=sha256.Sum256([]byte(c.Value))
		_,_ = a.db.ExecContext(r.Context(),`DELETE FROM sessions WHERE token_hash=$1`,hex.EncodeToString(sum[:]))
	}
	secure:=strings.EqualFold(env("RELIVE_COOKIE_SECURE","false"),"true")
	http.SetCookie(w,&http.Cookie{Name:"relive_session",Value:"",Path:"/",HttpOnly:true,Secure:secure,SameSite:http.SameSiteLaxMode,MaxAge:-1,Expires:time.Unix(0,0)})
	writeJSON(w,http.StatusOK,map[string]bool{"ok":true})
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	u,ok:=currentUser(r.Context())
	if !ok { writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"}); return }
	writeJSON(w,http.StatusOK,u)
}

func (a *app) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		var token string
		if c,err:=r.Cookie("relive_session"); err==nil {
			token=c.Value
		}
		if token=="" {
			if h:=r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h),"bearer ") {
				token=strings.TrimSpace(h[7:])
			}
		}
		if token=="" {
			writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"authentication required"})
			return
		}
		sum:=sha256.Sum256([]byte(token))
		var u authUser
		err:=a.db.QueryRowContext(r.Context(),`
			SELECT u.id,u.email,u.display_name,u.owner_id,u.role,u.active
			FROM sessions s JOIN users u ON u.id=s.user_id
			WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active=true`,
			hex.EncodeToString(sum[:])).
			Scan(&u.ID,&u.Email,&u.DisplayName,&u.OwnerID,&u.Role,&u.Active)
		if err!=nil {
			writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"invalid or expired session"})
			return
		}
		next.ServeHTTP(w,r.WithContext(context.WithValue(r.Context(),userContextKey,u)))
	})
}

func (a *app) requireRole(r *http.Request, minimum string) (authUser,bool) {
	u,ok:=currentUser(r.Context())
	if !ok || roleRank(u.Role)<roleRank(minimum) { return authUser{},false }
	return u,true
}

func (a *app) canAccessChannel(ctx context.Context, u authUser, channelID int64) bool {
	if u.Role==roleSuperAdministrator { return true }
	if u.Role==roleAdministrator {
		var ok bool
		err:=a.db.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM channels WHERE id=$1 AND owner_id=$2)`,channelID,u.OwnerID).Scan(&ok)
		return err==nil && ok
	}
	var ok bool
	err:=a.db.QueryRowContext(ctx,`
		SELECT EXISTS(SELECT 1 FROM user_channels WHERE user_id=$1 AND channel_id=$2)`,u.ID,channelID).Scan(&ok)
	return err==nil && ok
}

func validRole(role string) bool {
	return role==roleUser || role==roleAdministrator || role==roleSuperAdministrator
}

func (a *app) listUsers(w http.ResponseWriter,r *http.Request) {
	u,ok:=a.requireRole(r,roleAdministrator)
	if !ok { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"}); return }

	q:=`SELECT id,email,display_name,owner_id,role,active FROM users`
	args:=[]any{}
	if u.Role!=roleSuperAdministrator {
		q += " WHERE owner_id=$1 AND role<>'super_administrator'"
		args=append(args,u.OwnerID)
	}
	q += " ORDER BY display_name,email"
	rows,err:=a.db.QueryContext(r.Context(),q,args...)
	if err!=nil { serverError(w,err); return }
	defer rows.Close()
	out:=[]authUser{}
	for rows.Next() {
		var x authUser
		if err:=rows.Scan(&x.ID,&x.Email,&x.DisplayName,&x.OwnerID,&x.Role,&x.Active);err!=nil { serverError(w,err); return }
		out=append(out,x)
	}
	writeJSON(w,http.StatusOK,out)
}

func (a *app) createUser(w http.ResponseWriter,r *http.Request) {
	actor,ok:=a.requireRole(r,roleAdministrator)
	if !ok { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"}); return }
	var in createUserInput
	if err:=decodeJSON(r,&in);err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()}); return }
	in.Email=strings.ToLower(strings.TrimSpace(in.Email))
	in.Role=strings.TrimSpace(in.Role)
	if in.Email=="" || len(in.Password)<10 || !validRole(in.Role) {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"email, valid role and password >= 10 chars are required"})
		return
	}
	if actor.Role!=roleSuperAdministrator {
		if in.Role==roleSuperAdministrator { writeJSON(w,http.StatusForbidden,map[string]string{"error":"only super administrator can create super administrators"}); return }
		in.OwnerID=actor.OwnerID
	}
	if strings.TrimSpace(in.OwnerID)=="" { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"owner_id required"}); return }

	salt,hash,err:=hashPassword(in.Password)
	if err!=nil { serverError(w,err); return }
	tx,err:=a.db.BeginTx(r.Context(),nil)
	if err!=nil { serverError(w,err); return }
	defer tx.Rollback()

	var id int64
	err=tx.QueryRowContext(r.Context(),`
		INSERT INTO users(email,display_name,owner_id,role,password_salt,password_hash,active)
		VALUES($1,$2,$3,$4,$5,$6,true) RETURNING id`,
		in.Email,in.DisplayName,in.OwnerID,in.Role,salt,hash).Scan(&id)
	if err!=nil { serverError(w,err); return }

	if in.Role==roleUser {
		for _,cid:=range in.ChannelIDs {
			var owner string
			if err:=tx.QueryRowContext(r.Context(),`SELECT owner_id FROM channels WHERE id=$1`,cid).Scan(&owner);err!=nil || owner!=in.OwnerID {
				writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel assignment"})
				return
			}
			if _,err:=tx.ExecContext(r.Context(),`INSERT INTO user_channels(user_id,channel_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,id,cid);err!=nil {
				serverError(w,err);return
			}
		}
	}
	if err:=tx.Commit();err!=nil { serverError(w,err);return }
	writeJSON(w,http.StatusCreated,map[string]any{"id":id})
}

func (a *app) pruneSessions(ctx context.Context) {
	_,_ = a.db.ExecContext(ctx,`DELETE FROM sessions WHERE expires_at<=now()`)
}

func decodeJSONBody[T any](r *http.Request) (T,error) {
	var v T
	err:=json.NewDecoder(r.Body).Decode(&v)
	return v,err
}

var _ = errors.Is
var _ = sql.ErrNoRows
