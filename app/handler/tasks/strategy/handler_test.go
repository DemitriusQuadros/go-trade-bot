package handler_test

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/tasks/strategy"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
)

func TestHandleStrategyTask(t *testing.T) {
	strategy := entities.Strategy{
		Name: "TestStrategy",
	}

	payload, err := json.Marshal(strategy)
	if err != nil {
		t.Fatalf("Error on marshal strategy: %v ", err)
	}

	task := asynq.NewTask(handler.StrategyTask+strategy.Name, payload)
	processor := &handler.StrategyProcessor{}

	err = processor.HandleStrategyTask(context.Background(), task)

	assert.Nil(t, err)
}
