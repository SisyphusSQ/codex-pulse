package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
)

type (
	Config struct {
		Debug          bool          `mapstructure:"debug"`
		ContextTimeout time.Duration `mapstructure:"contextTimeout"`
		Server         Server        `mapstructure:"server"`
		Database       Database      `mapstructure:"database"`
		Log            Log           `mapstructure:"log"`
	}

	Server struct {
		Metrics           bool          `mapstructure:"metrics"`
		ReadinessTimeout  time.Duration `mapstructure:"readinessTimeout"`
		Address           string        `mapstructure:"address"`
		ReadHeaderTimeout time.Duration `mapstructure:"readHeaderTimeout"`
		ReadTimeout       time.Duration `mapstructure:"readTimeout"`
		WriteTimeout      time.Duration `mapstructure:"writeTimeout"`
		IdleTimeout       time.Duration `mapstructure:"idleTimeout"`
		ShutdownTimeout   time.Duration `mapstructure:"shutdownTimeout"`
		MaxBodyBytes      int64         `mapstructure:"maxBodyBytes"`
		CORSOrigins       []string      `mapstructure:"corsOrigins"`
		Origins           []string      `mapstructure:"origins"`
		AllowHTTP         bool          `mapstructure:"allowHTTP"`
		TrustedProxies    []string      `mapstructure:"trustedProxies"`
		WebDirectory      string        `mapstructure:"webDirectory"`
	}
	Database struct {
		Enabled         bool          `mapstructure:"enabled"`
		Driver          string        `mapstructure:"driver"`
		Host            string        `mapstructure:"host"`
		Port            int           `mapstructure:"port"`
		User            string        `mapstructure:"username"`
		Password        string        `mapstructure:"password"`
		Database        string        `mapstructure:"database"`
		MaxIdleConns    int           `mapstructure:"maxIdleConns"`
		MaxOpenConns    int           `mapstructure:"maxOpenConns"`
		ConnMaxLifetime time.Duration `mapstructure:"connMaxLifetime"`
		Charset         string        `mapstructure:"charset"`
		TimeZone        string        `mapstructure:"timeZone"`
		Name            string        `mapstructure:"name"`
		Path            string        `mapstructure:"path"`
		TLS             string        `mapstructure:"tls"`
	}

	Log struct {
		Output         string        `mapstructure:"output"`
		FileName       string        `mapstructure:"fileName"`
		LogLevel       zapcore.Level `mapstructure:"logLevel"`
		MaxSizeMb      int           `mapstructure:"maxSizeMB"`
		MaxBackupCount int           `mapstructure:"maxBackupCount"`
		MaxKeepDays    int           `mapstructure:"maxKeepDays"`
	}

	Cron struct {
		On bool `mapstructure:"on"`
	}

	Redis struct {
		Enabled    bool `mapstructure:"enabled"`
		PoolConfig `mapstructure:"pool"`

		Name         string        `mapstructure:"name"`
		Proto        string        `mapstructure:"proto"`
		Addr         string        `mapstructure:"addr"`
		Auth         string        `mapstructure:"auth"`
		DialTimeout  time.Duration `mapstructure:"dialTimeout"`
		ReadTimeout  time.Duration `mapstructure:"readTimeout"`
		WriteTimeout time.Duration `mapstructure:"writeTimeout"`
		DB           int           `mapstructure:"db"`
		SlowLog      time.Duration `mapstructure:"slowLog"`
	}

	PoolConfig struct {
		Active      int           `mapstructure:"active"`
		Idle        int           `mapstructure:"idle"`
		WaitTimeout time.Duration `mapstructure:"waitTimeout"`
		Wait        bool          `mapstructure:"wait"`
	}

	HTTPClient struct {
		Enabled bool          `mapstructure:"enabled"`
		URL     string        `mapstructure:"url"`
		Token   string        `mapstructure:"token"`
		Timeout time.Duration `mapstructure:"timeout"`
	}

	Lark struct {
		Enabled   bool          `mapstructure:"enabled"`
		AppID     string        `mapstructure:"appID"`
		AppSecret string        `mapstructure:"appSecret"`
		Timeout   time.Duration `mapstructure:"timeout"`
	}
)

func Load(file string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(file)
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := bindEnvironment(v); err != nil {
		return Config{}, fmt.Errorf("bind environment: %w", err)
	}
	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", file, err)
	}

	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", file, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", file, err)
	}
	return cfg, nil
}

func bindEnvironment(v *viper.Viper) error {
	keys := []string{
		"debug",
		"contextTimeout",
		"server.address",
		"server.metrics",
		"server.readinessTimeout",
		"server.readHeaderTimeout",
		"server.readTimeout",
		"server.writeTimeout",
		"server.idleTimeout",
		"server.shutdownTimeout",
		"server.maxBodyBytes",
		"server.corsOrigins",
		"server.origins",
		"server.allowHTTP",
		"server.trustedProxies",
		"server.webDirectory",
		"log.output",
		"log.fileName",
		"log.logLevel",
		"log.maxSizeMB",
		"log.maxBackupCount",
		"log.maxKeepDays",
		"database.enabled",
		"database.driver",
		"database.host",
		"database.port",
		"database.username",
		"database.password",
		"database.database",
		"database.maxIdleConns",
		"database.maxOpenConns",
		"database.connMaxLifetime",
		"database.charset",
		"database.timeZone",
		"database.name",
		"database.path",
		"database.tls",
	}
	for _, key := range keys {
		if err := v.BindEnv(key); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Server.Address); err != nil {
		return fmt.Errorf("server.address must be host:port")
	}
	if c.ContextTimeout <= 0 || c.Server.ReadinessTimeout <= 0 {
		return fmt.Errorf("context and readiness timeouts must be positive")
	}
	if c.Server.ReadHeaderTimeout <= 0 || c.Server.ReadTimeout <= 0 || c.Server.WriteTimeout <= 0 || c.Server.IdleTimeout <= 0 || c.Server.ShutdownTimeout <= 0 {
		return fmt.Errorf("server timeouts must be positive")
	}
	if c.Server.MaxBodyBytes <= 0 {
		return fmt.Errorf("server.maxBodyBytes must be positive")
	}
	for _, origin := range c.Server.CORSOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(u.Host, "*?") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("server.corsOrigins must contain exact HTTP(S) origins")
		}
	}
	if c.Log.Output != "stdout" && c.Log.Output != "file" && c.Log.Output != "both" {
		return fmt.Errorf("log.output must be stdout,file,both")
	}
	if c.Log.Output != "stdout" && (c.Log.FileName == "" || c.Log.MaxSizeMb <= 0 || c.Log.MaxBackupCount <= 0 || c.Log.MaxKeepDays <= 0) {
		return fmt.Errorf("file logging requires a path and positive rotation limits")
	}

	if c.Database.Enabled {
		if c.Database.MaxOpenConns <= 0 || c.Database.MaxIdleConns < 0 || c.Database.MaxIdleConns > c.Database.MaxOpenConns || c.Database.ConnMaxLifetime <= 0 {
			return fmt.Errorf("invalid enabled database configuration")
		}
		switch c.Database.Driver {
		case "sqlite":
			if strings.TrimSpace(c.Database.Path) == "" {
				return fmt.Errorf("sqlite requires database.path")
			}
		case "mysql":
			if c.Database.Host == "" || c.Database.User == "" || c.Database.Database == "" || c.Database.Port <= 0 || c.Database.Port > 65535 {
				return fmt.Errorf("mysql requires connection settings")
			}
			if c.Database.TLS != "true" && c.Database.TLS != "false" {
				return fmt.Errorf("mysql database.tls must explicitly be true or false")
			}
		default:
			return fmt.Errorf("unsupported database driver")
		}
	}

	if len(c.Server.Origins) == 0 {
		return fmt.Errorf("server.origins requires explicit entry origins")
	}
	for _, origin := range c.Server.Origins {
		u, err := url.Parse(origin)
		if err != nil || len(origin) > 255 || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(u.Host, "*?,\\") {
			return fmt.Errorf("server.origins requires exact HTTP(S) origins")
		}
		if u.Scheme == "http" && !c.Server.AllowHTTP {
			return fmt.Errorf("HTTP origin requires explicit server.allowHTTP")
		}
	}
	if c.Server.AllowHTTP {
		host, _, _ := net.SplitHostPort(c.Server.Address)
		ip := net.ParseIP(host)
		_, tailnet, _ := net.ParseCIDR("100.64.0.0/10")
		if ip == nil || !(ip.IsLoopback() || ip.IsPrivate() || tailnet.Contains(ip)) {
			return fmt.Errorf("private HTTP requires a concrete loopback, LAN or Tailnet listen address")
		}
	}
	for _, proxy := range c.Server.TrustedProxies {
		if _, _, err := net.ParseCIDR(proxy); err != nil {
			return fmt.Errorf("trusted proxy requires an explicit CIDR")
		}
	}

	return nil
}
