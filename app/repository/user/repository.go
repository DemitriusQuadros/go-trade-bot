// Package user is the GORM repository for app accounts, login sessions and
// per-user agent chat usage (auth-01 §1).
package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound is returned when a user or session does not exist.
var ErrNotFound = errors.New("not found")

// Repository is the persistence port for users, sessions and usage.
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

// GormRepository implements Repository.
type GormRepository struct {
	db *gorm.DB
}

// NewGormRepository builds a GormRepository.
func NewGormRepository(db *gorm.DB) *GormRepository { return &GormRepository{db: db} }

func (r *GormRepository) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&entities.User{}).Count(&n).Error
	return n, err
}

func (r *GormRepository) CreateUser(ctx context.Context, u entities.User) (entities.User, error) {
	u.Username = strings.ToLower(u.Username)
	err := r.db.WithContext(ctx).Create(&u).Error
	return u, err
}

func (r *GormRepository) findOne(ctx context.Context, query string, arg any) (entities.User, error) {
	var found []entities.User
	if err := r.db.WithContext(ctx).Where(query, arg).Limit(1).Find(&found).Error; err != nil {
		return entities.User{}, err
	}
	if len(found) == 0 {
		return entities.User{}, ErrNotFound
	}
	return found[0], nil
}

func (r *GormRepository) GetUser(ctx context.Context, id uint) (entities.User, error) {
	return r.findOne(ctx, "id = ?", id)
}

// GetUserByUsername matches case-insensitively (usernames are stored lowercased).
func (r *GormRepository) GetUserByUsername(ctx context.Context, username string) (entities.User, error) {
	return r.findOne(ctx, "username = ?", strings.ToLower(strings.TrimSpace(username)))
}

func (r *GormRepository) ListUsers(ctx context.Context) ([]entities.User, error) {
	var out []entities.User
	err := r.db.WithContext(ctx).Order("id ASC").Find(&out).Error
	return out, err
}

// UpdateUser saves every mutable column (CreatedAt is kept).
func (r *GormRepository) UpdateUser(ctx context.Context, u entities.User) error {
	u.Username = strings.ToLower(u.Username)
	return r.db.WithContext(ctx).Omit("CreatedAt").Save(&u).Error
}

// DeleteUser removes a user, their sessions and usage rows. Audit columns
// on other tables keep the (now dangling) id.
func (r *GormRepository) DeleteUser(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&entities.Session{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&entities.UserUsage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&entities.User{}, id).Error
	})
}

func (r *GormRepository) CountEnabledAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&entities.User{}).
		Where("role = ? AND disabled = ?", "admin", false).Count(&n).Error
	return n, err
}

func (r *GormRepository) TouchLogin(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&entities.User{}).Where("id = ?", id).
		UpdateColumn("last_login_at", at).Error
}

// UserNames maps user id -> display name (username when the display name is empty).
func (r *GormRepository) UserNames(ctx context.Context) (map[uint]string, error) {
	var rows []entities.User
	if err := r.db.WithContext(ctx).Select("id", "username", "display_name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]string, len(rows))
	for _, u := range rows {
		name := u.DisplayName
		if name == "" {
			name = u.Username
		}
		out[u.ID] = name
	}
	return out, nil
}

func (r *GormRepository) CreateSession(ctx context.Context, s entities.Session) (entities.Session, error) {
	err := r.db.WithContext(ctx).Create(&s).Error
	return s, err
}

func (r *GormRepository) GetSessionByHash(ctx context.Context, hash string) (entities.Session, error) {
	var found []entities.Session
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).Limit(1).Find(&found).Error; err != nil {
		return entities.Session{}, err
	}
	if len(found) == 0 {
		return entities.Session{}, ErrNotFound
	}
	return found[0], nil
}

func (r *GormRepository) TouchSession(ctx context.Context, id uint, lastSeen, expires time.Time) error {
	return r.db.WithContext(ctx).Model(&entities.Session{}).Where("id = ?", id).
		Updates(map[string]any{"last_seen_at": lastSeen, "expires_at": expires}).Error
}

func (r *GormRepository) DeleteSession(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&entities.Session{}, id).Error
}

// DeleteUserSessions deletes every session of userID except exceptID (0 = all).
func (r *GormRepository) DeleteUserSessions(ctx context.Context, userID uint, exceptID uint) error {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	return q.Delete(&entities.Session{}).Error
}

func (r *GormRepository) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&entities.Session{}).Error
}

// AddUsage adds one run and cost to the user's usage row for day (upsert).
func (r *GormRepository) AddUsage(ctx context.Context, userID uint, day time.Time, cost float64) error {
	row := entities.UserUsage{UserID: userID, Day: entities.UsageDay(day), Runs: 1, CostUSD: cost}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"runs":     gorm.Expr("user_usages.runs + 1"),
			"cost_usd": gorm.Expr("user_usages.cost_usd + ?", cost),
		}),
	}).Create(&row).Error
}

// GetUsage returns the usage row for (user, day), or a zero row.
func (r *GormRepository) GetUsage(ctx context.Context, userID uint, day time.Time) (entities.UserUsage, error) {
	d := entities.UsageDay(day)
	var found []entities.UserUsage
	if err := r.db.WithContext(ctx).Where("user_id = ? AND day = ?", userID, d).Limit(1).Find(&found).Error; err != nil {
		return entities.UserUsage{}, err
	}
	if len(found) == 0 {
		return entities.UserUsage{UserID: userID, Day: d}, nil
	}
	return found[0], nil
}

// ListUsageForDay returns every user's usage for day, keyed by user id.
func (r *GormRepository) ListUsageForDay(ctx context.Context, day time.Time) (map[uint]entities.UserUsage, error) {
	var rows []entities.UserUsage
	if err := r.db.WithContext(ctx).Where("day = ?", entities.UsageDay(day)).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]entities.UserUsage, len(rows))
	for _, u := range rows {
		out[u.UserID] = u
	}
	return out, nil
}
