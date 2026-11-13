// Package api dispatches decrypted JSON actions to the filesystem layer. It
// is transport-agnostic: the HTTP server handles envelopes and sessions.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/helviojunior/fmweb/internal/version"
	"github.com/helviojunior/fmweb/pkg/fileops"
)

// Error is an application-level error returned inside the encrypted body.
type Error struct {
	Msg    string
	Status int
}

func (e *Error) Error() string { return e.Msg }

// Caller describes the session invoking an action.
type Caller struct {
	Username string
	ReadOnly bool
}

// Options are the API-level settings.
type Options struct {
	Title    string
	ReadOnly bool // global read-only
	Auth     bool
}

// API is the action dispatcher.
type API struct {
	FS  *fileops.FS
	Opt Options
}

// Payload is the generic decrypted request body.
type Payload struct {
	Path        string   `json:"path"`
	Paths       []string `json:"paths"`
	Name        string   `json:"name"`
	Dest        string   `json:"dest"`
	Content     *string  `json:"content"`
	Query       string   `json:"query"`
	Mode        any      `json:"mode"`
	ToSubfolder bool     `json:"to_subfolder"`
}

var writeActions = map[string]bool{
	"save": true, "mkdir": true, "touch": true, "rename": true, "delete": true,
	"copy": true, "move": true, "chmod": true, "zip": true, "tar": true,
	"extract": true, "upload": true,
}

// IsWrite reports whether action mutates the filesystem.
func IsWrite(action string) bool { return writeActions[action] }

// Dispatch executes action and returns the result object.
func (a *API) Dispatch(action string, raw []byte, c Caller) (any, error) {
	var p Payload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &Error{Msg: "Malformed request", Status: 400}
		}
	}
	if writeActions[action] && (a.Opt.ReadOnly || c.ReadOnly) {
		return nil, &Error{Msg: "Read-only mode", Status: 403}
	}
	res, err := a.run(action, &p, c)
	if err != nil {
		var fe *fileops.Error
		if errors.As(err, &fe) {
			return nil, &Error{Msg: fe.Msg, Status: 400}
		}
		var ae *Error
		if errors.As(err, &ae) {
			return nil, ae
		}
		return nil, &Error{Msg: "Operation failed", Status: 500}
	}
	return res, nil
}

func paths(p *Payload) []string {
	if len(p.Paths) > 0 {
		return p.Paths
	}
	return []string{p.Path}
}

func (a *API) run(action string, p *Payload, c Caller) (any, error) {
	fs := a.FS
	switch action {
	case "info":
		return map[string]any{
			"username": c.Username,
			"readonly": a.Opt.ReadOnly || c.ReadOnly,
			"title":    a.Opt.Title,
			"version":  version.Version,
			"auth":     a.Opt.Auth,
		}, nil
	case "list":
		return fs.List(p.Path)
	case "stat":
		return fs.Stat(p.Path)
	case "probe":
		return fs.Probe(p.Path)
	case "read":
		s, err := fs.ReadText(p.Path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": p.Path, "content": s}, nil
	case "search":
		r, err := fs.Search(p.Path, p.Query)
		if err != nil {
			return nil, err
		}
		return map[string]any{"results": r}, nil
	case "save":
		content := ""
		if p.Content != nil {
			content = *p.Content
		}
		return fs.WriteText(p.Path, content)
	case "mkdir":
		return fs.Mkdir(p.Path, p.Name)
	case "touch":
		return fs.Touch(p.Path, p.Name)
	case "rename":
		return fs.Rename(p.Path, p.Name)
	case "delete":
		ps := paths(p)
		for _, x := range ps {
			if err := fs.Delete(x); err != nil {
				return nil, err
			}
		}
		return map[string]any{"deleted": ps}, nil
	case "copy", "move":
		out := []*fileops.Entry{}
		for _, x := range paths(p) {
			var e *fileops.Entry
			var err error
			if action == "copy" {
				e, err = fs.Copy(x, p.Dest)
			} else {
				e, err = fs.Move(x, p.Dest)
			}
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		key := "copied"
		if action == "move" {
			key = "moved"
		}
		return map[string]any{key: out}, nil
	case "chmod":
		m, err := parseMode(p.Mode)
		if err != nil {
			return nil, err
		}
		return fs.Chmod(p.Path, m)
	case "zip":
		return fs.Zip(p.Paths, p.Dest, p.Name)
	case "tar":
		return fs.Tar(p.Paths, p.Dest, p.Name)
	case "extract":
		return fs.Extract(p.Path, p.ToSubfolder)
	}
	return nil, &Error{Msg: "Unknown action: " + action, Status: 404}
}

func parseMode(v any) (uint32, error) {
	switch m := v.(type) {
	case string:
		n, err := strconv.ParseUint(m, 8, 32)
		if err != nil || n > 0o777 {
			return 0, &Error{Msg: "Invalid mode", Status: 400}
		}
		return uint32(n), nil
	case float64:
		if m < 0 || m > 0o777 || m != float64(int(m)) {
			return 0, &Error{Msg: "Invalid mode", Status: 400}
		}
		return uint32(m), nil
	}
	return 0, &Error{Msg: fmt.Sprintf("Invalid mode"), Status: 400}
}
