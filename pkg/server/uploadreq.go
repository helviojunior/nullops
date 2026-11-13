package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/helviojunior/nullops/pkg/crypto"
	"github.com/helviojunior/nullops/pkg/fileops"
	"github.com/helviojunior/nullops/pkg/session"
	"github.com/helviojunior/nullops/pkg/uploadreq"
)

func tokenHint(t string) string {
	if len(t) < 3 {
		return "?"
	}
	return t[:3] + "…"
}

// uploadReqAction handles the signed-in side of upload requests:
//
//	uploadreq_create {path}        -> {token, url, expires_at, path}
//	uploadreq_list   {path, all}   -> [requests]
//	uploadreq_revoke {token}       -> {}
func (s *Server) uploadReqAction(r *http.Request, user string, readonly bool, action string, plain []byte) (any, error) {
	st := s.cfg.UploadReqs
	if st == nil {
		return nil, errors.New("Upload requests are not available")
	}
	var req struct {
		Path  string `json:"path"`
		All   bool   `json:"all"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(plain, &req)
	ip := s.clientIP(r)
	switch action {
	case "uploadreq_list":
		return st.List(req.Path, req.All, time.Now())
	case "uploadreq_create":
		if readonly || s.cfg.ReadOnly {
			return nil, errors.New("Read-only mode")
		}
		e, err := s.cfg.FS.Stat(req.Path)
		if err != nil || !e.IsDir {
			return nil, errors.New("Destination is not a directory")
		}
		dir := e.Path
		rq, err := st.Create(dir, user)
		if err != nil {
			return nil, err
		}
		auditInfo("upload request created", "user", user, "ip", ip, "path", "/"+dir, "token", tokenHint(rq.Token),
			"expires", rq.ExpiresAt.Format(time.RFC3339))
		return map[string]any{"token": rq.Token, "url": "/public/upload/" + rq.Token,
			"expires_at": rq.ExpiresAt, "path": dir}, nil
	case "uploadreq_revoke":
		if readonly || s.cfg.ReadOnly {
			return nil, errors.New("Read-only mode")
		}
		if err := st.Revoke(req.Token); err != nil {
			return nil, err
		}
		auditInfo("upload request revoked", "user", user, "ip", ip, "token", tokenHint(req.Token))
		return map[string]any{}, nil
	}
	return nil, errors.New("Unknown action: " + action)
}

// publicUpload serves the public upload page (no account), over the same
// encrypted channel with an anonymous session:
//
//	public_upload_info {token}                                      -> {requested_by, expires_at, max_size, allow_ext}
//	public_upload {token, name, upload_id, size, status}             -> {received}
//	public_upload {token, name, upload_id, offset, size, last, chunk} -> {received} | {name, size}
//
// Files go to the folder of the request and never replace anything: they are
// stored as name-EPOCH.ext. Invalid tokens count towards the IP lockout.
func (s *Server) publicUpload(w http.ResponseWriter, r *http.Request, sess *session.Session, action string, body []byte) {
	plain, err := s.open(sess, body)
	if err != nil {
		writeErr(w, 400, "Decryption failed")
		return
	}
	fail := func(msg string) { writeSealed(w, sess, map[string]any{"ok": false, "error": msg}) }
	ok := func(res any) { writeSealed(w, sess, map[string]any{"ok": true, "result": res}) }

	ip, ua := s.clientIP(r), userAgent(r)
	if wait := s.limiter.blocked(ip); wait > 0 {
		fail("Too many attempts")
		return
	}
	if s.cfg.UploadReqs == nil || s.cfg.ReadOnly {
		fail("Uploads are disabled")
		return
	}
	var req struct {
		Token  string `json:"token"`
		Name   string `json:"name"`
		ID     string `json:"upload_id"`
		Offset int64  `json:"offset"`
		Total  *int64 `json:"size"`
		Last   *bool  `json:"last"`
		Chunk  string `json:"chunk"`
		Status bool   `json:"status"`
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		fail("Malformed request")
		return
	}
	// new uploads must start before the expiry; chunks of an upload already
	// in progress get a grace period to finish
	grace := time.Duration(0)
	if action == "public_upload" && !req.Status && req.Offset > 0 {
		grace = uploadreq.Grace
	}
	rq, err := s.cfg.UploadReqs.Get(req.Token, time.Now(), grace)
	if err != nil {
		s.limiter.fail(ip)
		auditWarn("public upload: invalid link", "ip", ip, "ua", ua, "token", tokenHint(req.Token))
		fail(uploadreq.ErrNotFound.Error())
		return
	}

	if action == "public_upload_info" {
		ok(map[string]any{"requested_by": rq.CreatedBy, "expires_at": rq.ExpiresAt,
			"max_size": s.cfg.MaxUpload, "allow_ext": s.cfg.AllowedExt})
		return
	}
	if action != "public_upload" {
		fail("Unknown action")
		return
	}

	// keep the partial file of this link apart from regular uploads
	sum := sha256.Sum256([]byte(rq.Token + ":" + req.ID))
	id := hex.EncodeToString(sum[:16])
	total := int64(-1)
	if req.Total != nil {
		total = *req.Total
	}
	if req.Status {
		if s.cfg.MaxUpload > 0 && total > s.cfg.MaxUpload {
			fail("File too large")
			return
		}
		n, err := s.cfg.FS.UploadStatus(rq.Dir, req.Name, id)
		if err != nil {
			fail(err.Error())
			return
		}
		if total >= 0 && n > total {
			n = 0
		}
		ok(map[string]any{"received": n, "max_size": s.cfg.MaxUpload})
		return
	}
	data, err := crypto.B64.DecodeString(req.Chunk)
	if err != nil {
		fail("Malformed chunk")
		return
	}
	last := req.Last == nil || *req.Last
	e, received, err := s.cfg.FS.UploadChunk(fileops.UploadRequest{
		Dir: rq.Dir, Name: req.Name, ID: id, Offset: req.Offset, Total: total, Data: data, Last: last,
		MaxSize: s.cfg.MaxUpload, AllowExt: s.cfg.AllowedExt, Unique: true,
	})
	if err != nil {
		auditWarn("public upload failed", "ip", ip, "token", tokenHint(rq.Token), "name", req.Name, "err", err.Error())
		fail(err.Error())
		return
	}
	if e != nil {
		s.cfg.UploadReqs.RecordUpload(rq.Token, e.Size)
		auditInfo("public upload", "ip", ip, "ua", ua, "token", tokenHint(rq.Token), "requested_by", rq.CreatedBy,
			"path", "/"+e.Path, "size", e.Size)
		ok(map[string]any{"name": e.Name, "size": e.Size})
		return
	}
	ok(map[string]any{"received": received})
}
