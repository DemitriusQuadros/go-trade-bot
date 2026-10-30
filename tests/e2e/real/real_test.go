package real

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestRealStackFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: register,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("real-stack godog suite failed")
	}
}
