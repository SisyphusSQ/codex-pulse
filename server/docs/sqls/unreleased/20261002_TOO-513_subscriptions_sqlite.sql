-- 用途：中心v1升v2新增订阅设置；仅显式db upgrade。
-- 前置：对应v1摘要和结构完整且已备份；不删除数据。MySQL DDL隐式提交，失败检查结构后重入。
-- 表创建后由升级程序检查全部结构，再CAS更新pulse_schema版本与摘要。

-- 中心手动账号订阅设置，不修改设备事实。
CREATE TABLE IF NOT EXISTS pulse_account_settings (
    account_key TEXT NOT NULL,
    revision INTEGER NOT NULL,
    alias TEXT,
    manual_plan TEXT,
    date_kind TEXT NOT NULL,
    renewal_day INTEGER,
    membership_date TEXT,
    time_zone TEXT NOT NULL,
    updated_at_ms INTEGER NOT NULL,
PRIMARY KEY (account_key),
CHECK (revision > 0),
CHECK ((date_kind = '' AND renewal_day IS NULL AND membership_date IS NULL) OR (date_kind = 'monthly_renewal' AND renewal_day IS NOT NULL AND renewal_day BETWEEN 1 AND 31 AND membership_date IS NULL) OR (date_kind = 'membership_expiry' AND renewal_day IS NULL AND membership_date IS NOT NULL))
);
