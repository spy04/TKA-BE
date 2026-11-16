package learning

import (
	"context"
	"database/sql"
	"time"
)

const saveProgressQuery = `INSERT INTO lesson_progress (
     student_id, lesson_id, last_position_seconds, completed_at, last_accessed_at, updated_at
 )
 SELECT $1, l.id, $2, $3, statement_timestamp(), statement_timestamp()
 FROM lessons l
 JOIN chapters c ON c.id = l.chapter_id
 JOIN materials m ON m.id = c.material_id
 WHERE l.id = $4 AND m.status = 'PUBLISHED'
   AND (m.access_tier = 'FREE' OR EXISTS (
       SELECT 1 FROM access_grants ag
       WHERE ag.student_id = $5 AND ag.material_id = m.id
         AND ag.starts_at <= statement_timestamp()
         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
   ))
 ON CONFLICT (student_id, lesson_id) DO UPDATE SET
     last_position_seconds = EXCLUDED.last_position_seconds,
     completed_at = COALESCE(lesson_progress.completed_at, EXCLUDED.completed_at),
     last_accessed_at = statement_timestamp(),
     updated_at = statement_timestamp()`

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) available() bool {
	return r != nil && r.db != nil
}

func (r *Repository) FindStudentProfile(ctx context.Context, studentID string) (studentProfile, error) {
	if !r.available() {
		return studentProfile{}, ErrServiceUnavailable
	}
	var profile studentProfile
	var school, curriculum sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT u.id, p.display_name, p.education_level, p.grade, p.school, p.curriculum
		 FROM users u
		 JOIN student_profiles p ON p.user_id = u.id
		 WHERE u.id = $1 AND u.role = 'STUDENT' AND u.status = 'ACTIVE'`,
		studentID,
	).Scan(
		&profile.ID, &profile.DisplayName, &profile.EducationLevel, &profile.Grade, &school, &curriculum,
	)
	if err != nil {
		return studentProfile{}, err
	}
	profile.School = nullableString(school)
	profile.Curriculum = nullableString(curriculum)
	return profile, nil
}

func (r *Repository) ListStartedMaterials(ctx context.Context, studentID string) ([]learningMaterial, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.id, m.slug, m.title, s.name, m.education_level, m.grade, m.access_tier,
		        COUNT(l.id) AS total_lessons,
		        SUM(CASE WHEN lp.completed_at IS NOT NULL THEN 1 ELSE 0 END) AS completed_lessons,
		        MAX(lp.last_accessed_at) AS last_activity
		 FROM materials m
		 JOIN subjects s ON s.id = m.subject_id
		 JOIN chapters c ON c.material_id = m.id
		 JOIN lessons l ON l.chapter_id = c.id
		 LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.student_id = $1
		 WHERE m.status = 'PUBLISHED'
		   AND (m.access_tier = 'FREE' OR EXISTS (
		       SELECT 1 FROM access_grants ag
		       WHERE ag.student_id = $2 AND ag.material_id = m.id
		         AND ag.starts_at <= statement_timestamp()
		         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		   ))
		 GROUP BY m.id, m.slug, m.title, s.name, m.education_level, m.grade, m.access_tier
		 HAVING COUNT(lp.lesson_id) > 0
		 ORDER BY MAX(lp.last_accessed_at) DESC, m.id ASC`,
		studentID, studentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	materials := make([]learningMaterial, 0)
	for rows.Next() {
		var material learningMaterial
		var lastActivity sql.NullTime
		if err := rows.Scan(
			&material.ID, &material.Slug, &material.Title, &material.Subject,
			&material.EducationLevel, &material.Grade, &material.AccessTier,
			&material.TotalLessons, &material.Completed, &lastActivity,
		); err != nil {
			return nil, err
		}
		material.LastActivity = nullableTime(lastActivity)
		materials = append(materials, material)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return materials, nil
}

func (r *Repository) FindMaterialLearning(ctx context.Context, studentID, slug string) (learningMaterial, error) {
	if !r.available() {
		return learningMaterial{}, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.id, m.slug, m.title, s.name, m.education_level, m.grade,
		        m.access_tier, m.image_url,
		        c.id, c.position, c.title, c.description,
		        l.id, l.lesson_code, l.position, l.title, l.description,
		        l.content_type, l.duration_seconds, l.video_url, l.poster_url, l.objective, l.summary,
		        lp.last_position_seconds, lp.completed_at, lp.last_accessed_at
		 FROM materials m
		 JOIN subjects s ON s.id = m.subject_id
		 LEFT JOIN chapters c ON c.material_id = m.id
		 LEFT JOIN lessons l ON l.chapter_id = c.id
		 LEFT JOIN lesson_progress lp ON lp.lesson_id = l.id AND lp.student_id = $1
		 WHERE m.slug = $2 AND m.status = 'PUBLISHED'
		   AND (m.access_tier = 'FREE' OR EXISTS (
		       SELECT 1 FROM access_grants ag
		       WHERE ag.student_id = $3 AND ag.material_id = m.id
		         AND ag.starts_at <= statement_timestamp()
		         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		   ))
		 ORDER BY c.position, c.id, l.position, l.id`,
		studentID, slug, studentID,
	)
	if err != nil {
		return learningMaterial{}, err
	}
	defer rows.Close()

	var material learningMaterial
	chapterIndexes := make(map[string]int)
	found := false
	for rows.Next() {
		found = true
		var imageURL sql.NullString
		var chapterID, chapterTitle, chapterDescription sql.NullString
		var chapterPosition sql.NullInt64
		var lessonID, lessonCode, lessonTitle, lessonDescription, contentType sql.NullString
		var lessonPosition, duration, lastPosition sql.NullInt64
		var videoURL, posterURL, objective, summary sql.NullString
		var completedAt, lastAccessedAt sql.NullTime
		if err := rows.Scan(
			&material.ID, &material.Slug, &material.Title, &material.Subject,
			&material.EducationLevel, &material.Grade, &material.AccessTier, &imageURL,
			&chapterID, &chapterPosition, &chapterTitle, &chapterDescription,
			&lessonID, &lessonCode, &lessonPosition, &lessonTitle, &lessonDescription,
			&contentType, &duration, &videoURL, &posterURL, &objective, &summary,
			&lastPosition, &completedAt, &lastAccessedAt,
		); err != nil {
			return learningMaterial{}, err
		}
		material.ImageURL = nullableString(imageURL)
		if !chapterID.Valid {
			continue
		}
		chapterIndex, exists := chapterIndexes[chapterID.String]
		if !exists {
			chapterIndex = len(material.Chapters)
			chapterIndexes[chapterID.String] = chapterIndex
			material.Chapters = append(material.Chapters, learningChapter{
				ID: chapterID.String, Position: int(chapterPosition.Int64),
				Title: chapterTitle.String, Description: nullableString(chapterDescription),
				Lessons: make([]learningLesson, 0),
			})
		}
		if !lessonID.Valid {
			continue
		}
		lesson := learningLesson{
			ID: lessonID.String, Code: lessonCode.String, Position: int(lessonPosition.Int64),
			Title: lessonTitle.String, Description: nullableString(lessonDescription),
			ContentType: contentType.String, DurationSeconds: uint64(duration.Int64),
			VideoURL: nullableString(videoURL), PosterURL: nullableString(posterURL),
			Objective: nullableString(objective), Summary: nullableString(summary),
			LastPositionSeconds: uint64(lastPosition.Int64),
			CompletedAt:         nullableTime(completedAt),
			LastAccessedAt:      nullableTime(lastAccessedAt),
		}
		if lastPosition.Valid {
			lesson.LastPositionSeconds = uint64(lastPosition.Int64)
		}
		material.TotalLessons++
		if lesson.CompletedAt != nil {
			material.Completed++
		}
		material.Chapters[chapterIndex].Lessons = append(material.Chapters[chapterIndex].Lessons, lesson)
	}
	if err := rows.Err(); err != nil {
		return learningMaterial{}, err
	}
	if !found {
		return learningMaterial{}, ErrMaterialNotFound
	}
	material.Chapters = nonNilChapters(material.Chapters)
	return material, nil
}

func (r *Repository) FindAccessibleLesson(ctx context.Context, studentID, lessonID string) (lessonAccess, error) {
	if !r.available() {
		return lessonAccess{}, ErrServiceUnavailable
	}
	var lesson lessonAccess
	err := r.db.QueryRowContext(ctx,
		`SELECT l.id, l.duration_seconds
		 FROM lessons l
		 JOIN chapters c ON c.id = l.chapter_id
		 JOIN materials m ON m.id = c.material_id
		 WHERE l.id = $1 AND m.status = 'PUBLISHED'
		   AND (m.access_tier = 'FREE' OR EXISTS (
		       SELECT 1 FROM access_grants ag
		       WHERE ag.student_id = $2 AND ag.material_id = m.id
		         AND ag.starts_at <= statement_timestamp()
		         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		   ))`,
		lessonID, studentID,
	).Scan(&lesson.ID, &lesson.DurationSeconds)
	return lesson, err
}

func (r *Repository) SaveProgress(ctx context.Context, studentID, lessonID string, position uint64, completedAt *time.Time) (progressRecord, error) {
	if !r.available() {
		return progressRecord{}, ErrServiceUnavailable
	}
	_, err := r.db.ExecContext(ctx,
		saveProgressQuery,
		studentID, position, completedAt, lessonID, studentID,
	)
	if err != nil {
		return progressRecord{}, err
	}
	var progress progressRecord
	var completed sql.NullTime
	err = r.db.QueryRowContext(ctx,
		`SELECT last_position_seconds, completed_at, last_accessed_at
		 FROM lesson_progress
		 WHERE student_id = $1 AND lesson_id = $2`,
		studentID, lessonID,
	).Scan(&progress.LastPositionSeconds, &completed, &progress.LastAccessedAt)
	if err != nil {
		return progressRecord{}, err
	}
	progress.CompletedAt = nullableTime(completed)
	progress.LastAccessedAt = progress.LastAccessedAt.UTC()
	return progress, nil
}

func (r *Repository) ListNotes(ctx context.Context, studentID, lessonID string) ([]learningNote, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT n.id, n.timestamp_seconds, n.content, n.created_at, n.updated_at
		 FROM learning_notes n
		 JOIN lessons l ON l.id = n.lesson_id
		 JOIN chapters c ON c.id = l.chapter_id
		 JOIN materials m ON m.id = c.material_id
		 WHERE n.student_id = $1 AND n.lesson_id = $2 AND m.status = 'PUBLISHED'
		   AND (m.access_tier = 'FREE' OR EXISTS (
		       SELECT 1 FROM access_grants ag
		       WHERE ag.student_id = n.student_id AND ag.material_id = m.id
		         AND ag.starts_at <= statement_timestamp()
		         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		   ))
		 ORDER BY n.created_at DESC, n.id DESC`,
		studentID, lessonID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := make([]learningNote, 0)
	for rows.Next() {
		var note learningNote
		if err := rows.Scan(&note.ID, &note.Timestamp, &note.Text, &note.CreatedAt, &note.UpdatedAt); err != nil {
			return nil, err
		}
		note.CreatedAt = note.CreatedAt.UTC()
		note.UpdatedAt = note.UpdatedAt.UTC()
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func (r *Repository) CreateNote(ctx context.Context, note learningNote, studentID, lessonID string) error {
	if !r.available() {
		return ErrServiceUnavailable
	}
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO learning_notes (
		     id, student_id, lesson_id, timestamp_seconds, content, created_at, updated_at
		 )
		 SELECT $1, $2, l.id, $3, $4, $5, $6
		 FROM lessons l
		 JOIN chapters c ON c.id = l.chapter_id
		 JOIN materials m ON m.id = c.material_id
		 WHERE l.id = $7 AND m.status = 'PUBLISHED'
		   AND (m.access_tier = 'FREE' OR EXISTS (
		       SELECT 1 FROM access_grants ag
		       WHERE ag.student_id = $8 AND ag.material_id = m.id
		         AND ag.starts_at <= statement_timestamp()
		         AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		   ))`,
		note.ID, studentID, note.Timestamp, note.Text, note.CreatedAt.UTC(), note.UpdatedAt.UTC(),
		lessonID, studentID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrLessonNotFound
	}
	return nil
}

func (r *Repository) DeleteNote(ctx context.Context, studentID, lessonID, noteID string) (bool, error) {
	if !r.available() {
		return false, ErrServiceUnavailable
	}
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM learning_notes AS n
		 WHERE n.id = $1 AND n.lesson_id = $2 AND n.student_id = $3
		   AND EXISTS (
		       SELECT 1
		       FROM lessons l
		       JOIN chapters c ON c.id = l.chapter_id
		       JOIN materials m ON m.id = c.material_id
		       WHERE l.id = n.lesson_id
		         AND m.status = 'PUBLISHED'
		         AND (m.access_tier = 'FREE' OR EXISTS (
		             SELECT 1 FROM access_grants ag
		             WHERE ag.student_id = $4 AND ag.material_id = m.id
		               AND ag.starts_at <= statement_timestamp()
		               AND (ag.expires_at IS NULL OR ag.expires_at > statement_timestamp())
		         ))
		   )`,
		noteID, lessonID, studentID, studentID,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
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

func nonNilChapters(chapters []learningChapter) []learningChapter {
	if chapters == nil {
		return make([]learningChapter, 0)
	}
	return chapters
}
