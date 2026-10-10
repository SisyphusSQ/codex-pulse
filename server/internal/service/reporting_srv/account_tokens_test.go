package reporting_srv

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

const accountTokensNow int64 = 1_790_000_000_000

func tokenBatch(count int, account string) reportingv1.Batch {
	u := reportingv1.AccountTokenUsage{
		Provider: "codex", AccountID: account, LocalScope: "scope-" + account,
		WindowStartAtMS: accountTokensNow - 10080*60000 + 3600000,
		ResetsAtMS:      accountTokensNow + 3600000, CollectedAtMS: accountTokensNow,
		Facts: []reportingv1.AccountTokenFact{},
	}
	b := reportingv1.Batch{
		Version: 1, ID: uuid.New().String(),
		Bindings:     []reportingv1.AccountBinding{{Provider: "codex", LocalScope: u.LocalScope, AccountID: account, ConfirmedAtMS: accountTokensNow}},
		AccountUsage: []reportingv1.AccountTokenUsage{u},
	}
	for start := 0; start < count; start += 367 {
		packet := u
		packet.Facts = make([]reportingv1.AccountTokenFact, 0, 367)
		for i := start; i < min(start+367, count); i++ {
			f := reportingv1.AccountTokenFact{SessionID: fmt.Sprintf("session-%d", i/367), ObservedAtMS: accountTokensNow - 10000 + int64(i), InputTokens: 10, TotalTokens: 10}
			f.ID = reportingv1.ContributionID("codex", f.SessionID, f.Contribution(), 0)
			packet.Facts = append(packet.Facts, f)
		}
		b.AccountUsage = append(b.AccountUsage, packet)
	}
	return b
}

func tokenCounts(t *testing.T, db *gorm.DB, facts, sources, periods, batches int64) {
	t.Helper()
	for _, check := range []struct {
		model any
		want  int64
	}{{&reporting_do.AccountTokenFact{}, facts}, {&reporting_do.AccountTokenSource{}, sources}, {&reporting_do.AccountTokenPeriod{}, periods}, {&reporting_do.Batch{}, batches}} {
		var got int64
		if err := db.Model(check.model).Count(&got).Error; err != nil || got != check.want {
			t.Fatalf("%T count = %d, want %d: %v", check.model, got, check.want, err)
		}
	}
}

func TestAccountTokensLargeBatchBoundedQueriesReplayAndCopies(t *testing.T) {
	s, db, clients := reportingFixture(t)
	s.now = func() time.Time { return time.UnixMilli(accountTokensNow) }
	b := tokenBatch(5132, "a")
	queries := 0
	// 数据库往返即使只有 5ms，逐事实执行也会耗尽预算；分块处理应保持固定查询规模。
	latency := func(tx *gorm.DB) {
		queries++
		select {
		case <-time.After(5 * time.Millisecond):
		case <-tx.Statement.Context.Done():
			_ = tx.AddError(tx.Statement.Context.Err())
		}
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:latency", latency); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:latency", latency); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, err := s.Accept(ctx, clients[0], b)
	if err != nil {
		t.Fatal("large batch did not fit request budget:", err)
	}
	if queries > 40 {
		t.Fatalf("large batch used %d queries; expected at most 40", queries)
	}
	t.Logf("5132 facts accepted with %d create/read operations", queries)
	tokenCounts(t, db, 5132, 5132, 1, 1)
	s.now = func() time.Time { return time.UnixMilli(accountTokensNow + 1000) }
	replay, err := s.Accept(t.Context(), clients[0], b)
	if err != nil || replay != first {
		t.Fatal("replay changed receipt:", err)
	}
	b.ID = uuid.New().String()
	if _, err := s.Accept(t.Context(), clients[1], b); err != nil {
		t.Fatal("device copy failed:", err)
	}
	tokenCounts(t, db, 5132, 10264, 2, 2)
}

func TestAccountTokensLateConflictRollsBackAllChunks(t *testing.T) {
	s, db, clients := reportingFixture(t)
	s.now = func() time.Time { return time.UnixMilli(accountTokensNow) }
	b := tokenBatch(2132, "a")
	var facts []reportingv1.AccountTokenFact
	for _, u := range b.AccountUsage {
		facts = append(facts, u.Facts...)
	}
	slices.SortFunc(facts, func(a, b reportingv1.AccountTokenFact) int { return strings.Compare(a.ID, b.ID) })
	seed := b
	seed.AccountUsage = slices.Clone(b.AccountUsage[:1])
	seed.AccountUsage[0].Facts = facts[len(facts)-1:]
	if _, err := s.Accept(t.Context(), clients[0], seed); err != nil {
		t.Fatal(err)
	}
	// 全局排序的最后一条事实已属于账号 a，冲突之前插入的两千余条也必须回滚。
	bad := tokenBatch(2132, "b")
	if _, err := s.Accept(t.Context(), clients[1], bad); !errors.Is(err, utils.ErrConflict) {
		t.Fatal("account reassignment was not rejected:", err)
	}
	tokenCounts(t, db, 1, 1, 1, 1)
	var bindings int64
	if err := db.Model(&reporting_do.AccountBinding{}).Count(&bindings).Error; err != nil || bindings != 1 {
		t.Fatal("conflicting batch leaked binding:", bindings, err)
	}
}

func TestAccountTokensPeriodsKeepLatestAndBindingCacheChecksIdentity(t *testing.T) {
	s, db, clients := reportingFixture(t)
	s.now = func() time.Time { return time.UnixMilli(accountTokensNow + 1000) }
	b := tokenBatch(0, "a")
	latest := b.AccountUsage[0]
	latest.CollectedAtMS += 500
	b.AccountUsage = append([]reportingv1.AccountTokenUsage{latest}, b.AccountUsage...)
	if _, err := s.Accept(t.Context(), clients[0], b); err != nil {
		t.Fatal(err)
	}
	var period reporting_do.AccountTokenPeriod
	if err := db.First(&period).Error; err != nil || period.CollectedAtMS != latest.CollectedAtMS {
		t.Fatal("period timestamp regressed:", period.CollectedAtMS, err)
	}
	// 同一 scope 后面的不同 account ID 仍须核对绑定，不能复用前一账号的缓存。
	b.ID = uuid.New().String()
	b.AccountUsage[1].AccountID = "b"
	if _, err := s.Accept(t.Context(), clients[0], b); !errors.Is(err, utils.ErrConflict) {
		t.Fatal("binding cache accepted mismatched identity:", err)
	}
	tokenCounts(t, db, 0, 0, 1, 1)
}

func TestAccountTokensSourceFailureAndCancellationRollBack(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelRequest), func(t *testing.T) {
			s, db, clients := reportingFixture(t)
			s.now = func() time.Time { return time.UnixMilli(accountTokensNow) }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			writes := 0
			failure := errors.New("source write failed")
			if err := db.Callback().Create().Before("gorm:create").Register("test:fail-source", func(tx *gorm.DB) {
				if tx.Statement.Table != "pulse_account_token_sources" {
					return
				}
				writes++
				if writes == 2 {
					if cancelRequest {
						cancel()
					} else {
						_ = tx.AddError(failure)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, err := s.Accept(ctx, clients[0], tokenBatch(2132, "a"))
			want := failure
			if cancelRequest {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatal("write failure/cancellation was not returned:", err)
			}
			tokenCounts(t, db, 0, 0, 0, 0)
		})
	}
}
