package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"binasiswa/backend/internal/config"
)

type Service struct {
	repository    *Repository
	tokens        tokenManager
	refreshTTL    time.Duration
	secureCookies bool
}

func NewService(repository *Repository, cfg config.AuthConfig) *Service {
	return &Service{
		repository:    repository,
		tokens:        newTokenManager(cfg.AccessTokenSecret, cfg.AccessTokenTTL),
		refreshTTL:    cfg.RefreshTokenTTL,
		secureCookies: cfg.SecureCookies,
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
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return sessionIssue{}, err
	}
	sessionID, err := newUUID()
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
	if err := s.repository.CreateSession(ctx, tx, sessionID, userID, refreshHash, now.Add(s.refreshTTL), ""); err != nil {
		return sessionIssue{}, fmt.Errorf("insert authentication session: %w", err)
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
		RefreshToken: refreshToken, RefreshExpiresAt: now.Add(s.refreshTTL),
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
	return s.issueSession(ctx, user, profile)
}

func (s *Service) issueSession(ctx context.Context, user userRecord, profile *studentProfile) (sessionIssue, error) {
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return sessionIssue{}, err
	}
	sessionID, err := newUUID()
	if err != nil {
		return sessionIssue{}, err
	}
	accessToken, accessExpires, err := s.tokens.issue(user.ID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("issue access token: %w", err)
	}
	now := time.Now().UTC()
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("begin session transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.repository.CreateSession(ctx, tx, sessionID, user.ID, refreshHash, now.Add(s.refreshTTL), ""); err != nil {
		return sessionIssue{}, fmt.Errorf("insert authentication session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return sessionIssue{}, fmt.Errorf("commit session transaction: %w", err)
	}
	return sessionIssue{
		User: toUserResponse(user, profile), AccessToken: accessToken, AccessExpiresAt: accessExpires,
		RefreshToken: refreshToken, RefreshExpiresAt: now.Add(s.refreshTTL),
	}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (sessionIssue, error) {
	if !s.Available() {
		return sessionIssue{}, ErrServiceUnavailable
	}
	tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("begin refresh transaction: %w", err)
	}
	defer tx.Rollback()

	session, err := s.repository.FindSessionForUpdate(ctx, tx, hashRefreshToken(refreshToken))
	if errors.Is(err, sql.ErrNoRows) {
		return sessionIssue{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return sessionIssue{}, fmt.Errorf("load refresh session: %w", err)
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(time.Now().UTC()) || session.Status != "ACTIVE" {
		return sessionIssue{}, ErrInvalidRefreshToken
	}
	profile, err := s.repository.FindProfile(ctx, tx, session.UserID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("load refresh profile: %w", err)
	}

	newToken, newHash, err := newRefreshToken()
	if err != nil {
		return sessionIssue{}, err
	}
	newSessionID, err := newUUID()
	if err != nil {
		return sessionIssue{}, err
	}
	now := time.Now().UTC()
	rowsAffected, err := s.repository.RevokeSession(ctx, tx, session.ID, now)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("revoke rotated session: %w", err)
	}
	if rowsAffected != 1 {
		return sessionIssue{}, ErrInvalidRefreshToken
	}
	if err := s.repository.CreateSession(ctx, tx, newSessionID, session.UserID, newHash, now.Add(s.refreshTTL), session.ID); err != nil {
		return sessionIssue{}, fmt.Errorf("insert rotated session: %w", err)
	}
	accessToken, accessExpires, err := s.tokens.issue(session.UserID)
	if err != nil {
		return sessionIssue{}, fmt.Errorf("issue access token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return sessionIssue{}, fmt.Errorf("commit refresh transaction: %w", err)
	}
	return sessionIssue{
		User: toUserResponse(userRecord{
			ID: session.UserID, Email: session.Email, Role: session.Role,
			Status: session.Status, CreatedAt: session.CreatedAt,
		}, profile),
		AccessToken: accessToken, AccessExpiresAt: accessExpires,
		RefreshToken: newToken, RefreshExpiresAt: now.Add(s.refreshTTL),
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if !s.Available() {
		return ErrServiceUnavailable
	}
	if err := s.repository.RevokeRefreshToken(ctx, hashRefreshToken(refreshToken), time.Now().UTC()); err != nil {
		return fmt.Errorf("revoke authentication session: %w", err)
	}
	return nil
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

func hashRefreshToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
