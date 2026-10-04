package fakes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// OpensheetSheet is one published sheet the fake opensheet serves.
type OpensheetSheet struct {
	Columns  []string
	Writable bool
}

// OpensheetRequest is one request the fake opensheet received.
type OpensheetRequest struct {
	Method         string
	Slug           string
	ID             string
	IdempotencyKey string
	Body           map[string]string
}

type opensheetTab struct {
	OpensheetSheet
	rows  map[string]map[string]string
	order []string
	idem  map[string]opensheetReplay
}

type opensheetReplay struct {
	hash string
	row  map[string]string
}

type opensheetFailure struct {
	status int
	code   string
	left   int
}

// Opensheet is a hand-written opensheet data plane: capabilities and row CRUD per sheet, the 404 mask, Idempotency-Key replay, soft delete, injected failures and a request log.
type Opensheet struct {
	srv     *httptest.Server
	mu      sync.Mutex
	org     string
	project string
	token   string
	tabs    map[string]*opensheetTab
	fail    map[string]*opensheetFailure
	log     []OpensheetRequest
	header  bool
}

// NewOpensheet starts a fake serving org/project behind token; it stops when t ends.
func NewOpensheet(t testing.TB, org, project, token string) *Opensheet {
	t.Helper()
	f := &Opensheet{org: org, project: project, token: token, tabs: map[string]*opensheetTab{}, fail: map[string]*opensheetFailure{}}
	const base = "/api/v1/orgs/{org}/projects/{project}/sheets/{slug}"
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base+"/capabilities", f.capabilities)
	mux.HandleFunc("POST "+base+"/rows", f.create)
	mux.HandleFunc("GET "+base+"/rows/{id}", f.read)
	mux.HandleFunc("PUT "+base+"/rows/{id}", f.replace)
	mux.HandleFunc("PATCH "+base+"/rows/{id}", f.patch)
	mux.HandleFunc("DELETE "+base+"/rows/{id}", f.remove)
	mux.HandleFunc("/", f.unrouted)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL a client is configured with.
func (f *Opensheet) URL() string { return f.srv.URL }

// AddSheet publishes slug with the header row s.Columns.
func (f *Opensheet) AddSheet(slug string, s OpensheetSheet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tabs[slug] = &opensheetTab{OpensheetSheet: s, rows: map[string]map[string]string{}, idem: map[string]opensheetReplay{}}
}

// SetHeaderColumns makes capabilities report the header row as columns, as opensheet will once it stops deriving them from the first live row.
func (f *Opensheet) SetHeaderColumns(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.header = on
}

// SeedRow stores row in slug as if the sheet already held it, every header key filled; it panics on an unknown slug.
func (f *Opensheet) SeedRow(slug string, row map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab := f.tabs[slug]
	stored := tab.blankRow()
	maps.Copy(stored, row)
	tab.put(stored["id"], stored)
}

// SetToken changes the bearer token the fake accepts.
func (f *Opensheet) SetToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = token
}

// FailNext answers the next n requests of method on slug with status and an error envelope carrying code.
func (f *Opensheet) FailNext(method, slug string, status int, code string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[method+" "+slug] = &opensheetFailure{status: status, code: code, left: n}
}

// Row returns a copy of one stored row, tombstones included.
func (f *Opensheet) Row(slug, id string) (map[string]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, ok := f.tabs[slug]
	if !ok {
		return nil, false
	}
	row, ok := tab.rows[id]
	return maps.Clone(row), ok
}

// Rows returns a copy of every stored row of slug, tombstones included.
func (f *Opensheet) Rows(slug string) map[string]map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]map[string]string{}
	if tab, ok := f.tabs[slug]; ok {
		for id, row := range tab.rows {
			out[id] = maps.Clone(row)
		}
	}
	return out
}

// Requests returns a copy of the request log.
func (f *Opensheet) Requests() []OpensheetRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

// CountRequests counts the logged requests of method on slug.
func (f *Opensheet) CountRequests(method, slug string) int {
	n := 0
	for _, r := range f.Requests() {
		if r.Method == method && r.Slug == slug {
			n++
		}
	}
	return n
}

const (
	opensheetDeletedAt = "deleted_at"
	opensheetTombstone = "2026-01-01T00:00:00Z"
)

// NOTE: every handler holds f.mu for the whole request, so the fake is linearizable.
func (f *Opensheet) admit(w http.ResponseWriter, r *http.Request, body map[string]string) (*opensheetTab, bool) {
	slug := r.PathValue("slug")
	f.log = append(f.log, OpensheetRequest{
		Method: r.Method, Slug: slug, ID: r.PathValue("id"),
		IdempotencyKey: r.Header.Get("Idempotency-Key"), Body: body,
	})
	tab, ok := f.tabs[slug]
	if !ok || r.Header.Get("Authorization") != "Bearer "+f.token ||
		r.PathValue("org") != f.org || r.PathValue("project") != f.project {
		writeOpensheetError(w, http.StatusNotFound, "SHT001")
		return nil, false
	}
	if fl, ok := f.fail[r.Method+" "+slug]; ok && fl.left > 0 {
		fl.left--
		writeOpensheetError(w, fl.status, fl.code)
		return nil, false
	}
	return tab, true
}

func (f *Opensheet) admitWrite(w http.ResponseWriter, r *http.Request) (*opensheetTab, map[string]string, bool) {
	raw, err := io.ReadAll(r.Body)
	body, valid := decodeOpensheetRow(raw)
	tab, ok := f.admit(w, r, body)
	if !ok {
		return nil, nil, false
	}
	if err != nil || !valid {
		writeOpensheetError(w, http.StatusBadRequest, "GEN004")
		return nil, nil, false
	}
	if !tab.Writable {
		writeOpensheetError(w, http.StatusForbidden, "SHT009")
		return nil, nil, false
	}
	return tab, body, true
}

func (f *Opensheet) unrouted(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, OpensheetRequest{Method: r.Method, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	writeOpensheetError(w, http.StatusNotFound, "SHT001")
}

func (f *Opensheet) capabilities(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, ok := f.admit(w, r, nil)
	if !ok {
		return
	}
	hasID := slices.Contains(tab.Columns, "id")
	reason := ""
	if !hasID {
		reason = "the tab has no id column"
	}
	live := 0
	for _, row := range tab.rows {
		if row[opensheetDeletedAt] == "" {
			live++
		}
	}
	columns := tab.reportedColumns(f.header)
	idColumn := hasID
	if len(columns) > 0 {
		idColumn = slices.Contains(columns, "id")
	}
	writeOpensheetJSON(w, http.StatusOK, map[string]any{
		"columns": columns, "idColumn": idColumn, "softDelete": tab.softDelete(),
		"writable": tab.Writable, "satisfiesContract": hasID, "contractReason": reason,
		"rowCount": live, "generation": 1, "validatedAt": opensheetTombstone,
	})
}

func (t *opensheetTab) softDelete() bool { return slices.Contains(t.Columns, opensheetDeletedAt) }

// NOTE: mirrors opensheet's tableColumns: the keys of the first live row by row order, plus deleted_at when soft delete is on, sorted.
func (t *opensheetTab) reportedColumns(header bool) []string {
	out := []string{}
	if header {
		out = append(out, t.Columns...)
		slices.Sort(out)
		return out
	}
	for _, id := range t.order {
		row := t.rows[id]
		if row[opensheetDeletedAt] != "" {
			continue
		}
		for col := range row {
			if col != opensheetDeletedAt {
				out = append(out, col)
			}
		}
		break
	}
	if t.softDelete() {
		out = append(out, opensheetDeletedAt)
	}
	slices.Sort(out)
	return out
}

func (t *opensheetTab) put(id string, row map[string]string) {
	if _, seen := t.rows[id]; !seen {
		t.order = append(t.order, id)
	}
	t.rows[id] = row
}

func (f *Opensheet) create(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, body, ok := f.admitWrite(w, r)
	if !ok {
		return
	}
	key, hash := r.Header.Get("Idempotency-Key"), opensheetHash(body)
	if prior, seen := tab.idem[key]; key != "" && seen {
		if prior.hash != hash {
			writeOpensheetError(w, http.StatusUnprocessableEntity, "SHT018")
			return
		}
		writeOpensheetJSON(w, http.StatusCreated, prior.row)
		return
	}
	if !tab.resolve(w, body, func(string) (int, string) { return 0, "" }) {
		return
	}
	id, supplied := body["id"]
	if !supplied {
		id = fmt.Sprintf("gen-%d", len(tab.rows)+1)
	}
	if strings.TrimSpace(id) == "" {
		writeOpensheetError(w, http.StatusBadRequest, "SHT016")
		return
	}
	if _, taken := tab.rows[id]; taken {
		writeOpensheetError(w, http.StatusConflict, "SHT012")
		return
	}
	row := tab.blankRow()
	maps.Copy(row, body)
	row["id"] = id
	tab.put(id, row)
	out := visibleOpensheetRow(row)
	if key != "" {
		tab.idem[key] = opensheetReplay{hash: hash, row: out}
	}
	writeOpensheetJSON(w, http.StatusCreated, out)
}

func (f *Opensheet) read(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, ok := f.admit(w, r, nil)
	if !ok {
		return
	}
	row, ok := tab.live(r.PathValue("id"))
	if !ok {
		writeOpensheetError(w, http.StatusNotFound, "SHT013")
		return
	}
	writeOpensheetJSON(w, http.StatusOK, visibleOpensheetRow(row))
}

func (f *Opensheet) patch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, body, ok := f.admitWrite(w, r)
	if !ok {
		return
	}
	if len(body) == 0 {
		writeOpensheetError(w, http.StatusBadRequest, "SHT016")
		return
	}
	row, ok := tab.rows[r.PathValue("id")]
	if !ok {
		writeOpensheetError(w, http.StatusNotFound, "SHT013")
		return
	}
	if !tab.resolve(w, body, func(string) (int, string) { return http.StatusBadRequest, "SHT015" }) {
		return
	}
	maps.Copy(row, body)
	writeOpensheetJSON(w, http.StatusOK, visibleOpensheetRow(row))
}

func (f *Opensheet) replace(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, body, ok := f.admitWrite(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	prior, ok := tab.live(id)
	if !ok {
		writeOpensheetError(w, http.StatusNotFound, "SHT013")
		return
	}
	idMustMatch := func(v string) (int, string) {
		if v == id {
			return 0, ""
		}
		return http.StatusUnprocessableEntity, "SHT027"
	}
	if !tab.resolve(w, body, idMustMatch) {
		return
	}
	row := tab.blankRow()
	maps.Copy(row, body)
	row["id"] = id
	if _, soft := row[opensheetDeletedAt]; soft {
		row[opensheetDeletedAt] = prior[opensheetDeletedAt]
	}
	tab.rows[id] = row
	writeOpensheetJSON(w, http.StatusOK, visibleOpensheetRow(row))
}

func (f *Opensheet) remove(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab, ok := f.admit(w, r, nil)
	if !ok {
		return
	}
	if !tab.Writable {
		writeOpensheetError(w, http.StatusForbidden, "SHT009")
		return
	}
	if !tab.softDelete() {
		writeOpensheetError(w, http.StatusUnprocessableEntity, "SHT029")
		return
	}
	row, ok := tab.rows[r.PathValue("id")]
	if !ok {
		writeOpensheetError(w, http.StatusNotFound, "SHT013")
		return
	}
	if row[opensheetDeletedAt] == "" {
		row[opensheetDeletedAt] = opensheetTombstone
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *opensheetTab) live(id string) (map[string]string, bool) {
	row, ok := t.rows[id]
	if !ok || row[opensheetDeletedAt] != "" {
		return nil, false
	}
	return row, true
}

func (t *opensheetTab) blankRow() map[string]string {
	row := make(map[string]string, len(t.Columns))
	for _, col := range t.Columns {
		row[col] = ""
	}
	return row
}

func (t *opensheetTab) resolve(w http.ResponseWriter, body map[string]string, onID func(value string) (int, string)) bool {
	for _, col := range slices.Sorted(maps.Keys(body)) {
		if !slices.Contains(t.Columns, col) {
			writeOpensheetError(w, http.StatusBadRequest, "SHT014")
			return false
		}
		if col == opensheetDeletedAt {
			writeOpensheetError(w, http.StatusBadRequest, "SHT015")
			return false
		}
		if col != "id" {
			continue
		}
		if status, code := onID(body[col]); status != 0 {
			writeOpensheetError(w, status, code)
			return false
		}
	}
	return true
}

func visibleOpensheetRow(row map[string]string) map[string]string {
	out := maps.Clone(row)
	delete(out, opensheetDeletedAt)
	return out
}

func decodeOpensheetRow(raw []byte) (map[string]string, bool) {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, false
	}
	delete(fields, "numeric_columns")
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		switch t := v.(type) {
		case nil:
			out[k] = ""
		case string:
			out[k] = t
		case bool:
			out[k] = strconv.FormatBool(t)
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		default:
			return nil, false
		}
	}
	return out, true
}

func opensheetHash(body map[string]string) string {
	keys := slices.Sorted(maps.Keys(body))
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "\x00" + body[k] + "\x00")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func writeOpensheetJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOpensheetError(w http.ResponseWriter, status int, code string) {
	writeOpensheetJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": code}})
}
