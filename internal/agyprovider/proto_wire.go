package agyprovider

import (
	"encoding/json"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 本文件仅白名单解码 Antigravity 本地 SQLite 中的结构化指标字段，
// 在解析边界立即跳过所有 prompt、response、thinking、tool 参数/输出及未知字段。

var protoStaticModels = map[uint64]string{
	235: "MODEL_CHAT_20706",
	246: "MODEL_GOOGLE_GEMINI_2_5_PRO",
	269: "MODEL_CHAT_23310",
	281: "MODEL_CLAUDE_4_SONNET",
	282: "MODEL_CLAUDE_4_SONNET_THINKING",
	290: "MODEL_CLAUDE_4_OPUS",
	291: "MODEL_CLAUDE_4_OPUS_THINKING",
	312: "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	313: "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING",
	327: "MODEL_GOOGLE_GEMINI_FOR_GOOGLE_2_5_PRO",
	329: "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING_TOOLS",
	330: "MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE",
	332: "MODEL_GOOGLE_GEMINI_2_5_FLASH_IMAGE_PREVIEW",
	333: "MODEL_CLAUDE_4_5_SONNET",
	334: "MODEL_CLAUDE_4_5_SONNET_THINKING",
	340: "MODEL_CLAUDE_4_5_HAIKU",
	341: "MODEL_CLAUDE_4_5_HAIKU_THINKING",
	342: "MODEL_OPENAI_GPT_OSS_120B_MEDIUM",
}

const (
	protoStepStatusCleared = 5
	protoTrajectoryTypeSQ  = 22
	protoSourceSubagent    = 16
)

type wireCursor struct {
	buf []byte
	off int
	end int
}

func (c *wireCursor) readVarint() (uint64, bool) {
	if c.off >= c.end {
		return 0, false
	}
	b0 := c.buf[c.off]
	if b0 < 0x80 {
		c.off++
		return uint64(b0), true
	}
	var result uint64
	var shift uint
	for i := 0; i < 10 && c.off < c.end; i++ {
		b := c.buf[c.off]
		c.off++
		result |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, true
		}
		shift += 7
	}
	for c.off < c.end && (c.buf[c.off]&0x80) != 0 {
		c.off++
	}
	if c.off < c.end {
		c.off++
	}
	return result, true
}

func (c *wireCursor) readSubEnd() (int, bool) {
	n, ok := c.readVarint()
	if !ok {
		return c.end, false
	}
	length := int(uint32(n))
	if length < 0 || length > c.end-c.off {
		return c.end, true
	}
	return c.off + length, true
}

func (c *wireCursor) readString() string {
	end, ok := c.readSubEnd()
	if !ok || end <= c.off {
		c.off = end
		return ""
	}
	s := string(c.buf[c.off:end])
	c.off = end
	return s
}

func (c *wireCursor) skipField(wireType uint64) {
	switch wireType {
	case 0:
		_, _ = c.readVarint()
	case 1:
		c.off = min(c.end, c.off+8)
	case 2:
		end, _ := c.readSubEnd()
		c.off = end
	case 5:
		c.off = min(c.end, c.off+4)
	case 3:
		for c.off < c.end {
			tag, ok := c.readVarint()
			if !ok {
				c.off = c.end
				return
			}
			wt := tag & 7
			if wt == 4 {
				return
			}
			c.skipField(wt)
		}
	case 4:
	default:
		c.off = c.end
	}
}

func decodeTimestampNS(buf []byte, start, end int) (int64, bool) {
	c := wireCursor{buf: buf, off: start, end: end}
	var seconds int64
	var nanos int64
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		if fn == 1 && wt == 0 {
			v, _ := c.readVarint()
			seconds = int64(v)
		} else if fn == 2 && wt == 0 {
			v, _ := c.readVarint()
			nanos = int64(int32(v))
		} else {
			c.skipField(wt)
		}
	}
	// 253402300799 = 9999-12-31T23:59:59Z; cap to int64 ns range (~2262)
	if seconds <= 0 || seconds > 9_000_000_000 {
		return 0, false
	}
	if nanos < 0 || nanos >= 1_000_000_000 {
		nanos = 0
	}
	return seconds*1_000_000_000 + nanos, true
}

func decodeModelEnum(val uint64) string {
	if val == 0 {
		return ""
	}
	if val >= 1000 && val < 100000 {
		return "MODEL_PLACEHOLDER_M" + strconv.FormatUint(val-1000, 10)
	}
	return protoStaticModels[val]
}

func decodeMapStringEntry(buf []byte, start, end int) (string, string) {
	c := wireCursor{buf: buf, off: start, end: end}
	var key, val string
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		if fn == 1 && wt == 2 {
			key = c.readString()
		} else if fn == 2 && wt == 2 {
			val = c.readString()
		} else {
			c.skipField(wt)
		}
	}
	return key, val
}

type protoModelUsage struct {
	Model                string
	InputTokens          int64
	CacheReadTokens      int64
	OutputTokens         int64
	ThinkingOutputTokens int64
	ResponseOutputTokens int64
	HasInput             bool
	HasCacheRead         bool
	HasOutput            bool
	HasThinking          bool
	HasResponse          bool
}

func (u *protoModelUsage) hasAnyTokens() bool {
	return u != nil && (u.InputTokens > 0 || u.CacheReadTokens > 0 || u.OutputTokens > 0 || u.ThinkingOutputTokens > 0)
}

func decodeModelUsage(buf []byte, start, end int) protoModelUsage {
	c := wireCursor{buf: buf, off: start, end: end}
	var out protoModelUsage
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 1 && wt == 0:
			v, _ := c.readVarint()
			if m := decodeModelEnum(v); m != "" {
				out.Model = m
			}
		case fn == 1 && wt == 2:
			if m := strings.TrimSpace(c.readString()); m != "" {
				out.Model = m
			}
		case fn == 2 && wt == 0:
			v, _ := c.readVarint()
			out.InputTokens = clampTokenCount(v)
			out.HasInput = true
		case fn == 3 && wt == 0:
			v, _ := c.readVarint()
			out.OutputTokens = clampTokenCount(v)
			out.HasOutput = true
		case fn == 5 && wt == 0:
			v, _ := c.readVarint()
			out.CacheReadTokens = clampTokenCount(v)
			out.HasCacheRead = true
		case fn == 9 && wt == 0:
			v, _ := c.readVarint()
			out.ThinkingOutputTokens = clampTokenCount(v)
			out.HasThinking = true
		case fn == 10 && wt == 0:
			v, _ := c.readVarint()
			out.ResponseOutputTokens = clampTokenCount(v)
			out.HasResponse = true
		default:
			c.skipField(wt)
		}
	}
	return out
}

func clampTokenCount(v uint64) int64 {
	const maxTokens = uint64(1 << 40)
	if v > maxTokens {
		return 0
	}
	return int64(v)
}

type protoTrajectoryMeta struct {
	CreatedNS       int64
	ParentID        string
	RootID          string
	NestingDepth    int32
	HasSubagentSpec bool
	SubagentType    string
	SubagentRole    string
	AgentScriptName string
}

func decodeTrajectoryMetadata(raw []byte) protoTrajectoryMeta {
	return decodeTrajectoryMetadataSlice(raw, 0, len(raw))
}

func decodeTrajectoryMetadataSlice(buf []byte, start, end int) protoTrajectoryMeta {
	c := wireCursor{buf: buf, off: start, end: end}
	var out protoTrajectoryMeta
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 2 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(buf, c.off, subEnd); ok {
				out.CreatedNS = ns
			}
			c.off = subEnd
		case fn == 4 && wt == 2:
			subEnd, _ := c.readSubEnd()
			sc := wireCursor{buf: buf, off: c.off, end: subEnd}
			for sc.off < sc.end {
				st, ok := sc.readVarint()
				if !ok {
					break
				}
				if (st>>3) == 1 && (st&7) == 2 {
					out.AgentScriptName = strings.TrimSpace(sc.readString())
				} else {
					sc.skipField(st & 7)
				}
			}
			c.off = subEnd
		case fn == 5 && wt == 2:
			out.ParentID = strings.ToLower(strings.TrimSpace(c.readString()))
		case fn == 6 && wt == 2:
			out.RootID = strings.ToLower(strings.TrimSpace(c.readString()))
		case fn == 8 && wt == 2:
			subEnd, _ := c.readSubEnd()
			sc := wireCursor{buf: buf, off: c.off, end: subEnd}
			for sc.off < sc.end {
				st, ok := sc.readVarint()
				if !ok {
					break
				}
				switch {
				case (st>>3) == 1 && (st&7) == 2:
					out.SubagentType = strings.TrimSpace(sc.readString())
				case (st>>3) == 2 && (st&7) == 2:
					out.SubagentRole = strings.TrimSpace(sc.readString())
				default:
					sc.skipField(st & 7)
				}
			}
			out.HasSubagentSpec = out.SubagentType != "" || out.SubagentRole != ""
			c.off = subEnd
		case fn == 17 && wt == 0:
			v, _ := c.readVarint()
			out.NestingDepth = int32(v)
		default:
			c.skipField(wt)
		}
	}
	return out
}

type protoCascadeSummary struct {
	StepCount          int64
	LastModifiedNS     int64
	CreatedNS          int64
	SourceEnum         uint64
	TrajectoryType     int32
	ForkParentID       string
	AnnotationTitle    string
	SideQuestionIDs    []string
	SideQuestionTitles map[string]string
	TrajectoryMeta     protoTrajectoryMeta
}

// decodeCascadeSummary 仅提取标题、时间戳、父子关系与轨迹类型，显式跳过正文摘要 field 1。
func decodeCascadeSummary(raw []byte) protoCascadeSummary {
	c := wireCursor{buf: raw, off: 0, end: len(raw)}
	var out protoCascadeSummary
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 2 && wt == 0:
			v, _ := c.readVarint()
			out.StepCount = int64(uint32(v))
		case fn == 3 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.LastModifiedNS = ns
			}
			c.off = subEnd
		case fn == 7 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.CreatedNS = ns
			}
			c.off = subEnd
		case fn == 15 && wt == 2:
			subEnd, _ := c.readSubEnd()
			ac := wireCursor{buf: raw, off: c.off, end: subEnd}
			for ac.off < ac.end {
				at, ok := ac.readVarint()
				if !ok {
					break
				}
				afn := at >> 3
				awt := at & 7
				switch {
				case afn == 1 && awt == 2:
					out.AnnotationTitle = strings.TrimSpace(ac.readString())
				case afn == 17 && awt == 2:
					if sq := strings.ToLower(strings.TrimSpace(ac.readString())); sq != "" {
						out.SideQuestionIDs = append(out.SideQuestionIDs, sq)
					}
				case afn == 18 && awt == 2:
					me, _ := ac.readSubEnd()
					k, v := decodeMapStringEntry(raw, ac.off, me)
					ac.off = me
					lk := strings.ToLower(strings.TrimSpace(k))
					if lk != "" {
						if out.SideQuestionTitles == nil {
							out.SideQuestionTitles = make(map[string]string)
						}
						out.SideQuestionTitles[lk] = strings.TrimSpace(v)
					}
				default:
					ac.skipField(awt)
				}
			}
			c.off = subEnd
		case fn == 17 && wt == 2:
			subEnd, _ := c.readSubEnd()
			out.TrajectoryMeta = decodeTrajectoryMetadataSlice(raw, c.off, subEnd)
			c.off = subEnd
		case fn == 20 && wt == 0:
			v, _ := c.readVarint()
			out.SourceEnum = v
		case fn == 22 && wt == 0:
			v, _ := c.readVarint()
			out.TrajectoryType = int32(uint32(v))
		case fn == 24 && wt == 2:
			out.ForkParentID = strings.ToLower(strings.TrimSpace(c.readString()))
		default:
			c.skipField(wt)
		}
	}
	return out
}

type protoStepMeta struct {
	CreatedNS            int64
	FinishedGeneratingNS int64
	CompletedNS          int64
	StartedNS            int64
	GeneratorModel       string
	ModelUsage           *protoModelUsage
	StepIndex            int64
	OriginCascadeID      string
}

// decodeStepMetadata 仅提取时间戳、Token 用量、模型枚举与源步骤归属（originCascadeId），
// 显式跳过 toolCall(4)、retryInfos(28)、toolSummary(30) 等非指标字段。
func decodeStepMetadata(raw []byte, fallbackIdx int64) protoStepMeta {
	c := wireCursor{buf: raw, off: 0, end: len(raw)}
	out := protoStepMeta{StepIndex: fallbackIdx}
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 1 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.CreatedNS = ns
			}
			c.off = subEnd
		case fn == 7 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.FinishedGeneratingNS = ns
			}
			c.off = subEnd
		case fn == 8 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.CompletedNS = ns
			}
			c.off = subEnd
		case fn == 9 && wt == 2:
			subEnd, _ := c.readSubEnd()
			u := decodeModelUsage(raw, c.off, subEnd)
			out.ModelUsage = &u
			c.off = subEnd
		case fn == 11 && wt == 0:
			v, _ := c.readVarint()
			out.GeneratorModel = decodeModelEnum(v)
		case fn == 20 && wt == 2:
			subEnd, _ := c.readSubEnd()
			sc := wireCursor{buf: raw, off: c.off, end: subEnd}
			for sc.off < sc.end {
				st, ok := sc.readVarint()
				if !ok {
					break
				}
				sfn := st >> 3
				swt := st & 7
				switch {
				case sfn == 2 && swt == 0:
					v, _ := sc.readVarint()
					out.StepIndex = int64(uint32(v))
				case sfn == 4 && swt == 2:
					out.OriginCascadeID = strings.ToLower(strings.TrimSpace(sc.readString()))
				default:
					sc.skipField(swt)
				}
			}
			c.off = subEnd
		case fn == 32 && wt == 2:
			subEnd, _ := c.readSubEnd()
			if ns, ok := decodeTimestampNS(raw, c.off, subEnd); ok {
				out.StartedNS = ns
			}
			c.off = subEnd
		default:
			c.skipField(wt)
		}
	}
	if out.CompletedNS == 0 && out.StartedNS > 0 && out.FinishedGeneratingNS == 0 {
		out.CompletedNS = out.StartedNS
	}
	return out
}

type protoGenMeta struct {
	StepIndices     []int64
	Model           string
	ResponseModel   string
	Usage           *protoModelUsage
	CascadeID       string
	ParentCascadeID string
	RootCascadeID   string
}

// decodeGeneratorMetadata 仅提取 stepIndices、模型标识、usage 与 cascade 血缘元数据，
// 跳过全部 promptSections(16)、messagePrompts(2)、tools(8)、contextWindowMetadata(9)。
func decodeGeneratorMetadata(raw []byte) protoGenMeta {
	c := wireCursor{buf: raw, off: 0, end: len(raw)}
	var out protoGenMeta
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 2 && wt == 0:
			v, _ := c.readVarint()
			out.StepIndices = append(out.StepIndices, int64(uint32(v)))
		case fn == 2 && wt == 2:
			subEnd, _ := c.readSubEnd()
			pc := wireCursor{buf: raw, off: c.off, end: subEnd}
			for pc.off < pc.end {
				v, ok := pc.readVarint()
				if !ok {
					break
				}
				out.StepIndices = append(out.StepIndices, int64(uint32(v)))
			}
			c.off = subEnd
		case fn == 1 && wt == 2:
			subEnd, _ := c.readSubEnd()
			decodeChatModelMetaSlice(raw, c.off, subEnd, &out)
			c.off = subEnd
		case fn == 3 && wt == 2:
			subEnd, _ := c.readSubEnd()
			decodePlannerConfigSlice(raw, c.off, subEnd, &out)
			c.off = subEnd
		default:
			c.skipField(wt)
		}
	}
	return out
}

func decodeChatModelMetaSlice(buf []byte, start, end int, out *protoGenMeta) {
	c := wireCursor{buf: buf, off: start, end: end}
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 3 && wt == 0:
			v, _ := c.readVarint()
			if m := decodeModelEnum(v); m != "" && out.Model == "" {
				out.Model = m
			}
		case fn == 4 && wt == 2:
			subEnd, _ := c.readSubEnd()
			u := decodeModelUsage(buf, c.off, subEnd)
			out.Usage = &u
			c.off = subEnd
		case fn == 19 && wt == 2:
			out.ResponseModel = strings.TrimSpace(c.readString())
		case fn == 20 && wt == 2:
			subEnd, _ := c.readSubEnd()
			k, v := decodeMapStringEntry(buf, c.off, subEnd)
			c.off = subEnd
			switch k {
			case "cascade_id":
				out.CascadeID = strings.ToLower(strings.TrimSpace(v))
			case "parent_cascade_id":
				out.ParentCascadeID = strings.ToLower(strings.TrimSpace(v))
			case "root_cascade_id":
				out.RootCascadeID = strings.ToLower(strings.TrimSpace(v))
			case "model_enum":
				clean := strings.TrimSpace(v)
				if clean != "" && clean != "0" &&
					!strings.EqualFold(clean, "MODEL_UNSPECIFIED") &&
					!strings.EqualFold(clean, "MODEL_UNKNOWN") {
					out.Model = clean
				}
			}
		default:
			c.skipField(wt)
		}
	}
	if out.Usage != nil && out.Usage.Model == "" && out.Model != "" {
		out.Usage.Model = out.Model
	}
}

func decodePlannerConfigSlice(buf []byte, start, end int, out *protoGenMeta) {
	c := wireCursor{buf: buf, off: start, end: end}
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		fn := tag >> 3
		wt := tag & 7
		switch {
		case fn == 1 && wt == 0:
			v, _ := c.readVarint()
			if m := decodeModelEnum(v); m != "" && out.Model == "" {
				out.Model = m
			}
		case fn == 28 && wt == 2:
			if m := strings.TrimSpace(c.readString()); m != "" && out.Model == "" {
				out.Model = m
			}
		default:
			c.skipField(wt)
		}
	}
}

func decodeParentReferenceCascadeID(raw []byte) string {
	c := wireCursor{buf: raw, off: 0, end: len(raw)}
	for c.off < c.end {
		tag, ok := c.readVarint()
		if !ok {
			break
		}
		if (tag>>3) == 6 && (tag&7) == 2 {
			return strings.ToLower(strings.TrimSpace(c.readString()))
		}
		c.skipField(tag & 7)
	}
	return ""
}

var (
	pbtxtTitleDoubleRe = regexp.MustCompile(`(?m)^\s*title\s*:\s*"((?:\\.|[^"\\])*)"`)
	pbtxtTitleSingleRe = regexp.MustCompile(`(?m)^\s*title\s*:\s*'((?:\\.|[^'\\])*)'`)
)

// parseAnnotationTitle 仅提取 annotations/<cascadeId>.pbtxt 中的顶层 title 字段。
func parseAnnotationTitle(raw string) string {
	if raw == "" {
		return ""
	}
	if m := pbtxtTitleDoubleRe.FindStringSubmatch(raw); len(m) == 2 {
		return sanitizeTitle(unescapePbtxtString(m[1]))
	}
	if m := pbtxtTitleSingleRe.FindStringSubmatch(raw); len(m) == 2 {
		return sanitizeTitle(unescapePbtxtString(m[1]))
	}
	return ""
}

func unescapePbtxtString(raw string) string {
	if !strings.ContainsRune(raw, '\\') {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			out = append(out, raw[i])
			continue
		}
		i++
		ch := raw[i]
		switch {
		case ch >= '0' && ch <= '7':
			val := int(ch - '0')
			for digits := 0; digits < 2 && i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7'; digits++ {
				i++
				val = val*8 + int(raw[i]-'0')
			}
			out = append(out, byte(val&0xff))
		case (ch == 'x' || ch == 'X') && i+2 < len(raw):
			if parsed, err := strconv.ParseUint(raw[i+1:i+3], 16, 8); err == nil {
				out = append(out, byte(parsed))
				i += 2
			} else {
				out = append(out, ch)
			}
		case ch == 'u' && i+4 < len(raw):
			if parsed, err := strconv.ParseUint(raw[i+1:i+5], 16, 16); err == nil {
				var buf [4]byte
				n := utf8.EncodeRune(buf[:], rune(parsed))
				out = append(out, buf[:n]...)
				i += 4
			} else {
				out = append(out, ch)
			}
		case ch == 'n':
			out = append(out, '\n')
		case ch == 'r':
			out = append(out, '\r')
		case ch == 't':
			out = append(out, '\t')
		default:
			out = append(out, ch)
		}
	}
	return string(out)
}

func sanitizeTitle(raw string) string {
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "")
	}
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned := strings.Join(strings.Fields(b.String()), " ")
	runes := []rune(cleaned)
	if len(runes) > 512 {
		cleaned = string(runes[:512])
	}
	return cleaned
}

// parseWorkspaceBasename 从 workspace_uris 字段解析规范化哈希种子与安全 basename，
// 绝不向外暴露完整本机路径。
func parseWorkspaceBasename(raw string) (canonicalSeed string, displayName string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	candidates := extractWorkspaceURIs(trimmed)
	for _, candidate := range candidates {
		normalized := normalizeWorkspaceURI(candidate)
		if normalized == "" {
			continue
		}
		base := workspaceBaseFromNormalized(normalized)
		if base == "" {
			continue
		}
		return normalized, sanitizeLabel(base, "未识别项目")
	}
	return "", ""
}

func extractWorkspaceURIs(raw string) []string {
	if strings.HasPrefix(raw, "[") {
		var list []string
		if json.Unmarshal([]byte(raw), &list) == nil {
			return list
		}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	return parts
}

func normalizeWorkspaceURI(raw string) string {
	s := strings.TrimSpace(strings.Trim(raw, `"'`))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(s), "file://") {
		if parsed, err := url.Parse(s); err == nil {
			decoded, decErr := url.PathUnescape(parsed.Path)
			if decErr == nil && decoded != "" {
				s = decoded
			} else if parsed.Path != "" {
				s = parsed.Path
			}
			// Windows file:///C:/repo -> C:/repo
			if len(s) >= 3 && s[0] == '/' && s[2] == ':' {
				s = s[1:]
			}
		}
	}
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	return path.Clean(s)
}

func workspaceBaseFromNormalized(normalized string) string {
	base := path.Base(normalized)
	if base == "" || base == "." || base == "/" {
		return filepath.Base(normalized)
	}
	return base
}
