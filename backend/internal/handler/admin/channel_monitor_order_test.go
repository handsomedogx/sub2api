//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type channelMonitorOrderRepoStub struct {
	service.ChannelMonitorRepository
	ids []int64
	err error
}

func (r *channelMonitorOrderRepoStub) UpdateDisplayOrder(_ context.Context, ids []int64) error {
	r.ids = ids
	return r.err
}

func (r *channelMonitorOrderRepoStub) ListDisplayOrder(context.Context) ([]service.ChannelMonitorOrderItem, error) {
	return []service.ChannelMonitorOrderItem{{ID: 9, Name: "Paused", Provider: "openai", GroupName: "GPT", Enabled: false}}, nil
}

func TestChannelMonitorOrderListIsOnlyLabels(t *testing.T) {
	h := NewChannelMonitorHandler(service.NewChannelMonitorService(&channelMonitorOrderRepoStub{}, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/display-order", nil)
	h.ListDisplayOrder(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[{"id":9,"name":"Paused","provider":"openai","group_name":"GPT","enabled":false}]}`, w.Body.String())
}

func TestChannelMonitorOrderValidation(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"monitor_ids":[9,3]}`, http.StatusOK},
		{`{"monitor_ids":[]}`, http.StatusOK},
		{`{"monitor_ids":[3,3]}`, http.StatusBadRequest},
		{`{"monitor_ids":[0]}`, http.StatusBadRequest},
		{`{"monitor_ids":[-1]}`, http.StatusBadRequest},
		{`{"monitor_ids":null}`, http.StatusBadRequest},
		{`{}`, http.StatusBadRequest},
		{`{"monitor_ids":"3,9"}`, http.StatusBadRequest},
	} {
		t.Run(tc.body, func(t *testing.T) {
			repo := &channelMonitorOrderRepoStub{}
			h := NewChannelMonitorHandler(service.NewChannelMonitorService(repo, nil))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPut, "/display-order", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			h.UpdateDisplayOrder(c)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status != http.StatusOK {
				require.Nil(t, repo.ids)
			}
		})
	}
	repo := &channelMonitorOrderRepoStub{err: service.ErrChannelMonitorOrderConflict}
	h := NewChannelMonitorHandler(service.NewChannelMonitorService(repo, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/display-order", strings.NewReader(`{"monitor_ids":[9,3]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.UpdateDisplayOrder(c)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, []int64{9, 3}, repo.ids)
}
