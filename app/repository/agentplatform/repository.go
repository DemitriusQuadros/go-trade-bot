// Package agentplatform is the GORM repository for the agents platform
// (agents-platform Phase A-01 §2): agent personas, strategy bindings, the
// per-strategy shared memory, agent reports, webhook targets and daily
// model-usage accounting.
package agentplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/customerror"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound is returned (wrapped in a 404 customerror) for missing rows.
var ErrNotFound = errors.New("agentplatform: not found")

// ReportFilter narrows ListReports. Zero values mean "no filter"; Limit <= 0
// defaults to 20 (capped at 200).
type ReportFilter struct {
	AgentID    *uint
	StrategyID *uint
	Severity   string
	Limit      int
	BeforeID   *uint
}

// Repository is the persistence port for the agents platform.
type Repository interface {
	// Agents
	CreateAgent(ctx context.Context, a entities.Agent) (entities.Agent, error)
	UpdateAgent(ctx context.Context, a entities.Agent) (entities.Agent, error)
	GetAgent(ctx context.Context, id uint) (entities.Agent, error)
	GetAgentByName(ctx context.Context, name string) (entities.Agent, error)
	GetDefaultAgent(ctx context.Context) (entities.Agent, error)
	ListAgents(ctx context.Context) ([]entities.Agent, error)
	// DeleteAgent refuses the IsDefault agent with a 409 customerror, and
	// removes the agent's bindings in the same transaction.
	DeleteAgent(ctx context.Context, id uint) error
	SetAgentPaused(ctx context.Context, id uint, paused bool) (entities.Agent, error)
	EnsureDefaultAgent(ctx context.Context) (entities.Agent, error)

	// Bindings
	SetBindings(ctx context.Context, agentID uint, strategyIDs []uint) error
	ListBindingsByAgent(ctx context.Context, agentID uint) ([]entities.AgentStrategyBinding, error)
	ListAgentsByStrategy(ctx context.Context, strategyID uint) ([]entities.Agent, error)
	// AddBinding binds one more strategy to the agent (no-op if bound).
	AddBinding(ctx context.Context, agentID, strategyID uint) error

	// Memory
	AppendMemory(ctx context.Context, entry entities.StrategyMemoryEntry) (entities.StrategyMemoryEntry, error)
	// ListMemory returns newest first. Empty kinds = all kinds. limit <= 0
	// defaults to 30.
	ListMemory(ctx context.Context, strategyID uint, kinds []entities.MemoryKind, limit int, beforeID *uint) ([]entities.StrategyMemoryEntry, error)

	// Reports
	CreateReport(ctx context.Context, r entities.AgentReport) (entities.AgentReport, error)
	GetReport(ctx context.Context, id uint) (entities.AgentReport, error)
	// ListReports returns newest first and omits RenderedHTML (list views
	// never need it).
	ListReports(ctx context.Context, filter ReportFilter) ([]entities.AgentReport, error)

	// Webhook targets
	CreateWebhookTarget(ctx context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error)
	UpdateWebhookTarget(ctx context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error)
	GetWebhookTarget(ctx context.Context, id uint) (entities.WebhookTarget, error)
	ListWebhookTargets(ctx context.Context) ([]entities.WebhookTarget, error)
	DeleteWebhookTarget(ctx context.Context, id uint) error
	ListWebhookTargetsByIDs(ctx context.Context, ids []uint) ([]entities.WebhookTarget, error)

	// Usage
	AddUsage(ctx context.Context, agentID uint, day time.Time, inTok, outTok int64, cost float64) error
	IncRuns(ctx context.Context, agentID uint, day time.Time) error
	// GetUsage returns a zero-valued row (AgentID/Day set) when none exists.
	GetUsage(ctx context.Context, agentID uint, day time.Time) (entities.AgentUsage, error)
	// ListUsage returns rows with from <= Day <= to, oldest first.
	ListUsage(ctx context.Context, agentID uint, from, to time.Time) ([]entities.AgentUsage, error)
	// MarkBudgetAlertSent atomically flips the day's BudgetAlertSent flag
	// false -> true and reports whether THIS call won (exactly-once across
	// processes).
	MarkBudgetAlertSent(ctx context.Context, agentID uint, day time.Time) (bool, error)
	// TryIncAutoDeploys atomically increments the day's AutoDeploys only
	// if it is below max, reporting whether it did (Phase B deploy cap).
	TryIncAutoDeploys(ctx context.Context, agentID uint, day time.Time, max int) (bool, error)
	// TryIncStrategiesCreated is the same for StrategiesCreated
	// (Phase B create_strategy rate limit).
	TryIncStrategiesCreated(ctx context.Context, agentID uint, day time.Time, max int) (bool, error)
}

// GormRepository implements Repository on GORM (Postgres in production,
// SQLite in tests).
type GormRepository struct {
	db *gorm.DB
}

// NewGormRepository builds a GormRepository.
func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func notFound(what string, id any) error {
	return &customerror.CustomError{Code: http.StatusNotFound, Message: fmt.Sprintf("%s %v not found", what, id)}
}

func wrapFirstErr(err error, what string, id any) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notFound(what, id)
	}
	return err
}

// --- Agents ----------------------------------------------------------------

func (r *GormRepository) CreateAgent(ctx context.Context, a entities.Agent) (entities.Agent, error) {
	normalizeAgent(&a)
	err := r.db.WithContext(ctx).Create(&a).Error
	return a, err
}

func (r *GormRepository) UpdateAgent(ctx context.Context, a entities.Agent) (entities.Agent, error) {
	normalizeAgent(&a)
	if err := r.db.WithContext(ctx).Save(&a).Error; err != nil {
		return entities.Agent{}, err
	}
	return r.GetAgent(ctx, a.ID)
}

// normalizeAgent keeps JSON columns non-NULL so reads always decode to an
// empty slice/object rather than nil.
func normalizeAgent(a *entities.Agent) {
	if a.Permissions == nil {
		a.Permissions = datatypes.JSONSlice[string]{}
	}
	if a.WebhookTargetIDs == nil {
		a.WebhookTargetIDs = datatypes.JSONSlice[uint]{}
	}
	if len(a.Triggers) == 0 {
		a.Triggers = datatypes.JSON(`{}`)
	}
}

func (r *GormRepository) GetAgent(ctx context.Context, id uint) (entities.Agent, error) {
	var a entities.Agent
	err := r.db.WithContext(ctx).First(&a, id).Error
	return a, wrapFirstErr(err, "agent", id)
}

func (r *GormRepository) GetAgentByName(ctx context.Context, name string) (entities.Agent, error) {
	var found []entities.Agent
	if err := r.db.WithContext(ctx).Where("name = ?", name).Limit(1).Find(&found).Error; err != nil {
		return entities.Agent{}, err
	}
	if len(found) == 0 {
		return entities.Agent{}, notFound("agent", name)
	}
	return found[0], nil
}

func (r *GormRepository) GetDefaultAgent(ctx context.Context) (entities.Agent, error) {
	// Find+Limit rather than First: "no default yet" is an expected state
	// on first migrate and should not be logged as a GORM error.
	var found []entities.Agent
	if err := r.db.WithContext(ctx).Where("is_default = ?", true).Order("id ASC").Limit(1).Find(&found).Error; err != nil {
		return entities.Agent{}, err
	}
	if len(found) == 0 {
		return entities.Agent{}, notFound("default agent", "")
	}
	return found[0], nil
}

func (r *GormRepository) ListAgents(ctx context.Context) ([]entities.Agent, error) {
	var out []entities.Agent
	err := r.db.WithContext(ctx).Order("is_default DESC, id ASC").Find(&out).Error
	return out, err
}

func (r *GormRepository) DeleteAgent(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a entities.Agent
		if err := tx.First(&a, id).Error; err != nil {
			return wrapFirstErr(err, "agent", id)
		}
		if a.IsDefault {
			return &customerror.CustomError{Code: http.StatusConflict, Message: "the default agent cannot be deleted"}
		}
		if err := tx.Where("agent_id = ?", id).Delete(&entities.AgentStrategyBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&entities.Agent{}, id).Error
	})
}

func (r *GormRepository) SetAgentPaused(ctx context.Context, id uint, paused bool) (entities.Agent, error) {
	res := r.db.WithContext(ctx).Model(&entities.Agent{}).Where("id = ?", id).
		Updates(map[string]any{"paused": paused, "updated_at": time.Now()})
	if res.Error != nil {
		return entities.Agent{}, res.Error
	}
	if res.RowsAffected == 0 {
		return entities.Agent{}, notFound("agent", id)
	}
	return r.GetAgent(ctx, id)
}

// DefaultAgentPermissions is every permission that grants something in
// Phase A - the default Copilot persona gets all of them.
func DefaultAgentPermissions() []string {
	return []string{
		string(entities.PermRead),
		string(entities.PermBacktest),
		string(entities.PermOptimize),
		string(entities.PermEditTesting),
		string(entities.PermNotify),
	}
}

// EnsureDefaultAgent creates the "Copilot" persona if no IsDefault row
// exists. Idempotent, and safe against two binaries migrating concurrently:
// the Name unique index makes the loser's insert fail, after which the
// winner's row is re-read.
func (r *GormRepository) EnsureDefaultAgent(ctx context.Context) (entities.Agent, error) {
	if a, err := r.GetDefaultAgent(ctx); err == nil {
		return a, nil
	}
	a := entities.Agent{
		Name:        entities.DefaultAgentName,
		Goal:        "",
		Permissions: DefaultAgentPermissions(),
		Triggers:    datatypes.JSON(`{}`),
		IsDefault:   true,
	}
	if created, err := r.CreateAgent(ctx, a); err == nil {
		return created, nil
	}
	// Either a concurrent migrator won the race, or a non-default agent is
	// already named "Copilot" - promote that row rather than failing
	// startup.
	if existing, err := r.GetDefaultAgent(ctx); err == nil {
		return existing, nil
	}
	existing, err := r.GetAgentByName(ctx, entities.DefaultAgentName)
	if err != nil {
		return entities.Agent{}, fmt.Errorf("agentplatform: failed to ensure default agent: %w", err)
	}
	existing.IsDefault = true
	return r.UpdateAgent(ctx, existing)
}

// --- Bindings --------------------------------------------------------------

func (r *GormRepository) SetBindings(ctx context.Context, agentID uint, strategyIDs []uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&entities.AgentStrategyBinding{}).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		rows := make([]entities.AgentStrategyBinding, 0, len(strategyIDs))
		for _, sid := range strategyIDs {
			if seen[sid] {
				continue
			}
			seen[sid] = true
			rows = append(rows, entities.AgentStrategyBinding{AgentID: agentID, StrategyID: sid, CreatedAt: time.Now()})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

func (r *GormRepository) ListBindingsByAgent(ctx context.Context, agentID uint) ([]entities.AgentStrategyBinding, error) {
	var out []entities.AgentStrategyBinding
	err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("strategy_id ASC").Find(&out).Error
	return out, err
}

func (r *GormRepository) ListAgentsByStrategy(ctx context.Context, strategyID uint) ([]entities.Agent, error) {
	var out []entities.Agent
	err := r.db.WithContext(ctx).
		Where("id IN (?)", r.db.Model(&entities.AgentStrategyBinding{}).Select("agent_id").Where("strategy_id = ?", strategyID)).
		Order("id ASC").Find(&out).Error
	return out, err
}

func (r *GormRepository) AddBinding(ctx context.Context, agentID, strategyID uint) error {
	row := entities.AgentStrategyBinding{AgentID: agentID, StrategyID: strategyID, CreatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// --- Memory ----------------------------------------------------------------

func (r *GormRepository) AppendMemory(ctx context.Context, entry entities.StrategyMemoryEntry) (entities.StrategyMemoryEntry, error) {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	err := r.db.WithContext(ctx).Create(&entry).Error
	return entry, err
}

func (r *GormRepository) ListMemory(ctx context.Context, strategyID uint, kinds []entities.MemoryKind, limit int, beforeID *uint) ([]entities.StrategyMemoryEntry, error) {
	if limit <= 0 {
		limit = 30
	}
	q := r.db.WithContext(ctx).Where("strategy_id = ?", strategyID)
	if len(kinds) > 0 {
		q = q.Where("kind IN ?", kinds)
	}
	if beforeID != nil {
		q = q.Where("id < ?", *beforeID)
	}
	var out []entities.StrategyMemoryEntry
	err := q.Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// --- Reports ---------------------------------------------------------------

func (r *GormRepository) CreateReport(ctx context.Context, rep entities.AgentReport) (entities.AgentReport, error) {
	if rep.StrategyIDs == nil {
		rep.StrategyIDs = datatypes.JSONSlice[uint]{}
	}
	if rep.CreatedAt.IsZero() {
		rep.CreatedAt = time.Now()
	}
	err := r.db.WithContext(ctx).Create(&rep).Error
	return rep, err
}

func (r *GormRepository) GetReport(ctx context.Context, id uint) (entities.AgentReport, error) {
	var rep entities.AgentReport
	err := r.db.WithContext(ctx).First(&rep, id).Error
	return rep, wrapFirstErr(err, "report", id)
}

// listReportColumns excludes rendered_html, which can be up to 1 MB per row.
var listReportColumns = []string{"id", "agent_id", "agent_run_id", "strategy_ids", "title", "severity", "summary", "blocks_json", "created_at"}

func (r *GormRepository) ListReports(ctx context.Context, f ReportFilter) ([]entities.AgentReport, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&entities.AgentReport{}).Select(listReportColumns)
		if f.AgentID != nil {
			q = q.Where("agent_id = ?", *f.AgentID)
		}
		if f.Severity != "" {
			q = q.Where("severity = ?", f.Severity)
		}
		return q
	}

	if f.StrategyID == nil {
		q := base()
		if f.BeforeID != nil {
			q = q.Where("id < ?", *f.BeforeID)
		}
		var out []entities.AgentReport
		err := q.Order("id DESC").Limit(limit).Find(&out).Error
		return out, err
	}

	// Postgres: containment on the jsonb array, fully in SQL.
	if r.db.Dialector.Name() == "postgres" {
		q := base().Where("strategy_ids @> ?", fmt.Sprintf("[%d]", *f.StrategyID))
		if f.BeforeID != nil {
			q = q.Where("id < ?", *f.BeforeID)
		}
		var out []entities.AgentReport
		err := q.Order("id DESC").Limit(limit).Find(&out).Error
		return out, err
	}

	// Portable fallback (SQLite in tests): page through bounded batches and
	// filter in Go.
	const batch = 200
	const maxBatches = 25
	out := make([]entities.AgentReport, 0, limit)
	cursor := f.BeforeID
	for i := 0; i < maxBatches && len(out) < limit; i++ {
		q := base()
		if cursor != nil {
			q = q.Where("id < ?", *cursor)
		}
		var page []entities.AgentReport
		if err := q.Order("id DESC").Limit(batch).Find(&page).Error; err != nil {
			return nil, err
		}
		for _, rep := range page {
			for _, sid := range rep.StrategyIDs {
				if sid == *f.StrategyID {
					out = append(out, rep)
					break
				}
			}
			if len(out) >= limit {
				break
			}
		}
		if len(page) < batch {
			break
		}
		last := page[len(page)-1].ID
		cursor = &last
	}
	return out, nil
}

// --- Webhook targets -------------------------------------------------------

func (r *GormRepository) CreateWebhookTarget(ctx context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error) {
	err := r.db.WithContext(ctx).Create(&t).Error
	return t, err
}

func (r *GormRepository) UpdateWebhookTarget(ctx context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error) {
	if err := r.db.WithContext(ctx).Save(&t).Error; err != nil {
		return entities.WebhookTarget{}, err
	}
	return r.GetWebhookTarget(ctx, t.ID)
}

func (r *GormRepository) GetWebhookTarget(ctx context.Context, id uint) (entities.WebhookTarget, error) {
	var t entities.WebhookTarget
	err := r.db.WithContext(ctx).First(&t, id).Error
	return t, wrapFirstErr(err, "webhook target", id)
}

func (r *GormRepository) ListWebhookTargets(ctx context.Context) ([]entities.WebhookTarget, error) {
	var out []entities.WebhookTarget
	err := r.db.WithContext(ctx).Order("id ASC").Find(&out).Error
	return out, err
}

func (r *GormRepository) DeleteWebhookTarget(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&entities.WebhookTarget{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return notFound("webhook target", id)
	}
	return nil
}

func (r *GormRepository) ListWebhookTargetsByIDs(ctx context.Context, ids []uint) ([]entities.WebhookTarget, error) {
	if len(ids) == 0 {
		return []entities.WebhookTarget{}, nil
	}
	var out []entities.WebhookTarget
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Order("id ASC").Find(&out).Error
	return out, err
}

// --- Usage -----------------------------------------------------------------

func (r *GormRepository) AddUsage(ctx context.Context, agentID uint, day time.Time, inTok, outTok int64, cost float64) error {
	row := entities.AgentUsage{AgentID: agentID, Day: entities.UsageDay(day), InputTokens: inTok, OutputTokens: outTok, CostUSD: cost}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"input_tokens":  gorm.Expr("agent_usages.input_tokens + ?", inTok),
			"output_tokens": gorm.Expr("agent_usages.output_tokens + ?", outTok),
			"cost_usd":      gorm.Expr("agent_usages.cost_usd + ?", cost),
		}),
	}).Create(&row).Error
}

func (r *GormRepository) IncRuns(ctx context.Context, agentID uint, day time.Time) error {
	row := entities.AgentUsage{AgentID: agentID, Day: entities.UsageDay(day), Runs: 1}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{"runs": gorm.Expr("agent_usages.runs + 1")}),
	}).Create(&row).Error
}

func (r *GormRepository) GetUsage(ctx context.Context, agentID uint, day time.Time) (entities.AgentUsage, error) {
	d := entities.UsageDay(day)
	// Find, not First: no row yet is the normal state (checked before
	// every model call by the RunGuard) and must not log as an error.
	var found []entities.AgentUsage
	if err := r.db.WithContext(ctx).Where("agent_id = ? AND day = ?", agentID, d).Limit(1).Find(&found).Error; err != nil {
		return entities.AgentUsage{}, err
	}
	if len(found) == 0 {
		return entities.AgentUsage{AgentID: agentID, Day: d}, nil
	}
	return found[0], nil
}

func (r *GormRepository) ListUsage(ctx context.Context, agentID uint, from, to time.Time) ([]entities.AgentUsage, error) {
	var out []entities.AgentUsage
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND day >= ? AND day <= ?", agentID, entities.UsageDay(from), entities.UsageDay(to)).
		Order("day ASC").Find(&out).Error
	return out, err
}

func (r *GormRepository) MarkBudgetAlertSent(ctx context.Context, agentID uint, day time.Time) (bool, error) {
	d := entities.UsageDay(day)
	// Make sure the row exists (no-op if it does), then race on the
	// conditional flip.
	row := entities.AgentUsage{AgentID: agentID, Day: d}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return false, err
	}
	res := r.db.WithContext(ctx).Model(&entities.AgentUsage{}).
		Where("agent_id = ? AND day = ? AND budget_alert_sent = ?", agentID, d, false).
		Update("budget_alert_sent", true)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *GormRepository) TryIncAutoDeploys(ctx context.Context, agentID uint, day time.Time, max int) (bool, error) {
	return r.tryIncUsageColumn(ctx, agentID, day, "auto_deploys", max)
}

func (r *GormRepository) TryIncStrategiesCreated(ctx context.Context, agentID uint, day time.Time, max int) (bool, error) {
	return r.tryIncUsageColumn(ctx, agentID, day, "strategies_created", max)
}

// tryIncUsageColumn is a conditional increment (col < max) so concurrent
// runs can never exceed the cap. column is one of a fixed set of
// identifiers (never user input).
func (r *GormRepository) tryIncUsageColumn(ctx context.Context, agentID uint, day time.Time, column string, max int) (bool, error) {
	if max <= 0 {
		return false, nil
	}
	d := entities.UsageDay(day)
	row := entities.AgentUsage{AgentID: agentID, Day: d}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return false, err
	}
	res := r.db.WithContext(ctx).Model(&entities.AgentUsage{}).
		Where("agent_id = ? AND day = ? AND "+column+" < ?", agentID, d, max).
		Update(column, gorm.Expr(column+" + 1"))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// MarshalTriggers is a small helper for callers building an Agent.
func MarshalTriggers(t entities.AgentTriggers) datatypes.JSON {
	b, err := json.Marshal(t)
	if err != nil {
		return datatypes.JSON(`{}`)
	}
	return datatypes.JSON(b)
}
