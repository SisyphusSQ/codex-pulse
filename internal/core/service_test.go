package core

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

// 测试 Service 只暴露 Go Helper 的业务方法，不继续携带 updater 平台职责。
func TestServiceExposesExactBusinessSurface(t *testing.T) {
	typeOfService := reflect.TypeFor[*Service]()
	got := make([]string, 0, typeOfService.NumMethod())
	for i := range typeOfService.NumMethod() {
		got = append(got, typeOfService.Method(i).Name)
	}
	sort.Strings(got)
	want := []string{
		"AnalyzeSessionIndexRepair", "ConfirmHomeSwitch", "Contracts", "DataHealth", "Health",
		"HealthProjection", "Job", "ListHealth", "ListJobs", "ListProjects", "ListSessions", "ListSources",
		"PlanHomeSwitch", "ProjectDetail", "QuotaCurrent", "RecoverHomeSwitch", "RequestQuotaRefresh",
		"RunRuntimeAction", "SessionDetail", "Settings", "Source", "UpdateSettings", "UsageCost",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Service methods = %v, want %v", got, want)
	}
}

// 测试 Contracts 的命令清单与方法 kind 一致，且不会向 client 发布重复能力。
func TestServiceContractsExposeUniqueCommandMethods(t *testing.T) {
	service, err := NewService(ServiceConfig{UsageCost: &usageQueryStub{}, RuntimeInfo: runtimeQueryStub{}})
	if err != nil {
		t.Fatal(err)
	}
	contract := service.Contracts()
	commandsFromMethods := make([]string, 0)
	for _, method := range contract.Methods {
		if method.Kind == MethodCommand {
			commandsFromMethods = append(commandsFromMethods, method.Name)
		}
	}
	sort.Strings(commandsFromMethods)
	commands := append([]string(nil), contract.CommandMethods...)
	sort.Strings(commands)
	if !reflect.DeepEqual(commands, commandsFromMethods) {
		t.Fatalf("CommandMethods = %v, want unique command methods %v", commands, commandsFromMethods)
	}
}

// 测试 Service 将 session 查询原样委托给现有业务 service。
func TestServiceDelegatesSessionQuery(t *testing.T) {
	usage := &usageQueryStub{sessions: usagecost.SessionListResponse{
		Meta: basequery.ResponseMeta{Version: usagecost.ContractVersion, Status: basequery.ResponsePartial},
	}}
	service, err := NewService(ServiceConfig{UsageCost: usage, RuntimeInfo: runtimeQueryStub{}})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	request := basequery.Request{Page: basequery.PageRequest{Limit: 17}}
	got, err := service.ListSessions(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if usage.request.Page.Limit != 17 || got.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("delegation request = %#v, response = %#v", usage.request, got)
	}
}

type usageQueryStub struct {
	request  basequery.Request
	sessions usagecost.SessionListResponse
}

func (*usageQueryStub) UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	return usagecost.UsageCostResponse{}, nil
}

func (stub *usageQueryStub) ListSessions(_ context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	stub.request = request
	return stub.sessions, nil
}

func (*usageQueryStub) SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	return usagecost.SessionDetailResponse{}, nil
}

func (*usageQueryStub) ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error) {
	return usagecost.ProjectListResponse{}, nil
}

func (*usageQueryStub) ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	return usagecost.ProjectDetailResponse{}, nil
}

type runtimeQueryStub struct{}

func (runtimeQueryStub) QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error) {
	return runtimeinfo.QuotaCurrentResponse{}, nil
}

func (runtimeQueryStub) ListSources(context.Context, basequery.Request) (runtimeinfo.SourceListResponse, error) {
	return runtimeinfo.SourceListResponse{}, nil
}

func (runtimeQueryStub) Source(context.Context, runtimeinfo.SourceDetailRequest) (runtimeinfo.SourceDetailResponse, error) {
	return runtimeinfo.SourceDetailResponse{}, nil
}

func (runtimeQueryStub) ListJobs(context.Context, basequery.Request) (runtimeinfo.JobListResponse, error) {
	return runtimeinfo.JobListResponse{}, nil
}

func (runtimeQueryStub) Job(context.Context, runtimeinfo.JobDetailRequest) (runtimeinfo.JobDetailResponse, error) {
	return runtimeinfo.JobDetailResponse{}, nil
}

func (runtimeQueryStub) ListHealth(context.Context, basequery.Request) (runtimeinfo.HealthListResponse, error) {
	return runtimeinfo.HealthListResponse{}, nil
}

func (runtimeQueryStub) Health(context.Context, runtimeinfo.HealthDetailRequest) (runtimeinfo.HealthDetailResponse, error) {
	return runtimeinfo.HealthDetailResponse{}, nil
}

func (runtimeQueryStub) DataHealth(context.Context, int64) (runtimeinfo.DataHealthResponse, error) {
	return runtimeinfo.DataHealthResponse{}, nil
}

func (runtimeQueryStub) Settings(context.Context) (runtimeinfo.SettingsResponse, error) {
	return runtimeinfo.SettingsResponse{}, nil
}
