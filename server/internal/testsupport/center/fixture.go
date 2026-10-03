// Package center 仅用于隔离测试夹具，不被生产装配导入。
package center

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"uuid"

	"go.uber.org/fx"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/reporting_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
)

func Engine(t *testing.T) *gormv2.Engine {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture source path unavailable")
	}
	cfg, err := config.Load(filepath.Join(filepath.Dir(file), "../../../config/config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Path = filepath.Join(t.TempDir(), "private", "center.sqlite")
	var engine *gormv2.Engine
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(gormv2.New), fx.Populate(&engine))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := schema_repo.NewSchema(engine).Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	return engine
}

type Access interface {
	Bootstrap(context.Context) (access_vo.PairingView, error)
	Pair(context.Context, string, string, string) (access_dto.PairedClient, error)
	Issue(context.Context, access_dto.Principal, string, string) (access_vo.PairingView, error)
}

func Admin(t *testing.T, s Access) access_dto.PairedClient {
	t.Helper()
	code, err := s.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	paired, err := s.Pair(t.Context(), code.Code, "browser", "https://pulse.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return paired
}
func Collector(t *testing.T, s Access, admin access_dto.Principal, name string) access_dto.Principal {
	t.Helper()
	code, err := s.Issue(t.Context(), admin, access_dto.PurposeCollector, name)
	if err != nil {
		t.Fatal(err)
	}
	paired, err := s.Pair(t.Context(), code.Code, "collector", "")
	if err != nil {
		t.Fatal(err)
	}
	return paired.Principal
}
func Contribution(input, at int64) reportingv1.Contribution {
	c := reportingv1.Contribution{ObservedAtMS: &at, InputTokens: &input, CachedTokens: new(int64(0)), OutputTokens: new(int64(0)), ReasoningTokens: new(int64(0)), TotalTokens: &input, CostStatus: "unpriced"}
	c.ID = reportingv1.ContributionID("codex", "session", c, 0)
	return c
}
func Snapshot() reportingv1.SessionSnapshot {
	return reportingv1.SessionSnapshot{Provider: "codex", HomeID: "home", SessionID: "session", Revision: 1, CollectedAtMS: 3000, Title: "会话标题", ProjectID: "project", ProjectName: "同名项目", SourceKind: "light_index", SessionKind: "session", Complete: true, Contributions: []reportingv1.Contribution{Contribution(100, 1000)}}
}

type Reporting interface {
	Accept(context.Context, access_dto.Principal, reportingv1.Batch) (reporting_vo.ReportingReceiptView, error)
}

func SendSnapshot(t *testing.T, s Reporting, p access_dto.Principal, snap reportingv1.SessionSnapshot) {
	t.Helper()
	if _, err := s.Accept(t.Context(), p, reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{snap}}); err != nil {
		t.Fatal(err)
	}
}
