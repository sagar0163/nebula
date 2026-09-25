package memory

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
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

// FindPattern returns the most-used pattern matching both failCmd and failOutput.
func (s *SQLiteStore) FindPattern(ctx context.Context, failCmd, failOutput string) (*models.Pattern, error) {
	var p models.Pattern
	err := s.db.GetContext(ctx, &p,
		`SELECT id, fail_cmd, fail_output, fix_cmd, success_rate, use_count, embedding, created_at, updated_at
			FROM patterns WHERE fail_cmd = ? AND fail_output = ? ORDER BY use_count DESC LIMIT 1`, failCmd, failOutput)
	switch {
	case err == nil:
		return &p, nil
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	default:
		return nil, fmt.Errorf("find pattern: %w", err)
	}
}

// score descending. Patterns with unreadable embeddings are skipped.
// A topK <= 0 returns all matches.



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

// FindPatternsByKeywords searches for patterns where fail_cmd or fail_output contains any of the keywords.
func (s *SQLiteStore) FindPatternsByKeywords(ctx context.Context, keywords []string, limit int) ([]*models.Pattern, error) {
	if len(keywords) == 0 {
		return nil, nil
	}

	var conditions []string
	var args []any
	for _, kw := range keywords {
		conditions = append(conditions, "(fail_cmd LIKE ? OR fail_output LIKE ?)")
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}

	q := "SELECT id, fail_cmd, fail_output, fix_cmd, success_rate, use_count, embedding, created_at, updated_at FROM patterns WHERE " + strings.Join(conditions, " OR ") + " ORDER BY use_count DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	var patterns []*models.Pattern
	if err := s.db.SelectContext(ctx, &patterns, q, args...); err != nil {
		return nil, fmt.Errorf("find patterns by keywords: %w", err)
	}
	return patterns, nil
}

// CancelWorkflowJob sets the workflow job's status to "cancelled".
func (s *SQLiteStore) CancelWorkflowJob(ctx context.Context, id string) error {
	const q = `UPDATE workflow_jobs SET status = 'cancelled', updated_at = ? WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, q, time.Now().UTC(), id); err != nil {
		return fmt.Errorf("cancel workflow job %q: %w", id, err)
	}
	return nil
}
