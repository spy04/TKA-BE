package learning

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	repository *Repository
	now        func() time.Time
}

func NewService(repository *Repository) *Service {
	return &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Overview(ctx context.Context, studentID string) (overviewResponse, error) {
	if !s.available() {
		return overviewResponse{}, ErrServiceUnavailable
	}
	profile, err := s.repository.FindStudentProfile(ctx, studentID)
	if errors.Is(err, sql.ErrNoRows) {
		return overviewResponse{}, ErrStudentNotFound
	}
	if err != nil {
		return overviewResponse{}, err
	}
	materials, err := s.repository.ListStartedMaterials(ctx, studentID)
	if err != nil {
		return overviewResponse{}, err
	}
	result := overviewResponse{
		Student: studentResponse{
			ID: profile.ID, DisplayName: profile.DisplayName, EducationLevel: profile.EducationLevel,
			Grade: profile.Grade, School: profile.School, Curriculum: profile.Curriculum,
		},
		Materials: make([]materialSummaryResponse, 0, len(materials)),
	}
	for _, material := range materials {
		result.Summary.TotalLessons += material.TotalLessons
		result.Summary.CompletedLessons += material.Completed
		result.Materials = append(result.Materials, materialSummaryResponse{
			ID: material.ID, Slug: material.Slug, Title: material.Title, Subject: material.Subject,
			EducationLevel: material.EducationLevel, Grade: material.Grade, AccessTier: material.AccessTier,
			TotalLessons: material.TotalLessons, CompletedLessons: material.Completed,
			Progress: percentCompleted(material.Completed, material.TotalLessons), LastActivity: material.LastActivity,
		})
	}
	result.Summary.Materials = len(result.Materials)
	result.Summary.Progress = percentCompleted(result.Summary.CompletedLessons, result.Summary.TotalLessons)
	return result, nil
}

func (s *Service) Material(ctx context.Context, studentID, slug string) (materialDetailResponse, error) {
	if !s.available() {
		return materialDetailResponse{}, ErrServiceUnavailable
	}
	material, err := s.repository.FindMaterialLearning(ctx, studentID, slug)
	if err != nil {
		return materialDetailResponse{}, err
	}
	result := materialDetailResponse{
		ID: material.ID, Slug: material.Slug, Title: material.Title, Subject: material.Subject,
		EducationLevel: material.EducationLevel, Grade: material.Grade, AccessTier: material.AccessTier,
		ImageURL: material.ImageURL, TotalLessons: material.TotalLessons,
		CompletedLessons: material.Completed, Progress: percentCompleted(material.Completed, material.TotalLessons),
		Chapters: make([]chapterResponse, 0, len(material.Chapters)),
	}
	for _, chapter := range material.Chapters {
		chapterItem := chapterResponse{
			ID: chapter.ID, Position: chapter.Position, Title: chapter.Title, Description: chapter.Description,
			Lessons: make([]lessonResponse, 0, len(chapter.Lessons)),
		}
		for _, lesson := range chapter.Lessons {
			chapterItem.Lessons = append(chapterItem.Lessons, lessonResponse{
				ID: lesson.ID, Code: lesson.Code, Position: lesson.Position, Title: lesson.Title,
				Description: lesson.Description, ContentType: lesson.ContentType, DurationSeconds: lesson.DurationSeconds,
				VideoURL: lesson.VideoURL, PosterURL: lesson.PosterURL, Objective: lesson.Objective, Summary: lesson.Summary,
				LastPositionSeconds: lesson.LastPositionSeconds, Completed: lesson.CompletedAt != nil,
				CompletedAt: lesson.CompletedAt, LastAccessedAt: lesson.LastAccessedAt,
			})
		}
		result.Chapters = append(result.Chapters, chapterItem)
	}
	return result, nil
}

func (s *Service) SaveProgress(ctx context.Context, studentID, lessonID string, request progressRequest) (progressResponse, error) {
	if !s.available() {
		return progressResponse{}, ErrServiceUnavailable
	}
	if request.LastPositionSeconds == nil {
		return progressResponse{}, fmt.Errorf("%w: lastPositionSeconds is required", ErrInvalidInput)
	}
	lesson, err := s.repository.FindAccessibleLesson(ctx, studentID, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return progressResponse{}, ErrLessonNotFound
	}
	if err != nil {
		return progressResponse{}, err
	}
	if *request.LastPositionSeconds > lesson.DurationSeconds {
		return progressResponse{}, fmt.Errorf("%w: lastPositionSeconds cannot exceed the lesson duration", ErrInvalidInput)
	}
	var completedAt *time.Time
	if request.Completed {
		now := s.now().UTC()
		completedAt = &now
	}
	progress, err := s.repository.SaveProgress(ctx, studentID, lessonID, *request.LastPositionSeconds, completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return progressResponse{}, ErrLessonNotFound
	}
	if err != nil {
		return progressResponse{}, err
	}
	return progressResponse{
		LessonID: lesson.ID, LastPositionSeconds: progress.LastPositionSeconds,
		Completed: progress.CompletedAt != nil, CompletedAt: progress.CompletedAt,
		LastAccessedAt: progress.LastAccessedAt,
	}, nil
}

func (s *Service) Notes(ctx context.Context, studentID, lessonID string) (notesResponse, error) {
	if !s.available() {
		return notesResponse{}, ErrServiceUnavailable
	}
	if _, err := s.accessibleLesson(ctx, studentID, lessonID); err != nil {
		return notesResponse{}, err
	}
	notes, err := s.repository.ListNotes(ctx, studentID, lessonID)
	if err != nil {
		return notesResponse{}, err
	}
	result := notesResponse{Notes: make([]noteResponse, 0, len(notes))}
	for _, note := range notes {
		result.Notes = append(result.Notes, noteResponse{
			ID: note.ID, Timestamp: note.Timestamp, Text: note.Text, CreatedAt: note.CreatedAt,
		})
	}
	return result, nil
}

func (s *Service) CreateNote(ctx context.Context, studentID, lessonID string, request createNoteRequest) (noteResponse, error) {
	if !s.available() {
		return noteResponse{}, ErrServiceUnavailable
	}
	if request.Timestamp == nil {
		return noteResponse{}, fmt.Errorf("%w: timestamp is required", ErrInvalidInput)
	}
	text := strings.TrimSpace(request.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 10_000 {
		return noteResponse{}, fmt.Errorf("%w: text must contain 1 to 10000 characters", ErrInvalidInput)
	}
	if *request.Timestamp < 0 {
		return noteResponse{}, fmt.Errorf("%w: timestamp must not be negative", ErrInvalidInput)
	}
	lesson, err := s.accessibleLesson(ctx, studentID, lessonID)
	if err != nil {
		return noteResponse{}, err
	}
	if uint64(*request.Timestamp) > lesson.DurationSeconds {
		return noteResponse{}, fmt.Errorf("%w: timestamp cannot exceed the lesson duration", ErrInvalidInput)
	}
	id, err := newUUID()
	if err != nil {
		return noteResponse{}, fmt.Errorf("generate learning note ID: %w", err)
	}
	now := s.now().UTC()
	note := learningNote{
		ID: id, Timestamp: uint64(*request.Timestamp), Text: text, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repository.CreateNote(ctx, note, studentID, lessonID); err != nil {
		return noteResponse{}, err
	}
	return noteResponse{ID: note.ID, Timestamp: note.Timestamp, Text: note.Text, CreatedAt: note.CreatedAt}, nil
}

func (s *Service) DeleteNote(ctx context.Context, studentID, lessonID, noteID string) error {
	if !s.available() {
		return ErrServiceUnavailable
	}
	if _, err := s.accessibleLesson(ctx, studentID, lessonID); err != nil {
		return err
	}
	deleted, err := s.repository.DeleteNote(ctx, studentID, lessonID, noteID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNoteNotFound
	}
	return nil
}

func (s *Service) accessibleLesson(ctx context.Context, studentID, lessonID string) (lessonAccess, error) {
	lesson, err := s.repository.FindAccessibleLesson(ctx, studentID, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return lessonAccess{}, ErrLessonNotFound
	}
	return lesson, err
}

func (s *Service) available() bool {
	return s != nil && s.repository.available()
}

func percentCompleted(completed, total int64) int {
	if total <= 0 || completed <= 0 {
		return 0
	}
	if completed >= total {
		return 100
	}
	return int(math.Round(float64(completed) * 100 / float64(total)))
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := fmt.Sprintf("%x", value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
