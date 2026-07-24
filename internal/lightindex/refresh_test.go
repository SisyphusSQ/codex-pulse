package lightindex

import "testing"

func TestDecideRefreshReuseRequiresExactCompletedSnapshot(t *testing.T) {
	t.Parallel()

	checkpoint := refreshFixture()
	decision := DecideRefresh(&checkpoint, checkpoint.Home, checkpoint.File, TokenParserVersion)
	if decision.Kind != RefreshReuse || decision.StartOffset != checkpoint.DurableOffset {
		t.Fatalf("decision = %+v, want reuse from %d", decision, checkpoint.DurableOffset)
	}
}

func TestDecideRefreshAppendForGrowthAndIncompleteTail(t *testing.T) {
	t.Parallel()

	t.Run("growth", func(t *testing.T) {
		checkpoint := refreshFixture()
		current := checkpoint.File
		current.SizeBytes += 500
		current.MTimeNS++
		decision := DecideRefresh(&checkpoint, checkpoint.Home, current, TokenParserVersion)
		if decision.Kind != RefreshAppend || decision.StartOffset != checkpoint.DurableOffset {
			t.Fatalf("decision = %+v, want append", decision)
		}
	})

	t.Run("same size with uncommitted tail", func(t *testing.T) {
		checkpoint := refreshFixture()
		checkpoint.Complete = false
		checkpoint.DurableOffset -= 125
		decision := DecideRefresh(&checkpoint, checkpoint.Home, checkpoint.File, TokenParserVersion)
		if decision.Kind != RefreshAppend || decision.StartOffset != checkpoint.DurableOffset {
			t.Fatalf("decision = %+v, want resume append", decision)
		}
	})

	t.Run("short file growth with previous prefix proof", func(t *testing.T) {
		checkpoint := refreshFixture()
		checkpoint.File.SizeBytes = 1_000
		checkpoint.File.PrefixBytes = 1_000
		checkpoint.File.PrefixSHA256 = "short-old-prefix"
		checkpoint.DurableOffset = 1_000
		current := checkpoint.File
		current.SizeBytes = 1_500
		current.MTimeNS++
		current.PrefixBytes = 1_500
		current.PrefixSHA256 = "short-grown-prefix"
		current.Comparison = &PrefixComparison{PrefixBytes: 1_000, PrefixSHA256: "short-old-prefix"}
		decision := DecideRefresh(&checkpoint, checkpoint.Home, current, TokenParserVersion)
		if decision.Kind != RefreshAppend || decision.StartOffset != checkpoint.DurableOffset {
			t.Fatalf("decision = %+v, want proved short append", decision)
		}
	})
}

func TestDecideRefreshRebuildsUnsafeFileChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*ScanCheckpoint, *RolloutFileIdentity)
	}{
		{name: "parser version", mutate: func(checkpoint *ScanCheckpoint, _ *RolloutFileIdentity) {
			checkpoint.ParserVersion = "old-parser"
		}},
		{name: "path", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.Path = "/confirmed-home/sessions/replaced.jsonl"
		}},
		{name: "device", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.DeviceID = "44"
		}},
		{name: "inode", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.Inode++
		}},
		{name: "truncate", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.SizeBytes = 100
			current.MTimeNS++
		}},
		{name: "same-size rewrite", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.MTimeNS++
		}},
		{name: "prefix changed", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.PrefixSHA256 = "different-prefix"
			current.SizeBytes++
			current.MTimeNS++
		}},
		{name: "mtime rollback", mutate: func(_ *ScanCheckpoint, current *RolloutFileIdentity) {
			current.SizeBytes++
			current.MTimeNS--
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkpoint := refreshFixture()
			current := checkpoint.File
			test.mutate(&checkpoint, &current)
			decision := DecideRefresh(&checkpoint, checkpoint.Home, current, TokenParserVersion)
			if decision.Kind != RefreshRebuild || decision.StartOffset != 0 {
				t.Fatalf("decision = %+v, want rebuild", decision)
			}
		})
	}
}

func TestDecideRefreshDefersAcrossHomeGenerationFence(t *testing.T) {
	t.Parallel()

	checkpoint := refreshFixture()
	currentHome := checkpoint.Home
	currentHome.Inode++
	decision := DecideRefresh(&checkpoint, currentHome, checkpoint.File, TokenParserVersion)
	if decision.Kind != RefreshDefer || decision.StartOffset != checkpoint.DurableOffset {
		t.Fatalf("decision = %+v, want defer at durable offset", decision)
	}
}

func TestDecideRefreshStartsNewFileWithRebuild(t *testing.T) {
	t.Parallel()

	checkpoint := refreshFixture()
	decision := DecideRefresh(nil, checkpoint.Home, checkpoint.File, TokenParserVersion)
	if decision.Kind != RefreshRebuild || decision.StartOffset != 0 {
		t.Fatalf("decision = %+v, want initial rebuild", decision)
	}
}

func refreshFixture() ScanCheckpoint {
	return ScanCheckpoint{
		Home: HomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2},
		File: RolloutFileIdentity{
			Path: "/confirmed-home/sessions/rollout.jsonl", DeviceID: "3", Inode: 4,
			SizeBytes: 8192, MTimeNS: 100, PrefixBytes: 4096, PrefixSHA256: "stable-prefix",
		},
		ParserVersion: TokenParserVersion,
		DurableOffset: 8192,
		Complete:      true,
		HighWater:     TokenTotals{Input: 100, CachedInput: 20, Output: 10, Reasoning: 2},
	}
}
