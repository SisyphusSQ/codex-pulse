package sqlite

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	driverName                = "sqlite3"
	applicationDirectoryName  = "Codex Pulse"
	databaseFilename          = "codex-pulse.db"
	defaultWriteQueueCapacity = 128
	defaultMaxReadConnections = 4
)

const defaultBusyTimeout = 5 * time.Second

// Config controls the local SQLite connection pools and bounded writer queue.
// Zero values select the production defaults. When Path is explicit, an
// existing parent directory and database file must already be private; Open
// validates them without changing caller-owned permissions.
type Config struct {
	Path               string
	BusyTimeout        time.Duration
	WriteQueueCapacity int
	MaxReadConnections int
}

// DefaultPath returns the macOS Application Support database path.
func DefaultPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", newClassifiedError("resolve default path", ErrInvalidPath, err)
	}
	return filepath.Join(configDirectory, applicationDirectoryName, databaseFilename), nil
}

func normalizeConfig(config Config) (Config, error) {
	if config.BusyTimeout < 0 {
		return Config{}, newClassifiedError("normalize config", ErrInvalidConfig, fmt.Errorf("busy timeout must not be negative"))
	}
	if config.BusyTimeout == 0 {
		config.BusyTimeout = defaultBusyTimeout
	}
	if config.BusyTimeout < time.Millisecond || config.BusyTimeout%time.Millisecond != 0 {
		return Config{}, newClassifiedError("normalize config", ErrInvalidConfig, fmt.Errorf("busy timeout must be a positive whole number of milliseconds"))
	}
	if config.BusyTimeout > time.Duration(math.MaxInt32)*time.Millisecond {
		return Config{}, newClassifiedError("normalize config", ErrInvalidConfig, fmt.Errorf("busy timeout exceeds SQLite's signed 32-bit millisecond limit"))
	}
	if config.WriteQueueCapacity < 0 {
		return Config{}, newClassifiedError("normalize config", ErrInvalidConfig, fmt.Errorf("write queue capacity must not be negative"))
	}
	if config.WriteQueueCapacity == 0 {
		config.WriteQueueCapacity = defaultWriteQueueCapacity
	}
	if config.MaxReadConnections < 0 {
		return Config{}, newClassifiedError("normalize config", ErrInvalidConfig, fmt.Errorf("maximum read connections must not be negative"))
	}
	if config.MaxReadConnections == 0 {
		config.MaxReadConnections = defaultMaxReadConnections
	}

	path := config.Path
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, newClassifiedError("resolve database path", ErrInvalidPath, err)
	}
	config.Path = filepath.Clean(absolutePath)

	return config, nil
}

func prepareDatabasePath(path string, repairExistingPermissions bool) (bool, error) {
	dataDirectory := filepath.Dir(path)
	directoryInfo, err := os.Lstat(dataDirectory)
	directoryExisted := err == nil
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
			return false, newClassifiedError("create data directory", ErrInvalidPath, err)
		}
		directoryInfo, err = os.Lstat(dataDirectory)
	} else if err != nil {
		return false, newClassifiedError("inspect data directory", ErrInvalidPath, err)
	}
	if err != nil {
		return false, newClassifiedError("inspect data directory", ErrInvalidPath, err)
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 {
		return false, newClassifiedError("inspect data directory", ErrInvalidPath, fmt.Errorf("%s is a symbolic link", dataDirectory))
	}
	if !directoryInfo.IsDir() {
		return false, newClassifiedError("inspect data directory", ErrInvalidPath, fmt.Errorf("%s is not a directory", dataDirectory))
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		if directoryExisted && !repairExistingPermissions {
			return false, newClassifiedError("validate custom data directory permissions", ErrPermission, fmt.Errorf("mode is %#o, want 0700", directoryInfo.Mode().Perm()))
		}
		if err := os.Chmod(dataDirectory, 0o700); err != nil {
			return false, newClassifiedError("secure data directory", ErrPermission, err)
		}
		directoryInfo, err = os.Stat(dataDirectory)
		if err != nil {
			return false, newClassifiedError("verify data directory permissions", ErrPermission, err)
		}
		if directoryInfo.Mode().Perm() != 0o700 {
			return false, newClassifiedError("verify data directory permissions", ErrPermission, fmt.Errorf("mode is %#o, want 0700", directoryInfo.Mode().Perm()))
		}
	}

	databaseInfo, err := os.Lstat(path)
	switch {
	case err == nil && databaseInfo.Mode()&os.ModeSymlink != 0:
		return false, newClassifiedError("inspect database path", ErrInvalidPath, fmt.Errorf("%s is a symbolic link", path))
	case err == nil && !databaseInfo.Mode().IsRegular():
		return false, newClassifiedError("inspect database path", ErrInvalidPath, fmt.Errorf("%s is not a regular file", path))
	case err == nil && databaseInfo.Mode().Perm() != 0o600 && !repairExistingPermissions:
		return false, newClassifiedError("validate custom database permissions", ErrPermission, fmt.Errorf("mode is %#o, want 0600", databaseInfo.Mode().Perm()))
	case err != nil && !os.IsNotExist(err):
		return false, newClassifiedError("inspect database path", ErrInvalidPath, err)
	}

	return err == nil, nil
}

func secureDatabaseFile(path string, repairPermissions bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return newClassifiedError("inspect database file", ErrPermission, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return newClassifiedError("inspect database file", ErrInvalidPath, fmt.Errorf("%s is not a regular non-symlink file", path))
	}
	if info.Mode().Perm() != 0o600 {
		if !repairPermissions {
			return newClassifiedError("validate database file permissions", ErrPermission, fmt.Errorf("mode is %#o, want 0600", info.Mode().Perm()))
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return newClassifiedError("secure database file", ErrPermission, err)
		}
	}
	info, err = os.Lstat(path)
	if err != nil {
		return newClassifiedError("verify database file permissions", ErrPermission, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return newClassifiedError("verify database file permissions", ErrPermission, fmt.Errorf("mode is %v, want regular 0600", info.Mode()))
	}
	return nil
}

func writerDSN(config Config) string {
	values := url.Values{
		"cache":         {"private"},
		"mode":          {"rwc"},
		"_busy_timeout": {strconv.FormatInt(config.BusyTimeout.Milliseconds(), 10)},
		"_foreign_keys": {"on"},
		"_journal_mode": {"WAL"},
		"_synchronous":  {"NORMAL"},
		"_txlock":       {"immediate"},
	}
	return databaseURI(config.Path, values)
}

func readerDSN(config Config) string {
	values := url.Values{
		"cache":         {"private"},
		"mode":          {"ro"},
		"_busy_timeout": {strconv.FormatInt(config.BusyTimeout.Milliseconds(), 10)},
		"_foreign_keys": {"on"},
		"_query_only":   {"on"},
		"_synchronous":  {"NORMAL"},
	}
	return databaseURI(config.Path, values)
}

func databaseURI(path string, values url.Values) string {
	uri := url.URL{Scheme: "file", Path: path, RawQuery: values.Encode()}
	return uri.String()
}
