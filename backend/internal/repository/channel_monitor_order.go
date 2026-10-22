package repository

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Unarranged monitors follow the saved list; new monitors need no order write.
func channelMonitorDisplayOrder(s *entsql.Selector) {
	s.OrderExpr(entsql.Expr(fmt.Sprintf("COALESCE((SELECT sort_order FROM channel_monitor_display_orders WHERE monitor_id = %s), 2147483647) ASC", s.C(channelmonitor.FieldID))))
	s.OrderBy(entsql.Desc(s.C(channelmonitor.FieldID)))
}

func (r *channelMonitorRepository) ListDisplayOrder(ctx context.Context) ([]service.ChannelMonitorOrderItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT m.id, m.name, m.provider, m.group_name, m.enabled
FROM channel_monitors m LEFT JOIN channel_monitor_display_orders o ON o.monitor_id = m.id
ORDER BY COALESCE(o.sort_order, 2147483647), m.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]service.ChannelMonitorOrderItem, 0)
	for rows.Next() {
		var item service.ChannelMonitorOrderItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Provider, &item.GroupName, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *channelMonitorRepository) UpdateDisplayOrder(ctx context.Context, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize order saves and hold creates/deletes until the complete list is checked.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE channel_monitors IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var total, matched int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE id = ANY($1)) FROM channel_monitors`, pq.Array(ids)).Scan(&total, &matched); err != nil {
		return err
	}
	if total != len(ids) || matched != len(ids) {
		return service.ErrChannelMonitorOrderConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_display_orders`); err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_display_orders (monitor_id, sort_order)
SELECT id, position FROM unnest($1::bigint[]) WITH ORDINALITY AS ordered(id, position)`, pq.Array(ids)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
