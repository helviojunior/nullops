package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/helviojunior/fmweb/pkg/auth"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/helviojunior/fmweb/pkg/session"
)

// loginTTL is how long a started login may take (user -> [MFA] -> password).
const loginTTL = 5 * time.Minute

// mfaIssuer is shown in authenticator apps.
const mfaIssuer = "Web File Manager"

// loginStep implements the multi-step login over the encrypted channel:
//
//	login_start {username}            -> {next: "mfa" | "password"}
//	login_mfa   {code}                -> {next: "password"}
//	login       {username, password}  -> {username, readonly, expires_at}
//
// Unknown users always get next="mfa" (as if they existed with MFA) so the
// first step does not tell an attacker which accounts exist; the attempt is
// logged. Their MFA step then always fails.
func (s *Server) loginStep(w http.ResponseWriter, r *http.Request, sess *session.Session, action string, body []byte) {
	plain, err := s.open(sess, body)
	if err != nil {
		writeErr(w, 400, "Decryption failed")
		return
	}
	if s.cfg.NoAuth {
		writeSealed(w, sess, map[string]any{"ok": true, "username": "anonymous", "readonly": s.cfg.ReadOnly, "next": "done"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	_ = json.Unmarshal(plain, &req)
	req.Username = strings.TrimSpace(req.Username)
	ip, ua := s.clientIP(r), userAgent(r)

	fail := func(msg string, kv ...any) {
		writeSealed(w, sess, map[string]any{"ok": false, "error": msg})
		_ = kv
	}
	if wait := s.limiter.blocked(ip); wait > 0 {
		auditWarn("login blocked (too many attempts)", "user", req.Username, "ip", ip, "ua", ua)
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Too many attempts",
			"retry_after": int(wait.Seconds()) + 1})
		return
	}

	switch action {
	case "login_start":
		if req.Username == "" {
			fail("Username required")
			return
		}
		st := &session.LoginState{Username: req.Username, Expires: time.Now().Add(loginTTL)}
		if u, ok := s.cfg.Users.Get(req.Username); ok {
			st.NeedMFA = u.HasMFA()
			st.MFAOK = !st.NeedMFA
		} else {
			st.Fake, st.NeedMFA = true, true
			auditWarn("login attempt with unknown user", "user", req.Username, "ip", ip, "ua", ua)
		}
		sess.SetLogin(st)
		next := "password"
		if st.NeedMFA {
			next = "mfa"
		}
		writeSealed(w, sess, map[string]any{"ok": true, "next": next})

	case "login_mfa":
		st := sess.Login()
		if st == nil || !st.NeedMFA {
			fail("Login expired")
			return
		}
		if st.Fake || !s.cfg.Users.VerifyMFA(st.Username, req.Code) {
			s.limiter.fail(ip)
			auditWarn("login failed (invalid MFA code)", "user", st.Username, "ip", ip, "ua", ua, "unknown_user", st.Fake)
			fail("Invalid code")
			return
		}
		st.MFAOK = true
		sess.SetLogin(st)
		writeSealed(w, sess, map[string]any{"ok": true, "next": "password"})

	case "login":
		st := sess.Login()
		if st == nil || !st.MFAOK || (req.Username != "" && req.Username != st.Username) {
			fail("Login expired")
			return
		}
		u, ok := s.cfg.Users.Verify(st.Username, req.Password)
		if !ok {
			s.limiter.fail(ip)
			auditWarn("login failed", "user", st.Username, "ip", ip, "ua", ua)
			fail("Invalid credentials")
			return
		}
		s.limiter.reset(ip)
		ro := u.ReadOnly || s.cfg.ReadOnly
		if err := s.sessions.Authenticate(sess, u.Username, ro, ip, ua); err != nil {
			log.Error("cannot persist session", "err", err)
		}
		auditInfo("login", "user", u.Username, "ip", ip, "ua", ua, "readonly", ro, "mfa", u.HasMFA())
		writeSealed(w, sess, map[string]any{"ok": true, "username": u.Username, "readonly": ro,
			"mfa": u.HasMFA(), "expires_at": sess.Expires().Unix(), "next": "done"})
	}
}

// accountAction handles the signed-in user's own MFA settings:
//
//	mfa_status  -> {enabled}
//	mfa_setup   -> {secret, uri, qr} (new secret, pending until confirmed)
//	mfa_enable  {code} -> enables MFA with the pending secret
//	mfa_disable {code} -> disables MFA (requires a valid current code)
func (s *Server) accountAction(r *http.Request, sess *session.Session, user, action string, plain []byte) (any, string) {
	if s.cfg.NoAuth {
		return nil, "Not available without authentication"
	}
	var req struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(plain, &req)
	u, ok := s.cfg.Users.Get(user)
	if !ok {
		return nil, "Not authenticated"
	}
	ip := s.clientIP(r)
	switch action {
	case "mfa_status":
		return map[string]any{"enabled": u.HasMFA()}, ""
	case "mfa_setup":
		if u.HasMFA() {
			return nil, "MFA is already enabled"
		}
		secret, err := auth.NewTOTPSecret()
		if err != nil {
			return nil, "Operation failed"
		}
		sess.SetMFAPending(secret)
		uri := auth.TOTPURI(mfaIssuer, user, secret)
		svg, err := auth.QRSVG(uri)
		if err != nil {
			return nil, "Operation failed"
		}
		return map[string]any{"secret": secret, "uri": uri,
			"qr": "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))}, ""
	case "mfa_enable":
		secret := sess.MFAPending()
		if secret == "" {
			return nil, "Start the MFA setup again"
		}
		if _, ok := auth.VerifyTOTP(secret, req.Code, time.Now()); !ok {
			return nil, "Invalid code"
		}
		if err := s.cfg.Users.SetMFA(user, secret); err != nil {
			return nil, "Operation failed"
		}
		sess.SetMFAPending("")
		auditInfo("mfa enabled", "user", user, "ip", ip)
		return map[string]any{"enabled": true}, ""
	case "mfa_disable":
		if !u.HasMFA() {
			return map[string]any{"enabled": false}, ""
		}
		if !s.cfg.Users.VerifyMFA(user, req.Code) {
			auditWarn("mfa disable failed (invalid code)", "user", user, "ip", ip)
			return nil, "Invalid code"
		}
		if err := s.cfg.Users.SetMFA(user, ""); err != nil {
			return nil, "Operation failed"
		}
		auditInfo("mfa disabled", "user", user, "ip", ip)
		return map[string]any{"enabled": false}, ""
	}
	return nil, "Unknown action"
}
