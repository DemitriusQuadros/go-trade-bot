package candleimport

import (
	"context"

	"go-trade-bot/app/entities"
	"gorm.io/gorm"
)

type Repository interface {
	CreateJob(ctx context.Context, job *entities.ImportJob) error
	UpdateJob(ctx context.Context, job *entities.ImportJob) error
	GetJob(ctx context.Context, id string) (*entities.ImportJob, error)
	
	CreateSchedule(ctx context.Context, schedule *entities.ImportSchedule) error
	UpdateSchedule(ctx context.Context, schedule *entities.ImportSchedule) error
	DeleteSchedule(ctx context.Context, id uint) error
	GetSchedule(ctx context.Context, id uint) (*entities.ImportSchedule, error)
	ListSchedules(ctx context.Context) ([]entities.ImportSchedule, error)
	ListEnabledSchedules(ctx context.Context) ([]entities.ImportSchedule, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) CreateJob(ctx context.Context, job *entities.ImportJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *repository) UpdateJob(ctx context.Context, job *entities.ImportJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *repository) GetJob(ctx context.Context, id string) (*entities.ImportJob, error) {
	var job entities.ImportJob
	err := r.db.WithContext(ctx).First(&job, "id = ?", id).Error
	return &job, err
}

func (r *repository) CreateSchedule(ctx context.Context, schedule *entities.ImportSchedule) error {
	return r.db.WithContext(ctx).Create(schedule).Error
}

func (r *repository) UpdateSchedule(ctx context.Context, schedule *entities.ImportSchedule) error {
	return r.db.WithContext(ctx).Save(schedule).Error
}

func (r *repository) DeleteSchedule(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&entities.ImportSchedule{}, id).Error
}

func (r *repository) GetSchedule(ctx context.Context, id uint) (*entities.ImportSchedule, error) {
	var sched entities.ImportSchedule
	err := r.db.WithContext(ctx).First(&sched, id).Error
	return &sched, err
}

func (r *repository) ListSchedules(ctx context.Context) ([]entities.ImportSchedule, error) {
	var schedules []entities.ImportSchedule
	err := r.db.WithContext(ctx).Find(&schedules).Error
	return schedules, err
}

func (r *repository) ListEnabledSchedules(ctx context.Context) ([]entities.ImportSchedule, error) {
	var schedules []entities.ImportSchedule
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&schedules).Error
	return schedules, err
}
