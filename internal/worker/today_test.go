package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── w.today() ────────────────────────────────────────────────────────────────
//
// Critical invariant: w.today() must always return UTC midnight for the
// current calendar day in the configured location.  The API's parseDate()
// always returns UTC midnight, so worker and API must agree on the same
// Day row.  If w.today() returned midnight in a non-UTC zone (e.g. CEST)
// SQLite would store it as e.g. 22:00 UTC the previous day, and the
// frontend would find an empty row.

func TestWorkerToday_IsUTCMidnight(t *testing.T) {
	w := &Worker{loc: time.UTC}
	got := w.today()
	assert.Equal(t, time.UTC, got.Location(), "today() must be in UTC")
	assert.Equal(t, 0, got.Hour())
	assert.Equal(t, 0, got.Minute())
	assert.Equal(t, 0, got.Second())
	assert.Equal(t, 0, got.Nanosecond())
}

func TestWorkerToday_NonUTCLocIsStillUTCMidnight(t *testing.T) {
	// Using Europe/Berlin (UTC+1/+2). Regardless of offset, the stored value
	// must be UTC midnight so that GORM/SQLite serialises it the same way
	// parseDate() produces it.
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	w := &Worker{loc: berlin}
	got := w.today()

	assert.Equal(t, time.UTC, got.Location(), "today() must be in UTC even for non-UTC loc")
	assert.Equal(t, 0, got.Hour())
	assert.Equal(t, 0, got.Minute())
	assert.Equal(t, 0, got.Second())
}

func TestWorkerToday_UsesLocalCalendarDate(t *testing.T) {
	// The date component (year/month/day) must reflect the calendar day in
	// the configured location, not necessarily the UTC calendar day.
	// We synthesise a location that is always UTC+14 (far ahead), then check
	// that w.today() returns the date that is current in that zone.
	plus14 := time.FixedZone("UTC+14", 14*60*60)
	now := time.Now().In(plus14)
	expectedDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	w := &Worker{loc: plus14}
	got := w.today()

	assert.Equal(t, expectedDate, got,
		"today() must use the calendar date from the configured location")
}

func TestWorkerToday_MatchesParseDate(t *testing.T) {
	// parseDate() in the API truncates a YYYY-MM-DD string to UTC midnight.
	// Simulate that: format w.today() as a date string and parse it back —
	// the round-trip must be identity.
	for _, locName := range []string{"UTC", "Europe/Berlin", "America/New_York", "Asia/Tokyo"} {
		t.Run(locName, func(t *testing.T) {
			loc, err := time.LoadLocation(locName)
			require.NoError(t, err)

			w := &Worker{loc: loc}
			today := w.today()

			// Simulate what the API's parseDate does.
			dateStr := today.Format("2006-01-02")
			parsed, err := time.Parse("2006-01-02", dateStr)
			require.NoError(t, err)
			parsedUTC := parsed.UTC().Truncate(24 * time.Hour)

			assert.Equal(t, today, parsedUTC,
				"w.today() round-tripped through date string must be identity for loc %s", locName)
		})
	}
}
