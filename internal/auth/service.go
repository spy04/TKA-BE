package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"binasiswa/backend/internal/config"
)

type Service struct {
	repository *Repository
	tokens     tokenManager
}

func NewService(repository *Repository, cfg config.AuthConfig) *Service {
	return &Service{
		repository: repository,
		tokens:     newTokenManager(cfg.AccessTokenSecret, cfg.AccessTokenTTL),
	}
}

func (s *Service) Available() bool {
	return s != nil && s.repository.available()
}

func (s *Service) Authenticate(ctx context.Context, accessToken string) (principal, error) {
	if s == nil {
		return principal{}, ErrServiceUnavailable
	}
	claims, err := s.tokens.verify(accessToken)
	if err != nil {
		return principal{}, ErrInvalidAccessToken
	}
	if !s.Available() {
		return principal{}, ErrServiceUnavailable
	}
	identity, status, err := s.repository.FindPrincipal(ctx, claims.Subject)
	if errors.Is(err, sql.ErrNoRows) || err == nil && status != "ACTIVE" {
		return principal{}, ErrAccountUnavailable
	}
	if err != nil {
		return principal{}, err
	}
	return identity, nil
}

func (s *Service) Register(ctx context.Context, request registerRequest, email, level string) (sessionIssue, error) {
	if !s.Available() {
		return sessionIssue{}, ErrServiceUnavailable
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	passwordHash, err := hashPassword(request.Password)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("hash registration password: %w", err)
	}
	userID, err := newUUID()
	if err != nil {
		return sessionIssue{}, err
	}
	accessToken, accessExpires, err := s.tokens.issue(userID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("issue access token: %w", err)
	}
	now := time.Now().UTC()

	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("begin registration transaction: %w", err)
	}
	defer tx.Rollback()

	user := userRecord{
		ID: userID, Email: email, Password: passwordHash, Role: "STUDENT",
		Status: "ACTIVE", CreatedAt: now,
	}
	if err := s.repository.CreateUser(ctx, tx, user); err != nil {
		return sessionIssue{}, err
	}
	if err := s.repository.CreateStudentProfile(ctx, tx, userID, request, level); err != nil {
		return sessionIssue{}, fmt.Errorf("insert student profile: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return sessionIssue{}, fmt.Errorf("commit registration transaction: %w", err)
	}

	return sessionIssue{
		User: toUserResponse(user, &studentProfile{
			DisplayName: request.DisplayName, EducationLevel: level, Grade: request.Grade,
			School: optionalTrimmed(request.School), Curriculum: optionalTrimmed(request.Curriculum),
		}),
		AccessToken: accessToken, AccessExpiresAt: accessExpires,
	}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (sessionIssue, error) {
	if !s.Available() {
		return sessionIssue{}, ErrServiceUnavailable
	}
	user, err := s.repository.FindUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		if _, hashErr := hashPassword(password); hashErr != nil {
			return sessionIssue{}, fmt.Errorf("verify login credentials: %w", hashErr)
		}
		return sessionIssue{}, ErrInvalidCredentials
	}
	if err != nil {
		return sessionIssue{}, fmt.Errorf("load login account: %w", err)
	}
	passwordMatches := verifyPassword(user.Password, password)
	if user.Status != "ACTIVE" || !passwordMatches {
		return sessionIssue{}, ErrInvalidCredentials
	}
	profile, err := s.repository.FindProfile(ctx, nil, user.ID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("load login profile: %w", err)
	}
	return s.issueAccessToken(user, profile)
}

func (s *Service) issueAccessToken(user userRecord, profile *studentProfile) (sessionIssue, error) {
	accessToken, accessExpires, err := s.tokens.issue(user.ID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("issue access token: %w", err)
	}
	return sessionIssue{
		User: toUserResponse(user, profile), AccessToken: accessToken, AccessExpiresAt: accessExpires,
	}, nil
}

func (s *Service) Me(ctx context.Context, userID string) (userResponse, error) {
	if !s.Available() {
		return userResponse{}, ErrServiceUnavailable
	}
	user, err := s.repository.FindUserByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return userResponse{}, ErrAccountUnavailable
	}
	if err != nil {
		return userResponse{}, fmt.Errorf("load current user: %w", err)
	}
	return toUserResponse(user, user.Profile), nil
}

func toUserResponse(user userRecord, profile *studentProfile) userResponse {
	return userResponse{
		ID: user.ID, Email: user.Email, Role: user.Role, Status: user.Status,
		CreatedAt: user.CreatedAt, Profile: toProfileResponse(profile),
	}
}

func toProfileResponse(profile *studentProfile) *profileResponse {
	if profile == nil {
		return nil
	}
	return &profileResponse{
		DisplayName: profile.DisplayName, EducationLevel: profile.EducationLevel,
		Grade: profile.Grade, School: profile.School, Curriculum: profile.Curriculum,
	}
}

func nullableTrimmed(value string) any {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return nil
}

func optionalTrimmed(value string) *string {
	if value = strings.TrimSpace(value); value != "" {
		return &value
	}
	return nil
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := fmt.Sprintf("%x", value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
