package repository

import (
	"context"
	"time"

	"github.com/lib/pq"
)

// MonitorUsageCacheRates uses the same exclusive prompt buckets as usage records.
// Weight by tokens, never by requests; output tokens do not enter the denominator.
func (r *channelMonitorRepository) MonitorUsageCacheRates(ctx context.Context, ids []int64, end time.Time) (map[int64]float64, error) {
	out := map[int64]float64{}
	if len(ids) == 0 {
		return out, nil
	}
	const query = `WITH targets AS (
 SELECT id, group_id FROM channel_monitors WHERE id = ANY($1) AND group_id IS NOT NULL
), totals AS (
 SELECT group_id, SUM(cache_read_tokens)::float8 AS hits,
 SUM(input_tokens::bigint + cache_read_tokens::bigint + cache_creation_tokens::bigint)::float8 AS prompt
 FROM usage_logs
 WHERE created_at >= $2 AND created_at < $3
 AND group_id IN (SELECT group_id FROM targets)
 GROUP BY group_id
)
SELECT t.id, u.hits / u.prompt FROM targets t JOIN totals u USING (group_id) WHERE u.prompt > 0`
	rows, err := r.db.QueryContext(ctx, query, pq.Array(ids), end.Add(-24*time.Hour), end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var rate float64
		if err := rows.Scan(&id, &rate); err != nil {
			return nil, err
		}
		if rate >= 0 && rate <= 1 {
			out[id] = rate
		}
	}
	return out, rows.Err()
}
