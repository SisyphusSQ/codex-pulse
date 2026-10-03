package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/schema_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
	"github.com/SisyphusSQ/codex-pulse/server/vars"
)

func backupCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []string{"backup", "restore"} {
		var timeout time.Duration
		command := &cobra.Command{Use: action + " DIRECTORY", Args: cobra.ExactArgs(1), Short: "SQLite 私有备份；恢复到新路径并撤销旧授权", RunE: func(cmd *cobra.Command, args []string) error {
			if timeout < 5*time.Second || timeout > 10*time.Minute {
				return fmt.Errorf("timeout must be between 5s and 10m")
			}
			cfg, err := config.Load(configure)
			if err != nil {
				return err
			}
			if !cfg.Database.Enabled || cfg.Database.Driver != "sqlite" {
				return fmt.Errorf("use SQLite config; MySQL has a separate documented entry")
			}
			cfg.ContextTimeout = timeout
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			if action == "backup" {
				err = backupSQLite(ctx, cfg, args[0])
			} else {
				err = restoreSQLite(ctx, cfg, args[0])
			}
			if err == nil {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "completed; restore requires new administrator and device pairing")
			}
			return err
		}}
		command.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "overall deadline (5s..10m)")
		commands = append(commands, command)
	}
	return commands
}

func backupSQLite(ctx context.Context, cfg config.Config, directory string) (err error) {
	if _, err = fileDigest(ctx, cfg.Database.Path); err != nil {
		return err
	}
	if err = os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("backup needs a new directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(directory)
		}
	}()
	destination := filepath.Join(directory, "center.sqlite")
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = withConfiguredDatabase(ctx, cfg, func(ctx context.Context, e *gormv2.Engine) error {
		return schema_repo.NewSchema(e).Snapshot(ctx, destination)
	}); err != nil {
		return err
	}
	sum, err := fileDigest(ctx, destination)
	if err != nil {
		return err
	}
	manifest, err := json.Marshal(schema_dto.BackupManifest{Format: 1, Driver: "sqlite", Schema: schema.Version, SHA256: sum, CreatedAtMS: time.Now().UnixMilli(), ServerVersion: vars.AppVersion})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "manifest.json"), manifest, 0600); err != nil {
		return err
	}
	complete = true
	return nil
}

func restoreSQLite(ctx context.Context, cfg config.Config, directory string) error {
	content, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest schema_dto.BackupManifest
	if len(content) > 4096 || json.Unmarshal(content, &manifest) != nil || manifest.Format != 1 || manifest.Driver != "sqlite" || manifest.Schema != schema.Version {
		return fmt.Errorf("incompatible backup manifest")
	}
	source := filepath.Join(directory, "center.sqlite")
	sum, err := fileDigest(ctx, source)
	if err != nil {
		return err
	}
	if sum != manifest.SHA256 {
		return fmt.Errorf("backup checksum mismatch")
	}
	destination, err := filepath.Abs(cfg.Database.Path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return fmt.Errorf("restore requires a missing destination; never overwrite a live database")
	}
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stat, err := os.Stat(parent)
	if err != nil || stat.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("restore directory must be private (0700)")
	}
	stage, err := os.CreateTemp(parent, ".restore-*.sqlite")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath); _ = os.Remove(stagePath + "-wal"); _ = os.Remove(stagePath + "-shm") }()
	if err = copyBackup(ctx, source, stage); err != nil {
		_ = stage.Close()
		return err
	}
	if err = stage.Close(); err != nil {
		return err
	}
	copiedSum, err := fileDigest(ctx, stagePath)
	if err != nil {
		return err
	}
	if copiedSum != manifest.SHA256 {
		return fmt.Errorf("copied backup checksum mismatch")
	}
	cfg.Database.Path = stagePath
	if err = withConfiguredDatabase(ctx, cfg, func(ctx context.Context, e *gormv2.Engine) error {
		s := schema_repo.NewSchema(e)
		if err := s.Integrity(ctx); err != nil {
			return err
		}
		if err := s.InvalidateRestoredAccess(ctx); err != nil {
			return err
		}
		return s.Checkpoint(ctx)
	}); err != nil {
		return err
	}
	// 同目录硬链接原子发布并拒绝覆盖；数据库已关闭后移除临时文件。
	return os.Link(stagePath, destination)
}

func fileDigest(ctx context.Context, path string) (string, error) {
	stat, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("backup file must be private regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if err := copyContext(ctx, h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyBackup(ctx context.Context, source string, destination *os.File) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = copyContext(ctx, destination, f); err != nil {
		return err
	}
	return destination.Sync()
}
func copyContext(ctx context.Context, destination io.Writer, source io.Reader) error {
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := source.Read(buffer)
		if n > 0 {
			written, writeErr := destination.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
