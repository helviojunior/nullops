package server

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/helviojunior/nullops/pkg/fileops"
	"github.com/helviojunior/nullops/pkg/pathref"
)

// maxRefPaths caps one path_refs request.
const maxRefPaths = 1000

// crumbs returns path and its ancestors (outermost first) with their refs.
func (s *Server) crumbs(path string) []fileops.Crumb {
	path = pathref.Clean(path)
	if path == "" {
		return []fileops.Crumb{}
	}
	parts := strings.Split(path, "/")
	paths := make([]string, len(parts))
	for i := range parts {
		paths[i] = strings.Join(parts[:i+1], "/")
	}
	refs := s.cfg.PathRefs.Refs(paths...)
	out := make([]fileops.Crumb, len(paths))
	for i := range paths {
		out[i] = fileops.Crumb{Path: paths[i], Ref: refs[i]}
	}
	return out
}

func (s *Server) entryRefs(entries []*fileops.Entry) {
	if len(entries) == 0 {
		return
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	for i, r := range s.cfg.PathRefs.Refs(paths...) {
		entries[i].Ref = r
	}
}

// attachRefs adds the URL refs to the results that carry paths (listings,
// search results, stat).
func (s *Server) attachRefs(res any) {
	if s.cfg.PathRefs == nil {
		return
	}
	switch v := res.(type) {
	case *fileops.Listing:
		s.entryRefs(v.Entries)
		v.Crumbs = s.crumbs(v.Path)
		v.Ref = s.cfg.PathRefs.Of(v.Path)
	case *fileops.Entry:
		v.Ref = s.cfg.PathRefs.Ref(v.Path)
	case map[string]any:
		if r, ok := v["results"].([]*fileops.Entry); ok {
			s.entryRefs(r)
		}
	}
}

// pathAction:
//
//	path_resolve {ref}     -> {path, ref, crumbs}
//	path_refs    {paths}   -> {refs: {path: ref}}
func (s *Server) pathAction(action string, plain []byte) (any, error) {
	st := s.cfg.PathRefs
	if st == nil {
		return nil, errors.New("Not available")
	}
	var req struct {
		Ref   string   `json:"ref"`
		Paths []string `json:"paths"`
	}
	if json.Unmarshal(plain, &req) != nil {
		return nil, errors.New("Malformed request")
	}
	switch action {
	case "path_resolve":
		p, err := st.Resolve(req.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": p, "ref": req.Ref, "crumbs": s.crumbs(p)}, nil
	case "path_refs":
		if len(req.Paths) > maxRefPaths {
			return nil, errors.New("Too many paths")
		}
		refs := st.Refs(req.Paths...)
		out := make(map[string]string, len(refs))
		for i, p := range req.Paths {
			out[p] = refs[i]
		}
		return map[string]any{"refs": out}, nil
	}
	return nil, errors.New("Unknown action")
}
