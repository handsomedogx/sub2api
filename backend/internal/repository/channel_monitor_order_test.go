package repository

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorDisplayOrderTransaction(t *testing.T) {
	for _, outcome := range []string{"saved", "new monitor", "unknown monitor", "write failure"} {
		t.Run(outcome, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &channelMonitorRepository{db: db}
			ids := []int64{9, 3}
			mock.ExpectBegin()
			mock.ExpectExec("LOCK TABLE channel_monitors").WillReturnResult(sqlmock.NewResult(0, 0))
			total, matched := 2, 2
			if outcome == "new monitor" {
				total = 3
			}
			if outcome == "unknown monitor" {
				matched = 1
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\), COUNT`).WithArgs(pq.Array(ids)).WillReturnRows(sqlmock.NewRows([]string{"total", "matched"}).AddRow(total, matched))
			if total != 2 || matched != 2 {
				mock.ExpectRollback()
				require.ErrorIs(t, repo.UpdateDisplayOrder(context.Background(), ids), service.ErrChannelMonitorOrderConflict)
			} else {
				mock.ExpectExec("DELETE FROM channel_monitor_display_orders").WillReturnResult(sqlmock.NewResult(0, 2))
				insert := mock.ExpectExec("INSERT INTO channel_monitor_display_orders").WithArgs(pq.Array(ids))
				if outcome == "write failure" {
					insert.WillReturnError(errors.New("write failed"))
					mock.ExpectRollback()
					require.ErrorContains(t, repo.UpdateDisplayOrder(context.Background(), ids), "write failed")
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 2))
					mock.ExpectCommit()
					require.NoError(t, repo.UpdateDisplayOrder(context.Background(), ids))
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelMonitorDisplayOrderLabels(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT m.id, m.name, m.provider, m.group_name, m.enabled").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "provider", "group_name", "enabled"}).
			AddRow(9, "Paused", "openai", "GPT", false).AddRow(3, "Claude", "anthropic", "", true))
	items, err := (&channelMonitorRepository{db: db}).ListDisplayOrder(context.Background())
	require.NoError(t, err)
	require.Equal(t, []service.ChannelMonitorOrderItem{{ID: 9, Name: "Paused", Provider: "openai", GroupName: "GPT"}, {ID: 3, Name: "Claude", Provider: "anthropic", Enabled: true}}, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorDisplayOrderUsedBeforeAdminPaginationAndUserFiltering(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &channelMonitorRepository{db: db, client: dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))}
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	// The same order expression precedes LIMIT/OFFSET on the paginated admin list.
	mock.ExpectQuery(`ORDER BY COALESCE\(\(SELECT sort_order FROM channel_monitor_display_orders WHERE monitor_id = "channel_monitors"\."id"\), 2147483647\) ASC, "channel_monitors"\."id" DESC LIMIT`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "enabled"}).AddRow(3, "third", true))
	items, total, err := repo.List(context.Background(), service.ChannelMonitorListParams{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.EqualValues(t, 3, items[0].ID)
	mock.ExpectQuery(`WHERE "channel_monitors"\."enabled" ORDER BY COALESCE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "enabled"}).AddRow(9, "first", true).AddRow(3, "third", true))
	items, err = repo.ListEnabled(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 9, items[0].ID)
	require.EqualValues(t, 3, items[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
