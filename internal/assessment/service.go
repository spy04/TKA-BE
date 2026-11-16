package assessment

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Start(ctx context.Context, studentID, packageID string) (attemptResponse, error) {
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return attemptResponse{}, err
	}
	defer tx.Rollback()

	if err := s.repository.LockStudentTx(ctx, tx, studentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return attemptResponse{}, ErrAccessDenied
		}
		return attemptResponse{}, err
	}

	finalizedExpired := false
	activeID, err := s.repository.FindActiveAttemptTx(ctx, tx, studentID, packageID)
	switch {
	case err == nil:
		active, lockErr := s.repository.LockAttemptTx(ctx, tx, studentID, activeID)
		if lockErr != nil {
			return attemptResponse{}, lockErr
		}
		if active.DeadlinePassed {
			if err := s.finalizeTx(ctx, tx, active, "EXPIRED", "DEADLINE"); err != nil {
				return attemptResponse{}, err
			}
			finalizedExpired = true
		} else {
			allowed, accessErr := s.repository.HasPackageAccessTx(ctx, tx, studentID, packageID)
			if accessErr != nil {
				return attemptResponse{}, accessErr
			}
			if !allowed {
				return attemptResponse{}, ErrAccessDenied
			}
			response, responseErr := s.loadAttemptResponseTx(ctx, tx, active, true)
			if responseErr != nil {
				return attemptResponse{}, responseErr
			}
			if err := tx.Commit(); err != nil {
				return attemptResponse{}, err
			}
			return response, nil
		}
	case errors.Is(err, sql.ErrNoRows):
		// No current attempt; proceed to create one.
	default:
		return attemptResponse{}, err
	}

	item, err := s.repository.LoadPublishedPackageTx(ctx, tx, packageID)
	if err != nil {
		if finalizedExpired {
			if commitErr := tx.Commit(); commitErr != nil {
				return attemptResponse{}, commitErr
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return attemptResponse{}, ErrPackageNotFound
		}
		return attemptResponse{}, err
	}
	allowed, err := s.repository.HasPackageAccessTx(ctx, tx, studentID, packageID)
	if err != nil {
		if finalizedExpired {
			if commitErr := tx.Commit(); commitErr != nil {
				return attemptResponse{}, commitErr
			}
		}
		return attemptResponse{}, err
	}
	if !allowed {
		if finalizedExpired {
			if commitErr := tx.Commit(); commitErr != nil {
				return attemptResponse{}, commitErr
			}
		}
		return attemptResponse{}, ErrAccessDenied
	}

	attemptID, err := newUUID()
	if err != nil {
		return attemptResponse{}, err
	}
	if err := s.repository.CreateAttemptTx(ctx, tx, attemptID, studentID, item); err != nil {
		return attemptResponse{}, err
	}
	attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if err != nil {
		return attemptResponse{}, err
	}
	response, err := s.loadAttemptResponseTx(ctx, tx, attempt, false)
	if err != nil {
		return attemptResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return attemptResponse{}, err
	}
	return response, nil
}

func (s *Service) GetAttempt(ctx context.Context, studentID, attemptID string) (attemptResponse, error) {
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return attemptResponse{}, err
	}
	defer tx.Rollback()

	attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return attemptResponse{}, ErrAttemptNotFound
	}
	if err != nil {
		return attemptResponse{}, err
	}
	finalized := false
	if attempt.Status == "IN_PROGRESS" && attempt.DeadlinePassed {
		if err := s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE"); err != nil {
			return attemptResponse{}, err
		}
		attempt.Status = "EXPIRED"
		finalized = true
	}
	allowed, err := s.repository.HasPackageAccessTx(ctx, tx, studentID, attempt.PackageID)
	if err != nil {
		if finalized {
			if commitErr := tx.Commit(); commitErr != nil {
				return attemptResponse{}, commitErr
			}
		}
		return attemptResponse{}, err
	}
	if !allowed {
		if finalized {
			if commitErr := tx.Commit(); commitErr != nil {
				return attemptResponse{}, commitErr
			}
		}
		return attemptResponse{}, ErrAccessDenied
	}
	response, err := s.loadAttemptResponseTx(ctx, tx, attempt, false)
	if err != nil {
		return attemptResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return attemptResponse{}, err
	}
	return response, nil
}

func (s *Service) UpdateAnswer(ctx context.Context, studentID, attemptID, questionID string, request updateAnswerRequest) (answerResponse, error) {
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return answerResponse{}, err
	}
	defer tx.Rollback()

	attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return answerResponse{}, ErrAttemptNotFound
	}
	if err != nil {
		return answerResponse{}, err
	}
	if attempt.Status != "IN_PROGRESS" {
		return answerResponse{}, ErrAttemptClosed
	}
	if attempt.DeadlinePassed {
		if err := s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE"); err != nil {
			return answerResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			return answerResponse{}, err
		}
		return answerResponse{}, ErrAttemptClosed
	}
	allowed, err := s.repository.HasPackageAccessTx(ctx, tx, studentID, attempt.PackageID)
	if err != nil {
		return answerResponse{}, err
	}
	if !allowed {
		return answerResponse{}, ErrAccessDenied
	}
	exists, err := s.repository.HasQuestionTx(ctx, tx, attemptID, questionID)
	if err != nil {
		return answerResponse{}, err
	}
	if !exists {
		return answerResponse{}, ErrQuestionNotInAttempt
	}
	if request.SelectedOptionKey != nil {
		exists, err = s.repository.HasOptionTx(ctx, tx, questionID, *request.SelectedOptionKey)
		if err != nil {
			return answerResponse{}, err
		}
		if !exists {
			return answerResponse{}, ErrOptionNotInQuestion
		}
	}
	answer, err := s.repository.SaveAnswerTx(ctx, tx, attemptID, questionID, request.SelectedOptionKey, request.Flagged)
	if err != nil {
		return answerResponse{}, err
	}

	// Recheck the database clock after writing so a request racing the deadline
	// cannot leave an expired attempt active.
	attempt, err = s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if err != nil {
		return answerResponse{}, err
	}
	if attempt.Status == "IN_PROGRESS" && attempt.DeadlinePassed {
		if err := s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE"); err != nil {
			return answerResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			return answerResponse{}, err
		}
		return answerResponse{}, ErrAttemptClosed
	}
	if attempt.Status != "IN_PROGRESS" {
		return answerResponse{}, ErrAttemptClosed
	}
	if err := tx.Commit(); err != nil {
		return answerResponse{}, err
	}
	return answerResponse{
		QuestionID: answer.QuestionID, SelectedOptionKey: answer.SelectedOptionKey,
		Flagged: answer.Flagged, AnsweredAt: answer.AnsweredAt,
	}, nil
}

func (s *Service) Submit(ctx context.Context, studentID, attemptID string) (resultResponse, error) {
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return resultResponse{}, err
	}
	defer tx.Rollback()

	attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return resultResponse{}, ErrAttemptNotFound
	}
	if err != nil {
		return resultResponse{}, err
	}
	if attempt.Status == "IN_PROGRESS" {
		allowed, accessErr := s.repository.HasPackageAccessTx(ctx, tx, studentID, attempt.PackageID)
		if accessErr != nil {
			return resultResponse{}, accessErr
		}
		if !allowed {
			return resultResponse{}, ErrAccessDenied
		}
		if attempt.DeadlinePassed {
			err = s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE")
		} else {
			err = s.finalizeTx(ctx, tx, attempt, "SUBMITTED", "MANUAL")
		}
		if err != nil {
			return resultResponse{}, err
		}
	}
	if attempt.Status != "IN_PROGRESS" && attempt.Status != "SUBMITTED" && attempt.Status != "EXPIRED" {
		return resultResponse{}, ErrAttemptClosed
	}
	if err := tx.Commit(); err != nil {
		return resultResponse{}, err
	}
	return s.Result(ctx, studentID, attemptID)
}

func (s *Service) Result(ctx context.Context, studentID, attemptID string) (resultResponse, error) {
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return resultResponse{}, err
	}
	defer tx.Rollback()

	attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return resultResponse{}, ErrAttemptNotFound
	}
	if err != nil {
		return resultResponse{}, err
	}
	finalized := false
	if attempt.Status == "IN_PROGRESS" && attempt.DeadlinePassed {
		if err := s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE"); err != nil {
			return resultResponse{}, err
		}
		attempt, err = s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
		if err != nil {
			return resultResponse{}, err
		}
		finalized = true
	}
	if attempt.Status == "IN_PROGRESS" {
		return resultResponse{}, ErrResultNotReady
	}
	allowed, err := s.repository.HasPackageAccessTx(ctx, tx, studentID, attempt.PackageID)
	if err != nil {
		if finalized {
			if commitErr := tx.Commit(); commitErr != nil {
				return resultResponse{}, commitErr
			}
		}
		return resultResponse{}, err
	}
	if !allowed {
		if finalized {
			if commitErr := tx.Commit(); commitErr != nil {
				return resultResponse{}, commitErr
			}
		}
		return resultResponse{}, ErrAccessDenied
	}
	questions, err := s.repository.LoadScoringQuestionsTx(ctx, tx, attemptID, attempt.PackageID)
	if err != nil {
		return resultResponse{}, err
	}
	response, err := buildResultResponse(attempt, questions)
	if err != nil {
		return resultResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return resultResponse{}, err
	}
	return response, nil
}

func (s *Service) History(ctx context.Context, studentID string, query pageQuery) (historyResponse, error) {
	if err := s.finalizeExpiredAttempts(ctx, studentID); err != nil {
		return historyResponse{}, err
	}
	total, err := s.repository.CountAttempts(ctx, studentID)
	if err != nil {
		return historyResponse{}, err
	}
	attempts, err := s.repository.ListAttempts(ctx, studentID, query)
	if err != nil {
		return historyResponse{}, err
	}
	items := make([]historyItem, 0, len(attempts))
	for _, attempt := range attempts {
		var score *float64
		if attempt.Score != nil {
			value, err := strconv.ParseFloat(*attempt.Score, 64)
			if err != nil {
				return historyResponse{}, err
			}
			score = &value
		}
		items = append(items, historyItem{
			ID: attempt.ID, PackageID: attempt.PackageID, PackageSlug: attempt.PackageSlug,
			Title: attempt.PackageTitle, Kind: attempt.PackageKind, Status: attempt.Status,
			StartedAt: attempt.StartedAt, DeadlineAt: attempt.DeadlineAt, SubmittedAt: attempt.SubmittedAt,
			DurationSeconds: attempt.DurationSeconds, Score: score,
			AnsweredCount: attempt.AnsweredCount, CorrectCount: attempt.CorrectCount,
			WrongCount: attempt.WrongCount, UnansweredCount: attempt.UnansweredCount,
		})
	}
	return historyResponse{
		Items:      items,
		Pagination: pagination{Page: query.Page, PageSize: query.PageSize, Total: total},
	}, nil
}

func (s *Service) loadAttemptResponseTx(ctx context.Context, tx *sql.Tx, attempt attemptRecord, resumed bool) (attemptResponse, error) {
	questions, err := s.repository.LoadAttemptQuestionsTx(ctx, tx, attempt.ID, attempt.PackageID)
	if err != nil {
		return attemptResponse{}, err
	}
	if int64(len(questions)) == 0 {
		return attemptResponse{}, ErrInvalidScoringData
	}
	remaining, err := s.repository.RemainingSecondsTx(ctx, tx, attempt.DeadlineAt)
	if err != nil {
		return attemptResponse{}, err
	}
	responses := make([]attemptQuestionResponse, 0, len(questions))
	var answered, flagged int64
	for _, question := range questions {
		points, err := strconv.ParseFloat(question.Points, 64)
		if err != nil {
			return attemptResponse{}, err
		}
		if question.SelectedOptionKey != nil {
			answered++
		}
		if question.Flagged {
			flagged++
		}
		options := make([]optionResponse, 0, len(question.Options))
		for _, option := range question.Options {
			options = append(options, optionResponse{Value: option.Key, Label: option.Label})
		}
		responses = append(responses, attemptQuestionResponse{
			ID: question.ID, Number: question.Position, Points: points,
			Topic: question.Topic, Difficulty: question.Difficulty,
			Type: question.QuestionType, Prompt: question.Prompt,
			Options: options, SelectedOptionKey: question.SelectedOptionKey, Flagged: question.Flagged,
		})
	}
	return attemptResponse{
		ID: attempt.ID, Resumed: resumed, PackageID: attempt.PackageID,
		PackageSlug: attempt.PackageSlug, Title: attempt.PackageTitle, Kind: attempt.PackageKind,
		EducationLevel: attempt.EducationLevel, Grade: attempt.Grade, Subject: attempt.Subject,
		Status: attempt.Status, StartedAt: attempt.StartedAt, DeadlineAt: attempt.DeadlineAt,
		RemainingSeconds: remaining, TotalQuestions: int64(len(questions)),
		AnsweredCount: answered, FlaggedCount: flagged,
		UnansweredCount: int64(len(questions)) - answered, Questions: responses,
	}, nil
}

func (s *Service) finalizeExpiredAttempts(ctx context.Context, studentID string) error {
	ids, err := s.repository.ExpiredAttemptIDs(ctx, studentID)
	if err != nil {
		return err
	}
	for _, attemptID := range ids {
		tx, err := s.repository.BeginTx(ctx)
		if err != nil {
			return err
		}
		attempt, err := s.repository.LockAttemptTx(ctx, tx, studentID, attemptID)
		if err != nil {
			tx.Rollback()
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		if attempt.Status == "IN_PROGRESS" && attempt.DeadlinePassed {
			if err := s.finalizeTx(ctx, tx, attempt, "EXPIRED", "DEADLINE"); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finalizeTx(ctx context.Context, tx *sql.Tx, attempt attemptRecord, status, finishReason string) error {
	questions, err := s.repository.LoadScoringQuestionsTx(ctx, tx, attempt.ID, attempt.PackageID)
	if err != nil {
		return err
	}
	summary, earnedByQuestion, err := calculateScore(questions, attempt.TotalPoints)
	if err != nil {
		return err
	}
	for _, question := range questions {
		earned := earnedByQuestion[question.ID]
		correct := question.SelectedOptionKey != nil && *question.SelectedOptionKey == question.CorrectOptionKey
		if err := s.repository.SaveEvaluatedAnswerTx(ctx, tx, attempt.ID, question.ID, correct, earned); err != nil {
			return err
		}
	}
	return s.repository.FinalizeAttemptTx(ctx, tx, attempt.ID, status, finishReason, summary)
}

func calculateScore(questions []attemptQuestion, storedTotal string) (scoreSummary, map[string]string, error) {
	total, err := parseDecimal(storedTotal)
	if err != nil || total.Sign() <= 0 || len(questions) == 0 {
		return scoreSummary{}, nil, ErrInvalidScoringData
	}
	earned := new(big.Rat)
	questionTotal := new(big.Rat)
	earnedByQuestion := make(map[string]string, len(questions))
	var answered, correctCount, wrong, unanswered int64
	for _, question := range questions {
		weight, err := parseDecimal(question.Points)
		if err != nil || weight.Sign() <= 0 {
			return scoreSummary{}, nil, ErrInvalidScoringData
		}
		questionTotal.Add(questionTotal, weight)
		isCorrect := question.SelectedOptionKey != nil && *question.SelectedOptionKey == question.CorrectOptionKey
		var questionEarned *big.Rat
		switch {
		case question.SelectedOptionKey == nil:
			unanswered++
			questionEarned = new(big.Rat)
		case isCorrect:
			answered++
			correctCount++
			questionEarned = new(big.Rat).Set(weight)
			earned.Add(earned, weight)
		default:
			answered++
			wrong++
			questionEarned = new(big.Rat)
		}
		earnedByQuestion[question.ID] = questionEarned.FloatString(2)
	}
	if questionTotal.Cmp(total) != 0 {
		return scoreSummary{}, nil, ErrInvalidScoringData
	}
	score := new(big.Rat).Mul(earned, big.NewRat(100, 1))
	score.Quo(score, total)
	return scoreSummary{
		Score: score.FloatString(2), EarnedPoints: earned.FloatString(2),
		TotalPoints: total.FloatString(2), AnsweredCount: answered,
		CorrectCount: correctCount, WrongCount: wrong, UnansweredCount: unanswered,
	}, earnedByQuestion, nil
}

func buildResultResponse(attempt attemptRecord, questions []attemptQuestion) (resultResponse, error) {
	if attempt.Score == nil || attempt.EarnedPoints == nil || attempt.SubmittedAt == nil {
		return resultResponse{}, ErrInvalidScoringData
	}
	score, err := strconv.ParseFloat(*attempt.Score, 64)
	if err != nil {
		return resultResponse{}, err
	}
	earnedPoints, err := strconv.ParseFloat(*attempt.EarnedPoints, 64)
	if err != nil {
		return resultResponse{}, err
	}
	totalPoints, err := strconv.ParseFloat(attempt.TotalPoints, 64)
	if err != nil {
		return resultResponse{}, err
	}
	duration := uint64(0)
	if attempt.DurationSeconds != nil {
		duration = *attempt.DurationSeconds
	}
	items := make([]resultQuestionResponse, 0, len(questions))
	for _, question := range questions {
		points, err := strconv.ParseFloat(question.Points, 64)
		if err != nil {
			return resultResponse{}, err
		}
		status := "unanswered"
		var selected *optionResponse
		if question.SelectedOptionKey != nil {
			selected = &optionResponse{
				Value: *question.SelectedOptionKey,
				Label: dereference(question.SelectedOptionLabel),
			}
			if *question.SelectedOptionKey == question.CorrectOptionKey {
				status = "correct"
			} else {
				status = "wrong"
			}
		}
		items = append(items, resultQuestionResponse{
			ID: question.ID, Number: question.Position, Points: points,
			Topic: question.Topic, Difficulty: question.Difficulty, Status: status,
			Prompt: question.Prompt, SelectedAnswer: selected,
			CorrectAnswer: optionResponse{Value: question.CorrectOptionKey, Label: question.CorrectOptionLabel},
			Explanation:   question.Explanation,
		})
	}
	return resultResponse{
		AttemptID: attempt.ID, PackageID: attempt.PackageID, PackageSlug: attempt.PackageSlug,
		Title: attempt.PackageTitle, Kind: attempt.PackageKind, Status: attempt.Status,
		FinishReason: attempt.FinishReason, StartedAt: attempt.StartedAt,
		SubmittedAt: attempt.SubmittedAt, DurationSeconds: duration,
		Score: score, EarnedPoints: earnedPoints, TotalPoints: totalPoints,
		TotalQuestions: int64(len(questions)), AnsweredCount: attempt.AnsweredCount,
		CorrectCount: attempt.CorrectCount, WrongCount: attempt.WrongCount,
		UnansweredCount: attempt.UnansweredCount, Passed: attempt.Passed,
		Questions: items,
	}, nil
}

func parseDecimal(value string) (*big.Rat, error) {
	decimal, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return nil, fmt.Errorf("invalid decimal value %q", value)
	}
	return decimal, nil
}

func dereference(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
