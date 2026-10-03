package http_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	nativeapp "github.com/SisyphusSQ/codex-pulse/internal/app"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/helper"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	access_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
)

func TestNativeCoreReportingPairUploadRevokeAndClose(t *testing.T) {
	// 真实 CoreService / UDS / HTTP / SQLite，仅空 synthetic Home，绝不探测个人 Agent 数据。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "missing-codex"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	server, access := testServer(t, origin)
	var uploads atomic.Int64
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/batches" {
			uploads.Add(1)
		}
		server.Echo.ServeHTTP(w, r)
	}))
	_ = httpServer.Listener.Close()
	httpServer.Listener = listener
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	browser := admin(t, access, origin)
	issued := request(server, "POST", origin, "/api/v1/pairings", `{"purpose":"collector","name":"合成 CoreService 设备"}`, &browser, true)
	if issued.Code != 200 {
		t.Fatal("admin issue", issued.Code)
	}
	var envelope struct {
		Data access_vo.PairingView `json:"data"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}

	private, err := os.MkdirTemp("", "cp-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(private) })
	broker, err := core.NewInvalidationBroker(16)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := nativeapp.Open(t.Context(), nativeapp.Config{Broker: broker, Store: storesqlite.Config{Path: filepath.Join(private, "app.db")}, PreferencesPath: filepath.Join(private, "preferences.json"), DefaultCodexHome: filepath.Join(home, "missing-codex"), HelperVersion: "native-e2e"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	auth, err := helper.NewAuthenticator([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	rpcServer, err := helper.NewGRPCServer(helper.ServerConfig{Authenticator: auth, HelperVersion: "native-e2e", Service: runtime.Service(), Broker: broker})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(private, "core.sock")
	uds, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	go func() { _ = rpcServer.Serve(uds) }()
	t.Cleanup(func() { rpcServer.Stop(); _ = uds.Close() })
	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := corev1.NewCoreServiceClient(conn)
	rpcContext, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	ctx := metadata.NewOutgoingContext(rpcContext, metadata.Pairs("authorization", "Bearer "+token))
	paired, err := client.PairReporting(ctx, &corev1.PairReportingRequest{Endpoint: origin, AllowHttp: true, Code: envelope.Data.Code})
	if err != nil {
		t.Fatal("pair RPC", err)
	}
	if paired.Enabled || paired.ClientId == "" || paired.Endpoint != origin {
		t.Fatal("pair must stay disabled")
	}
	serialized, _ := json.Marshal(paired)
	if strings.Contains(string(serialized), envelope.Data.Code) || strings.Contains(string(serialized), "credential") {
		t.Fatal("pair RPC leaked private material")
	}
	if _, err := client.ConfigureReporting(ctx, &corev1.ConfigureReportingRequest{Enabled: true, IntervalSeconds: 15}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncReportingNow(ctx, &corev1.Empty{}); err != nil {
		t.Fatal(err)
	}
	waitForStatus := func(condition func(*corev1.ReportingStatusResponse) bool) *corev1.ReportingStatusResponse {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			value, err := client.ReportingStatus(ctx, &corev1.Empty{})
			if err != nil {
				t.Fatal(err)
			}
			if condition(value) {
				return value
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("reporting state deadline")
		return nil
	}
	synced := waitForStatus(func(value *corev1.ReportingStatusResponse) bool { return value.LastSuccessAtMs != nil })
	if !synced.Enabled || uploads.Load() == 0 {
		t.Fatal("no real HTTP receipt")
	}
	clients, err := access.Clients(t.Context(), browser.Principal)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := false
	for _, value := range clients.Clients {
		if value.ID == paired.ClientId {
			confirmed = value.LastReceivedAtMS != nil
		}
	}
	if !confirmed {
		t.Fatal("center did not record collector confirmation")
	}
	if got := request(server, "POST", origin, "/api/v1/clients/"+paired.ClientId+"/revoke", "{}", &browser, true); got.Code != 200 {
		t.Fatal(got.Code)
	}
	if _, err := client.SyncReportingNow(ctx, &corev1.Empty{}); err != nil {
		t.Fatal(err)
	}
	after := waitForStatus(func(value *corev1.ReportingStatusResponse) bool { return value.State == "reconnect_required" })
	if after.PendingBatches == 0 {
		t.Fatal("revocation must preserve unconfirmed queue")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	count := uploads.Load()
	time.Sleep(100 * time.Millisecond)
	if uploads.Load() != count {
		t.Fatal("closed application continued uploads")
	}
}
