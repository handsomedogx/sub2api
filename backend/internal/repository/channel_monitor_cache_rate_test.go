package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorCacheRatesUseBoundGroupsAndRollingUsageWindow(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	end := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WITH targets AS .*SELECT id, group_id FROM channel_monitors.*FROM usage_logs.*created_at >= \$2 AND created_at < \$3.*group_id IN \(SELECT group_id FROM targets\)`).
		WithArgs(pq.Array([]int64{10, 20, 30}), end.Add(-24*time.Hour), end).
		WillReturnRows(sqlmock.NewRows([]string{"id", "rate"}).AddRow(10, 0).AddRow(20, 0.8))
	repo := &channelMonitorRepository{db: db}
	rates, err := repo.MonitorUsageCacheRates(context.Background(), []int64{10, 20, 30}, end)
	require.NoError(t, err)
	require.Equal(t, map[int64]float64{10: 0, 20: 0.8}, rates)
	require.NotContains(t, rates, int64(30), "absent usage must remain unknown")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorCacheRateQueryFailureIsNotZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("WITH targets AS").WillReturnError(errors.New("database unavailable"))
	rates, err := (&channelMonitorRepository{db: db}).MonitorUsageCacheRates(context.Background(), []int64{1}, time.Now())
	require.Error(t, err)
	require.Nil(t, rates)
}
