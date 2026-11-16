package auth

import (
	"errors"
	"time"
)

var (
	ErrServiceUnavailable  = errors.New("authentication service unavailable")
	ErrDuplicateEmail      = errors.New("email already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidAccessToken  = errors.New("invalid access token")
	ErrAccountUnavailable  = errors.New("account unavailable")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)

type principal struct {
	userID string
	role   string
}

type registerRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	DisplayName    string `json:"displayName"`
	EducationLevel string `json:"educationLevel"`
	Grade          int    `json:"grade"`
	School         string `json:"school"`
	Curriculum     string `json:"curriculum"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type studentProfile struct {
	DisplayName    string
	EducationLevel string
	Grade          int
	School         *string
	Curriculum     *string
}

type userRecord struct {
	ID        string
	Email     string
	Password  string
	Role      string
	Status    string
	CreatedAt time.Time
	Profile   *studentProfile
}

type authSession struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time
	Email     string
	Role      string
	Status    string
	CreatedAt time.Time
}

type profileResponse struct {
	DisplayName    string  `json:"displayName"`
	EducationLevel string  `json:"educationLevel"`
	Grade          int     `json:"grade"`
	School         *string `json:"school"`
	Curriculum     *string `json:"curriculum"`
}

type userResponse struct {
	ID        string           `json:"id"`
	Email     string           `json:"email"`
	Role      string           `json:"role"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"createdAt"`
	Profile   *profileResponse `json:"profile"`
}

type authResponse struct {
	User        userResponse `json:"user"`
	AccessToken string       `json:"accessToken"`
	TokenType   string       `json:"tokenType"`
	ExpiresIn   int64        `json:"expiresIn"`
}

type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresIn   int64  `json:"expiresIn"`
}

type sessionIssue struct {
	User             userResponse
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}
