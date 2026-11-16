package learning

import (
	"errors"
	"time"
)

var (
	ErrServiceUnavailable = errors.New("learning service unavailable")
	ErrStudentNotFound    = errors.New("student profile not found")
	ErrMaterialNotFound   = errors.New("accessible published material not found")
	ErrLessonNotFound     = errors.New("accessible published lesson not found")
	ErrNoteNotFound       = errors.New("learning note not found")
	ErrInvalidInput       = errors.New("invalid learning request")
)

type progressRequest struct {
	LastPositionSeconds *uint64 `json:"lastPositionSeconds"`
	Completed           bool    `json:"completed"`
}

type createNoteRequest struct {
	Timestamp *int64 `json:"timestamp"`
	Text      string `json:"text"`
}

type studentProfile struct {
	ID             string
	DisplayName    string
	EducationLevel string
	Grade          int
	School         *string
	Curriculum     *string
}

type learningMaterial struct {
	ID             string
	Slug           string
	Title          string
	Subject        string
	EducationLevel string
	Grade          int
	AccessTier     string
	ImageURL       *string
	TotalLessons   int64
	Completed      int64
	LastActivity   *time.Time
	Chapters       []learningChapter
}

type learningChapter struct {
	ID          string
	Position    int
	Title       string
	Description *string
	Lessons     []learningLesson
}

type learningLesson struct {
	ID                  string
	Code                string
	Position            int
	Title               string
	Description         *string
	ContentType         string
	DurationSeconds     uint64
	VideoURL            *string
	PosterURL           *string
	Objective           *string
	Summary             *string
	LastPositionSeconds uint64
	CompletedAt         *time.Time
	LastAccessedAt      *time.Time
}

type lessonAccess struct {
	ID              string
	DurationSeconds uint64
}

type learningNote struct {
	ID        string
	Timestamp uint64
	Text      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type progressRecord struct {
	LastPositionSeconds uint64
	CompletedAt         *time.Time
	LastAccessedAt      time.Time
}

type studentResponse struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	EducationLevel string  `json:"educationLevel"`
	Grade          int     `json:"grade"`
	School         *string `json:"school"`
	Curriculum     *string `json:"curriculum"`
}

type overviewSummary struct {
	Materials        int   `json:"materials"`
	TotalLessons     int64 `json:"totalLessons"`
	CompletedLessons int64 `json:"completedLessons"`
	Progress         int   `json:"progress"`
}

type materialSummaryResponse struct {
	ID               string     `json:"id"`
	Slug             string     `json:"slug"`
	Title            string     `json:"title"`
	Subject          string     `json:"subject"`
	EducationLevel   string     `json:"educationLevel"`
	Grade            int        `json:"grade"`
	AccessTier       string     `json:"accessTier"`
	TotalLessons     int64      `json:"totalLessons"`
	CompletedLessons int64      `json:"completedLessons"`
	Progress         int        `json:"progress"`
	LastActivity     *time.Time `json:"lastActivity"`
}

type overviewResponse struct {
	Student   studentResponse           `json:"student"`
	Summary   overviewSummary           `json:"summary"`
	Materials []materialSummaryResponse `json:"materials"`
}

type materialDetailResponse struct {
	ID               string            `json:"id"`
	Slug             string            `json:"slug"`
	Title            string            `json:"title"`
	Subject          string            `json:"subject"`
	EducationLevel   string            `json:"educationLevel"`
	Grade            int               `json:"grade"`
	AccessTier       string            `json:"accessTier"`
	ImageURL         *string           `json:"imageUrl"`
	TotalLessons     int64             `json:"totalLessons"`
	CompletedLessons int64             `json:"completedLessons"`
	Progress         int               `json:"progress"`
	Chapters         []chapterResponse `json:"chapters"`
}

type chapterResponse struct {
	ID          string           `json:"id"`
	Position    int              `json:"position"`
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	Lessons     []lessonResponse `json:"lessons"`
}

type lessonResponse struct {
	ID                  string     `json:"id"`
	Code                string     `json:"code"`
	Position            int        `json:"position"`
	Title               string     `json:"title"`
	Description         *string    `json:"description"`
	ContentType         string     `json:"contentType"`
	DurationSeconds     uint64     `json:"durationSeconds"`
	VideoURL            *string    `json:"videoUrl"`
	PosterURL           *string    `json:"posterUrl"`
	Objective           *string    `json:"objective"`
	Summary             *string    `json:"summary"`
	LastPositionSeconds uint64     `json:"lastPositionSeconds"`
	Completed           bool       `json:"completed"`
	CompletedAt         *time.Time `json:"completedAt"`
	LastAccessedAt      *time.Time `json:"lastAccessedAt"`
}

type noteResponse struct {
	ID        string    `json:"id"`
	Timestamp uint64    `json:"timestamp"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type notesResponse struct {
	Notes []noteResponse `json:"notes"`
}

type progressResponse struct {
	LessonID            string     `json:"lessonId"`
	LastPositionSeconds uint64     `json:"lastPositionSeconds"`
	Completed           bool       `json:"completed"`
	CompletedAt         *time.Time `json:"completedAt"`
	LastAccessedAt      time.Time  `json:"lastAccessedAt"`
}
