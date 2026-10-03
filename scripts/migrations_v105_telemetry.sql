-- WarLink v1.0.5 Telemetry and Multi-Server Observability Migration
-- Applied to: warlink_db on Stockholm Core (138.124.103.99)
-- Database user: warlink

-- 1. Create table for automated client telemetry beacons
CREATE TABLE IF NOT EXISTS routing_telemetry_auto (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    account_number VARCHAR(120),
    device_id VARCHAR(120),
    app_version VARCHAR(32),
    route_mode VARCHAR(64) DEFAULT 'transit',
    ping_moscow_ms INT DEFAULT 0,
    ping_stockholm_ms INT DEFAULT 0,
    in_game_ping_ms INT DEFAULT 0,
    jitter_ms DOUBLE PRECISION DEFAULT 0,
    packet_loss DOUBLE PRECISION DEFAULT 0,
    game_name VARCHAR(120) DEFAULT 'wardogs',
    client_ip VARCHAR(64),
    country VARCHAR(120),
    city VARCHAR(120),
    isp VARCHAR(160),
    telemetry_data JSONB DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_rta_created ON routing_telemetry_auto(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rta_route ON routing_telemetry_auto(route_mode);
CREATE INDEX IF NOT EXISTS idx_rta_dev ON routing_telemetry_auto(device_id);

-- 2. Extend user connection history with server and route telemetry
ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS route_mode VARCHAR(64) DEFAULT 'transit';
ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS connected_node VARCHAR(120) DEFAULT 'Стокгольм Core (Прямой)';
ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS gateway_ip VARCHAR(64) DEFAULT '138.124.103.99';
CREATE INDEX IF NOT EXISTS idx_uch_route_mode ON user_connection_history(route_mode);

-- 3. Extend GeoIP cache with ISP provider resolution
ALTER TABLE ip_geo_cache ADD COLUMN IF NOT EXISTS isp VARCHAR(160) DEFAULT '';

-- 4. Grant table and sequence permissions to service user
GRANT ALL PRIVILEGES ON TABLE routing_telemetry_auto TO warlink;
GRANT USAGE, SELECT ON SEQUENCE routing_telemetry_auto_id_seq TO warlink;
GRANT ALL PRIVILEGES ON TABLE user_connection_history TO warlink;
GRANT ALL PRIVILEGES ON TABLE ip_geo_cache TO warlink;
