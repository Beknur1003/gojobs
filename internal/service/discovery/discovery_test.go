package discovery

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Beknur1003/gojobs/internal/models"
	"github.com/Beknur1003/gojobs/internal/repository/httpx"
)

var now = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func TestPlan_Registry_ActiveAndDueSlice(t *testing.T) {
	cfg := Config{PerRun: map[string]int{"greenhouse": 2, "lever": 1}, Recheck: 30 * 24 * time.Hour}
	seeds := map[string][]string{
		"greenhouse": {"fresh1", "fresh2", "fresh3", "recent", "stale", "active", "manual"},
		"lever":      {"neon"},
	}
	boards := map[string]models.BoardStatus{
		"greenhouse:recent": {Checked: now.Add(-24 * time.Hour), OK: true},
		"greenhouse:stale":  {Checked: now.Add(-40 * 24 * time.Hour), OK: true},
		"greenhouse:active": {Checked: now.Add(-24 * time.Hour), OK: true, Go: 3},
	}
	manual := map[string]bool{"greenhouse:manual": true}

	active, probe := Plan(cfg, seeds, manual, boards, now, false)

	assert.Equal(t, []string{"greenhouse:active"}, active)
	assert.Equal(t, []string{"greenhouse:fresh1", "lever:neon", "greenhouse:fresh2"}, probe, "never-checked first, PerRun per kind, kinds interleaved")

	_, all := Plan(cfg, seeds, manual, boards, now, true)
	assert.Equal(t, []string{"greenhouse:fresh1", "lever:neon", "greenhouse:fresh2", "greenhouse:fresh3", "greenhouse:stale"}, all,
		"a sweep takes every due board: never checked, then stale; recent and manual are skipped")
}

func TestRecord_Outcomes_Registry(t *testing.T) {
	boards := map[string]models.BoardStatus{
		"greenhouse:flaky": {Checked: now.Add(-24 * time.Hour), OK: true, Go: 2},
	}
	Record(boards, map[string]Result{
		"greenhouse:ok":    {Postings: 40, Go: 3},
		"greenhouse:gone":  {Err: fmt.Errorf("greenhouse.Fetch gone: %w", httpx.ErrNotFound)},
		"greenhouse:flaky": {Err: errors.New("status 503")},
	}, now)

	assert.Equal(t, models.BoardStatus{Checked: now, OK: true, Postings: 40, Go: 3}, boards["greenhouse:ok"])
	assert.Equal(t, models.BoardStatus{Checked: now, OK: false, Error: "not found"}, boards["greenhouse:gone"])
	flaky := boards["greenhouse:flaky"]
	assert.True(t, flaky.OK, "a transient failure keeps the board active")
	assert.Equal(t, 2, flaky.Go)
	assert.Equal(t, "status 503", flaky.Error)
	assert.Equal(t, now, flaky.Checked, "a failure still counts as a check")
	assert.Equal(t, 1, flaky.Fails)
}

func TestRecord_RepeatedFailures_Demoted(t *testing.T) {
	boards := map[string]models.BoardStatus{"workday:x": {Checked: now.Add(-48 * time.Hour), OK: true, Go: 4, Fails: 2}}
	Record(boards, map[string]Result{"workday:x": {Err: errors.New("status 500")}}, now)
	assert.Equal(t, models.BoardStatus{Checked: now, OK: false, Go: 0, Fails: 3, Error: "status 500"}, boards["workday:x"])
}

func TestRecord_PartialFetchWithGo_Active(t *testing.T) {
	boards := map[string]models.BoardStatus{}
	Record(boards, map[string]Result{"workday:new": {Postings: 3, Go: 2, Err: errors.New("detail: status 500")}}, now)
	got := boards["workday:new"]
	assert.True(t, got.OK)
	assert.Equal(t, 2, got.Go, "its Go roles must be refreshed daily, not in 30 days")
}

func TestIsBoard_Names_Classified(t *testing.T) {
	assert.True(t, IsBoard("greenhouse:stripe"))
	assert.True(t, IsBoard("workday:crowdstrike|wd5|crowdstrikecareers"))
	assert.False(t, IsBoard("telegram:rabota_golang"))
	assert.False(t, IsBoard("remoteok"))
}

func TestRecord_ProductivePartialThenOutage_StaysActive(t *testing.T) {
	boards := map[string]models.BoardStatus{}
	for day := 0; day < 5; day++ {
		Record(boards, map[string]Result{"workday:x": {Postings: 3, Go: 2, Err: errors.New("role /job/1: status 500")}}, now)
	}
	Record(boards, map[string]Result{"workday:x": {Err: errors.New("status 503")}}, now)

	got := boards["workday:x"]
	assert.True(t, got.OK, "five productive days and one outage: still active")
	assert.Equal(t, 1, got.Fails)
}
