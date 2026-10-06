package dshprovider

import (
	"encoding/json"
	"strings"
	"testing"
)

func titleFixture(version int, seeded bool, titleData ...map[string]any) string {
	lines := strings.Split(strings.TrimSpace(fixture(seeded)), "\n")
	var header map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &header)
	header["version"] = version
	if seeded && version == 3 {
		header["seedLength"] = 2 + len(titleData)
	}
	b, _ := json.Marshal(header)
	result := string(b) + "\n"
	rows := make([]map[string]any, 0, len(lines)+len(titleData))
	for i, line := range lines[1:] {
		var row map[string]any
		_ = json.Unmarshal([]byte(line), &row)
		rows = append(rows, row)
		if seeded && i == 0 {
			for _, data := range titleData {
				rows = append(rows, map[string]any{"type": "session/title", "time": 1002, "data": data})
			}
		}
	}
	if !seeded {
		for _, data := range titleData {
			rows = append(rows, map[string]any{"type": "session/title", "time": 1100, "data": data})
		}
	}
	for i, row := range rows {
		row["seq"] = i
		b, _ = json.Marshal(row)
		result += string(b) + "\n"
	}
	return result
}

func TestDSHTitlesLatestRenameAndInheritedSeeds(t *testing.T) {
	auto := map[string]any{"title": "自动名称", "source": map[string]any{"kind": "provider"}, "messageSeqs": []int{0}}
	manual := map[string]any{"title": "用户改名 🚀", "source": map[string]any{"kind": "user"}, "messageSeqs": []int{}}
	for _, tc := range []struct {
		name    string
		version int
		seeded  bool
	}{
		{"latest-manual", 4, false}, {"inherited-v4", 4, true}, {"inherited-v3", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := parseSession(t.Context(), strings.NewReader(titleFixture(tc.version, tc.seeded, auto, manual)), tc.version, 2000)
			if err != nil {
				t.Fatal(err)
			}
			if s.session.DisplayTitle != "用户改名 🚀" || s.session.TitleSource != "dsh_title_event" || len(s.usage) != 2 {
				t.Fatal("latest title or inherited usage exclusion lost")
			}
			_, source, confidence, reason := sessionTitlePresentation(s.session)
			if source != "dsh_title_event" || confidence != "high" || reason != "provider_metadata" {
				t.Fatal("title provenance lost")
			}
		})
	}
}

func TestDSHTitlesFallbackAndRequestBodyPrivacy(t *testing.T) {
	content := fixture(false) + `{"seq":8,"type":"session/title-llm-request","time":1100,"data":{"title":"DO NOT USE","messages":[{"content":"PRIVATE PROMPT"}],"system":"PRIVATE SYSTEM"}}` + "\n"
	s, err := parseSession(t.Context(), strings.NewReader(content), 4, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if s.session.DisplayTitle != "未命名会话" || s.session.TitleSource != "fallback" {
		t.Fatal("auxiliary request used as title")
	}
	b, _ := json.Marshal(s.session)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "DO NOT USE") {
		t.Fatal("request body retained")
	}
	// DSH's own fallback generator still emits authoritative session/title metadata.
	s, err = parseSession(t.Context(), strings.NewReader(titleFixture(4, false, map[string]any{"title": "回退名称", "source": map[string]string{"kind": "fallback"}})), 4, 2000)
	if err != nil || s.session.TitleSource != "dsh_title_event" {
		t.Fatal("provider title confused with missing metadata", err)
	}
}

func TestDSHTitlesUnicodeBoundsAndCorruptMetadata(t *testing.T) {
	s, err := parseSession(t.Context(), strings.NewReader(titleFixture(4, false, map[string]any{"title": strings.Repeat("名", 700)})), 4, 2000)
	if err != nil || s.session.DisplayTitle != strings.Repeat("名", 512) {
		t.Fatal("Unicode title bound invalid", err)
	}
	for _, data := range []map[string]any{{}, {"title": "   "}, {"title": "bad\nname"}, {"title": 42}} {
		if _, err := parseSession(t.Context(), strings.NewReader(titleFixture(4, false, data)), 4, 2000); err == nil {
			t.Fatal("accepted corrupt title metadata")
		}
	}
}
