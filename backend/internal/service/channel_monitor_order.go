package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrChannelMonitorOrderConflict = infraerrors.Conflict("CHANNEL_MONITOR_ORDER_CONFLICT", "Monitor list changed; reopen sorting and try again")

// ChannelMonitorOrderItem contains only the labels needed to arrange V1 cards.
type ChannelMonitorOrderItem struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	GroupName string `json:"group_name"`
	Enabled   bool   `json:"enabled"`
}

func (s *ChannelMonitorService) ListDisplayOrder(ctx context.Context) ([]ChannelMonitorOrderItem, error) {
	return s.repo.ListDisplayOrder(ctx)
}

func (s *ChannelMonitorService) UpdateDisplayOrder(ctx context.Context, ids []int64) error {
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return infraerrors.BadRequest("INVALID_CHANNEL_MONITOR_ORDER", "Monitor IDs must be positive and unique")
		}
		seen[id] = true
	}
	return s.repo.UpdateDisplayOrder(ctx, ids)
}
