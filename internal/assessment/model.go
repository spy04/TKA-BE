package assessment

import (
	"errors"
	"time"
)

var (
	ErrServiceUnavailable   = errors.New("assessment service unavailable")
	ErrPackageNotFound      = errors.New("published assessment package not found")
	ErrAccessDenied         = errors.New("assessment package access denied")
	ErrAttemptNotFound      = errors.New("assessment attempt not found")
	ErrAttemptClosed        = errors.New("assessment attempt is closed")
	ErrResultNotReady       = errors.New("assessment result is not ready")
	ErrQuestionNotInAttempt = errors.New("question is not part of this attempt")
	ErrOptionNotInQuestion  = errors.New("option is not part of this question")
	ErrInvalidInput         = errors.New("invalid assessment request")
	ErrInvalidPackage       = errors.New("assessment package is not ready")
	ErrInvalidScoringData   = errors.New("assessment scoring data is invalid")
)

const scoringPolicyVersion = "WEIGHTED_POINTS_V1"

type startAttemptRequest struct {
	PackageID string `json:"packageId"`
}

type updateAnswerRequest struct {
	SelectedOptionKey *string `json:"selectedOptionKey"`
	Flagged           bool    `json:"flagged"`
}

type packageRecord struct {
	ID                 string
	Slug               string
	Title              string
	Kind               string
	EducationLevel     string
	Grade              int
	Subject            string
	AccessTier         string
	MaterialID         *string
	MaterialAccessTier string
	DurationSeconds    uint64
	QuestionCount      int64
	TotalPoints        string
}

type attemptRecord struct {
	ID              string
	StudentID       string
	PackageID       string
	Status          string
	StartedAt       time.Time
	DeadlineAt      time.Time
	SubmittedAt     *time.Time
	FinishReason    *string
	Score           *string
	EarnedPoints    *string
	TotalPoints     string
	AnsweredCount   int64
	CorrectCount    int64
	WrongCount      int64
	UnansweredCount int64
	DurationSeconds *uint64
	Passed          *bool
	ScoringPolicy   *string
	DeadlinePassed  bool
	PackageSlug     string
	PackageTitle    string
	PackageKind     string
	EducationLevel  string
	Grade           int
	Subject         string
}

type attemptQuestion struct {
	ID                  string
	Position            int
	Points              string
	Topic               *string
	Difficulty          string
	QuestionType        string
	Prompt              string
	Explanation         *string
	SelectedOptionKey   *string
	SelectedOptionLabel *string
	CorrectOptionKey    string
	CorrectOptionLabel  string
	Flagged             bool
	Options             []answerOption
}

type answerOption struct {
	Key   string
	Label string
}

type savedAnswer struct {
	QuestionID        string
	SelectedOptionKey *string
	Flagged           bool
	AnsweredAt        *time.Time
}

type attemptResponse struct {
	ID               string                    `json:"id"`
	Resumed          bool                      `json:"resumed"`
	PackageID        string                    `json:"packageId"`
	PackageSlug      string                    `json:"packageSlug"`
	Title            string                    `json:"title"`
	Kind             string                    `json:"kind"`
	EducationLevel   string                    `json:"educationLevel"`
	Grade            int                       `json:"grade"`
	Subject          string                    `json:"subject"`
	Status           string                    `json:"status"`
	StartedAt        time.Time                 `json:"startedAt"`
	DeadlineAt       time.Time                 `json:"deadlineAt"`
	RemainingSeconds int64                     `json:"remainingSeconds"`
	TotalQuestions   int64                     `json:"totalQuestions"`
	AnsweredCount    int64                     `json:"answeredCount"`
	FlaggedCount     int64                     `json:"flaggedCount"`
	UnansweredCount  int64                     `json:"unansweredCount"`
	Questions        []attemptQuestionResponse `json:"questions"`
}

type attemptQuestionResponse struct {
	ID                string           `json:"id"`
	Number            int              `json:"number"`
	Points            float64          `json:"points"`
	Topic             *string          `json:"topic"`
	Difficulty        string           `json:"difficulty"`
	Type              string           `json:"type"`
	Prompt            string           `json:"prompt"`
	Options           []optionResponse `json:"options"`
	SelectedOptionKey *string          `json:"selectedOptionKey"`
	Flagged           bool             `json:"flagged"`
}

type optionResponse struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type answerResponse struct {
	QuestionID        string     `json:"questionId"`
	SelectedOptionKey *string    `json:"selectedOptionKey"`
	Flagged           bool       `json:"flagged"`
	AnsweredAt        *time.Time `json:"answeredAt"`
}

type resultResponse struct {
	AttemptID       string                   `json:"attemptId"`
	PackageID       string                   `json:"packageId"`
	PackageSlug     string                   `json:"packageSlug"`
	Title           string                   `json:"title"`
	Kind            string                   `json:"kind"`
	Status          string                   `json:"status"`
	FinishReason    *string                  `json:"finishReason"`
	StartedAt       time.Time                `json:"startedAt"`
	SubmittedAt     *time.Time               `json:"submittedAt"`
	DurationSeconds uint64                   `json:"durationSeconds"`
	Score           float64                  `json:"score"`
	EarnedPoints    float64                  `json:"earnedPoints"`
	TotalPoints     float64                  `json:"totalPoints"`
	TotalQuestions  int64                    `json:"totalQuestions"`
	AnsweredCount   int64                    `json:"answeredCount"`
	CorrectCount    int64                    `json:"correctCount"`
	WrongCount      int64                    `json:"wrongCount"`
	UnansweredCount int64                    `json:"unansweredCount"`
	Passed          *bool                    `json:"passed"`
	Questions       []resultQuestionResponse `json:"questions"`
}

type resultQuestionResponse struct {
	ID             string          `json:"id"`
	Number         int             `json:"number"`
	Points         float64         `json:"points"`
	Topic          *string         `json:"topic"`
	Difficulty     string          `json:"difficulty"`
	Status         string          `json:"status"`
	Prompt         string          `json:"prompt"`
	SelectedAnswer *optionResponse `json:"selectedAnswer"`
	CorrectAnswer  optionResponse  `json:"correctAnswer"`
	Explanation    *string         `json:"explanation"`
}

type historyItem struct {
	ID              string     `json:"id"`
	PackageID       string     `json:"packageId"`
	PackageSlug     string     `json:"packageSlug"`
	Title           string     `json:"title"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"startedAt"`
	DeadlineAt      time.Time  `json:"deadlineAt"`
	SubmittedAt     *time.Time `json:"submittedAt"`
	DurationSeconds *uint64    `json:"durationSeconds"`
	Score           *float64   `json:"score"`
	AnsweredCount   int64      `json:"answeredCount"`
	CorrectCount    int64      `json:"correctCount"`
	WrongCount      int64      `json:"wrongCount"`
	UnansweredCount int64      `json:"unansweredCount"`
}

type historyResponse struct {
	Items      []historyItem `json:"items"`
	Pagination pagination    `json:"pagination"`
}

type pagination struct {
	Page     int64 `json:"page"`
	PageSize int64 `json:"pageSize"`
	Total    int64 `json:"total"`
}

type pageQuery struct {
	Page     int64
	PageSize int64
	Offset   int64
}

type scoreSummary struct {
	Score           string
	EarnedPoints    string
	TotalPoints     string
	AnsweredCount   int64
	CorrectCount    int64
	WrongCount      int64
	UnansweredCount int64
}
