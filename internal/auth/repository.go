package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) available() bool {
	return r != nil && r.db != nil
}

func (r *Repository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	return r.db.BeginTx(ctx, nil)
}

func (r *Repository) CreateUser(ctx context.Context, tx *sql.Tx, user userRecord) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role, status, created_at)
		 VALUES (?, ?, ?, 'STUDENT', 'ACTIVE', ?)`,
		user.ID, user.Email, user.Password, user.CreatedAt,
	)
	if isDuplicateKey(err) {
		return ErrDuplicateEmail
	}
	return err
}

func (r *Repository) CreateStudentProfile(ctx context.Context, tx *sql.Tx, userID string, request registerRequest, level string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO student_profiles (user_id, display_name, education_level, grade, school, curriculum)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, request.DisplayName, level, request.Grade,
		nullableTrimmed(request.School), nullableTrimmed(request.Curriculum),
	)
	return err
}

func (r *Repository) CreateSession(ctx context.Context, tx *sql.Tx, sessionID, userID, refreshHash string, expiresAt time.Time, rotatedFromID string) error {
	var rotatedFrom any
	if rotatedFromID != "" {
		rotatedFrom = rotatedFromID
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO auth_sessions (id, user_id, refresh_token_hash, expires_at, rotated_from_id)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, userID, refreshHash, expiresAt.UTC(), rotatedFrom,
	)
	return err
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (userRecord, error) {
	if !r.available() {
		return userRecord{}, ErrServiceUnavailable
	}
	var user userRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role, status, created_at
		 FROM users WHERE email = ?`,
		email,
	).Scan(&user.ID, &user.Email, &user.Password, &user.Role, &user.Status, &user.CreatedAt)
	return user, err
}

func (r *Repository) FindPrincipal(ctx context.Context, userID string) (principal, string, error) {
	if !r.available() {
		return principal{}, "", ErrServiceUnavailable
	}
	var identity principal
	var status string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, role, status FROM users WHERE id = ?`,
		userID,
	).Scan(&identity.userID, &identity.role, &status)
	return identity, status, err
}

func (r *Repository) FindProfile(ctx context.Context, tx *sql.Tx, userID string) (*studentProfile, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	const query = `SELECT display_name, education_level, grade, school, curriculum
		FROM student_profiles WHERE user_id = ?`
	var row *sql.Row
	if tx == nil {
		row = r.db.QueryRowContext(ctx, query, userID)
	} else {
		row = tx.QueryRowContext(ctx, query, userID)
	}
	var profile studentProfile
	var school, curriculum sql.NullString
	err := row.Scan(&profile.DisplayName, &profile.EducationLevel, &profile.Grade, &school, &curriculum)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	profile.School = nullStringPointer(school)
	profile.Curriculum = nullStringPointer(curriculum)
	return &profile, nil
}

func (r *Repository) FindUserByID(ctx context.Context, userID string) (userRecord, error) {
	if !r.available() {
		return userRecord{}, ErrServiceUnavailable
	}
	var user userRecord
	var displayName, level, school, curriculum sql.NullString
	var grade sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.role, u.status, u.created_at,
		        p.display_name, p.education_level, p.grade, p.school, p.curriculum
		 FROM users u
		 LEFT JOIN student_profiles p ON p.user_id = u.id
		 WHERE u.id = ?`,
		userID,
	).Scan(&user.ID, &user.Email, &user.Role, &user.Status, &user.CreatedAt,
		&displayName, &level, &grade, &school, &curriculum)
	if err != nil {
		return userRecord{}, err
	}
	if displayName.Valid && level.Valid && grade.Valid {
		user.Profile = &studentProfile{
			DisplayName: displayName.String, EducationLevel: level.String, Grade: int(grade.Int64),
			School: nullStringPointer(school), Curriculum: nullStringPointer(curriculum),
		}
	}
	return user, nil
}

func (r *Repository) FindSessionForUpdate(ctx context.Context, tx *sql.Tx, refreshHash string) (authSession, error) {
	var session authSession
	var revokedAt sql.NullTime
	err := tx.QueryRowContext(ctx,
		`SELECT s.id, s.user_id, s.expires_at, s.revoked_at, u.email, u.role, u.status, u.created_at
		 FROM auth_sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token_hash = ?
		 FOR UPDATE`,
		refreshHash,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &revokedAt,
		&session.Email, &session.Role, &session.Status, &session.CreatedAt)
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.Time
	}
	return session, err
}

func (r *Repository) RevokeSession(ctx context.Context, tx *sql.Tx, sessionID string, revokedAt time.Time) (int64, error) {
	result, err := tx.ExecContext(ctx,
		`UPDATE auth_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		revokedAt.UTC(), sessionID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, refreshHash string, revokedAt time.Time) error {
	if !r.available() {
		return ErrServiceUnavailable
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE auth_sessions SET revoked_at = ? WHERE refresh_token_hash = ? AND revoked_at IS NULL`,
		revokedAt.UTC(), refreshHash,
	)
	return err
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func isDuplicateKey(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
