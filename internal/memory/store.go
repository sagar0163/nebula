package memory

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"

	"github.com/sagar0163/nebula/internal/models"
)

const (
	dbDriverName      = "sqlite3"
	busyTimeoutMillis = 5000
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLiteStore is a persistent, SQLite-backed implementation of Store.
type SQLiteStore struct {
	db *sqlx.DB
}

var _ Store = (*SQLiteStore)(nil)

// New opens (creating if necessary) the SQLite database at dbPath,
// configures WAL mode and busy_timeout, and applies any pending migrations.
func New(dbPath string) (*SQLiteStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("memory: empty database path")
	}

	db, err := sql.Open(dbDriverName, buildDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// A single connection serializes access and avoids SQLITE_BUSY for the
	// CLI's read-mostly workload.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	subFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("sub migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, subFS)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return &SQLiteStore{db: sqlx.NewDb(db, dbDriverName)}, nil
}

// buildDSN builds the database/sql data source name for the ncruces SQLite
// driver, enabling WAL mode, a busy timeout, and foreign keys via _pragma.
// The pragmas are applied by the driver whenever a connection is opened.
func buildDSN(path string) string {
	params := url.Values{}
	params.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillis))
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "foreign_keys(1)")

	switch {
	case path == ":memory:" || strings.HasPrefix(path, "file::memory:"):
		return "file::memory:?cache=shared&" + params.Encode()
	case strings.HasPrefix(path, "file:"):
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return path + sep + params.Encode()
	default:
		return "file:" + url.PathEscape(path) + "?" + params.Encode()
	}
}

// CreateSession inserts the session, or updates it if the ID already exists.
func (s *SQLiteStore) CreateSession(ctx context.Context, sess *models.Session) error {
	if sess == nil {
		return errors.New("memory: nil session")
	}
	if strings.TrimSpace(sess.ID) == "" {
		sess.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt.IsZero() {
		sess.UpdatedAt = now
	}

	const q = `INSERT INTO sessions (id, created_at, updated_at, work_dir, summary)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			updated_at = excluded.updated_at,
			work_dir   = excluded.work_dir,
			summary    = excluded.summary`

	if _, err := s.db.ExecContext(ctx, q,
		sess.ID, sess.CreatedAt, sess.UpdatedAt, sess.WorkDir, sess.Summary,
	); err != nil {
		return fmt.Errorf("create session %q: %w", sess.ID, err)
	}
	return nil
}

// GetSession returns the session with the given ID, or database/sql.ErrNoRows
// if no such session exists.
func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*models.Session, error) {
	var sess models.Session
	err := s.db.GetContext(ctx, &sess,
		`SELECT id, created_at, updated_at, work_dir, summary FROM sessions WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get session %q: %w", id, err)
	}
	return &sess, nil
}

// ListSessions returns sessions most recently updated first.
// A limit <= 0 returns all sessions.
func (s *SQLiteStore) ListSessions(ctx context.Context, limit int) ([]*models.Session, error) {
	q := `SELECT id, created_at, updated_at, work_dir, summary FROM sessions ORDER BY updated_at DESC, rowid DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	sessions := []*models.Session{}
	if err := s.db.SelectContext(ctx, &sessions, q, args...); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// SaveCommand persists a shell command and refreshes its session timestamp.
func (s *SQLiteStore) SaveCommand(ctx context.Context, cmd *models.Command) error {
	if cmd == nil {
		return errors.New("memory: nil command")
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now().UTC()
	}

	const q = `INSERT INTO commands (session_id, raw, exit_code, stdout, stderr, elapsed_ms, work_dir, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q,
		cmd.SessionID, cmd.Raw, cmd.ExitCode, cmd.Stdout, cmd.Stderr,
		cmd.Elapsed, cmd.WorkDir, cmd.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save command: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		cmd.ID = id
	}

	if cmd.SessionID != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET updated_at = ? WHERE id = ?`, cmd.CreatedAt, cmd.SessionID,
		); err != nil {
			return fmt.Errorf("touch session %q: %w", cmd.SessionID, err)
		}
	}
	return nil
}

// RecentCommands returns commands for a session (or all sessions when
// sessionID is empty), most recent first. A limit <= 0 returns no limit.
func (s *SQLiteStore) RecentCommands(ctx context.Context, sessionID string, limit int) ([]*models.Command, error) {
	q := `SELECT id, session_id, raw, exit_code, stdout, stderr, elapsed_ms, work_dir, created_at
		FROM commands`
	args := []any{}
	if sessionID != "" {
		q += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	cmds := []*models.Command{}
	if err := s.db.SelectContext(ctx, &cmds, q, args...); err != nil {
		return nil, fmt.Errorf("recent commands: %w", err)
	}
	return cmds, nil
}

// SavePattern persists a learned fix pattern. The embedding is stored verbatim
// as its gob-encoded []float32 bytes.
func (s *SQLiteStore) SavePattern(ctx context.Context, p *models.Pattern) error {
	if p == nil {
		return errors.New("memory: nil pattern")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	const q = `INSERT INTO patterns (fail_cmd, fail_output, fix_cmd, success_rate, use_count, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q,
		p.FailCmd, p.FailOutput, p.FixCmd, p.SuccessRate, p.UseCount,
		p.Embedding, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save pattern: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		p.ID = id
	}
	return nil
}

// FindSimilarPatterns decodes every stored pattern embedding and returns the
// top-K patterns whose embedding is most similar to the input, sorted by
// score descending. Patterns with unreadable embeddings are skipped.
// A topK <= 0 returns all matches.
func (s *SQLiteStore) FindSimilarPatterns(ctx context.Context, embedding []float32, topK int) ([]*models.Pattern, error) {
	patterns := []*models.Pattern{}
	if err := s.db.SelectContext(ctx, &patterns,
		`SELECT id, fail_cmd, fail_output, fix_cmd, success_rate, use_count, embedding, created_at, updated_at
			FROM patterns`,
	); err != nil {
		return nil, fmt.Errorf("load patterns: %w", err)
	}

	type scoredPattern struct {
		pattern *models.Pattern
		score   float32
	}
	results := make([]scoredPattern, 0, len(patterns))
	for _, p := range patterns {
		vec, err := gobDecodeFloats(p.Embedding)
		if err != nil {
			continue
		}
		results = append(results, scoredPattern{pattern: p, score: CosineSimilarity(embedding, vec)})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].pattern.UseCount > results[j].pattern.UseCount
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	out := make([]*models.Pattern, len(results))
	for i := range results {
		out[i] = results[i].pattern
	}
	return out, nil
}

// SavePermission persists an allow/deny/ask rule, replacing the rule for an
// already-known pattern.
func (s *SQLiteStore) SavePermission(ctx context.Context, p *models.Permission) error {
	if p == nil {
		return errors.New("memory: nil permission")
	}
	if strings.TrimSpace(p.Pattern) == "" {
		return errors.New("memory: save permission: empty pattern")
	}
	switch p.Decision {
	case "allow", "deny", "ask":
	default:
		return fmt.Errorf("memory: save permission: invalid decision %q", p.Decision)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}

	const q = `INSERT INTO permissions (pattern, decision, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(pattern) DO UPDATE SET
			decision   = excluded.decision,
			created_at = excluded.created_at`

	res, err := s.db.ExecContext(ctx, q, p.Pattern, p.Decision, p.CreatedAt)
	if err != nil {
		return fmt.Errorf("save permission: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		p.ID = id
	}
	return nil
}

// FindPermission returns the permission rule for the given command. An exact
// pattern match wins; otherwise rules are matched as globs (filepath.Match
// semantics), with the most specific match taking precedence. Returns
// (nil, nil) when no rule applies.
func (s *SQLiteStore) FindPermission(ctx context.Context, cmdPattern string) (*models.Permission, error) {
	if strings.TrimSpace(cmdPattern) == "" {
		return nil, nil
	}

	var perm models.Permission
	err := s.db.GetContext(ctx, &perm,
		`SELECT id, pattern, decision, created_at FROM permissions WHERE pattern = ? LIMIT 1`, cmdPattern)
	switch {
	case err == nil:
		return &perm, nil
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, fmt.Errorf("find permission %q: %w", cmdPattern, err)
	}

	rules := []models.Permission{}
	if err := s.db.SelectContext(ctx, &rules,
		`SELECT id, pattern, decision, created_at FROM permissions`,
	); err != nil {
		return nil, fmt.Errorf("find permission %q: %w", cmdPattern, err)
	}

	matches := []models.Permission{}
	for _, r := range rules {
		if globMatch(r.Pattern, cmdPattern) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return len(matches[i].Pattern) > len(matches[j].Pattern)
	})
	return &matches[0], nil
}

// SaveTask persists a general-purpose task and its response.
func (s *SQLiteStore) SaveTask(ctx context.Context, t *models.Task) error {
	if t == nil {
		return errors.New("memory: nil task")
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = uuid.NewString()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	const q = `INSERT INTO tasks (id, session_id, input, response, domain, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`

	if _, err := s.db.ExecContext(ctx, q,
		t.ID, t.SessionID, t.Input, t.Response, t.Domain, t.CreatedAt,
	); err != nil {
		return fmt.Errorf("save task: %w", err)
	}
	return nil
}

// ListTasks returns tasks for a session (or all sessions when sessionID is
// empty), most recent first. A limit <= 0 returns no limit.
func (s *SQLiteStore) ListTasks(ctx context.Context, sessionID string, limit int) ([]*models.Task, error) {
	q := `SELECT id, session_id, input, response, domain, created_at FROM tasks`
	args := []any{}
	if sessionID != "" {
		q += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY created_at DESC, rowid DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	tasks := []*models.Task{}
	if err := s.db.SelectContext(ctx, &tasks, q, args...); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// gobDecodeFloats decodes a gob-encoded []float32 embedding.
func gobDecodeFloats(data []byte) ([]float32, error) {
	var v []float32
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// globMatch reports whether s matches pattern using filepath.Match semantics.
func globMatch(pattern, s string) bool {
	ok, err := filepath.Match(pattern, s)
	return err == nil && ok
}

// SaveWorkflowJob inserts a new workflow job.
func (s *SQLiteStore) SaveWorkflowJob(ctx context.Context, j *models.WorkflowJob) error {
	if j == nil {
		return errors.New("memory: nil workflow job")
	}
	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}
	if j.UpdatedAt.IsZero() {
		j.UpdatedAt = now
	}

	const q = `INSERT INTO workflow_jobs (id, workflow_file, inputs, status, current_step, output, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := s.db.ExecContext(ctx, q,
		j.ID, j.WorkflowFile, j.Inputs, j.Status, j.CurrentStep, j.Output, j.Error, j.CreatedAt, j.UpdatedAt,
	); err != nil {
		return fmt.Errorf("save workflow job: %w", err)
	}
	return nil
}

// GetWorkflowJob returns the workflow job with the given ID.
func (s *SQLiteStore) GetWorkflowJob(ctx context.Context, id string) (*models.WorkflowJob, error) {
	var j models.WorkflowJob
	err := s.db.GetContext(ctx, &j,
		`SELECT id, workflow_file, inputs, status, current_step, output, error, created_at, updated_at FROM workflow_jobs WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("get workflow job %q: %w", id, err)
	}
	return &j, nil
}

// UpdateWorkflowJob updates the workflow job's status, current_step, output, error, and updated_at.
func (s *SQLiteStore) UpdateWorkflowJob(ctx context.Context, j *models.WorkflowJob) error {
	if j == nil {
		return errors.New("memory: nil workflow job")
	}
	j.UpdatedAt = time.Now().UTC()

	const q = `UPDATE workflow_jobs SET status = ?, current_step = ?, output = ?, error = ?, updated_at = ? WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, q, j.Status, j.CurrentStep, j.Output, j.Error, j.UpdatedAt, j.ID); err != nil {
		return fmt.Errorf("update workflow job %q: %w", j.ID, err)
	}
	return nil
}

// ListWorkflowJobs returns workflow jobs, most recently created first.
func (s *SQLiteStore) ListWorkflowJobs(ctx context.Context, limit int) ([]*models.WorkflowJob, error) {
	q := `SELECT id, workflow_file, inputs, status, current_step, output, error, created_at, updated_at FROM workflow_jobs ORDER BY created_at DESC`
	var args []any
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	jobs := []*models.WorkflowJob{}
	if err := s.db.SelectContext(ctx, &jobs, q, args...); err != nil {
		return nil, fmt.Errorf("list workflow jobs: %w", err)
	}
	return jobs, nil
}
