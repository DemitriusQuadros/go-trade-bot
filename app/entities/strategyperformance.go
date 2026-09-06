package entities

type StrategyPerformance struct {
	Name   string  `json:"name"`
	Symbol string  `json:"symbol"`
	Profit float64 `json:"profit"`
	Trades int     `json:"trades"`
}
