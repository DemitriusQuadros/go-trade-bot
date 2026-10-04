package repository

import (
	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type AccountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) AccountRepository {
	return AccountRepository{
		db: db,
	}
}

func (r AccountRepository) Create(account entities.Account) error {
	return r.db.Create(&account).Error
}

func (r AccountRepository) GetAccountByID(id int64) (entities.Account, error) {
	var account entities.Account
	err := r.db.Where("id = ?", id).First(&account).Error
	if err != nil {
		return entities.Account{}, err
	}
	return account, nil
}

func (r AccountRepository) GetByMode(mode entities.AccountMode) (entities.Account, error) {
	var account entities.Account
	err := r.db.Where("mode = ?", mode).First(&account).Error
	if err != nil {
		// Backward compatibility: fallback to ID 1 if dryrun
		if mode == entities.AccountModeDryRun {
			return r.GetAccountByID(1)
		}
		return entities.Account{}, err
	}
	return account, nil
}

func (r AccountRepository) GetAllAccounts() ([]entities.Account, error) {
	var accounts []entities.Account
	err := r.db.Order("id ASC").Find(&accounts).Error
	return accounts, err
}

func (r AccountRepository) UpdateAccount(account entities.Account) error {
	return r.db.Save(&account).Error
}

func (r AccountRepository) EnsureDefaultAccounts() error {
	var dryRun entities.Account
	err := r.db.Where("mode = ?", entities.AccountModeDryRun).First(&dryRun).Error
	if err != nil {
		var legacy entities.Account
		if legacyErr := r.db.Where("id = ?", 1).First(&legacy).Error; legacyErr == nil {
			legacy.Mode = entities.AccountModeDryRun
			if legacy.InitialAmount == 0 && legacy.Amount > 0 {
				legacy.InitialAmount = legacy.Amount
			}
			if err := r.db.Save(&legacy).Error; err != nil {
				return err
			}
		} else {
			dryRun = entities.Account{
				ID:              1,
				Mode:            entities.AccountModeDryRun,
				Amount:          10000,
				InitialAmount:   10000,
				AvailableOrders: 5,
				Currency:        "USDT",
			}
			if err := r.db.Create(&dryRun).Error; err != nil {
				return err
			}
		}
	}

	var live entities.Account
	err = r.db.Where("mode = ?", entities.AccountModeLive).First(&live).Error
	if err != nil {
		live = entities.Account{
			ID:              2,
			Mode:            entities.AccountModeLive,
			Amount:          0,
			InitialAmount:   0,
			AvailableOrders: 5,
			Currency:        "USDT",
		}
		if err := r.db.Create(&live).Error; err != nil {
			return err
		}
	}
	return nil
}
