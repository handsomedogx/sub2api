-- V1 display order is independent of the probe configuration and scheduler.
CREATE TABLE IF NOT EXISTS channel_monitor_display_orders (
    monitor_id BIGINT PRIMARY KEY REFERENCES channel_monitors(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL CHECK (sort_order > 0)
);
