package assessment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
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

func (r *Repository) LockStudentTx(ctx context.Context, tx *sql.Tx, studentID string) error {
	var id string
	return tx.QueryRowContext(ctx,
		`SELECT id FROM users
		 WHERE id = ? AND role = 'STUDENT' AND status = 'ACTIVE'
		 FOR UPDATE`,
		studentID,
	).Scan(&id)
}

func (r *Repository) FindActiveAttemptTx(ctx context.Context, tx *sql.Tx, studentID, packageID string) (string, error) {
	var attemptID string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM assessment_attempts
		 WHERE student_id = ? AND package_id = ?
		   AND status = 'IN_PROGRESS' AND active_marker = 1
		 FOR UPDATE`,
		studentID, packageID,
	).Scan(&attemptID)
	return attemptID, err
}

func (r *Repository) LoadPublishedPackageTx(ctx context.Context, tx *sql.Tx, packageID string) (packageRecord, error) {
	var item packageRecord
	var materialID sql.NullString
	var materialTier sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT p.id, p.slug, p.title, p.kind, p.education_level, p.grade,
		        s.name, p.access_tier, p.material_id, COALESCE(m.access_tier, ''),
		        p.duration_seconds
		 FROM assessment_packages p
		 JOIN subjects s ON s.id = p.subject_id
		 LEFT JOIN materials m ON m.id = p.material_id
		 WHERE p.id = ? AND p.status = 'PUBLISHED'
		 FOR UPDATE`,
		packageID,
	).Scan(
		&item.ID, &item.Slug, &item.Title, &item.Kind, &item.EducationLevel, &item.Grade,
		&item.Subject, &item.AccessTier, &materialID, &materialTier, &item.DurationSeconds,
	)
	if err != nil {
		return packageRecord{}, err
	}
	if materialID.Valid {
		item.MaterialID = &materialID.String
	}
	item.MaterialAccessTier = materialTier.String

	var invalidQuestions sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        CAST(COALESCE(SUM(pq.points), 0) AS CHAR),
		        COALESCE(SUM(CASE
		            WHEN q.id IS NULL
		              OR q.status <> 'PUBLISHED'
		              OR q.question_type <> 'MULTIPLE_CHOICE'
		              OR pq.points <= 0
		              OR COALESCE(option_counts.option_count, 0) < 2
		              OR answer_key.correct_option_key IS NULL
		              OR correct_option.option_key IS NULL
		            THEN 1 ELSE 0 END), 0)
		 FROM package_questions pq
		 LEFT JOIN questions q ON q.id = pq.question_id
		 LEFT JOIN question_answer_keys answer_key ON answer_key.question_id = q.id
		 LEFT JOIN question_options correct_option
		   ON correct_option.question_id = q.id
		  AND correct_option.option_key = answer_key.correct_option_key
		 LEFT JOIN (
		     SELECT question_id, COUNT(*) AS option_count
		     FROM question_options
		     GROUP BY question_id
		 ) option_counts ON option_counts.question_id = q.id
		 WHERE pq.package_id = ?`,
		packageID,
	).Scan(&item.QuestionCount, &item.TotalPoints, &invalidQuestions)
	if err != nil {
		return packageRecord{}, err
	}
	total, err := parseDecimal(item.TotalPoints)
	if err != nil || item.QuestionCount == 0 || total.Sign() <= 0 ||
		item.DurationSeconds == 0 || !invalidQuestions.Valid || invalidQuestions.Int64 != 0 {
		return packageRecord{}, ErrInvalidPackage
	}
	return item, nil
}

func (r *Repository) HasPackageAccessTx(ctx context.Context, tx *sql.Tx, studentID, packageID string) (bool, error) {
	var allowed bool
	err := tx.QueryRowContext(ctx,
		`SELECT (
		     p.access_tier = 'FREE'
		     OR EXISTS (
		         SELECT 1 FROM access_grants package_grant
		         WHERE package_grant.student_id = ?
		           AND package_grant.package_id = p.id
		           AND package_grant.starts_at <= UTC_TIMESTAMP(6)
		           AND (package_grant.expires_at IS NULL OR package_grant.expires_at > UTC_TIMESTAMP(6))
		     )
		 )
		 AND (
		     p.material_id IS NULL
		     OR m.access_tier = 'FREE'
		     OR EXISTS (
		         SELECT 1 FROM access_grants material_grant
		         WHERE material_grant.student_id = ?
		           AND material_grant.material_id = m.id
		           AND material_grant.starts_at <= UTC_TIMESTAMP(6)
		           AND (material_grant.expires_at IS NULL OR material_grant.expires_at > UTC_TIMESTAMP(6))
		     )
		 )
		 FROM assessment_packages p
		 LEFT JOIN materials m ON m.id = p.material_id
		 WHERE p.id = ? AND p.status = 'PUBLISHED'`,
		studentID, studentID, packageID,
	).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return allowed, err
}

func (r *Repository) CreateAttemptTx(ctx context.Context, tx *sql.Tx, attemptID, studentID string, item packageRecord) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO assessment_attempts (
		     id, student_id, package_id, status, started_at, deadline_at,
		     submitted_at, finish_reason, score, earned_points, total_points,
		     answered_count, correct_count, wrong_count, unanswered_count,
		     duration_seconds, passed, scoring_policy_version, created_at, updated_at
		 )
		 VALUES (
		     ?, ?, ?, 'IN_PROGRESS', UTC_TIMESTAMP(6),
		     TIMESTAMPADD(SECOND, ?, UTC_TIMESTAMP(6)),
		     NULL, NULL, NULL, NULL, ?, 0, 0, 0, ?, NULL, NULL, ?,
		     UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)
		 )`,
		attemptID, studentID, item.ID, item.DurationSeconds,
		item.TotalPoints, item.QuestionCount, scoringPolicyVersion,
	)
	if err != nil {
		return err
	}

	// While an attempt is active, earned_points holds the question's snapshotted
	// weight; finalization replaces it with the earned score for that question.
	result, err := tx.ExecContext(ctx,
		`INSERT INTO attempt_answers (
		     attempt_id, package_id, question_id, selected_option_key, flagged,
		     answered_at, is_correct, earned_points, evaluated_at
		 )
		 SELECT ?, pq.package_id, pq.question_id, NULL, 0, NULL, NULL, pq.points, NULL
		 FROM package_questions pq
		 WHERE pq.package_id = ?`,
		attemptID, item.ID,
	)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted != item.QuestionCount {
		return fmt.Errorf("%w: question snapshot count mismatch", ErrInvalidPackage)
	}
	return nil
}

func (r *Repository) LockAttemptTx(ctx context.Context, tx *sql.Tx, studentID, attemptID string) (attemptRecord, error) {
	var item attemptRecord
	var submittedAt sql.NullTime
	var finishReason, score, earnedPoints, passed, policy sql.NullString
	var duration sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT a.id, a.student_id, a.package_id, a.status, a.started_at, a.deadline_at,
		        a.submitted_at, a.finish_reason, a.score, a.earned_points, a.total_points,
		        a.answered_count, a.correct_count, a.wrong_count, a.unanswered_count,
		        a.duration_seconds, CAST(a.passed AS CHAR), a.scoring_policy_version,
		        a.deadline_at <= UTC_TIMESTAMP(6),
		        p.slug, p.title, p.kind, p.education_level, p.grade, s.name
		 FROM assessment_attempts a
		 JOIN assessment_packages p ON p.id = a.package_id
		 JOIN subjects s ON s.id = p.subject_id
		 WHERE a.id = ? AND a.student_id = ?
		 FOR UPDATE`,
		attemptID, studentID,
	).Scan(
		&item.ID, &item.StudentID, &item.PackageID, &item.Status, &item.StartedAt, &item.DeadlineAt,
		&submittedAt, &finishReason, &score, &earnedPoints, &item.TotalPoints,
		&item.AnsweredCount, &item.CorrectCount, &item.WrongCount, &item.UnansweredCount,
		&duration, &passed, &policy, &item.DeadlinePassed,
		&item.PackageSlug, &item.PackageTitle, &item.PackageKind, &item.EducationLevel, &item.Grade, &item.Subject,
	)
	if err != nil {
		return attemptRecord{}, err
	}
	item.StartedAt = item.StartedAt.UTC()
	item.DeadlineAt = item.DeadlineAt.UTC()
	item.SubmittedAt = nullableTime(submittedAt)
	item.FinishReason = nullableString(finishReason)
	item.Score = nullableString(score)
	item.EarnedPoints = nullableString(earnedPoints)
	item.ScoringPolicy = nullableString(policy)
	if duration.Valid {
		value := uint64(duration.Int64)
		item.DurationSeconds = &value
	}
	if passed.Valid {
		value, err := strconv.ParseBool(passed.String)
		if err != nil {
			return attemptRecord{}, err
		}
		item.Passed = &value
	}
	return item, nil
}

func (r *Repository) HasQuestionTx(ctx context.Context, tx *sql.Tx, attemptID, questionID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(
		     SELECT 1 FROM attempt_answers
		     WHERE attempt_id = ? AND question_id = ?
		 )`,
		attemptID, questionID,
	).Scan(&exists)
	return exists, err
}

func (r *Repository) HasOptionTx(ctx context.Context, tx *sql.Tx, questionID, optionKey string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(
		     SELECT 1 FROM question_options
		     WHERE question_id = ? AND option_key = ?
		 )`,
		questionID, optionKey,
	).Scan(&exists)
	return exists, err
}

func (r *Repository) SaveAnswerTx(ctx context.Context, tx *sql.Tx, attemptID, questionID string, selectedOptionKey *string, flagged bool) (savedAnswer, error) {
	var selected any
	var selectedForTime any
	if selectedOptionKey != nil {
		selected = *selectedOptionKey
		selectedForTime = *selectedOptionKey
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE attempt_answers
		 SET selected_option_key = NULLIF(?, ''),
		     flagged = ?,
		     answered_at = CASE WHEN NULLIF(?, '') IS NULL THEN NULL ELSE UTC_TIMESTAMP(6) END
		 WHERE attempt_id = ? AND question_id = ?
		   AND EXISTS (
		       SELECT 1 FROM assessment_attempts a
		       WHERE a.id = attempt_answers.attempt_id
		         AND a.status = 'IN_PROGRESS'
		         AND a.deadline_at > UTC_TIMESTAMP(6)
		   )`,
		selected, flagged, selectedForTime, attemptID, questionID,
	)
	if err != nil {
		return savedAnswer{}, err
	}
	return r.FindAnswerTx(ctx, tx, attemptID, questionID)
}

func (r *Repository) FindAnswerTx(ctx context.Context, tx *sql.Tx, attemptID, questionID string) (savedAnswer, error) {
	var answer savedAnswer
	var selected sql.NullString
	var answeredAt sql.NullTime
	err := tx.QueryRowContext(ctx,
		`SELECT question_id, selected_option_key, flagged, answered_at
		 FROM attempt_answers
		 WHERE attempt_id = ? AND question_id = ?`,
		attemptID, questionID,
	).Scan(&answer.QuestionID, &selected, &answer.Flagged, &answeredAt)
	if err != nil {
		return savedAnswer{}, err
	}
	answer.SelectedOptionKey = nullableString(selected)
	answer.AnsweredAt = nullableTime(answeredAt)
	return answer, nil
}

func (r *Repository) LoadAttemptQuestionsTx(ctx context.Context, tx *sql.Tx, attemptID, packageID string) ([]attemptQuestion, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT pq.question_id, pq.position, COALESCE(aa.earned_points, pq.points),
		        q.topic, q.difficulty, q.question_type, q.prompt,
		        aa.selected_option_key, aa.flagged,
		        o.option_key, o.label
		 FROM package_questions pq
		 JOIN questions q ON q.id = pq.question_id
		 JOIN attempt_answers aa ON aa.attempt_id = ? AND aa.question_id = pq.question_id
		 LEFT JOIN question_options o ON o.question_id = q.id
		 WHERE pq.package_id = ? AND aa.package_id = ?
		 ORDER BY pq.position, o.position`,
		attemptID, packageID, packageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	questions := make([]attemptQuestion, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		var id, difficulty, questionType, prompt string
		var position int
		var points string
		var topic, selectedKey sql.NullString
		var flagged bool
		var optionKey, optionLabel sql.NullString
		if err := rows.Scan(
			&id, &position, &points, &topic, &difficulty, &questionType, &prompt,
			&selectedKey, &flagged, &optionKey, &optionLabel,
		); err != nil {
			return nil, err
		}
		index, exists := indexes[id]
		if !exists {
			index = len(questions)
			indexes[id] = index
			questions = append(questions, attemptQuestion{
				ID: id, Position: position, Points: points, Topic: nullableString(topic),
				Difficulty: difficulty, QuestionType: questionType, Prompt: prompt,
				SelectedOptionKey: nullableString(selectedKey), Flagged: flagged,
				Options: make([]answerOption, 0),
			})
		}
		if optionKey.Valid {
			questions[index].Options = append(questions[index].Options, answerOption{
				Key: optionKey.String, Label: optionLabel.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return questions, nil
}

func (r *Repository) LoadScoringQuestionsTx(ctx context.Context, tx *sql.Tx, attemptID, packageID string) ([]attemptQuestion, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT pq.question_id, pq.position, COALESCE(aa.earned_points, pq.points),
		        q.topic, q.difficulty, q.question_type, q.prompt, q.explanation,
		        aa.selected_option_key, aa.flagged,
		        answer_key.correct_option_key,
		        selected_option.label, correct_option.label
		 FROM assessment_attempts a
		 JOIN package_questions pq ON pq.package_id = a.package_id
		 JOIN questions q ON q.id = pq.question_id
		 JOIN attempt_answers aa ON aa.attempt_id = a.id AND aa.question_id = q.id
		 LEFT JOIN question_answer_keys answer_key ON answer_key.question_id = q.id
		 LEFT JOIN question_options selected_option
		   ON selected_option.question_id = q.id
		  AND selected_option.option_key = aa.selected_option_key
		 LEFT JOIN question_options correct_option
		   ON correct_option.question_id = q.id
		  AND correct_option.option_key = answer_key.correct_option_key
		 WHERE a.id = ? AND a.package_id = ?
		 ORDER BY pq.position`,
		attemptID, packageID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	questions := make([]attemptQuestion, 0)
	for rows.Next() {
		var question attemptQuestion
		var topic, explanation, selectedKey, selectedLabel, correctKey, correctLabel sql.NullString
		if err := rows.Scan(
			&question.ID, &question.Position, &question.Points,
			&topic, &question.Difficulty, &question.QuestionType, &question.Prompt, &explanation,
			&selectedKey, &question.Flagged, &correctKey, &selectedLabel, &correctLabel,
		); err != nil {
			return nil, err
		}
		question.Topic = nullableString(topic)
		question.Explanation = nullableString(explanation)
		question.SelectedOptionKey = nullableString(selectedKey)
		question.SelectedOptionLabel = nullableString(selectedLabel)
		if !correctKey.Valid || !correctLabel.Valid {
			return nil, ErrInvalidScoringData
		}
		question.CorrectOptionKey = correctKey.String
		question.CorrectOptionLabel = correctLabel.String
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return questions, nil
}

func (r *Repository) SaveEvaluatedAnswerTx(ctx context.Context, tx *sql.Tx, attemptID, questionID string, correct bool, earnedPoints string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE attempt_answers
		 SET is_correct = ?, earned_points = ?, evaluated_at = UTC_TIMESTAMP(6)
		 WHERE attempt_id = ? AND question_id = ?`,
		correct, earnedPoints, attemptID, questionID,
	)
	return err
}

func (r *Repository) FinalizeAttemptTx(ctx context.Context, tx *sql.Tx, attemptID, status, finishReason string, score scoreSummary) error {
	result, err := tx.ExecContext(ctx,
		`UPDATE assessment_attempts
		 SET status = ?, submitted_at = UTC_TIMESTAMP(6), finish_reason = ?,
		     score = ?, earned_points = ?, answered_count = ?, correct_count = ?,
		     wrong_count = ?, unanswered_count = ?,
		     duration_seconds = GREATEST(0, TIMESTAMPDIFF(SECOND, started_at, UTC_TIMESTAMP(6))),
		     passed = NULL, scoring_policy_version = ?,
		     updated_at = UTC_TIMESTAMP(6)
		 WHERE id = ? AND status = 'IN_PROGRESS' AND active_marker = 1`,
		status, finishReason, score.Score, score.EarnedPoints,
		score.AnsweredCount, score.CorrectCount, score.WrongCount, score.UnansweredCount,
		scoringPolicyVersion, attemptID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrAttemptClosed
	}
	return nil
}

func (r *Repository) RemainingSecondsTx(ctx context.Context, tx *sql.Tx, deadline time.Time) (int64, error) {
	var remaining int64
	err := tx.QueryRowContext(ctx,
		`SELECT GREATEST(0, TIMESTAMPDIFF(SECOND, UTC_TIMESTAMP(6), ?))`,
		deadline.UTC(),
	).Scan(&remaining)
	return remaining, err
}

func (r *Repository) ExpiredAttemptIDs(ctx context.Context, studentID string) ([]string, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM assessment_attempts
		 WHERE student_id = ? AND status = 'IN_PROGRESS'
		   AND deadline_at <= UTC_TIMESTAMP(6)
		 ORDER BY deadline_at, id`,
		studentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) CountAttempts(ctx context.Context, studentID string) (int64, error) {
	if !r.available() {
		return 0, ErrServiceUnavailable
	}
	var total int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assessment_attempts WHERE student_id = ?`,
		studentID,
	).Scan(&total)
	return total, err
}

func (r *Repository) ListAttempts(ctx context.Context, studentID string, query pageQuery) ([]attemptRecord, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT a.id, a.student_id, a.package_id, a.status, a.started_at, a.deadline_at,
		        a.submitted_at, a.finish_reason, a.score, a.earned_points, a.total_points,
		        a.answered_count, a.correct_count, a.wrong_count, a.unanswered_count,
		        a.duration_seconds, CAST(a.passed AS CHAR), a.scoring_policy_version, 0,
		        p.slug, p.title, p.kind, p.education_level, p.grade, s.name
		 FROM assessment_attempts a
		 JOIN assessment_packages p ON p.id = a.package_id
		 JOIN subjects s ON s.id = p.subject_id
		 WHERE a.student_id = ?
		 ORDER BY a.started_at DESC, a.id DESC
		 LIMIT ? OFFSET ?`,
		studentID, query.PageSize, query.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]attemptRecord, 0)
	for rows.Next() {
		var item attemptRecord
		var submittedAt sql.NullTime
		var finishReason, score, earnedPoints, passed, policy sql.NullString
		var duration sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.StudentID, &item.PackageID, &item.Status, &item.StartedAt, &item.DeadlineAt,
			&submittedAt, &finishReason, &score, &earnedPoints, &item.TotalPoints,
			&item.AnsweredCount, &item.CorrectCount, &item.WrongCount, &item.UnansweredCount,
			&duration, &passed, &policy, &item.DeadlinePassed,
			&item.PackageSlug, &item.PackageTitle, &item.PackageKind, &item.EducationLevel, &item.Grade, &item.Subject,
		); err != nil {
			return nil, err
		}
		item.StartedAt = item.StartedAt.UTC()
		item.DeadlineAt = item.DeadlineAt.UTC()
		item.SubmittedAt = nullableTime(submittedAt)
		item.FinishReason = nullableString(finishReason)
		item.Score = nullableString(score)
		item.EarnedPoints = nullableString(earnedPoints)
		item.ScoringPolicy = nullableString(policy)
		if duration.Valid {
			value := uint64(duration.Int64)
			item.DurationSeconds = &value
		}
		if passed.Valid {
			value, err := strconv.ParseBool(passed.String)
			if err != nil {
				return nil, err
			}
			item.Passed = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	value.Time = value.Time.UTC()
	return &value.Time
}
