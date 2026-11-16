package catalog

import (
	"errors"
	"time"
)

var (
	ErrServiceUnavailable    = errors.New("catalog service unavailable")
	ErrMaterialNotFound      = errors.New("published material not found")
	ErrPremiumAccessRequired = errors.New("premium access required")
)

type Subject struct {
	ID   string
	Slug string
	Name string
}

type subjectResponse struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type Material struct {
	ID             string
	Slug           string
	Title          string
	Description    *string
	Subject        Subject
	EducationLevel string
	Grade          int
	Curriculum     *string
	Category       *string
	AccessTier     string
	PriceAmount    *int64
	Currency       string
	ImageURL       *string
	PublishedAt    *time.Time
	LessonCount    int64
	ExerciseCount  int64
	LearnerCount   int64
	Instructor     *Instructor
	Outcomes       []LearningOutcome
	Chapters       []Chapter
}

type Instructor struct {
	ID          string
	Name        string
	Role        *string
	Description *string
	ImageURL    *string
	Expertise   *string
}

type LearningOutcome struct {
	ID          string
	Position    int
	Title       string
	Description *string
}

type Chapter struct {
	ID          string
	Position    int
	Title       string
	Description *string
	Lessons     []Lesson
}

type Lesson struct {
	ID              string
	Code            string
	Position        int
	Title           string
	Description     *string
	ContentType     string
	DurationSeconds int64
	VideoURL        *string
	PosterURL       *string
	Objective       *string
	Summary         *string
}

type AssessmentPackage struct {
	ID              string
	Slug            string
	Title           string
	Description     *string
	Kind            string
	Subject         Subject
	Material        *MaterialReference
	EducationLevel  string
	Grade           int
	AccessTier      string
	PriceAmount     *int64
	Difficulty      string
	DurationSeconds int64
	QuestionCount   int64
	PublishedAt     *time.Time
}

type MaterialReference struct {
	ID    string
	Slug  string
	Title string
}

type materialListItem struct {
	ID             string          `json:"id"`
	Slug           string          `json:"slug"`
	Title          string          `json:"title"`
	Description    *string         `json:"description"`
	Subject        subjectResponse `json:"subject"`
	EducationLevel string          `json:"educationLevel"`
	Grade          int             `json:"grade"`
	Curriculum     *string         `json:"curriculum"`
	Category       *string         `json:"category"`
	AccessTier     string          `json:"accessTier"`
	PriceAmount    *int64          `json:"priceAmount"`
	Currency       string          `json:"currency"`
	ImageURL       *string         `json:"imageUrl"`
	LessonCount    int64           `json:"lessonCount"`
	ExerciseCount  int64           `json:"exerciseCount"`
	LearnerCount   int64           `json:"learnerCount"`
	PublishedAt    *time.Time      `json:"publishedAt"`
}

type instructorResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Role        *string `json:"role"`
	Description *string `json:"description"`
	ImageURL    *string `json:"imageUrl"`
	Expertise   *string `json:"expertise"`
}

type outcomeResponse struct {
	ID          string  `json:"id"`
	Position    int     `json:"position"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
}

type lessonResponse struct {
	ID              string  `json:"id"`
	Code            string  `json:"code"`
	Position        int     `json:"position"`
	Title           string  `json:"title"`
	Description     *string `json:"description"`
	ContentType     string  `json:"contentType"`
	DurationSeconds int64   `json:"durationSeconds"`
	VideoURL        *string `json:"videoUrl"`
	PosterURL       *string `json:"posterUrl"`
	Objective       *string `json:"objective"`
	Summary         *string `json:"summary"`
}

type chapterResponse struct {
	ID          string           `json:"id"`
	Position    int              `json:"position"`
	Title       string           `json:"title"`
	Description *string          `json:"description"`
	Lessons     []lessonResponse `json:"lessons"`
}

type materialDetail struct {
	ID             string              `json:"id"`
	Slug           string              `json:"slug"`
	Title          string              `json:"title"`
	Description    *string             `json:"description"`
	Subject        subjectResponse     `json:"subject"`
	EducationLevel string              `json:"educationLevel"`
	Grade          int                 `json:"grade"`
	Curriculum     *string             `json:"curriculum"`
	Category       *string             `json:"category"`
	AccessTier     string              `json:"accessTier"`
	PriceAmount    *int64              `json:"priceAmount"`
	Currency       string              `json:"currency"`
	ImageURL       *string             `json:"imageUrl"`
	PublishedAt    *time.Time          `json:"publishedAt"`
	Instructor     *instructorResponse `json:"instructor"`
	Outcomes       []outcomeResponse   `json:"outcomes"`
	Chapters       []chapterResponse   `json:"chapters"`
}

type packageListItem struct {
	ID              string           `json:"id"`
	Slug            string           `json:"slug"`
	Title           string           `json:"title"`
	Description     *string          `json:"description"`
	Kind            string           `json:"kind"`
	Subject         subjectResponse  `json:"subject"`
	Material        *packageMaterial `json:"material"`
	EducationLevel  string           `json:"educationLevel"`
	Grade           int              `json:"grade"`
	AccessTier      string           `json:"accessTier"`
	PriceAmount     *int64           `json:"priceAmount"`
	Difficulty      string           `json:"difficulty"`
	DurationSeconds int64            `json:"durationSeconds"`
	QuestionCount   int64            `json:"questionCount"`
	PublishedAt     *time.Time       `json:"publishedAt"`
}

type packageMaterial struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type pagination struct {
	Page     int64 `json:"page"`
	PageSize int64 `json:"pageSize"`
	Total    int64 `json:"total"`
}

type listFilters struct {
	EducationLevels []filterOption    `json:"educationLevels"`
	AccessTiers     []filterOption    `json:"accessTiers"`
	Sorts           []filterOption    `json:"sorts"`
	Subjects        []subjectResponse `json:"subjects"`
}

type filterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type materialCatalogResponse struct {
	Items      []materialListItem `json:"items"`
	Pagination pagination         `json:"pagination"`
	Filters    listFilters        `json:"filters"`
}

type packageCatalogResponse struct {
	Items      []packageListItem `json:"items"`
	Pagination pagination        `json:"pagination"`
}

type listQuery struct {
	Page, PageSize, Offset int64
	Search                 string
	Level                  string
	Grade                  int
	Subject                string
	Access                 string
	Sort                   string
	Kind                   string
	Difficulty             string
}
