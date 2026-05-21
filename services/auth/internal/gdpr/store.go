package gdpr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnonymisedSentinelID is the well-known UUID used to replace user_id in
// referencing tables (audit_log, editorial_calendar.created_by) when the
// underlying user requests deletion. Seeded by migration 010 into
// auth_svc.users with email "anonymised@deleted.local".
const AnonymisedSentinelID = "00000000-0000-0000-0000-000000000001"

// ErrAlreadyRequested is returned when a deletion has already been recorded
// for the user. The handler maps this to 200 OK (idempotent) rather than 409.
var ErrAlreadyRequested = errors.New("deletion already requested")

// RecordRequest inserts a row into user_deletions and returns the
// ledger row's requested_at. Idempotent: if a row already exists for user_id,
// returns ErrAlreadyRequested with the existing timestamp.
func RecordRequest(ctx context.Context, db *pgxpool.Pool, userID, requestedBy string) (time.Time, error) {
	var requestedAt time.Time
	err := db.QueryRow(ctx, `
		INSERT INTO user_deletions (user_id, requested_by)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING requested_at
	`, userID, requestedBy).Scan(&requestedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		// Existing row — fetch + return ErrAlreadyRequested.
		err = db.QueryRow(ctx,
			`SELECT requested_at FROM user_deletions WHERE user_id = $1::uuid`,
			userID,
		).Scan(&requestedAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("fetch existing deletion: %w", err)
		}
		return requestedAt, ErrAlreadyRequested
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("insert deletion request: %w", err)
	}
	return requestedAt, nil
}

// AnonymiseAuthLocal scrubs PII from auth's own tables for the given user_id:
//   1. auth_svc.users — replace email with deleted-<uuid>@anonymised.local,
//      blank the password_hash, stamp deleted_at.
//   2. audit_log.user_id — repoint at AnonymisedSentinelID so historical
//      audit rows are preserved (legal need) but cannot be tied to the user.
//
// All in one transaction so partial anonymisation is impossible. Caller must
// have already inserted a user_deletions row (see RecordRequest).
func AnonymiseAuthLocal(ctx context.Context, db *pgxpool.Pool, userID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — rollback after Commit is harmless.

	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET email         = 'deleted-' || $1::text || '@anonymised.local',
		    password_hash = '',
		    deleted_at    = now()
		WHERE id = $1::uuid
		  AND id <> $2::uuid          -- never anonymise the sentinel itself
	`, userID, AnonymisedSentinelID); err != nil {
		return fmt.Errorf("anonymise users: %w", err)
	}

	// audit_log lives in public schema (migration 001). Repoint user_id at the
	// sentinel; do NOT delete rows — audit log is append-only by policy.
	if _, err := tx.Exec(ctx, `
		UPDATE audit_log
		SET user_id = $2::uuid
		WHERE user_id = $1::uuid
	`, userID, AnonymisedSentinelID); err != nil {
		return fmt.Errorf("anonymise audit_log: %w", err)
	}

	return tx.Commit(ctx)
}

// MarkServiceCompleted records that ``service`` finished its part of the
// anonymisation for ``userID``. When all expected services have stamped in,
// completed_at is also set so the lag cron stops alerting.
//
// expectedServices is the set we await (e.g. ["auth", "analytics"]). When
// services_completed keys are a superset, we mark the row fully complete.
func MarkServiceCompleted(
	ctx context.Context,
	db *pgxpool.Pool,
	userID, service string,
	expectedServices []string,
) error {
	_, err := db.Exec(ctx, `
		UPDATE user_deletions
		SET services_completed = services_completed || jsonb_build_object($2::text, to_jsonb(now())),
		    completed_at       = CASE
		        WHEN (services_completed || jsonb_build_object($2::text, true)) ?& $3::text[]
		            THEN now()
		        ELSE completed_at
		    END
		WHERE user_id = $1::uuid
	`, userID, service, expectedServices)
	if err != nil {
		return fmt.Errorf("mark service completed: %w", err)
	}
	return nil
}

// PendingOlderThan returns user_ids whose deletion was requested more than
// ``threshold`` ago and has not yet been marked completed. Used by the lag
// cron to fire the GDPRDeletionLag alert.
func PendingOlderThan(ctx context.Context, db *pgxpool.Pool, threshold time.Duration) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT user_id::text
		FROM user_deletions
		WHERE completed_at IS NULL
		  AND requested_at < now() - make_interval(secs => $1)
		ORDER BY requested_at ASC
	`, int(threshold.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("pending older: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
