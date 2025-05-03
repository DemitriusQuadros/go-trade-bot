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

func (r AccountRepository) UpdateAccount(account entities.Account) error {
	return r.db.Save(&account).Error
}
