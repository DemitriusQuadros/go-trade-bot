package cronspec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	for _, ok := range []string{"0 */6 * * *", "*/1 * * * *", "0 0 * * *", "@hourly", "@every 30m"} {
		assert.NoError(t, Validate(ok), ok)
	}
	for _, bad := range []string{"", "not a cron", "61 * * * *", "* * * *", "0 0 * * * *", "CRON_TZ=UTC 0 * * * *"} {
		assert.Error(t, Validate(bad), bad)
	}
}

func TestNext(t *testing.T) {
	from := time.Date(2026, 9, 27, 10, 15, 0, 0, time.UTC)
	n, ok := Next([]string{"0 */6 * * *", "30 10 * * *", "bogus"}, from)
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC), n)

	_, ok = Next(nil, from)
	assert.False(t, ok)
}
