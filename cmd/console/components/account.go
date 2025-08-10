package components

import (
	repository "go-trade-bot/app/repository/account"
	"go-trade-bot/cmd/console/dependencies"

	"strconv"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func Account(d *dependencies.Dependencies) *widgets.List {
	list := widgets.NewList()
	list.Title = "Account Data"

	accountRepository := repository.NewAccountRepository(d.Db)
	account, err := accountRepository.GetAccountByID(1)

	if err != nil {
		list.Rows = []string{"Error fetching account data: " + err.Error()}
		return list
	}

	list.Rows = []string{
		"ID: " + strconv.FormatInt(account.ID, 10),
		"Amount: " + strconv.FormatFloat(float64(account.Amount), 'f', 2, 64),
		"Available: " + strconv.FormatInt(account.AvailableOrders, 10),
		"Currency: " + account.Currency,
		"Created At: " + account.CreatedAt.String(),
		"Updated At: " + account.UpdatedAt.String(),
	}

	list.TextStyle = ui.NewStyle(ui.ColorWhite)
	list.BorderStyle = ui.NewStyle(ui.ColorCyan)
	list.SetRect(0, 7, 50, 20)
	list.BorderStyle.Fg = ui.ColorCyan
	return list

}
