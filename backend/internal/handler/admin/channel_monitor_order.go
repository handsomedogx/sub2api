package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *ChannelMonitorHandler) ListDisplayOrder(c *gin.Context) {
	items, err := h.monitorService.ListDisplayOrder(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *ChannelMonitorHandler) UpdateDisplayOrder(c *gin.Context) {
	var req struct {
		MonitorIDs *[]int64 `json:"monitor_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "monitor_ids is required")
		return
	}
	if err := h.monitorService.UpdateDisplayOrder(c.Request.Context(), *req.MonitorIDs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"saved": true})
}
