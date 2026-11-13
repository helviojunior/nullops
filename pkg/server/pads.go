package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/helviojunior/nullops/pkg/pads"
	"github.com/helviojunior/nullops/pkg/session"
)

// padAction handles PastePad over the encrypted channel:
//
//	pad_list                                  -> [{name, updated_at, size, editors}]
//	pad_create {name}                         -> {}
//	pad_delete {name}                         -> {}
//	pad_open   {name, client}                 -> {text, rev, editors}
//	pad_edit   {name, client, seq, rev, op}   -> {rev}       (op: ot.js format)
//	pad_poll   {name, client, rev}            -> {rev, changes, editors, snapshot?}
//
// pad_poll is a long poll: it returns as soon as someone changes the pad (or
// after ~25 s). Every signed-in user can see and edit every pad.
func (s *Server) padAction(w http.ResponseWriter, r *http.Request, sess *session.Session, user, action string, plain []byte) {
	reply := func(res any, err error) {
		if err != nil {
			out := map[string]any{"ok": false, "error": err.Error()}
			if errors.Is(err, pads.ErrResync) {
				out["code"] = "resync"
			}
			writeSealed(w, sess, out)
			return
		}
		writeSealed(w, sess, map[string]any{"ok": true, "result": res})
	}
	if s.cfg.Pads == nil {
		reply(nil, errors.New("PastePad is not available"))
		return
	}
	var req struct {
		Name   string           `json:"name"`
		Client string           `json:"client"`
		Seq    int              `json:"seq"`
		Rev    int              `json:"rev"`
		Op     *pads.Operation  `json:"op"`
		Raw    *json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		reply(nil, errors.New("Malformed request"))
		return
	}
	if action != "pad_list" && !pads.ValidName(req.Name) {
		reply(nil, pads.ErrInvalidName)
		return
	}
	if len(req.Client) > 64 {
		reply(nil, errors.New("Malformed request"))
		return
	}
	ip := s.clientIP(r)
	m := s.cfg.Pads
	switch action {
	case "pad_list":
		list, err := m.List()
		reply(list, err)
	case "pad_create":
		err := m.Create(req.Name, user)
		if err == nil {
			auditInfo("pad create", "user", user, "ip", ip, "pad", req.Name)
		}
		reply(map[string]any{"name": req.Name}, err)
	case "pad_delete":
		err := m.Delete(req.Name)
		if err == nil {
			auditInfo("pad delete", "user", user, "ip", ip, "pad", req.Name)
		}
		reply(map[string]any{}, err)
	case "pad_open":
		snap, err := m.Open(req.Name, user, req.Client)
		if err == nil {
			auditInfo("pad open", "user", user, "ip", ip, "pad", req.Name)
		}
		reply(snap, err)
	case "pad_edit":
		if req.Op == nil || req.Client == "" || req.Seq <= 0 {
			reply(nil, errors.New("Malformed request"))
			return
		}
		rev, err := m.Edit(req.Name, user, req.Client, req.Seq, req.Rev, req.Op)
		reply(map[string]any{"rev": rev}, err)
	case "pad_poll":
		res, err := m.Poll(r.Context(), req.Name, user, req.Client, req.Rev, pads.PollWait)
		if r.Context().Err() != nil {
			return // client went away
		}
		reply(res, err)
	default:
		reply(nil, errors.New("Unknown action: "+action))
	}
}
