package fakes

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// OpensheetProject is a project the fake control plane lists for any token it accepts.
type OpensheetProject struct {
	ID, OrgID, OrgSlug, Slug, Name string
}

// OpensheetCredential is a Google credential of a fake opensheet project.
type OpensheetCredential struct {
	ID, ProjectID, Name, Kind, Email string
}

// OpensheetProvision is one Provision call the fake control plane received.
type OpensheetProvision struct {
	Token          string
	ProjectID      string
	CredentialID   string
	Title          string
	IdempotencyKey string
	Tabs           []OpensheetProvisionTab
}

// OpensheetProvisionTab is one tab of a Provision call.
type OpensheetProvisionTab struct {
	Title   string   `json:"title"`
	Headers []string `json:"headers"`
	Slug    string   `json:"slug"`
}

type opensheetControl struct {
	projects   []OpensheetProject
	creds      []OpensheetCredential
	provisions []OpensheetProvision
	unknown    bool
}

// AddProject lists p, and cred under it when cred.ID is set, to every bearer token.
func (f *Opensheet) AddProject(p OpensheetProject, creds ...OpensheetCredential) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.control.projects = append(f.control.projects, p)
	for _, c := range creds {
		c.ProjectID = p.ID
		f.control.creds = append(f.control.creds, c)
	}
}

// RefusePerson makes the control plane answer every call unauthenticated, as opensheet does for a person it does not know.
func (f *Opensheet) RefusePerson() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.control.unknown = true
}

// Provisions returns a copy of every Provision call.
func (f *Opensheet) Provisions() []OpensheetProvision {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.control.provisions)
}

func (f *Opensheet) routeControl(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/project.v1.ProjectService/ListProjects", f.listProjects)
	mux.HandleFunc("POST /api/credential.v1.CredentialService/List", f.listCredentials)
	mux.HandleFunc("POST /api/spreadsheet.v1.SpreadsheetService/Provision", f.provision)
}

func (f *Opensheet) controlToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" || f.control.unknown {
		writeOpensheetJSON(w, http.StatusUnauthorized, map[string]string{"code": "unauthenticated", "message": "unauthenticated"})
		return "", false
	}
	return token, true
}

func (f *Opensheet) listProjects(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.controlToken(w, r); !ok {
		return
	}
	out := make([]map[string]string, 0, len(f.control.projects))
	for _, p := range f.control.projects {
		out = append(out, map[string]string{"id": p.ID, "orgId": p.OrgID, "orgSlug": p.OrgSlug, "slug": p.Slug, "name": p.Name})
	}
	writeOpensheetJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func (f *Opensheet) listCredentials(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.controlToken(w, r); !ok {
		return
	}
	var in struct {
		ProjectID string `json:"projectId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	out := []map[string]string{}
	for _, c := range f.control.creds {
		if c.ProjectID == in.ProjectID {
			out = append(out, map[string]string{
				"id": c.ID, "projectId": c.ProjectID, "name": c.Name, "kind": c.Kind, "status": "active", "googleAccountEmail": c.Email,
			})
		}
	}
	writeOpensheetJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// NOTE: the same idempotency key replays the first spreadsheet; each tab becomes a writable sheet with its headers, so a sync can follow.
func (f *Opensheet) provision(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := f.controlToken(w, r)
	if !ok {
		return
	}
	var in OpensheetProvision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeOpensheetJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_argument", "message": err.Error()})
		return
	}
	in.Token = token
	f.control.provisions = append(f.control.provisions, in)
	for _, t := range in.Tabs {
		if _, exists := f.tabs[t.Slug]; !exists {
			f.tabs[t.Slug] = &opensheetTab{
				OpensheetSheet: OpensheetSheet{Columns: slices.Clone(t.Headers), Writable: true},
				rows:           map[string]map[string]string{}, idem: map[string]opensheetReplay{},
			}
		}
	}
	writeOpensheetJSON(w, http.StatusOK, map[string]any{
		"spreadsheet": map[string]string{"id": "ss-" + in.IdempotencyKey, "title": in.Title, "googleFileId": "gf-" + in.IdempotencyKey},
	})
}
