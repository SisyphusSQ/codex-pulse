package gormv2

import (
	"fmt"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

const (
	defaultCharset  = "utf8mb4"
	defaultTimeZone = "Local"
)

func normalizeConfig(conf config.Database) (config.Database, error) {
	if conf.Driver == "" {
		conf.Driver = "mysql"
	}
	if conf.Driver == "sqlite" {
		if strings.TrimSpace(conf.Path) == "" {
			return config.Database{}, fmt.Errorf("sqlite requires database.path")
		}
		return conf, nil
	}
	if conf.Driver != "mysql" {
		return config.Database{}, fmt.Errorf("unsupported database driver %q", conf.Driver)
	}
	if strings.TrimSpace(conf.User) == "" || strings.TrimSpace(conf.Host) == "" || strings.TrimSpace(conf.Database) == "" {
		return config.Database{}, fmt.Errorf("database username, host and database are required")
	}
	if conf.Charset == "" {
		conf.Charset = defaultCharset
	}
	if conf.TimeZone == "" {
		conf.TimeZone = defaultTimeZone
	}
	return conf, nil
}
