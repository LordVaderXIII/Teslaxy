package services

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"teslaxy/models"
)

// Round-3 selection, kept as an oracle. It rescans every inode for every
// candidate, which is why 8,000 clips regressed to tens of seconds.
func selectRetentionCandidatesRound3(evals []clipEval, cfg RetentionConfig, now time.Time, totalBytes int64) []RetentionCandidate {
	maxAgeCutoff := time.Time{}
	if cfg.MaxAgeDays > 0 {
		maxAgeCutoff = now.AddDate(0, 0, -cfg.MaxAgeDays)
	}
	maxBytes := cfg.MaxSizeBytes()

	sortEvals(evals)

	inodeIndex := buildInodeLinkIndex(evals)
	ageIDs := map[uint]bool{}
	sizeIDs := map[uint]bool{}
	selectedOrder := make([]clipEval, 0)
	selectedSet := map[uint]bool{}

	for _, ev := range evals {
		if ev.protected || len(ev.deletable) == 0 {
			continue
		}
		if cfg.MaxAgeDays > 0 && ev.clip.Timestamp.Before(maxAgeCutoff) {
			ageIDs[ev.clip.ID] = true
			selectedOrder = append(selectedOrder, ev)
			selectedSet[ev.clip.ID] = true
		}
	}

	if maxBytes > 0 {
		for {
			freed := estimateFreedBytes(inodeIndex, selectedSet)
			if totalBytes-freed <= maxBytes {
				break
			}
			added := false
			for _, ev := range evals {
				if ev.protected || len(ev.deletable) == 0 || selectedSet[ev.clip.ID] {
					continue
				}
				sizeIDs[ev.clip.ID] = true
				selectedOrder = append(selectedOrder, ev)
				selectedSet[ev.clip.ID] = true
				added = true
				break
			}
			if !added {
				break
			}
		}
		ageOnly := map[uint]bool{}
		for id := range ageIDs {
			ageOnly[id] = true
		}
		ageFreed := estimateFreedBytes(inodeIndex, ageOnly)
		if totalBytes > maxBytes && totalBytes-ageFreed > maxBytes {
			for id := range ageIDs {
				sizeIDs[id] = true
			}
		}
	}

	out := make([]RetentionCandidate, 0, len(selectedOrder))
	running := map[uint]bool{}
	var prevFreed int64
	for _, ev := range selectedOrder {
		running[ev.clip.ID] = true
		freed := estimateFreedBytes(inodeIndex, running)
		incr := freed - prevFreed
		prevFreed = freed

		reason := "age"
		hasAge := ageIDs[ev.clip.ID]
		hasSize := sizeIDs[ev.clip.ID]
		switch {
		case hasAge && hasSize:
			reason = "age+size"
		case hasSize:
			reason = "size"
		case hasAge:
			reason = "age"
		}
		paths := make([]string, 0, len(ev.deletable))
		for _, f := range ev.deletable {
			paths = append(paths, f.path)
		}
		out = append(out, RetentionCandidate{
			ClipID:    ev.clip.ID,
			Event:     ev.clip.Event,
			SourceDir: ev.clip.SourceDir,
			Timestamp: ev.clip.Timestamp,
			Reason:    reason,
			Bytes:     incr,
			FilePaths: paths,
		})
	}
	return out
}

func sortEvals(evals []clipEval) {
	// Same ordering as selectRetentionCandidates. Local copy so the oracle
	// does not depend on the production sort staying inline.
	if len(evals) < 2 {
		return
	}
	// Insertion sort keeps the test file free of an extra sort import while
	// matching the production oldest-first, then id, order. n is small here.
	for i := 1; i < len(evals); i++ {
		j := i
		for j > 0 && evalOlder(evals[j], evals[j-1]) {
			evals[j], evals[j-1] = evals[j-1], evals[j]
			j--
		}
	}
}

func evalOlder(a, b clipEval) bool {
	if a.clip.Timestamp.Equal(b.clip.Timestamp) {
		return a.clip.ID < b.clip.ID
	}
	return a.clip.Timestamp.Before(b.clip.Timestamp)
}

func cloneEvals(in []clipEval) []clipEval {
	out := make([]clipEval, len(in))
	copy(out, in)
	return out
}

func uniqueEvalBytes(evals []clipEval) int64 {
	seen := map[string]bool{}
	var sum int64
	for _, ev := range evals {
		for _, f := range ev.files {
			key := inodeKey(f)
			if seen[key] {
				continue
			}
			seen[key] = true
			sum += f.size
		}
	}
	return sum
}

func fileLink(path string, size int64, ino uint64, fresh bool) fileRef {
	return fileRef{
		path:     path,
		size:     size,
		dev:      1,
		ino:      ino,
		identity: true,
		fresh:    fresh,
	}
}

// mixedHardlinkEvals builds protected links, fresh links, shared inodes, and
// equal timestamps so the linear ledger has to match round-3 selection.
func mixedHardlinkEvals(n int, now time.Time) []clipEval {
	evals := make([]clipEval, 0, n)
	for i := 1; i <= n; i++ {
		id := uint(i)
		ts := now.AddDate(0, 0, -(n - i + 30))
		if i%4 == 0 {
			ts = now.AddDate(0, 0, -(n - (i - 1) + 30))
		}
		ev := clipEval{
			clip: models.Clip{
				ID:        id,
				Timestamp: ts,
				Event:     "Recent",
				SourceDir: fmt.Sprintf("RecentClips/%d", id),
			},
		}
		ino := uint64(1000 + i)
		if i%5 == 0 {
			ino = uint64(1000 + i - 1) // share the previous clip's inode
		}
		size := int64(1000 + i*10)
		f := fileLink(fmt.Sprintf("/footage/%d.mp4", id), size, ino, false)
		ev.files = append(ev.files, f)
		protected := i%7 == 0
		fresh := i%11 == 0
		if fresh {
			f.fresh = true
			ev.files[0] = f
		}
		if protected {
			ev.protected = true
			ev.clip.Protected = true
		}
		if !protected && !fresh {
			ev.deletable = append(ev.deletable, ev.files[0])
		}
		if i%9 == 0 && !protected {
			extra := fileLink(fmt.Sprintf("/footage/%d-b.mp4", id), 2048, uint64(5000+i), false)
			ev.files = append(ev.files, extra)
			ev.deletable = append(ev.deletable, extra)
		}
		ev.bytes = uniqueInodeBytes(ev.deletable)
		evals = append(evals, ev)
	}
	return evals
}

func TestSelectRetentionCandidatesMatchesRound3Oracle(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	evals := mixedHardlinkEvals(80, now)
	total := uniqueEvalBytes(evals)
	cases := []RetentionConfig{
		{MaxAgeDays: 90, MaxSizeGB: 0},
		{MaxAgeDays: 0, MaxSizeGB: float64(total/2) / float64(1<<30)},
		{MaxAgeDays: 90, MaxSizeGB: float64(total/3) / float64(1<<30)},
		{MaxAgeDays: 400, MaxSizeGB: float64(1024) / float64(1<<30)},
	}
	for i, cfg := range cases {
		got := selectRetentionCandidates(cloneEvals(evals), cfg, now, total)
		want := selectRetentionCandidatesRound3(cloneEvals(evals), cfg, now, total)
		if len(got) != len(want) {
			t.Fatalf("case %d len got %d want %d", i, len(got), len(want))
		}
		for j := range got {
			if !reflect.DeepEqual(got[j], want[j]) {
				t.Fatalf("case %d candidate %d\ngot=%+v\nwant=%+v", i, j, got[j], want[j])
			}
		}
	}
}

func syntheticNoHardlinkEvals(n int, oldest time.Time) []clipEval {
	evals := make([]clipEval, n)
	for i := 0; i < n; i++ {
		id := uint(i + 1)
		f := fileLink(fmt.Sprintf("/footage/%d.mp4", id), 1024, uint64(id), false)
		evals[i] = clipEval{
			clip: models.Clip{
				ID:        id,
				Timestamp: oldest.Add(time.Duration(i) * time.Second),
				Event:     "Recent",
				SourceDir: fmt.Sprintf("RecentClips/%d", id),
			},
			files:     []fileRef{f},
			deletable: []fileRef{f},
			bytes:     1024,
		}
	}
	return evals
}

func syntheticHardlinkPairEvals(pairs int, oldest time.Time) []clipEval {
	evals := make([]clipEval, 0, pairs*2)
	for p := 0; p < pairs; p++ {
		ino := uint64(p + 1)
		for k := 0; k < 2; k++ {
			id := uint(p*2 + k + 1)
			f := fileLink(fmt.Sprintf("/footage/%d.mp4", id), 1024, ino, false)
			evals = append(evals, clipEval{
				clip: models.Clip{
					ID:        id,
					Timestamp: oldest.Add(time.Duration(id) * time.Second),
					Event:     "Recent",
					SourceDir: fmt.Sprintf("RecentClips/%d", id),
				},
				files:     []fileRef{f},
				deletable: []fileRef{f},
				bytes:     1024,
			})
		}
	}
	return evals
}

func gbForBytes(n int64) float64 {
	return float64(n) / float64(1<<30)
}

func TestSelectRetentionCandidates8000NearLinear(t *testing.T) {
	const n = 8000
	oldest := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	evals := syntheticNoHardlinkEvals(n, oldest)
	total := int64(n) * 1024

	// Age selects every clip. Round 3 then recomputed freed bytes once per
	// clip (8,000 × full inode scan ≈ 35.7s). This must stay under a second.
	ageCfg := RetentionConfig{MaxAgeDays: 90, MaxSizeGB: 300}
	start := time.Now()
	aged := selectRetentionCandidates(cloneEvals(evals), ageCfg, now, total)
	ageElapsed := time.Since(start)
	t.Logf("8000 age-only select: %s candidates=%d", ageElapsed, len(aged))
	if ageElapsed > time.Second {
		t.Fatalf("8000 age select took %s, want < 1s", ageElapsed)
	}
	if len(aged) != n {
		t.Fatalf("age selected %d want %d", len(aged), n)
	}
	var ageSum int64
	for i, c := range aged {
		ageSum += c.Bytes
		if c.Reason != "age" {
			t.Fatalf("candidate %d reason %q want age", i, c.Reason)
		}
		if c.ClipID != uint(i+1) {
			t.Fatalf("candidate %d id %d want %d", i, c.ClipID, i+1)
		}
	}
	if ageSum != total {
		t.Fatalf("age bytes %d want %d", ageSum, total)
	}

	// Size cap with no hardlinks still used to rescan the inode index per clip.
	keep := int64(10 * 1024)
	sizeCfg := RetentionConfig{MaxAgeDays: 0, MaxSizeGB: gbForBytes(keep)}
	start = time.Now()
	sized := selectRetentionCandidates(cloneEvals(evals), sizeCfg, now, total)
	sizeElapsed := time.Since(start)
	t.Logf("8000 size-cap select: %s candidates=%d", sizeElapsed, len(sized))
	if sizeElapsed > time.Second {
		t.Fatalf("8000 size-cap select took %s, want < 1s", sizeElapsed)
	}
	if len(sized) != n-10 {
		t.Fatalf("size selected %d want %d", len(sized), n-10)
	}
	var sizeSum int64
	selected := map[uint]bool{}
	for _, c := range sized {
		sizeSum += c.Bytes
		selected[c.ClipID] = true
		if c.Reason != "size" {
			t.Fatalf("reason %q want size", c.Reason)
		}
	}
	if sizeSum != total-keep {
		t.Fatalf("size bytes %d want %d", sizeSum, total-keep)
	}
	if got := estimateFreedBytes(buildInodeLinkIndex(evals), selected); got != sizeSum {
		t.Fatalf("ledger freed %d oracle %d", sizeSum, got)
	}

	// Shared inodes: 4,000 hardlink pairs. Bytes free only when both links
	// are selected, and the walk must still finish in under a second.
	pairs := syntheticHardlinkPairEvals(4000, oldest)
	pairTotal := int64(4000) * 1024
	pairCfg := RetentionConfig{MaxAgeDays: 0, MaxSizeGB: gbForBytes(10 * 1024)}
	start = time.Now()
	paired := selectRetentionCandidates(cloneEvals(pairs), pairCfg, now, pairTotal)
	pairElapsed := time.Since(start)
	t.Logf("8000 hardlink-pair select: %s candidates=%d", pairElapsed, len(paired))
	if pairElapsed > time.Second {
		t.Fatalf("8000 hardlink-pair select took %s, want < 1s", pairElapsed)
	}
	if len(paired) != 7980 {
		t.Fatalf("pair selected %d want 7980", len(paired))
	}
	var pairSum int64
	pairIDs := map[uint]bool{}
	for _, c := range paired {
		pairSum += c.Bytes
		pairIDs[c.ClipID] = true
	}
	if pairSum != pairTotal-10*1024 {
		t.Fatalf("pair bytes %d want %d", pairSum, pairTotal-10*1024)
	}
	if got := estimateFreedBytes(buildInodeLinkIndex(pairs), pairIDs); got != pairSum {
		t.Fatalf("pair ledger freed %d oracle %d", pairSum, got)
	}
}

func BenchmarkSelectRetentionCandidates8000(b *testing.B) {
	const n = 8000
	oldest := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	evals := syntheticNoHardlinkEvals(n, oldest)
	total := int64(n) * 1024
	// All clips are older than 90 days, so every clip is selected. This is the
	// dry-run path that took ~35.7s in round 3.
	cfg := RetentionConfig{MaxAgeDays: 90, MaxSizeGB: 300}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selectRetentionCandidates(cloneEvals(evals), cfg, now, total)
	}
}

func BenchmarkSelectRetentionCandidates8000SizeCap(b *testing.B) {
	const n = 8000
	oldest := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	evals := syntheticNoHardlinkEvals(n, oldest)
	total := int64(n) * 1024
	cfg := RetentionConfig{MaxAgeDays: 0, MaxSizeGB: gbForBytes(10 * 1024)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selectRetentionCandidates(cloneEvals(evals), cfg, now, total)
	}
}

func TestRetentionOrphanSweepMemoryBounded(t *testing.T) {
	db := setupRetentionTestDB(t)
	const clips = 64
	const payload = 1 << 20
	blob := strings.Repeat("x", payload)
	for i := 0; i < clips; i++ {
		clip := models.Clip{
			Timestamp: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
			Event:     "Recent",
			SourceDir: fmt.Sprintf("RecentClips/%d", i),
		}
		if err := db.Create(&clip).Error; err != nil {
			t.Fatalf("clip: %v", err)
		}
		tel := models.Telemetry{ClipID: clip.ID, Speed: 1, FullDataJson: blob}
		if err := db.Create(&tel).Error; err != nil {
			t.Fatalf("telemetry: %v", err)
		}
	}

	svc := &RetentionService{DB: db}
	report := &RetentionReport{Errors: []string{}}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	svc.recoverOrphanTelemetry(report)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if len(report.Errors) != 0 {
		t.Fatalf("sweep errors: %v", report.Errors)
	}
	delta := after.TotalAlloc - before.TotalAlloc
	t.Logf("orphan sweep alloc with %d×1MiB payloads and 0 orphans: %d bytes", clips, delta)
	// Round 3 loaded every payload (~67MB). The id query must stay far below that.
	const bound = 8 << 20
	if delta > bound {
		t.Fatalf("sweep allocated %d bytes, want <= %d", delta, bound)
	}
	var n int
	db.Unscoped().Model(&models.Telemetry{}).Count(&n)
	if n != clips {
		t.Fatalf("telemetry count=%d want %d (0 orphans)", n, clips)
	}
}

func TestRetentionOrphanSweepKeepsSoftDeletedClip(t *testing.T) {
	db := setupRetentionTestDB(t)
	root := t.TempDir()
	cfgPath := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	path := root + "/RecentClips/keep.mp4"
	writeFixtureFile(t, path, 128, now.Add(-time.Hour))
	clip := seedClip(t, db, "Recent", now.AddDate(0, 0, -10), false, root+"/RecentClips", map[string]string{"Front": path})
	if err := db.Delete(&clip).Error; err != nil {
		t.Fatalf("soft-delete clip: %v", err)
	}
	orphan := models.Telemetry{ClipID: 99999, Speed: 7, FullDataJson: "orphan"}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatalf("orphan: %v", err)
	}

	svc := newTestRetention(t, db, root, cfgPath, RetentionConfig{
		Enabled: true, DryRun: false, MaxAgeDays: 0, MaxSizeGB: 0, MinFileAgeMinutes: 0,
	})
	report, err := svc.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("errors: %v", report.Errors)
	}
	var n int
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", clip.ID).Count(&n)
	if n != 1 {
		t.Fatalf("soft-deleted clip telemetry must remain, count=%d", n)
	}
	db.Unscoped().Model(&models.Telemetry{}).Where("clip_id = ?", 99999).Count(&n)
	if n != 0 {
		t.Fatalf("orphan telemetry must be removed, count=%d", n)
	}
	db.Unscoped().Model(&models.Clip{}).Where("id = ?", clip.ID).Count(&n)
	if n != 1 {
		t.Fatalf("soft-deleted clip must not be resurrected or dropped, count=%d", n)
	}
}
