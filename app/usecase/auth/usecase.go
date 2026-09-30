// Package auth is the app-account usecase (auth-01 §4-§6): login with a
// rate limit, cookie sessions (sliding 30-day expiry), the current user,
// admin user management, first-admin bootstrap and the per-user daily agent
// chat budget.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	userrepo "go-trade-bot/app/repository/user"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"

	"golang.org/x/crypto/bcrypt"
)

// Repository is the persistence this usecase needs (app/repository/user).
type Repository interface {
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, u entities.User) (entities.User, error)
	GetUser(ctx context.Context, id uint) (entities.User, error)
	GetUserByUsername(ctx context.Context, username string) (entities.User, error)
	ListUsers(ctx context.Context) ([]entities.User, error)
	UpdateUser(ctx context.Context, u entities.User) error
	DeleteUser(ctx context.Context, id uint) error
	CountEnabledAdmins(ctx context.Context) (int64, error)
	TouchLogin(ctx context.Context, id uint, at time.Time) error
	UserNames(ctx context.Context) (map[uint]string, error)

	CreateSession(ctx context.Context, s entities.Session) (entities.Session, error)
	GetSessionByHash(ctx context.Context, hash string) (entities.Session, error)
	TouchSession(ctx context.Context, id uint, lastSeen, expires time.Time) error
	DeleteSession(ctx context.Context, id uint) error
	DeleteUserSessions(ctx context.Context, userID uint, exceptID uint) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error

	AddUsage(ctx context.Context, userID uint, day time.Time, cost float64) error
	GetUsage(ctx context.Context, userID uint, day time.Time) (entities.UserUsage, error)
	ListUsageForDay(ctx context.Context, day time.Time) (map[uint]entities.UserUsage, error)
}

// Counter is the login-failure counter (internal/ratelimit).
type Counter interface {
	Count(ctx context.Context, key string) (int64, error)
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	Reset(ctx context.Context, key string) error
}

// Policy constants (auth-01 §4).
const (
	SessionTTL         = 30 * 24 * time.Hour
	SessionSlideAfter  = time.Hour
	LoginWindow        = 15 * time.Minute
	MaxFailuresPerUser = 5
	MaxFailuresPerIP   = 20
	MinPasswordLen     = 10
	// DefaultBcryptCost is the production bcrypt cost (spec: >= 12).
	DefaultBcryptCost = 12
)

// Error codes (the JSON "error" field).
const (
	CodeInvalidCredentials = "invalid_credentials"
	CodeRateLimited        = "rate_limited"
	CodeUnauthorized       = "unauthorized"
	CodeBudgetExceeded     = "user_budget_exceeded"
	CodeValidation         = "validation_error"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
)

// ErrNoSession is returned by ResolveSession for a missing, unknown or
// expired session, or a disabled user.
var ErrNoSession = errors.New("auth: no valid session")

var usernameRe = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

// UseCase implements auth-01's account logic.
type UseCase struct {
	repo    Repository
	limiter Counter
	// BcryptCost is the cost for new hashes (tests lower it).
	BcryptCost int
	// Now is injectable for tests.
	Now       func() time.Time
	dummyHash []byte
}

// NewUseCase builds a UseCase.
func NewUseCase(repo Repository, limiter Counter) *UseCase {
	return &UseCase{repo: repo, limiter: limiter, BcryptCost: DefaultBcryptCost, Now: time.Now}
}

func invalidCredentials() error {
	return customerror.NewCoded(http.StatusUnauthorized, CodeInvalidCredentials, "invalid username or password")
}

func badRequest(format string, args ...any) error {
	return customerror.NewCoded(http.StatusBadRequest, CodeValidation, fmt.Sprintf(format, args...))
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (u *UseCase) cost() int {
	if u.BcryptCost < bcrypt.MinCost {
		return DefaultBcryptCost
	}
	return u.BcryptCost
}

// HashPassword bcrypt-hashes a password.
func (u *UseCase) HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), u.cost())
	return string(h), err
}

// dummy returns a hash to compare against for unknown users, so login
// timing does not reveal which usernames exist.
func (u *UseCase) dummy() []byte {
	if u.dummyHash == nil {
		h, _ := bcrypt.GenerateFromPassword([]byte("gtb-dummy-password-for-timing"), u.cost())
		u.dummyHash = h
	}
	return u.dummyHash
}

// NormalizeUsername lowercases and validates a username.
func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernameRe.MatchString(s) {
		return "", badRequest("username must be 3-32 characters of a-z, 0-9, '_', '.', '-'")
	}
	return s, nil
}

func validatePassword(pw string) error {
	if len([]rune(pw)) < MinPasswordLen {
		return badRequest("password must be at least %d characters", MinPasswordLen)
	}
	if len(pw) > 72 {
		// bcrypt only uses the first 72 bytes.
		return badRequest("password must be at most 72 bytes")
	}
	return nil
}

// PrincipalFor builds the request principal of a user.
func PrincipalFor(user entities.User, sessionID uint) authz.Principal {
	name := user.DisplayName
	if name == "" {
		name = user.Username
	}
	return authz.Principal{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: name,
		Role:        user.Role,
		Caps:        authz.NormalizeCapabilities(user.Capabilities),
		Locale:      user.Locale,
		SessionID:   sessionID,
	}
}

// --- login / sessions ----------------------------------------------------------

// LoginResult is a successful login.
type LoginResult struct {
	User  entities.User
	Token string // raw session token, for the cookie only
}

// Login checks credentials under the rate limit and creates a session.
// Unknown user, bad password and disabled user are the same 401
// invalid_credentials; too many failures is 429 rate_limited.
func (u *UseCase) Login(ctx context.Context, username, password, ip, userAgent string) (LoginResult, error) {
	uname := strings.ToLower(strings.TrimSpace(username))
	userKey := "auth:login-fail:user:" + uname
	ipKey := "auth:login-fail:ip:" + ip
	if u.limiter != nil {
		nu, _ := u.limiter.Count(ctx, userKey)
		ni, _ := u.limiter.Count(ctx, ipKey)
		if nu >= MaxFailuresPerUser || ni >= MaxFailuresPerIP {
			log.Printf("auth: login rate_limited user=%s ip=%s", uname, ip)
			return LoginResult{}, customerror.NewCoded(http.StatusTooManyRequests, CodeRateLimited, "too many failed login attempts, try again later")
		}
	}

	fail := func() (LoginResult, error) {
		if u.limiter != nil {
			_, _ = u.limiter.Incr(ctx, userKey, LoginWindow)
			_, _ = u.limiter.Incr(ctx, ipKey, LoginWindow)
		}
		log.Printf("auth: login failed user=%s ip=%s", uname, ip)
		return LoginResult{}, invalidCredentials()
	}

	user, err := u.repo.GetUserByUsername(ctx, uname)
	if err != nil {
		if !errors.Is(err, userrepo.ErrNotFound) {
			return LoginResult{}, err
		}
		_ = bcrypt.CompareHashAndPassword(u.dummy(), []byte(password))
		return fail()
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil || user.Disabled {
		return fail()
	}

	token, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := u.Now().UTC()
	if _, err := u.repo.CreateSession(ctx, entities.Session{
		UserID: user.ID, TokenHash: hashToken(token), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(SessionTTL),
		UserAgent: truncate(userAgent, 200), IP: truncate(ip, 64),
	}); err != nil {
		return LoginResult{}, err
	}
	_ = u.repo.TouchLogin(ctx, user.ID, now)
	user.LastLoginAt = &now
	if u.limiter != nil {
		_ = u.limiter.Reset(ctx, userKey)
	}
	// Housekeeping: expired sessions are useless rows.
	_ = u.repo.DeleteExpiredSessions(ctx, now)
	log.Printf("auth: login ok user=%s ip=%s", uname, ip)
	return LoginResult{User: user, Token: token}, nil
}

// ResolveSession turns a raw cookie token into a principal, sliding the
// expiry when the session was last seen more than an hour ago.
func (u *UseCase) ResolveSession(ctx context.Context, raw string) (authz.Principal, error) {
	if raw == "" {
		return authz.Principal{}, ErrNoSession
	}
	s, err := u.repo.GetSessionByHash(ctx, hashToken(raw))
	if err != nil {
		if errors.Is(err, userrepo.ErrNotFound) {
			return authz.Principal{}, ErrNoSession
		}
		return authz.Principal{}, err
	}
	now := u.Now().UTC()
	if !now.Before(s.ExpiresAt) {
		_ = u.repo.DeleteSession(ctx, s.ID)
		return authz.Principal{}, ErrNoSession
	}
	user, err := u.repo.GetUser(ctx, s.UserID)
	if err != nil || user.Disabled {
		_ = u.repo.DeleteSession(ctx, s.ID)
		return authz.Principal{}, ErrNoSession
	}
	if now.Sub(s.LastSeenAt) > SessionSlideAfter {
		_ = u.repo.TouchSession(ctx, s.ID, now, now.Add(SessionTTL))
	}
	return PrincipalFor(user, s.ID), nil
}

// Logout deletes the session behind raw (a no-op when there is none).
func (u *UseCase) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	s, err := u.repo.GetSessionByHash(ctx, hashToken(raw))
	if err != nil {
		return nil
	}
	return u.repo.DeleteSession(ctx, s.ID)
}

// SetupRequired reports whether there are no users yet.
func (u *UseCase) SetupRequired(ctx context.Context) (bool, error) {
	n, err := u.repo.CountUsers(ctx)
	return n == 0, err
}

// Bootstrap creates the first admin when the users table is empty and both
// values are set. It never logs the password.
func (u *UseCase) Bootstrap(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return nil
	}
	n, err := u.repo.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	name, err := NormalizeUsername(username)
	if err != nil {
		return fmt.Errorf("auth: AUTH.BOOTSTRAP_ADMIN_USERNAME: %w", err)
	}
	if err := validatePassword(password); err != nil {
		return fmt.Errorf("auth: AUTH.BOOTSTRAP_ADMIN_PASSWORD: %w", err)
	}
	hash, err := u.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := u.repo.CreateUser(ctx, entities.User{
		Username: name, DisplayName: name, PasswordHash: hash, Role: authz.RoleAdmin,
		Capabilities: authz.RolePreset(authz.RoleAdmin), DailyAgentBudgetUSD: entities.DefaultUserDailyAgentBudgetUSD,
	}); err != nil {
		return err
	}
	log.Printf("auth: bootstrap admin %q created - remove AUTH.BOOTSTRAP_ADMIN_PASSWORD from config", name)
	return nil
}

// --- current user --------------------------------------------------------------

// Me is GET /auth/me.
type Me struct {
	ID                  uint
	Username            string
	DisplayName         string
	Email               *string
	Role                string
	Capabilities        []string
	Locale              string
	DailyAgentBudgetUSD float64
	TodayAgentCostUSD   float64
}

// Me returns the current user's view. Synthetic principals (service token,
// insecure mode) get a view built from the principal.
func (u *UseCase) Me(ctx context.Context, p authz.Principal) (Me, error) {
	if p.UserID == 0 {
		return Me{Username: p.Username, DisplayName: p.DisplayName, Role: p.Role, Capabilities: p.Caps, Locale: p.Locale}, nil
	}
	user, err := u.repo.GetUser(ctx, p.UserID)
	if err != nil {
		return Me{}, err
	}
	usage, _ := u.repo.GetUsage(ctx, user.ID, u.Now())
	return meOf(user, usage.CostUSD), nil
}

func meOf(user entities.User, todayCost float64) Me {
	return Me{
		ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Email: user.Email, Role: user.Role,
		Capabilities: authz.NormalizeCapabilities(user.Capabilities), Locale: user.Locale,
		DailyAgentBudgetUSD: user.DailyAgentBudgetUSD, TodayAgentCostUSD: todayCost,
	}
}

// UpdateMeRequest is PATCH /auth/me (all optional).
type UpdateMeRequest struct {
	DisplayName     *string
	Locale          *string
	CurrentPassword *string
	NewPassword     *string
}

// UpdateMe edits the current user. A password change needs the current
// password and signs out the user's other sessions.
func (u *UseCase) UpdateMe(ctx context.Context, p authz.Principal, req UpdateMeRequest) (Me, error) {
	if p.UserID == 0 {
		return Me{}, badRequest("the %s principal has no editable profile", p.Username)
	}
	user, err := u.repo.GetUser(ctx, p.UserID)
	if err != nil {
		return Me{}, err
	}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if len([]rune(name)) > 64 {
			return Me{}, badRequest("display_name must be at most 64 characters")
		}
		user.DisplayName = name
	}
	if req.Locale != nil {
		if !entities.IsValidLocale(*req.Locale) {
			return Me{}, badRequest("locale must be one of \"\", en, es, pt-BR")
		}
		user.Locale = *req.Locale
	}
	passwordChanged := false
	if req.NewPassword != nil {
		if req.CurrentPassword == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(*req.CurrentPassword)) != nil {
			return Me{}, badRequest("current_password is wrong")
		}
		if err := validatePassword(*req.NewPassword); err != nil {
			return Me{}, err
		}
		h, err := u.HashPassword(*req.NewPassword)
		if err != nil {
			return Me{}, err
		}
		user.PasswordHash = h
		passwordChanged = true
	}
	user.UpdatedAt = u.Now()
	if err := u.repo.UpdateUser(ctx, user); err != nil {
		return Me{}, err
	}
	if passwordChanged {
		if err := u.repo.DeleteUserSessions(ctx, user.ID, p.SessionID); err != nil {
			return Me{}, err
		}
	}
	usage, _ := u.repo.GetUsage(ctx, user.ID, u.Now())
	return meOf(user, usage.CostUSD), nil
}

// --- per-user agent chat budget (auth-01 §6) ------------------------------------

// CheckChatBudget returns a 409 user_budget_exceeded when the user's spend
// today reached their DailyAgentBudgetUSD. Synthetic principals are not
// limited (the persona budgets still apply).
func (u *UseCase) CheckChatBudget(ctx context.Context, p authz.Principal) error {
	if p.UserID == 0 {
		return nil
	}
	user, err := u.repo.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	usage, err := u.repo.GetUsage(ctx, user.ID, u.Now())
	if err != nil {
		return err
	}
	if usage.CostUSD >= user.DailyAgentBudgetUSD {
		return customerror.NewCoded(http.StatusConflict, CodeBudgetExceeded,
			fmt.Sprintf("your daily agent budget ($%.2f) is used up; it resets at 00:00 UTC", user.DailyAgentBudgetUSD))
	}
	return nil
}

// RecordChatUsage adds one run and its cost to the user's usage today.
func (u *UseCase) RecordChatUsage(ctx context.Context, p authz.Principal, costUSD float64) error {
	if p.UserID == 0 {
		return nil
	}
	return u.repo.AddUsage(ctx, p.UserID, u.Now(), costUSD)
}

// UserNames maps user id -> display name (for audit DTOs).
func (u *UseCase) UserNames(ctx context.Context) map[uint]string {
	names, err := u.repo.UserNames(ctx)
	if err != nil {
		return map[uint]string{}
	}
	return names
}

// --- user management (admin, auth-01 §5) -----------------------------------------

// UserView is one row of GET /users.
type UserView struct {
	User              entities.User
	TodayAgentCostUSD float64
}

// ListUsers returns every user with today's agent cost.
func (u *UseCase) ListUsers(ctx context.Context) ([]UserView, error) {
	users, err := u.repo.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := u.repo.ListUsageForDay(ctx, u.Now())
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(users))
	for _, x := range users {
		out = append(out, UserView{User: x, TodayAgentCostUSD: usage[x.ID].CostUSD})
	}
	return out, nil
}

func (u *UseCase) viewOf(ctx context.Context, user entities.User) UserView {
	usage, _ := u.repo.GetUsage(ctx, user.ID, u.Now())
	return UserView{User: user, TodayAgentCostUSD: usage.CostUSD}
}

// CreateUserRequest is POST /users.
type CreateUserRequest struct {
	Username    string
	DisplayName string
	Email       *string
	Role        string
	Password    string
}

func normalizeEmail(e *string) (*string, error) {
	if e == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*e)
	if v == "" {
		return nil, nil
	}
	if !strings.Contains(v, "@") || len(v) > 254 {
		return nil, badRequest("email is not valid")
	}
	v = strings.ToLower(v)
	return &v, nil
}

func (u *UseCase) conflictIfTaken(ctx context.Context, username string, email *string, selfID uint) error {
	if other, err := u.repo.GetUserByUsername(ctx, username); err == nil && other.ID != selfID {
		return customerror.NewCoded(http.StatusConflict, CodeConflict, fmt.Sprintf("username %q is taken", username))
	}
	if email != nil {
		users, err := u.repo.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, x := range users {
			if x.ID != selfID && x.Email != nil && strings.EqualFold(*x.Email, *email) {
				return customerror.NewCoded(http.StatusConflict, CodeConflict, "email is already used by another user")
			}
		}
	}
	return nil
}

// CreateUser creates a user with the role's preset capabilities.
func (u *UseCase) CreateUser(ctx context.Context, req CreateUserRequest) (UserView, error) {
	name, err := NormalizeUsername(req.Username)
	if err != nil {
		return UserView{}, err
	}
	if !authz.IsValidRole(req.Role) {
		return UserView{}, badRequest("role must be admin, friend or viewer")
	}
	if err := validatePassword(req.Password); err != nil {
		return UserView{}, err
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return UserView{}, err
	}
	if err := u.conflictIfTaken(ctx, name, email, 0); err != nil {
		return UserView{}, err
	}
	hash, err := u.HashPassword(req.Password)
	if err != nil {
		return UserView{}, err
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = name
	}
	created, err := u.repo.CreateUser(ctx, entities.User{
		Username: name, DisplayName: display, Email: email, PasswordHash: hash, Role: req.Role,
		Capabilities: authz.RolePreset(req.Role), DailyAgentBudgetUSD: entities.DefaultUserDailyAgentBudgetUSD,
	})
	if err != nil {
		return UserView{}, err
	}
	return UserView{User: created}, nil
}

// UpdateUserRequest is PUT /users/{id}; nil fields are left unchanged.
type UpdateUserRequest struct {
	DisplayName         *string
	Email               *string
	Role                *string
	Capabilities        *[]string
	DailyAgentBudgetUSD *float64
	Disabled            *bool
}

func (u *UseCase) getUser(ctx context.Context, id uint) (entities.User, error) {
	user, err := u.repo.GetUser(ctx, id)
	if errors.Is(err, userrepo.ErrNotFound) {
		return entities.User{}, customerror.NewCoded(http.StatusNotFound, CodeNotFound, fmt.Sprintf("user %d not found", id))
	}
	return user, err
}

func isEnabledAdmin(x entities.User) bool { return x.Role == authz.RoleAdmin && !x.Disabled }

// UpdateUser edits a user. Changing the role resets capabilities to the
// preset unless capabilities are also sent. Disabling deletes the user's
// sessions. The last enabled admin cannot be demoted or disabled.
func (u *UseCase) UpdateUser(ctx context.Context, id uint, req UpdateUserRequest) (UserView, error) {
	user, err := u.getUser(ctx, id)
	if err != nil {
		return UserView{}, err
	}
	wasAdmin := isEnabledAdmin(user)
	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		email, err := normalizeEmail(req.Email)
		if err != nil {
			return UserView{}, err
		}
		if err := u.conflictIfTaken(ctx, user.Username, email, user.ID); err != nil {
			return UserView{}, err
		}
		user.Email = email
	}
	if req.Role != nil && *req.Role != user.Role {
		if !authz.IsValidRole(*req.Role) {
			return UserView{}, badRequest("role must be admin, friend or viewer")
		}
		user.Role = *req.Role
		user.Capabilities = authz.RolePreset(user.Role)
	}
	if req.Capabilities != nil {
		for _, c := range *req.Capabilities {
			if !authz.IsValidCapability(c) {
				return UserView{}, badRequest("unknown capability %q", c)
			}
		}
		user.Capabilities = authz.NormalizeCapabilities(*req.Capabilities)
	}
	if req.DailyAgentBudgetUSD != nil {
		b := *req.DailyAgentBudgetUSD
		if b < 0 || b > 1000 || b != b {
			return UserView{}, badRequest("daily_agent_budget_usd must be between 0 and 1000")
		}
		user.DailyAgentBudgetUSD = b
	}
	disabling := false
	if req.Disabled != nil {
		disabling = *req.Disabled && !user.Disabled
		user.Disabled = *req.Disabled
	}
	if wasAdmin && !isEnabledAdmin(user) {
		n, err := u.repo.CountEnabledAdmins(ctx)
		if err != nil {
			return UserView{}, err
		}
		if n <= 1 {
			return UserView{}, badRequest("cannot demote or disable the last enabled admin")
		}
	}
	user.UpdatedAt = u.Now()
	if err := u.repo.UpdateUser(ctx, user); err != nil {
		return UserView{}, err
	}
	if disabling {
		if err := u.repo.DeleteUserSessions(ctx, user.ID, 0); err != nil {
			return UserView{}, err
		}
	}
	return u.viewOf(ctx, user), nil
}

// ResetPassword sets a new password and signs out all of that user's sessions.
func (u *UseCase) ResetPassword(ctx context.Context, id uint, password string) error {
	user, err := u.getUser(ctx, id)
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	h, err := u.HashPassword(password)
	if err != nil {
		return err
	}
	user.PasswordHash = h
	user.UpdatedAt = u.Now()
	if err := u.repo.UpdateUser(ctx, user); err != nil {
		return err
	}
	return u.repo.DeleteUserSessions(ctx, user.ID, 0)
}

// DeleteUser deletes a user; refused (400) for yourself and for the last
// enabled admin.
func (u *UseCase) DeleteUser(ctx context.Context, actor authz.Principal, id uint) error {
	if actor.UserID != 0 && actor.UserID == id {
		return badRequest("you cannot delete yourself")
	}
	user, err := u.getUser(ctx, id)
	if err != nil {
		return err
	}
	if isEnabledAdmin(user) {
		n, err := u.repo.CountEnabledAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return badRequest("cannot delete the last enabled admin")
		}
	}
	return u.repo.DeleteUser(ctx, id)
}
