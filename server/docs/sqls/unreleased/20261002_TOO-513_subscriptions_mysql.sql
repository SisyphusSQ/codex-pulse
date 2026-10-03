-- 用途：中心v1升v2新增订阅设置；仅显式db upgrade。
-- 前置：对应v1摘要和结构完整且已备份；不删除数据。MySQL DDL隐式提交，失败检查结构后重入。
-- 表创建后由升级程序检查全部结构，再CAS更新pulse_schema版本与摘要。

-- 中心手动账号订阅设置；与设备采集事实分离，不按邮箱归并。
CREATE TABLE IF NOT EXISTS pulse_account_settings (
    account_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT '确认账号中心键',
    revision BIGINT NOT NULL COMMENT '乐观修订号',
    alias VARCHAR(128) NULL COMMENT '手动账号备注',
    manual_plan VARCHAR(64) NULL COMMENT '手动套餐覆盖',
    date_kind VARCHAR(32) NOT NULL COMMENT '月续费或完整到期日；空为未设置',
    renewal_day INT NULL COMMENT '每月续费日1至31',
    membership_date VARCHAR(10) NULL COMMENT '完整会员到期日YYYY-MM-DD',
    time_zone VARCHAR(64) NOT NULL COMMENT 'IANA日期计算时区',
    updated_at_ms BIGINT NOT NULL COMMENT '中心修改UTC毫秒',
PRIMARY KEY (account_key),
CONSTRAINT chk_settings_revision CHECK (revision > 0),
CONSTRAINT chk_settings_date CHECK ((date_kind = '' AND renewal_day IS NULL AND membership_date IS NULL) OR (date_kind = 'monthly_renewal' AND renewal_day IS NOT NULL AND renewal_day BETWEEN 1 AND 31 AND membership_date IS NULL) OR (date_kind = 'membership_expiry' AND renewal_day IS NULL AND membership_date IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='中心手动订阅设置';
