package main

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) people(r *http.Request, format string, args ...any) {
	log.Printf("people: %s "+format, append([]any{currentUser(r)}, args...)...)
}

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"people": s.store.People()})
}

func (s *Server) addPerson(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Role     string   `json:"role"`
		Apps     []string `json:"apps"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.store.AddPerson(req.Username, req.Password, req.Role, req.Apps)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.people(r, "added %s as %s", p.Username, p.Role)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updatePerson(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role     *string   `json:"role"`
		Apps     *[]string `json:"apps"`
		Disabled *bool     `json:"disabled"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := r.PathValue("name")
	p, err := s.store.UpdatePerson(name, req.Role, req.Apps, req.Disabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Disabled {
		s.sessions.DeleteUserExcept(p.Username, "") // signs out every device, TVs included
	}
	s.people(r, "updated %s (role=%s disabled=%v apps=%v)", p.Username, p.Role, p.Disabled, p.Apps)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePerson(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.DeletePerson(name); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.sessions.DeleteUserExcept(name, "")
	s.people(r, "removed %s", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) resetPerson(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := r.PathValue("name")
	if err := s.store.ResetPassword(name, req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.sessions.DeleteUserExcept(name, "")
	s.people(r, "reset the password of %s", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// transferAdmin hands over the admin role. It needs the admin's own password,
// so a stolen open browser can't give the server away.
func (s *Server) transferAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct{ To, Password string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := currentUser(r)
	if !s.store.CheckPassword(me, req.Password) {
		jsonError(w, http.StatusBadRequest, "your password is incorrect")
		return
	}
	if err := s.store.TransferAdmin(me, strings.TrimSpace(req.To)); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.people(r, "handed the admin role to %s", req.To)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// checkFolders stops a manager from pointing an app at a folder outside the
// safe folders. An app can read whatever folder it is given, so this closes
// the back door around the file explorer's limits. The admin is not limited.
func (s *Server) checkFolders(r *http.Request, catalogID string, values map[string]string) error {
	if person(r).Role == RoleAdmin {
		return nil
	}
	ca, ok := s.catalog.Get(catalogID)
	if !ok {
		return nil
	}
	f := s.files.As(false)
	for _, st := range ca.Settings {
		if st.Type != "path" {
			continue
		}
		v := strings.TrimSpace(values[st.Key])
		if v == "" {
			continue
		}
		if !filepath.IsAbs(v) || !f.lexicallyAllowed(filepath.Clean(v)) {
			return errors.New("the folder " + v + " is outside the folders your account may use. Ask the admin, or choose a folder inside /srv")
		}
	}
	return nil
}
