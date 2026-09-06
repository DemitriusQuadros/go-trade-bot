package e2e

import (
	"testing"

	"go-trade-bot/tests/e2e/steps"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	tc, err := steps.NewTestContext()
	if err != nil {
		t.Fatalf("failed to initialize test context: %v", err)
	}

	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			tc.T = t
			steps.RegisterAPISteps(sc, tc)
			steps.RegisterStrategySteps(sc, tc)
			steps.RegisterExchangeSteps(sc, tc)
			steps.RegisterDBAndRiskSteps(sc, tc)
			steps.RegisterNotificationSteps(sc, tc)
			steps.RegisterCandleSteps(sc, tc)
			steps.RegisterBacktestSteps(sc, tc)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if code := suite.Run(); code != 0 {
		t.Fatalf("godog suite failed with exit code %d", code)
	}
}
