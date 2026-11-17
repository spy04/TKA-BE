package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
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
		 VALUES ($1, $2, $3, 'STUDENT', 'ACTIVE', $4)`,
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
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, request.DisplayName, level, request.Grade,
		nullableTrimmed(request.School), nullableTrimmed(request.Curriculum),
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
		 FROM users WHERE email = $1`,
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
		`SELECT id, role, status FROM users WHERE id = $1`,
		userID,
	).Scan(&identity.userID, &identity.role, &status)
	return identity, status, err
}

func (r *Repository) FindProfile(ctx context.Context, tx *sql.Tx, userID string) (*studentProfile, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	const query = `SELECT display_name, education_level, grade, school, curriculum
		FROM student_profiles WHERE user_id = $1`
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
		 WHERE u.id = $1`,
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

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func isDuplicateKey(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
