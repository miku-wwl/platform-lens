package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	_ "modernc.org/sqlite"
)

type SQLiteRepository struct {
	db    *sql.DB
	clock runtime.Clock
}

func OpenSQLite(path string, clock runtime.Clock) (*SQLiteRepository, error) {
	if clock == nil {
		clock = runtime.RealClock{}
	}
	if err := ensureParent(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(16)
	r := &SQLiteRepository{db: db, clock: clock}
	if err := r.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *SQLiteRepository) Close() error { return r.db.Close() }

func (r *SQLiteRepository) Ready(ctx context.Context) error {
	var value int
	return r.db.QueryRowContext(ctx, "SELECT 1").Scan(&value)
}

func (r *SQLiteRepository) initialize(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=30000; CREATE TABLE IF NOT EXISTS runs (
run_id TEXT PRIMARY KEY, repository_url TEXT NOT NULL, requested_ref TEXT NOT NULL, resolved_ref TEXT, ref_type TEXT, commit_oid TEXT,
requested_path TEXT, state TEXT NOT NULL, attempt_no INTEGER NOT NULL, lease_owner TEXT, lease_acquired_at TEXT, lease_expires_at TEXT,
analysis_outcome TEXT, coverage_status TEXT, review_status TEXT, evaluation_status TEXT, manifest_uri TEXT, manifest_hash TEXT,
winning_attempt INTEGER, failure_code TEXT, failure_message TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS runs_state_lease_idx ON runs(state, lease_expires_at);`)
	return err
}

func (r *SQLiteRepository) CreateRun(ctx context.Context, repositoryURL, requestedRef, requestedPath string) (domain.AnalysisRun, error) {
	now := r.clock.Now()
	run := domain.AnalysisRun{RunID: runtime.NewID(), RepositoryURL: repositoryURL, RequestedRef: requestedRef, RequestedPath: requestedPath, State: domain.StateQueued, CreatedAt: now, UpdatedAt: now}
	_, err := r.db.ExecContext(ctx, `INSERT INTO runs(run_id,repository_url,requested_ref,requested_path,state,attempt_no,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, run.RunID, run.RepositoryURL, run.RequestedRef, run.RequestedPath, domain.StateQueued, 0, encodeTime(now), encodeTime(now))
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	return run, nil
}

func (r *SQLiteRepository) FindQueuedCandidates(ctx context.Context, limit int) ([]domain.AnalysisRun, error) {
	return r.findBy(ctx, `SELECT `+columns+` FROM runs WHERE state=? ORDER BY created_at LIMIT ?`, domain.StateQueued, limit)
}

func (r *SQLiteRepository) ClaimRun(ctx context.Context, runID, workerID string, lease time.Duration) (domain.AnalysisRun, error) {
	now := r.clock.Now()
	expiry := now.Add(lease)
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET state=?,attempt_no=1,lease_owner=?,lease_acquired_at=?,lease_expires_at=?,updated_at=? WHERE run_id=? AND state=? AND attempt_no=0`, domain.StateClaimed, workerID, encodeTime(now), encodeTime(expiry), encodeTime(now), runID, domain.StateQueued)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) PinSourceIfAbsent(ctx context.Context, runID string, attempt int, worker, oid, resolved string, refType domain.RefType) (domain.AnalysisRun, error) {
	now := encodeTime(r.clock.Now())
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET commit_oid=?,resolved_ref=?,ref_type=?,updated_at=? WHERE run_id=? AND commit_oid IS NULL AND attempt_no=? AND lease_owner=? AND state=?`, oid, resolved, refType, now, runID, attempt, worker, domain.StateRetrieving)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) RenewLease(ctx context.Context, runID string, attempt int, worker string, expected, next time.Time) (domain.AnalysisRun, error) {
	now := encodeTime(r.clock.Now())
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET lease_expires_at=?,updated_at=? WHERE run_id=? AND attempt_no=? AND lease_owner=? AND lease_expires_at=? AND state IN (?,?,?,?,?,?)`, encodeTime(next), now, runID, attempt, worker, encodeTime(expected), domain.StateClaimed, domain.StateRetrieving, domain.StateValidating, domain.StateReviewing, domain.StateEvaluating, domain.StatePersisting)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrFencingLost
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) FindReclaimCandidates(ctx context.Context, observer time.Time, limit int) ([]domain.AnalysisRun, error) {
	return r.findBy(ctx, `SELECT `+columns+` FROM runs WHERE state IN (?,?,?,?,?,?) AND lease_expires_at IS NOT NULL AND lease_expires_at<=? ORDER BY lease_expires_at LIMIT ?`, domain.StateClaimed, domain.StateRetrieving, domain.StateValidating, domain.StateReviewing, domain.StateEvaluating, domain.StatePersisting, encodeTime(observer), limit)
}

func (r *SQLiteRepository) ReclaimExpiredRun(ctx context.Context, runID string, expectedAttempt int, expectedOwner string, expectedExpiry time.Time, expectedState domain.RunState, newWorker string, observer time.Time, lease time.Duration) (domain.AnalysisRun, error) {
	nextExpiry := observer.Add(lease)
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET state=?,attempt_no=?,lease_owner=?,lease_acquired_at=?,lease_expires_at=?,updated_at=? WHERE run_id=? AND attempt_no=? AND lease_owner=? AND lease_expires_at=? AND state=? AND lease_expires_at<=?`, domain.StateClaimed, expectedAttempt+1, newWorker, encodeTime(observer), encodeTime(nextExpiry), encodeTime(observer), runID, expectedAttempt, expectedOwner, encodeTime(expectedExpiry), expectedState, encodeTime(observer))
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) UpdatePhase(ctx context.Context, runID string, attempt int, worker string, expected, next domain.RunState) (domain.AnalysisRun, error) {
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET state=?,updated_at=? WHERE run_id=? AND attempt_no=? AND lease_owner=? AND state=?`, next, encodeTime(r.clock.Now()), runID, attempt, worker, expected)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) CompleteRun(ctx context.Context, runID string, attempt int, worker string, outcome domain.AnalysisOutcome, coverage domain.CoverageStatus, review domain.ReviewStatus, evaluation domain.EvaluationStatus, uri, hash string) (domain.AnalysisRun, error) {
	now := encodeTime(r.clock.Now())
	winning := attempt
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET state=?,analysis_outcome=?,coverage_status=?,review_status=?,evaluation_status=?,winning_attempt=?,manifest_uri=?,manifest_hash=?,lease_owner=NULL,lease_acquired_at=NULL,lease_expires_at=NULL,updated_at=? WHERE run_id=? AND attempt_no=? AND lease_owner=? AND state=?`, domain.StateCompleted, outcome, coverage, review, evaluation, winning, uri, hash, now, runID, attempt, worker, domain.StatePersisting)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) FailRun(ctx context.Context, runID string, attempt int, worker, code, message string) (domain.AnalysisRun, error) {
	now := encodeTime(r.clock.Now())
	return r.immediate(ctx, func(conn *sql.Conn) (domain.AnalysisRun, error) {
		result, err := conn.ExecContext(ctx, `UPDATE runs SET state=?,failure_code=?,failure_message=?,lease_owner=NULL,lease_acquired_at=NULL,lease_expires_at=NULL,updated_at=? WHERE run_id=? AND attempt_no=? AND lease_owner=? AND state IN (?,?,?,?,?,?)`, domain.StateFailed, code, truncate(message, 4096), now, runID, attempt, worker, domain.StateClaimed, domain.StateRetrieving, domain.StateValidating, domain.StateReviewing, domain.StateEvaluating, domain.StatePersisting)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return domain.AnalysisRun{}, ErrConditional
		}
		return r.getConn(ctx, conn, runID)
	})
}

func (r *SQLiteRepository) GetRun(ctx context.Context, runID string) (domain.AnalysisRun, error) {
	return r.getConn(ctx, nil, runID)
}

const columns = `run_id,repository_url,requested_ref,resolved_ref,ref_type,commit_oid,requested_path,state,attempt_no,lease_owner,lease_acquired_at,lease_expires_at,analysis_outcome,coverage_status,review_status,evaluation_status,manifest_uri,manifest_hash,winning_attempt,failure_code,failure_message,created_at,updated_at`

func (r *SQLiteRepository) getConn(ctx context.Context, conn *sql.Conn, runID string) (domain.AnalysisRun, error) {
	var row *sql.Row
	if conn != nil {
		row = conn.QueryRowContext(ctx, `SELECT `+columns+` FROM runs WHERE run_id=?`, runID)
	} else {
		row = r.db.QueryRowContext(ctx, `SELECT `+columns+` FROM runs WHERE run_id=?`, runID)
	}
	return scanRun(row)
}

func (r *SQLiteRepository) findBy(ctx context.Context, query string, args ...any) ([]domain.AnalysisRun, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.AnalysisRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanRun(s scanner) (domain.AnalysisRun, error) {
	var run domain.AnalysisRun
	var resolved, refType, oid, path, owner, acquired, expires, outcome, coverage, review, evaluation, uri, hash, failureCode, failureMessage sql.NullString
	var winning sql.NullInt64
	var state string
	var attempt int
	var created, updated string
	err := s.Scan(&run.RunID, &run.RepositoryURL, &run.RequestedRef, &resolved, &refType, &oid, &path, &state, &attempt, &owner, &acquired, &expires, &outcome, &coverage, &review, &evaluation, &uri, &hash, &winning, &failureCode, &failureMessage, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AnalysisRun{}, ErrNotFound
	}
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.State = domain.RunState(state)
	run.AttemptNo = attempt
	run.ResolvedRef = resolved.String
	run.RefType = domain.RefType(refType.String)
	run.CommitOID = oid.String
	run.RequestedPath = path.String
	run.LeaseOwner = owner.String
	run.ManifestURI = uri.String
	run.ManifestHash = hash.String
	run.FailureCode = failureCode.String
	run.FailureMessage = failureMessage.String
	run.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.AnalysisRun{}, fmt.Errorf("created_at: %w", err)
	}
	run.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.AnalysisRun{}, fmt.Errorf("updated_at: %w", err)
	}
	run.LeaseAcquiredAt = parseNullableTime(acquired)
	run.LeaseExpiresAt = parseNullableTime(expires)
	if winning.Valid {
		value := int(winning.Int64)
		run.WinningAttempt = &value
	}
	if outcome.Valid {
		value := domain.AnalysisOutcome(outcome.String)
		run.AnalysisOutcome = &value
	}
	if coverage.Valid {
		value := domain.CoverageStatus(coverage.String)
		run.CoverageStatus = &value
	}
	if review.Valid {
		value := domain.ReviewStatus(review.String)
		run.ReviewStatus = &value
	}
	if evaluation.Valid {
		value := domain.EvaluationStatus(evaluation.String)
		run.EvaluationStatus = &value
	}
	return run, nil
}

func (r *SQLiteRepository) immediate(ctx context.Context, fn func(*sql.Conn) (domain.AnalysisRun, error)) (domain.AnalysisRun, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return domain.AnalysisRun{}, err
	}
	result, fnErr := fn(conn)
	if fnErr != nil {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		return domain.AnalysisRun{}, fnErr
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return domain.AnalysisRun{}, err
	}
	return result, nil
}

func encodeTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
func ensureParent(path string) error {
	parent := filepath.Dir(path)
	if parent == "." {
		return nil
	}
	return nil
}
