-- 创建独立的访问者日志表
CREATE TABLE IF NOT EXISTS visitor_stats (
    id SERIAL PRIMARY KEY,
    ip_address INET NOT NULL,
    user_agent TEXT,
    metadata JSONB NOT NULL, -- 存储路径、引用页、语言等
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 为 JSONB 字段创建 GIN 索引，支持高效检索
CREATE INDEX IF NOT EXISTS idx_visitor_metadata ON visitor_stats USING GIN (metadata);
