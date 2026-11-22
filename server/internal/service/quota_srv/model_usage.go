package quota_srv

import (
	"cmp"
	"context"
	"maps"
	"math/big"
	"slices"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Quota) AccountModelUsage(ctx context.Context, principal access_dto.Principal, q quota_dto.Query) (out quota_vo.AccountModelUsage, err error) {
	if err = access_srv.RequireAdmin(principal); err != nil {
		return
	}
	if q.AccountKey == "" || q.WindowKey == "" || (q.Provider != "" && q.Provider != "codex") {
		return out, utils.ErrBadParamInput
	}
	q.Provider = "codex"
	now := s.now().UnixMilli()
	out = quota_vo.AccountModelUsage{EvaluatedAtMS: now, Models: []quota_vo.AccountModelRow{}}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		scopes, err := s.repository.Windows(ctx, q)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scopeKey(scope) != q.WindowKey || scope.WindowMinutes == nil || *scope.WindowMinutes != 10080 {
				continue
			}
			headers, err := s.repository.Headers(ctx, q, scope, now, store.DefaultQuotaArbitrationRule().MaxClockSkewMS)
			if err != nil {
				return err
			}
			if len(headers) == 0 {
				return utils.ErrNotFound
			}
			window, err := buildWindow(q.WindowKey, headers, nil, now)
			if err != nil {
				return err
			}
			out.WindowStartAtMS, out.ResetsAtMS = window.Current.WindowStartAtMS, window.Current.ResetsAtMS
			if !activeAccountTokenWindow(&window, now) {
				return nil
			}
			return s.collectAccountModels(ctx, q, &out)
		}
		return utils.ErrNotFound
	})
	return
}

// accountModelSum 只记录查询内的精确累加状态。
type accountModelSum struct {
	row     quota_vo.AccountModelRow
	total   big.Int
	parts   [4]big.Int
	missing [4]bool
}

func (s *Quota) collectAccountModels(ctx context.Context, q quota_dto.Query, out *quota_vo.AccountModelUsage) error {
	groups := map[string]*accountModelSum{}
	var total big.Int
	available, err := s.repository.StreamAccountModelFacts(ctx, q, q.AccountKey, *out.WindowStartAtMS, *out.ResetsAtMS, out.EvaluatedAtMS, func(fact quota_dto.AccountModelFact) {
		key, kind := ":unmatched", "unmatched"
		var model attribution.ModelDecision
		if fact.Matched {
			key, kind = ":unknown_model", "unknown_model"
			if fact.Model != nil {
				model = attribution.NormalizeModel(*fact.Model)
				if model.Key != "" {
					key, kind = model.Key, "model"
				}
			}
		}
		group := groups[key]
		if group == nil {
			group = &accountModelSum{row: quota_vo.AccountModelRow{Kind: kind}}
			if model.Key != "" {
				group.row.Model, group.row.Name = new(model.Key), new(model.DisplayName)
			}
			groups[key] = group
		}
		var value big.Int
		total.Add(&total, value.SetInt64(fact.TotalTokens))
		group.total.Add(&group.total, &value)
		for i, part := range [...]*int64{fact.InputTokens, fact.CachedTokens, fact.OutputTokens, fact.ReasoningTokens} {
			if part == nil {
				group.missing[i] = true
			} else {
				group.parts[i].Add(&group.parts[i], value.SetInt64(*part))
			}
		}
	})
	if err != nil || !available {
		return err
	}
	out.RecordedTokens = new(total.String())
	ordered := slices.Collect(maps.Values(groups))
	slices.SortFunc(ordered, func(a, b *accountModelSum) int {
		if order := cmp.Compare(a.row.Kind, b.row.Kind); order != 0 {
			return order
		}
		if order := b.total.Cmp(&a.total); order != 0 {
			return order
		}
		if a.row.Kind != "model" {
			return 0
		}
		return cmp.Compare(*a.row.Model, *b.row.Model)
	})
	for _, group := range ordered {
		row := group.row
		row.TotalTokens = group.total.String()
		for i, part := range [...]**string{&row.InputTokens, &row.CachedTokens, &row.OutputTokens, &row.ReasoningTokens} {
			if !group.missing[i] {
				*part = new(group.parts[i].String())
			}
		}
		out.Models = append(out.Models, row)
	}
	return nil
}
