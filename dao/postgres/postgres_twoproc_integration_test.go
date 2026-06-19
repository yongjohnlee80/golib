//go:build integration

// Two-process integration harness for golib/dao + dao/postgres, built on golib's
// own HTTP server component (golib/server/http) and driven by the golib request
// client. It extends the single-process scaffold in postgres_integration_test.go
// (which it reuses widget/widgetFields/itoa from) into a concurrent, multi-session
// suite against a REAL Postgres.
//
// "Two processes" are modelled as two independent httpserver.Server instances,
// each owning its OWN distinctly-named dao.DataConn (its own pgx pool) to the one
// shared `golib` database — the realistic shape for testing cross-session DB
// behaviour inside one test binary. Distinct connection names ("pg-a"/"pg-b") are
// required so a single dao transaction can hold one tx per session (ADR-0005).
//
// Set TEST_PGURL (e.g. postgres://user:pass@localhost:5432/golib?sslmode=disable)
// and run:  go test -tags integration ./dao/postgres/
// Without TEST_PGURL every test here skips.
//
// Out of scope (separate follow-up): true two-phase commit. With single-DB
// sessions, CommitError.AlreadyDurable can only be non-empty under genuine 2PC,
// which PostgresDialect.TwoPhaseSupported() intentionally reports false for.
package postgres

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/request"
	httpserver "github.com/yongjohnlee80/golib/server/http"
)

// --- the related `part` entity (for on-demand joins + cross-table tx) -------

type part struct {
	ID         int
	WidgetID   int
	Label      string
	WidgetName string // populated only when the optional "widget" join is applied
}

type partField string

const (
	pID       partField = "id"
	pWidgetID partField = "widget_id"
	pLabel    partField = "label"
	pWgName   partField = "widget_name"
)

type partSort string

// Select columns are qualified so they stay unambiguous once the widget join is
// applied (both tables have an "id"); dao derives the bare write column from the
// tail after the last dot, so INSERTs still target plain widget_id / label.
var partFields = map[partField]dao.Field[*part]{
	pID:       {Column: "golib_dao_part.id", Scan: func(p *part) any { return &p.ID }},
	pWidgetID: {Column: "golib_dao_part.widget_id", Scan: func(p *part) any { return &p.WidgetID }},
	pLabel:    {Column: "golib_dao_part.label", Scan: func(p *part) any { return &p.Label }},
	pWgName: {
		Column:   "COALESCE(golib_dao_widget.name, '')",
		Join:     "widget",
		ReadOnly: true,
		Scan:     func(p *part) any { return &p.WidgetName },
	},
}

type (
	widgetSchema = dao.Schema[*widget, widgetField, widgetSort, int]
	partSchema   = dao.Schema[*part, partField, partSort, int]
)

func buildWidgetSchema(conn dao.DataConn) *widgetSchema {
	return dao.New[*widget, widgetField, widgetSort, int](conn,
		dao.Table[*widget, widgetField, widgetSort, int]("golib_dao_widget"),
		dao.ID[*widget, widgetField, widgetSort, int](wID),
		dao.Fields[*widget, widgetField, widgetSort, int](widgetFields),
		dao.Default[*widget, widgetField, widgetSort, int](wID, wName, wQty),
		dao.Conflict[*widget, widgetField, widgetSort, int](wName),
	)
}

func buildPartSchema(conn dao.DataConn) *partSchema {
	return dao.New[*part, partField, partSort, int](conn,
		dao.Table[*part, partField, partSort, int]("golib_dao_part"),
		dao.ID[*part, partField, partSort, int](pID),
		dao.Fields[*part, partField, partSort, int](partFields),
		dao.Default[*part, partField, partSort, int](pID, pWidgetID, pLabel, pWgName),
		dao.OptionalJoin[*part, partField, partSort, int]("widget",
			"LEFT JOIN golib_dao_widget ON golib_dao_widget.id = golib_dao_part.widget_id"),
		dao.Conflict[*part, partField, partSort, int](pWidgetID, pLabel),
	)
}

// --- HTTP DTOs (shared request/response shapes for the dao-backed endpoints) -

type widgetIn struct {
	Name string `json:"name"`
	Qty  int    `json:"qty"`
}

type batchIn struct {
	Mode   string `json:"mode"` // "copy" (native COPY) or "insert" (chunked INSERT)
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
}

type txIn struct {
	Name  string   `json:"name"`
	Parts []string `json:"parts"`
}

type idResp struct {
	ID int `json:"id"`
}

// --- the two-process harness ------------------------------------------------

// proc is one "process": a server bound to an ephemeral port, backed by its own
// named connection and a widget+part schema pair on that connection.
type proc struct {
	name string
	conn dao.DataConn
	ws   *widgetSchema
	ps   *partSchema
	base string // e.g. http://127.0.0.1:54321
}

// registerRoutes wires the dao-backed endpoints onto srv. The error-returning
// httpserver.Handler adapter maps a *StatusError to its status; dao.ErrDuplicate
// is surfaced as 409 and dao.ErrNoRows as 404.
func registerRoutes(srv *httpserver.Server, conn dao.DataConn, ws *widgetSchema, ps *partSchema) {
	// Insert one widget; RETURNING id. Unique-name violation -> 409.
	srv.Post("/widgets", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		in, err := httpserver.Decode[widgetIn](r, 0)
		if err != nil {
			return httpserver.Status(http.StatusBadRequest, err.Error())
		}
		id, err := ws.DAO().Set(wName, in.Name).Set(wQty, in.Qty).Insert()
		switch {
		case errors.Is(err, dao.ErrDuplicate):
			return httpserver.Status(http.StatusConflict, "duplicate")
		case err != nil:
			return err
		}
		return httpserver.JSON(w, http.StatusCreated, idResp{ID: id})
	}))

	// Upsert on the unique name (ON CONFLICT DO UPDATE) — concurrency-safe.
	srv.Post("/widgets/upsert", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		in, err := httpserver.Decode[widgetIn](r, 0)
		if err != nil {
			return httpserver.Status(http.StatusBadRequest, err.Error())
		}
		if err := ws.DAO().Set(wName, in.Name).Set(wQty, in.Qty).Upsert(); err != nil {
			return err
		}
		return httpserver.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))

	// Bulk insert via the native COPY fast-path or the chunked-INSERT path.
	srv.Post("/widgets/batch", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		in, err := httpserver.Decode[batchIn](r, 0)
		if err != nil {
			return httpserver.Status(http.StatusBadRequest, err.Error())
		}
		b := ws.DAO().Batch()
		if in.Mode == "copy" {
			b.ForceCopy()
		} else {
			b.ForceInsert()
		}
		for i := 0; i < in.Count; i++ {
			b.Add(map[widgetField]any{wName: in.Prefix + itoa(i), wQty: i})
		}
		if err := b.Flush(); err != nil {
			return err
		}
		return httpserver.JSON(w, http.StatusOK, map[string]int{"inserted": in.Count})
	}))

	// Multi-statement transaction across two tables on ONE session: insert a
	// widget then its parts. Any failure (e.g. a duplicate part label) rolls back
	// the whole unit (ADR-0005).
	srv.Post("/tx/widget-with-parts", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		in, err := httpserver.Decode[txIn](r, 0)
		if err != nil {
			return httpserver.Status(http.StatusBadRequest, err.Error())
		}
		var widgetID int
		txErr := dao.RunTx(r.Context(), []dao.DataConn{conn}, func(tx *dao.Transaction) error {
			id, e := ws.On(tx).Set(wName, in.Name).Insert()
			if e != nil {
				return e
			}
			widgetID = id
			for _, label := range in.Parts {
				if _, e := ps.On(tx).Set(pWidgetID, id).Set(pLabel, label).Insert(); e != nil {
					return e
				}
			}
			return nil
		})
		switch {
		case errors.Is(txErr, dao.ErrDuplicate):
			return httpserver.Status(http.StatusConflict, "duplicate")
		case txErr != nil:
			return txErr
		}
		return httpserver.JSON(w, http.StatusCreated, idResp{ID: widgetID})
	}))

	// Read a part WITH its widget's name — selecting widget_name triggers the
	// optional join on demand.
	srv.Get("/parts/{id}", httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		id, err := strconv.Atoi(httpserver.URLParam(r, "id"))
		if err != nil {
			return httpserver.Status(http.StatusBadRequest, "bad id")
		}
		got, err := ps.DAO().With(pID, id).Get()
		switch {
		case errors.Is(err, dao.ErrNoRows):
			return httpserver.Status(http.StatusNotFound, "not found")
		case err != nil:
			return err
		}
		return httpserver.JSON(w, http.StatusOK, got)
	}))
}

// startProc opens a named connection, builds the schemas, and starts a server on
// an ephemeral port. Cleanup cancels the server and closes the connection.
func startProc(t *testing.T, name, url string) *proc {
	t.Helper()
	conn, err := OpenNamed(context.Background(), name, url)
	if err != nil {
		t.Fatalf("OpenNamed(%s): %v", name, err)
	}
	ws := buildWidgetSchema(conn)
	ps := buildPartSchema(conn)

	srv := httpserver.New(httpserver.Addr("127.0.0.1:0"))
	registerRoutes(srv, conn, ws, ps)
	if err := srv.Listen(context.Background()); err != nil {
		_ = conn.Close()
		t.Fatalf("Listen(%s): %v", name, err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Run(runCtx) }()

	t.Cleanup(func() {
		cancel()
		_ = conn.Close()
	})
	return &proc{name: name, conn: conn, ws: ws, ps: ps, base: "http://" + srv.Addr()}
}

// setupTwoProc applies the migration schema once (clean slate) then starts two
// processes sharing the one database. It skips when TEST_PGURL is unset.
func setupTwoProc(t *testing.T) (a, b *proc) {
	t.Helper()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping two-process postgres integration tests")
	}
	admin, err := OpenNamed(context.Background(), "pg-admin", url)
	if err != nil {
		t.Fatalf("OpenNamed(admin): %v", err)
	}
	applyMigrations(t, admin, dirDown) // tolerate + clear any leftovers
	applyMigrations(t, admin, dirUp)
	t.Cleanup(func() {
		applyMigrations(t, admin, dirDown)
		_ = admin.Close()
	})

	return startProc(t, "pg-a", url), startProc(t, "pg-b", url)
}

// --- tests ------------------------------------------------------------------

// TestPG2P_ConcurrentInsertUniqueRace fires many concurrent inserts of the SAME
// name across both processes: exactly one wins, every other maps to ErrDuplicate
// (SQLSTATE 23505) -> 409.
func TestPG2P_ConcurrentInsertUniqueRace(t *testing.T) {
	a, b := setupTwoProc(t)
	const n = 24
	const name = "race-widget"

	var success, dup atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		base := a.base
		if i%2 == 1 {
			base = b.base
		}
		go func(base string) {
			defer wg.Done()
			var out idResp
			_, err := request.Post(base+"/widgets", widgetIn{Name: name, Qty: 1}, &out)
			if err == nil {
				success.Add(1)
				return
			}
			var re *request.Error
			if errors.As(err, &re) && re.Status == http.StatusConflict {
				dup.Add(1)
				return
			}
			t.Errorf("unexpected error: %v", err)
		}(base)
	}
	wg.Wait()

	if success.Load() != 1 {
		t.Errorf("exactly one insert should win, got %d", success.Load())
	}
	if dup.Load() != n-1 {
		t.Errorf("expected %d duplicates, got %d", n-1, dup.Load())
	}
	if got, _ := a.ws.DAO().With(wName, name).Count(); got != 1 {
		t.Errorf("db row count = %d, want 1", got)
	}
}

// TestPG2P_ConcurrentUpsertConverge runs many concurrent upserts of the same name
// across both processes; ON CONFLICT DO UPDATE serialises them, so all succeed and
// converge to a single row.
func TestPG2P_ConcurrentUpsertConverge(t *testing.T) {
	a, b := setupTwoProc(t)
	const n = 24
	const name = "upsert-widget"

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		base := a.base
		if i%2 == 1 {
			base = b.base
		}
		go func(base string, qty int) {
			defer wg.Done()
			var out map[string]string
			if _, err := request.Post(base+"/widgets/upsert", widgetIn{Name: name, Qty: qty}, &out); err != nil {
				t.Errorf("upsert: %v", err)
			}
		}(base, i)
	}
	wg.Wait()

	if got, _ := a.ws.DAO().With(wName, name).Count(); got != 1 {
		t.Errorf("upserts should converge to one row, got %d", got)
	}
}

// TestPG2P_CrossTableTxOverHTTP drives the multi-statement transaction endpoint:
// the commit path persists a widget + its parts atomically; the rollback path
// (a duplicate part label) discards the whole unit, including the widget.
func TestPG2P_CrossTableTxOverHTTP(t *testing.T) {
	a, _ := setupTwoProc(t)

	// Commit: widget + two parts.
	var out idResp
	if _, err := request.Post(a.base+"/tx/widget-with-parts",
		txIn{Name: "gizmo", Parts: []string{"left", "right"}}, &out); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if out.ID <= 0 {
		t.Fatalf("RETURNING widget id = %d, want > 0", out.ID)
	}
	if got, _ := a.ws.DAO().With(wName, "gizmo").Count(); got != 1 {
		t.Errorf("committed widget count = %d, want 1", got)
	}
	if got, _ := a.ps.DAO().With(pWidgetID, out.ID).Count(); got != 2 {
		t.Errorf("committed part count = %d, want 2", got)
	}

	// Rollback: duplicate label within the same widget violates UNIQUE(widget_id,
	// label); the widget insert must roll back too.
	var out2 idResp
	_, err := request.Post(a.base+"/tx/widget-with-parts",
		txIn{Name: "gadget", Parts: []string{"dup", "dup"}}, &out2)
	var re *request.Error
	if !errors.As(err, &re) || re.Status != http.StatusConflict {
		t.Fatalf("rollback tx: err = %v, want 409 *request.Error", err)
	}
	if got, _ := a.ws.DAO().With(wName, "gadget").Count(); got != 0 {
		t.Errorf("rolled-back widget should not persist, count = %d", got)
	}
}

// TestPG2P_CrossInstanceTx coordinates ONE dao transaction across both processes'
// connections (distinct names, same database): RunTx commits them in order on
// success and rolls both back on a returned error. With single-DB sessions a
// caller-returned error never surfaces as a CommitError (no 2PC; out of scope).
//
// NOTE: this case drives the two servers' underlying connections DIRECTLY (not
// over HTTP) — it proves dao's two-connection ordered-commit/rollback path, not a
// distributed transaction coordinated across the HTTP boundary.
func TestPG2P_CrossInstanceTx(t *testing.T) {
	a, b := setupTwoProc(t)
	ctx := context.Background()

	// Commit: each session inserts an independent widget under one coordinated tx.
	if err := dao.RunTx(ctx, []dao.DataConn{a.conn, b.conn}, func(tx *dao.Transaction) error {
		if _, e := a.ws.On(tx).Set(wName, "ci-a").Insert(); e != nil {
			return e
		}
		_, e := b.ws.On(tx).Set(wName, "ci-b").Insert()
		return e
	}); err != nil {
		t.Fatalf("cross-instance commit: %v", err)
	}
	if got, _ := a.ws.DAO().With(wName, "ci-a").Count(); got != 1 {
		t.Errorf("ci-a missing after commit")
	}
	if got, _ := a.ws.DAO().With(wName, "ci-b").Count(); got != 1 {
		t.Errorf("ci-b missing after commit")
	}

	// Rollback: fn returns an error after staging inserts on both sessions.
	rbErr := dao.RunTx(ctx, []dao.DataConn{a.conn, b.conn}, func(tx *dao.Transaction) error {
		if _, e := a.ws.On(tx).Set(wName, "ci-rb-a").Insert(); e != nil {
			return e
		}
		if _, e := b.ws.On(tx).Set(wName, "ci-rb-b").Insert(); e != nil {
			return e
		}
		return errors.New("forced rollback")
	})
	if rbErr == nil {
		t.Fatal("expected the forced rollback error")
	}
	var ce *dao.CommitError
	if errors.As(rbErr, &ce) {
		t.Errorf("a caller-returned rollback should not be a CommitError, got %+v", ce)
	}
	if got, _ := a.ws.DAO().With(wName, "ci-rb-a").Count(); got != 0 {
		t.Errorf("ci-rb-a should have rolled back, count = %d", got)
	}
	if got, _ := a.ws.DAO().With(wName, "ci-rb-b").Count(); got != 0 {
		t.Errorf("ci-rb-b should have rolled back, count = %d", got)
	}
}

// TestPG2P_BatchAcrossProcesses runs a native COPY batch on one process and a
// chunked-INSERT batch on the other, concurrently, into the shared table.
func TestPG2P_BatchAcrossProcesses(t *testing.T) {
	a, b := setupTwoProc(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var out map[string]int
		if _, err := request.Post(a.base+"/widgets/batch",
			batchIn{Mode: "copy", Prefix: "copy-", Count: 500}, &out); err != nil {
			t.Errorf("COPY batch: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		var out map[string]int
		if _, err := request.Post(b.base+"/widgets/batch",
			batchIn{Mode: "insert", Prefix: "ins-", Count: 300}, &out); err != nil {
			t.Errorf("chunked batch: %v", err)
		}
	}()
	wg.Wait()

	if got, _ := a.ws.DAO().Count(); got != 800 {
		t.Errorf("total rows = %d, want 800 (500 COPY + 300 chunked)", got)
	}
}

// TestPG2P_ReturningDistinctIDs confirms concurrent inserts across both processes
// each get a distinct, positive RETURNING id.
func TestPG2P_ReturningDistinctIDs(t *testing.T) {
	a, b := setupTwoProc(t)
	const n = 10

	ids := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		base := a.base
		if i%2 == 1 {
			base = b.base
		}
		go func(base string, i int) {
			defer wg.Done()
			var out idResp
			if _, err := request.Post(base+"/widgets", widgetIn{Name: "ret-" + itoa(i), Qty: i}, &out); err != nil {
				t.Errorf("insert: %v", err)
				return
			}
			ids <- out.ID
		}(base, i)
	}
	wg.Wait()
	close(ids)

	seen := make(map[int]bool, n)
	for id := range ids {
		if id <= 0 {
			t.Errorf("non-positive RETURNING id %d", id)
		}
		if seen[id] {
			t.Errorf("duplicate RETURNING id %d", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("got %d distinct ids, want %d", len(seen), n)
	}
}

// TestPG2P_OnDemandJoin verifies the optional widget join fires when widget_name
// is selected (here via the default columns on GET /parts/{id}).
func TestPG2P_OnDemandJoin(t *testing.T) {
	a, _ := setupTwoProc(t)

	var seed idResp
	if _, err := request.Post(a.base+"/tx/widget-with-parts",
		txIn{Name: "joiner", Parts: []string{"only"}}, &seed); err != nil {
		t.Fatalf("seed widget+part: %v", err)
	}
	p, err := a.ps.DAO().With(pWidgetID, seed.ID).Get()
	if err != nil {
		t.Fatalf("locate seeded part: %v", err)
	}

	var got part
	if _, err := request.Get(a.base+"/parts/"+itoa(p.ID), nil, &got); err != nil {
		t.Fatalf("GET part: %v", err)
	}
	if got.Label != "only" {
		t.Errorf("label = %q, want only", got.Label)
	}
	if got.WidgetName != "joiner" {
		t.Errorf("on-demand join: widget_name = %q, want joiner", got.WidgetName)
	}
}

// --- migration loader (parses the sql-migrate sections in testdata/migrations) -

type direction int

const (
	dirUp direction = iota
	dirDown
)

// applyMigrations applies the requested section of every migration file in
// lexical order (reversed for Down). Down errors are tolerated so teardown is
// idempotent even if setup half-failed.
func applyMigrations(t *testing.T, conn dao.DataConn, dir direction) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	if dir == dirDown {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, stmt := range splitStatements(extractSection(string(body), dir)) {
			if _, err := conn.ExecContext(context.Background(), stmt); err != nil {
				if dir == dirDown {
					continue // teardown is best-effort
				}
				t.Fatalf("apply %s:\n%s\n%v", f, stmt, err)
			}
		}
	}
}

// extractSection returns the SQL between the "-- +migrate Up"/"-- +migrate Down"
// markers for the requested direction.
func extractSection(body string, dir direction) string {
	const upMark, downMark = "-- +migrate Up", "-- +migrate Down"
	up := strings.Index(body, upMark)
	down := strings.Index(body, downMark)
	if dir == dirUp {
		if up < 0 {
			return ""
		}
		start := up + len(upMark)
		if down > start {
			return body[start:down]
		}
		return body[start:]
	}
	if down < 0 {
		return ""
	}
	return body[down+len(downMark):]
}

// splitStatements strips line comments first, THEN splits on ";", returning the
// non-empty statements. Stripping comments up front is essential: a ";" inside a
// comment (e.g. "-- concurrency tests; serial id ...") must not split a statement.
//
// It is intentionally simple: it strips from the first "--" to end-of-line and
// does NOT understand semicolons or "--" inside string literals or function
// bodies, which is sufficient for the simple DDL in testdata/migrations.
// Migrations with such statements would need the sql-migrate
// "-- +migrate StatementBegin/End" markers and a real parser.
func splitStatements(block string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(block, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i] // drop the line comment (and any ";" within it)
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	var out []string
	for _, raw := range strings.Split(cleaned.String(), ";") {
		if stmt := strings.TrimSpace(raw); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}
