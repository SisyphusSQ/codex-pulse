package catalog_srv

import (
	"cmp"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	catalog_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/catalog_dto"
	catalog_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/catalog_vo"
)

//go:embed releases-20261027.json
var publicReleases []byte

//go:embed releases-20261118.json
var publicReleases20261118 []byte

//go:embed releases-20261126.json
var publicReleases20261126 []byte

func applyModelReleases(models []catalog_vo.Model) error {
	var records []catalog_dto.ModelRelease
	if err := json.Unmarshal(publicReleases, &records, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("read model releases: %w", err)
	}
	var additions []catalog_dto.ModelRelease
	if err := json.Unmarshal(publicReleases20261118, &additions, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("read model release additions: %w", err)
	}
	records = append(records, additions...)
	var agyAdditions []catalog_dto.ModelRelease
	if err := json.Unmarshal(publicReleases20261126, &agyAdditions, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("read agy model release additions: %w", err)
	}
	records = append(records, agyAdditions...)
	releases := make(map[string]catalog_dto.ReleaseInfo)
	for _, record := range records {
		date, err := time.Parse(time.DateOnly, record.ReleasedOn)
		if err != nil {
			return fmt.Errorf("read model release date: %w", err)
		}
		for _, model := range record.Models {
			key := releaseModelKey(model)
			if _, exists := releases[key]; exists {
				return fmt.Errorf("duplicate model release: %s", model)
			}
			releases[key] = catalog_dto.ReleaseInfo{ReleasedAtMS: date.UnixMilli(), SourceURL: record.SourceURL}
		}
	}
	for i := range models {
		model := models[i].Model
		if _, exists := releases[releaseModelKey(model)]; !exists {
			// 复用已有的明确型号/别名匹配，不从费率生效日或版本号猜发布日期。
			switch models[i].Provider {
			case "codex":
				model = attribution.NormalizeModel(model).Key
			case "cursor":
				if rate, ok := pricing.CursorRateForModel(model, pricing.CursorPricingVerifiedAtMS); ok {
					model = rate.ModelID
				}
			case "grok":
				if rate, ok := pricing.GrokRateForModel(model); ok {
					model = rate.ModelID
				}
			case "agy":
				if canonical, _ := pricing.ResolveAGYModelID(model, ""); canonical != "" {
					model = canonical
				}
				if _, ok := releases[releaseModelKey(model)]; !ok {
					model = agyReleaseBaseModel(model)
				}
			}
		}
		if release, exists := releases[releaseModelKey(model)]; exists {
			models[i].ReleasedAtMS = new(release.ReleasedAtMS)
			models[i].ReleaseSourceURL = release.SourceURL
		}
	}
	return nil
}

func agyReleaseBaseModel(model string) string {
	key := releaseModelKey(model)
	if key == "claude-opus-4-7" {
		return "claude-4.7-opus"
	}
	if key == "gemini-3-pro-image" {
		return "gemini-3-pro-image-preview"
	}
	for _, suffix := range []string{"-thinking", "-xhigh", "-high", "-medium", "-low", "-max", "-tiered", "-extra-low", "-preview"} {
		key = strings.TrimSuffix(key, suffix)
	}
	if key == "claude-opus-4-7" {
		return "claude-4.7-opus"
	}
	return key
}

func releaseModelKey(model string) string {
	return strings.ToLower(strings.Join(strings.Fields(model), "-"))
}

func compareModelRelease(a, b catalog_vo.Model) int {
	if a.ReleasedAtMS == nil && b.ReleasedAtMS != nil {
		return 1
	}
	if a.ReleasedAtMS != nil && b.ReleasedAtMS == nil {
		return -1
	}
	if a.ReleasedAtMS != nil && b.ReleasedAtMS != nil {
		if order := cmp.Compare(*b.ReleasedAtMS, *a.ReleasedAtMS); order != 0 {
			return order
		}
	}
	if order := strings.Compare(strings.ToLower(a.Model), strings.ToLower(b.Model)); order != 0 {
		return order
	}
	return strings.Compare(a.Key, b.Key)
}
