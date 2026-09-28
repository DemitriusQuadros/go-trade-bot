// Package cronspec validates agent cron specs and computes their next fire
// time, using the same robfig/cron parser asynq's Scheduler uses so a spec
// the API accepts is a spec cmd/agent can schedule. All specs are UTC.
package cronspec

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// parser accepts standard 5-field specs plus descriptors (@hourly, @every 1h).
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Validate returns a descriptive error for an invalid spec.
func Validate(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("cron spec is empty")
	}
	if strings.HasPrefix(strings.ToUpper(spec), "TZ=") || strings.HasPrefix(strings.ToUpper(spec), "CRON_TZ=") {
		return fmt.Errorf("cron spec %q: time zones are not supported, specs are always UTC", spec)
	}
	if _, err := parser.Parse(spec); err != nil {
		return fmt.Errorf("invalid cron spec %q: %v", spec, err)
	}
	return nil
}

// Next returns the earliest next fire time (UTC) across specs after from,
// ignoring invalid specs; ok is false if there is none.
func Next(specs []string, from time.Time) (time.Time, bool) {
	var best time.Time
	found := false
	for _, s := range specs {
		sched, err := parser.Parse(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		n := sched.Next(from.UTC())
		if n.IsZero() {
			continue
		}
		if !found || n.Before(best) {
			best, found = n, true
		}
	}
	return best.UTC(), found
}
