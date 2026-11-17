package catalog

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
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

func (r *Repository) ListMaterials(ctx context.Context, query listQuery) ([]Material, int64, error) {
	if !r.available() {
		return nil, 0, ErrServiceUnavailable
	}
	where, args := materialWhere(query)
	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM materials m
		 JOIN subjects s ON s.id = m.subject_id`+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	order, orderArgs := materialOrder(query, len(args)+1)
	args = append(args, orderArgs...)
	limitPlaceholder := appendPostgresArg(&args, query.PageSize)
	offsetPlaceholder := appendPostgresArg(&args, query.Offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.id, m.slug, m.title, m.description, s.id, s.slug, s.name,
		        m.education_level, m.grade, m.curriculum, m.category, m.access_tier,
		        m.price_amount, m.currency, m.image_url, m.published_at,
		        (SELECT COUNT(*) FROM chapters c
		         JOIN lessons l ON l.chapter_id = c.id
		         WHERE c.material_id = m.id) AS lesson_count,
		        (SELECT COUNT(*) FROM assessment_packages ap
		         JOIN package_questions pq ON pq.package_id = ap.id
		         WHERE ap.material_id = m.id AND ap.kind = 'PRACTICE' AND ap.status = 'PUBLISHED') AS exercise_count,
		        (SELECT COUNT(DISTINCT lp.student_id) FROM chapters c
		         JOIN lessons l ON l.chapter_id = c.id
		         JOIN lesson_progress lp ON lp.lesson_id = l.id
		         WHERE c.material_id = m.id) AS learner_count
		 FROM materials m
		 JOIN subjects s ON s.id = m.subject_id`+where+order+
			` LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder,
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Material, 0)
	for rows.Next() {
		var item Material
		var description, curriculum, category, imageURL sql.NullString
		var priceAmount sql.NullInt64
		var publishedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Title, &description, &item.Subject.ID, &item.Subject.Slug, &item.Subject.Name,
			&item.EducationLevel, &item.Grade, &curriculum, &category, &item.AccessTier,
			&priceAmount, &item.Currency, &imageURL, &publishedAt,
			&item.LessonCount, &item.ExerciseCount, &item.LearnerCount,
		); err != nil {
			return nil, 0, err
		}
		item.Description = nullableString(description)
		item.Curriculum = nullableString(curriculum)
		item.Category = nullableString(category)
		item.ImageURL = nullableString(imageURL)
		item.PriceAmount = nullableInt64(priceAmount)
		item.PublishedAt = nullableTime(publishedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) ListPublishedSubjects(ctx context.Context) ([]Subject, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.id, s.slug, s.name
		 FROM subjects s
		 WHERE EXISTS (SELECT 1 FROM materials m WHERE m.subject_id = s.id AND m.status = 'PUBLISHED')
		 ORDER BY s.name, s.id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subjects := make([]Subject, 0)
	for rows.Next() {
		var subject Subject
		if err := rows.Scan(&subject.ID, &subject.Slug, &subject.Name); err != nil {
			return nil, err
		}
		subjects = append(subjects, subject)
	}
	return subjects, rows.Err()
}

func (r *Repository) FindMaterialBySlug(ctx context.Context, slug string) (Material, error) {
	if !r.available() {
		return Material{}, ErrServiceUnavailable
	}
	var item Material
	var description, curriculum, category, imageURL sql.NullString
	var priceAmount sql.NullInt64
	var publishedAt sql.NullTime
	var instructorID, instructorName, instructorRole, instructorDescription, instructorImage, instructorExpertise sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT m.id, m.slug, m.title, m.description, s.id, s.slug, s.name,
		        m.education_level, m.grade, m.curriculum, m.category, m.access_tier,
		        m.price_amount, m.currency, m.image_url, m.published_at,
		        i.id, i.name, i.role, i.description, i.image_url, i.expertise
		 FROM materials m
		 JOIN subjects s ON s.id = m.subject_id
		 LEFT JOIN instructors i ON i.id = m.instructor_id
		 WHERE m.slug = $1 AND m.status = 'PUBLISHED'`,
		slug,
	).Scan(
		&item.ID, &item.Slug, &item.Title, &description, &item.Subject.ID, &item.Subject.Slug, &item.Subject.Name,
		&item.EducationLevel, &item.Grade, &curriculum, &category, &item.AccessTier,
		&priceAmount, &item.Currency, &imageURL, &publishedAt,
		&instructorID, &instructorName, &instructorRole, &instructorDescription, &instructorImage, &instructorExpertise,
	)
	if err != nil {
		return Material{}, err
	}
	item.Description = nullableString(description)
	item.Curriculum = nullableString(curriculum)
	item.Category = nullableString(category)
	item.ImageURL = nullableString(imageURL)
	item.PriceAmount = nullableInt64(priceAmount)
	item.PublishedAt = nullableTime(publishedAt)
	if instructorID.Valid {
		item.Instructor = &Instructor{
			ID: instructorID.String, Name: instructorName.String,
			Role: nullableString(instructorRole), Description: nullableString(instructorDescription),
			ImageURL: nullableString(instructorImage), Expertise: nullableString(instructorExpertise),
		}
	}
	return item, nil
}

func (r *Repository) ListOutcomes(ctx context.Context, materialID string) ([]LearningOutcome, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, position, title, description
		 FROM learning_outcomes WHERE material_id = $1 ORDER BY position, id`,
		materialID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	outcomes := make([]LearningOutcome, 0)
	for rows.Next() {
		var outcome LearningOutcome
		var description sql.NullString
		if err := rows.Scan(&outcome.ID, &outcome.Position, &outcome.Title, &description); err != nil {
			return nil, err
		}
		outcome.Description = nullableString(description)
		outcomes = append(outcomes, outcome)
	}
	return outcomes, rows.Err()
}

func (r *Repository) ListChapters(ctx context.Context, materialID string) ([]Chapter, error) {
	if !r.available() {
		return nil, ErrServiceUnavailable
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.position, c.title, c.description,
		        l.id, l.lesson_code, l.position, l.title, l.description,
		        l.content_type, l.duration_seconds, l.video_url, l.poster_url, l.objective, l.summary
		 FROM chapters c
		 LEFT JOIN lessons l ON l.chapter_id = c.id
		 WHERE c.material_id = $1
		 ORDER BY c.position, c.id, l.position, l.id`,
		materialID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chapters := make([]Chapter, 0)
	chapterIndexes := make(map[string]int)
	for rows.Next() {
		var chapterID, chapterTitle string
		var chapterPosition int
		var chapterDescription sql.NullString
		var lessonID, lessonCode, lessonTitle, contentType sql.NullString
		var lessonPosition, duration sql.NullInt64
		var lessonDescription, videoURL, posterURL, objective, summary sql.NullString
		if err := rows.Scan(
			&chapterID, &chapterPosition, &chapterTitle, &chapterDescription,
			&lessonID, &lessonCode, &lessonPosition, &lessonTitle, &lessonDescription,
			&contentType, &duration, &videoURL, &posterURL, &objective, &summary,
		); err != nil {
			return nil, err
		}
		index, exists := chapterIndexes[chapterID]
		if !exists {
			index = len(chapters)
			chapterIndexes[chapterID] = index
			chapters = append(chapters, Chapter{
				ID: chapterID, Position: chapterPosition, Title: chapterTitle,
				Description: nullableString(chapterDescription), Lessons: make([]Lesson, 0),
			})
		}
		if lessonID.Valid {
			chapters[index].Lessons = append(chapters[index].Lessons, Lesson{
				ID: lessonID.String, Code: lessonCode.String, Position: int(lessonPosition.Int64),
				Title: lessonTitle.String, Description: nullableString(lessonDescription),
				ContentType: contentType.String, DurationSeconds: duration.Int64,
				VideoURL: nullableString(videoURL), PosterURL: nullableString(posterURL),
				Objective: nullableString(objective), Summary: nullableString(summary),
			})
		}
	}
	return chapters, rows.Err()
}

func (r *Repository) HasMaterialGrant(ctx context.Context, studentID, materialID string) (bool, error) {
	if !r.available() {
		return false, ErrServiceUnavailable
	}
	var hasGrant bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM access_grants
			WHERE student_id = $1 AND material_id = $2
			  AND starts_at <= statement_timestamp()
			  AND (expires_at IS NULL OR expires_at > statement_timestamp())
		)`,
		studentID, materialID,
	).Scan(&hasGrant)
	return hasGrant, err
}

func (r *Repository) ListPackages(ctx context.Context, query listQuery) ([]AssessmentPackage, int64, error) {
	if !r.available() {
		return nil, 0, ErrServiceUnavailable
	}
	where, args := packageWhere(query)
	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assessment_packages p JOIN subjects s ON s.id = p.subject_id`+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := ` ORDER BY p.published_at DESC, p.id ASC`
	if query.Sort == "soal" || query.Sort == "questions" {
		order = ` ORDER BY question_count DESC, p.published_at DESC, p.id ASC`
	}
	args = append(args, query.PageSize, query.Offset)
	limitPlaceholder := "$" + strconv.Itoa(len(args)-1)
	offsetPlaceholder := "$" + strconv.Itoa(len(args))
	rows, err := r.db.QueryContext(ctx,
		`SELECT p.id, p.slug, p.title, p.description, p.kind,
		        s.id, s.slug, s.name, p.material_id, m.slug, m.title,
		        p.education_level, p.grade, p.access_tier, p.price_amount,
		        p.difficulty, p.duration_seconds, p.published_at, COUNT(pq.question_id) AS question_count
		 FROM assessment_packages p
		 JOIN subjects s ON s.id = p.subject_id
		 LEFT JOIN materials m ON m.id = p.material_id
		 LEFT JOIN package_questions pq ON pq.package_id = p.id`+where+
			` GROUP BY p.id, p.slug, p.title, p.description, p.kind, s.id, s.slug, s.name,
			          p.material_id, m.slug, m.title, p.education_level, p.grade, p.access_tier,
			          p.price_amount, p.difficulty, p.duration_seconds, p.published_at`+order+
			` LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder,
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AssessmentPackage, 0)
	for rows.Next() {
		var item AssessmentPackage
		var description, materialID, materialSlug, materialTitle sql.NullString
		var priceAmount sql.NullInt64
		var publishedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Title, &description, &item.Kind,
			&item.Subject.ID, &item.Subject.Slug, &item.Subject.Name,
			&materialID, &materialSlug, &materialTitle,
			&item.EducationLevel, &item.Grade, &item.AccessTier, &priceAmount,
			&item.Difficulty, &item.DurationSeconds, &publishedAt, &item.QuestionCount,
		); err != nil {
			return nil, 0, err
		}
		item.Description = nullableString(description)
		item.PriceAmount = nullableInt64(priceAmount)
		item.PublishedAt = nullableTime(publishedAt)
		if materialID.Valid {
			item.Material = &MaterialReference{ID: materialID.String, Slug: materialSlug.String, Title: materialTitle.String}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func materialWhere(query listQuery) (string, []any) {
	conditions := []string{"m.status = 'PUBLISHED'"}
	args := make([]any, 0)
	if query.Search != "" {
		search := "%" + query.Search + "%"
		conditions = append(conditions, "(m.title ILIKE "+appendPostgresArg(&args, search)+
			" OR COALESCE(m.description, '') ILIKE "+appendPostgresArg(&args, search)+
			" OR COALESCE(m.category, '') ILIKE "+appendPostgresArg(&args, search)+
			" OR s.name ILIKE "+appendPostgresArg(&args, search)+")")
	}
	if query.Level != "" {
		conditions = append(conditions, "m.education_level = "+appendPostgresArg(&args, query.Level))
	}
	if query.Grade != 0 {
		conditions = append(conditions, "m.grade = "+appendPostgresArg(&args, query.Grade))
	}
	if query.Subject != "" {
		conditions = append(conditions, "(s.slug = "+appendPostgresArg(&args, query.Subject)+
			" OR s.name = "+appendPostgresArg(&args, query.Subject)+")")
	}
	if query.Access != "" {
		conditions = append(conditions, "m.access_tier = "+appendPostgresArg(&args, query.Access))
	}
	if query.Landing {
		conditions = append(conditions, "m.is_landing = TRUE")
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func packageWhere(query listQuery) (string, []any) {
	conditions := []string{"p.status = 'PUBLISHED'"}
	args := make([]any, 0)
	if query.Search != "" {
		search := "%" + query.Search + "%"
		conditions = append(conditions, "(p.title ILIKE "+appendPostgresArg(&args, search)+
			" OR COALESCE(p.description, '') ILIKE "+appendPostgresArg(&args, search)+
			" OR s.name ILIKE "+appendPostgresArg(&args, search)+")")
	}
	if query.Kind != "" {
		conditions = append(conditions, "p.kind = "+appendPostgresArg(&args, query.Kind))
	}
	if query.Level != "" {
		conditions = append(conditions, "p.education_level = "+appendPostgresArg(&args, query.Level))
	}
	if query.Grade != 0 {
		conditions = append(conditions, "p.grade = "+appendPostgresArg(&args, query.Grade))
	}
	if query.Subject != "" {
		conditions = append(conditions, "(s.slug = "+appendPostgresArg(&args, query.Subject)+
			" OR s.name = "+appendPostgresArg(&args, query.Subject)+")")
	}
	if query.Access != "" {
		conditions = append(conditions, "p.access_tier = "+appendPostgresArg(&args, query.Access))
	}
	if query.Difficulty != "" {
		conditions = append(conditions, "p.difficulty = "+appendPostgresArg(&args, query.Difficulty))
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func materialOrder(query listQuery, firstPlaceholder int) (string, []any) {
	switch query.Sort {
	case "populer":
		return " ORDER BY learner_count DESC, m.published_at DESC, m.id ASC", nil
	case "latihan":
		return " ORDER BY exercise_count DESC, m.published_at DESC, m.id ASC", nil
	case "relevan":
		if query.Search != "" {
			search := "%" + query.Search + "%"
			first := "$" + strconv.Itoa(firstPlaceholder)
			second := "$" + strconv.Itoa(firstPlaceholder+1)
			third := "$" + strconv.Itoa(firstPlaceholder+2)
			return " ORDER BY CASE WHEN LOWER(m.title) = LOWER(" + first + ") THEN 0 WHEN m.title ILIKE " + second + " THEN 1 WHEN s.name ILIKE " + third + " THEN 2 ELSE 3 END, m.published_at DESC, m.id ASC",
				[]any{query.Search, search, search}
		}
	}
	return " ORDER BY m.published_at DESC, m.id ASC", nil
}

func appendPostgresArg(args *[]any, value any) string {
	*args = append(*args, value)
	return "$" + strconv.Itoa(len(*args))
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	value.Time = value.Time.UTC()
	return &value.Time
}
