package hlsmouflon

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	hlsDownloadWorkers = 4
	hlsDownloadQueue   = 128
)

var (
	hlsPendingGapWait  = 300 * time.Millisecond
	hlsObservedGapWait = 20 * time.Second
	hlsRetryBase       = 300 * time.Millisecond
	hlsRetryMax        = 2 * time.Second
	hlsLiveEdgeHold    = 1
	hlsLiveEdgeMaxWait = 4 * time.Second
)

type hlsSegmentKey struct {
	msn  int
	part int
}

func (k hlsSegmentKey) less(o hlsSegmentKey) bool {
	if k.msn != o.msn {
		return k.msn < o.msn
	}
	return k.part < o.part
}

type hlsSegmentRef struct {
	key     hlsSegmentKey
	url     string
	partial bool
}

type hlsWritableSegment struct {
	key  hlsSegmentKey
	body []byte
}

type hlsSegmentStats struct {
	written          int
	gaps             int
	suspectedMissed  int
	suspectedTotal   int
	discovered       int
	downloadFailures int
	retrySuccess     int
	queued           int
	writeWaits       int
	currentMSN       int
	lastSeenMSN      int
	windowMinMSN     int
	windowMaxMSN     int
	windowSegments   int
	liveLagMSN       int
	maxDownloadMs    int
}

type hlsSegmentFetcher func(url string) ([]byte, error)

type hlsDownloadJob struct {
	generation int
	ref        hlsSegmentRef
}

type hlsDownloadResult struct {
	generation int
	ref        hlsSegmentRef
	body       []byte
	err        error
	elapsedMs  int
}

type hlsSegmentScheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	fetch  hlsSegmentFetcher

	jobs    chan hlsDownloadJob
	results chan hlsDownloadResult
	wg      sync.WaitGroup

	mu         sync.Mutex
	generation int
	known      map[hlsSegmentKey]hlsSegmentRef
	inflight   map[hlsSegmentKey]bool
	ready      map[hlsSegmentKey]hlsWritableSegment
	finished   map[hlsSegmentKey]bool
	missing    map[hlsSegmentKey]time.Time
	attempts   map[hlsSegmentKey]int
	retryAt    map[hlsSegmentKey]time.Time

	hasLast     bool
	lastWritten hlsSegmentKey
	gapSince    time.Time
	edgeSince   time.Time
	edgeHoldKey hlsSegmentKey
	stats       hlsSegmentStats
}

func parseMouflonSegments(body []byte, decode func(string) (string, bool)) ([]hlsSegmentRef, int) {
	byKey := make(map[hlsSegmentKey]hlsSegmentRef)
	var failedDecode int
	for _, l := range strings.Split(string(body), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "#EXT-X-MOUFLON:URI:") {
			continue
		}
		encURL := strings.TrimPrefix(l, "#EXT-X-MOUFLON:URI:")
		m := segPartRe.FindStringSubmatch(encURL)
		if m == nil {
			continue
		}
		msn, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		part := 0
		if m[3] != "" {
			if part, err = strconv.Atoi(m[3]); err != nil {
				continue
			}
		}
		realHash, ok := decode(m[2])
		if !ok {
			failedDecode++
			continue
		}
		key := hlsSegmentKey{msn: msn, part: part}
		ref := hlsSegmentRef{
			key:     key,
			url:     strings.Replace(encURL, m[2], realHash, 1),
			partial: m[3] != "",
		}
		if existing, ok := byKey[key]; ok {
			if existing.partial && !ref.partial {
				byKey[key] = ref
			}
			continue
		}
		byKey[key] = ref
	}
	segs := make([]hlsSegmentRef, 0, len(byKey))
	for _, seg := range byKey {
		segs = append(segs, seg)
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].key.less(segs[j].key) })
	return segs, failedDecode
}

func preferCompleteMouflonSegments(segs []hlsSegmentRef) ([]hlsSegmentRef, int, int) {
	fullCount, partCount := 0, 0
	for _, seg := range segs {
		if seg.partial {
			partCount++
		} else {
			fullCount++
		}
	}
	if fullCount == 0 {
		return segs, fullCount, partCount
	}
	full := make([]hlsSegmentRef, 0, fullCount)
	for _, seg := range segs {
		if !seg.partial {
			full = append(full, seg)
		}
	}
	return full, fullCount, partCount
}

func newHLSSegmentScheduler(ctx context.Context, fetch hlsSegmentFetcher) *hlsSegmentScheduler {
	workerCtx, cancel := context.WithCancel(ctx)
	s := &hlsSegmentScheduler{
		ctx:      workerCtx,
		cancel:   cancel,
		fetch:    fetch,
		jobs:     make(chan hlsDownloadJob, hlsDownloadQueue),
		results:  make(chan hlsDownloadResult, hlsDownloadQueue),
		known:    make(map[hlsSegmentKey]hlsSegmentRef),
		inflight: make(map[hlsSegmentKey]bool),
		ready:    make(map[hlsSegmentKey]hlsWritableSegment),
		finished: make(map[hlsSegmentKey]bool),
		missing:  make(map[hlsSegmentKey]time.Time),
		attempts: make(map[hlsSegmentKey]int),
		retryAt:  make(map[hlsSegmentKey]time.Time),
	}
	for i := 0; i < hlsDownloadWorkers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

func (s *hlsSegmentScheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.jobs:
			t0 := time.Now()
			body, err := s.fetch(job.ref.url)
			res := hlsDownloadResult{
				generation: job.generation,
				ref:        job.ref,
				body:       body,
				err:        err,
				elapsedMs:  int(time.Since(t0).Milliseconds()),
			}
			select {
			case s.results <- res:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *hlsSegmentScheduler) stop() {
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (s *hlsSegmentScheduler) reset() {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.known = make(map[hlsSegmentKey]hlsSegmentRef)
	s.inflight = make(map[hlsSegmentKey]bool)
	s.ready = make(map[hlsSegmentKey]hlsWritableSegment)
	s.finished = make(map[hlsSegmentKey]bool)
	s.missing = make(map[hlsSegmentKey]time.Time)
	s.attempts = make(map[hlsSegmentKey]int)
	s.retryAt = make(map[hlsSegmentKey]time.Time)
	s.hasLast = false
	s.lastWritten = hlsSegmentKey{}
	s.gapSince = time.Time{}
	s.stats = hlsSegmentStats{}
}

func (s *hlsSegmentScheduler) nextRequestMSN() int {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasLast {
		if s.stats.lastSeenMSN > 0 {
			return s.stats.lastSeenMSN + 1
		}
		return 0
	}
	expected := s.lastWritten.msn + 1
	if expected <= 0 {
		return 0
	}
	if !s.hasRealMSNLocked(expected) {
		return expected
	}
	if s.stats.lastSeenMSN >= expected {
		return s.stats.lastSeenMSN + 1
	}
	return expected
}

func (s *hlsSegmentScheduler) nextLiveEdgeMSN() int {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stats.lastSeenMSN > 0 {
		return s.stats.lastSeenMSN + 1
	}
	if s.hasLast {
		return s.lastWritten.msn + 1
	}
	return 0
}

func (s *hlsSegmentScheduler) targetProbeMSNs(limit int) []int {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		return nil
	}
	seen := make(map[int]bool)
	add := func(out []int, msn int) []int {
		if msn <= 0 || seen[msn] || len(out) >= limit {
			return out
		}
		seen[msn] = true
		return append(out, msn)
	}

	var out []int
	if s.hasLast {
		expected := s.lastWritten.msn + 1
		if !s.hasRealMSNLocked(expected) {
			out = add(out, expected)
		}
	}

	missingKeys := make([]hlsSegmentKey, 0, len(s.missing))
	for key := range s.missing {
		if s.finished[key] {
			continue
		}
		if s.hasLast && !s.lastWritten.less(key) {
			continue
		}
		missingKeys = append(missingKeys, key)
	}
	sort.Slice(missingKeys, func(i, j int) bool { return missingKeys[j].less(missingKeys[i]) })
	for _, key := range missingKeys {
		out = add(out, key.msn)
	}
	return out
}

func (s *hlsSegmentScheduler) add(segs []hlsSegmentRef) {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updatePlaylistWindowLocked(segs)
	s.addSegmentsLocked(segs)
	s.scheduleLocked(time.Now())
}

func (s *hlsSegmentScheduler) addTarget(segs []hlsSegmentRef) {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addSegmentsLocked(segs)
	s.scheduleLocked(time.Now())
}

func (s *hlsSegmentScheduler) addSegmentsLocked(segs []hlsSegmentRef) {
	for _, seg := range segs {
		if seg.key.msn <= 0 || s.finished[seg.key] {
			continue
		}
		if seg.key.msn > s.stats.lastSeenMSN {
			s.stats.lastSeenMSN = seg.key.msn
		}
		if s.hasLast && !s.lastWritten.less(seg.key) {
			s.finished[seg.key] = true
			continue
		}
		delete(s.missing, seg.key)
		if _, ok := s.known[seg.key]; !ok {
			if _, ok := s.ready[seg.key]; !ok && !s.inflight[seg.key] {
				s.stats.discovered++
			}
		}
		s.known[seg.key] = seg
	}
}

func (s *hlsSegmentScheduler) updatePlaylistWindowLocked(segs []hlsSegmentRef) {
	s.stats.windowSegments = len(segs)
	if len(segs) == 0 {
		s.stats.windowMinMSN = 0
		s.stats.windowMaxMSN = 0
		return
	}
	prevLastSeen := s.stats.lastSeenMSN
	minMSN, maxMSN := segs[0].key.msn, segs[0].key.msn
	for _, seg := range segs[1:] {
		if seg.key.msn < minMSN {
			minMSN = seg.key.msn
		}
		if seg.key.msn > maxMSN {
			maxMSN = seg.key.msn
		}
	}
	s.stats.windowMinMSN = minMSN
	s.stats.windowMaxMSN = maxMSN
	if prevLastSeen > 0 && minMSN > prevLastSeen+1 {
		missed := minMSN - prevLastSeen - 1
		s.stats.suspectedMissed += missed
		s.stats.suspectedTotal += missed
		s.markMissingRangeLocked(prevLastSeen+1, minMSN, time.Now())
	}
}

func (s *hlsSegmentScheduler) takeWritable(now time.Time) []hlsWritableSegment {
	return s.takeWritableInternal(now, false)
}

func (s *hlsSegmentScheduler) takeWritableFinal(now time.Time) []hlsWritableSegment {
	return s.takeWritableInternal(now, true)
}

func (s *hlsSegmentScheduler) takeWritableInternal(now time.Time, force bool) []hlsWritableSegment {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []hlsWritableSegment
	for {
		candidate, ok := s.firstReadyLocked()
		if !ok {
			break
		}
		if s.hasLast && !s.lastWritten.less(candidate.key) {
			s.finishLocked(candidate.key)
			continue
		}
		if !s.canAdvanceLocked(candidate.key, now, force) {
			break
		}
		seg := s.ready[candidate.key]
		s.finishLocked(candidate.key)
		s.hasLast = true
		s.lastWritten = candidate.key
		s.gapSince = time.Time{}
		s.stats.written++
		s.stats.currentMSN = candidate.key.msn
		out = append(out, seg)
		s.pruneFinishedLocked()
	}
	s.scheduleLocked(now)
	return out
}

func (s *hlsSegmentScheduler) snapshot(resetPeriod bool) hlsSegmentStats {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.queued = s.queueLenLocked()
	if s.hasLast && st.lastSeenMSN >= s.lastWritten.msn {
		st.liveLagMSN = st.lastSeenMSN - s.lastWritten.msn
	}
	if resetPeriod {
		s.stats.discovered = 0
		s.stats.suspectedMissed = 0
		s.stats.maxDownloadMs = 0
	}
	return st
}

func (s *hlsSegmentScheduler) collectResults() {
	for {
		select {
		case res := <-s.results:
			s.handleResult(res)
		default:
			return
		}
	}
}

func (s *hlsSegmentScheduler) handleResult(res hlsDownloadResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if res.generation != s.generation {
		return
	}
	key := res.ref.key
	delete(s.inflight, key)
	if s.finished[key] {
		return
	}
	if res.elapsedMs > s.stats.maxDownloadMs {
		s.stats.maxDownloadMs = res.elapsedMs
	}
	if res.err != nil {
		s.stats.downloadFailures++
		delay := time.Duration(s.attempts[key]) * hlsRetryBase
		if delay < hlsRetryBase {
			delay = hlsRetryBase
		}
		if delay > hlsRetryMax {
			delay = hlsRetryMax
		}
		s.retryAt[key] = time.Now().Add(delay)
		s.scheduleLocked(time.Now())
		return
	}
	if s.attempts[key] > 1 {
		s.stats.retrySuccess++
	}
	delete(s.retryAt, key)
	s.ready[key] = hlsWritableSegment{key: key, body: res.body}
	s.scheduleLocked(time.Now())
}

func (s *hlsSegmentScheduler) scheduleLocked(now time.Time) {
	keys := make([]hlsSegmentKey, 0, len(s.known))
	for key := range s.known {
		if s.finished[key] || s.inflight[key] {
			continue
		}
		if _, ok := s.ready[key]; ok {
			continue
		}
		if t, ok := s.retryAt[key]; ok && now.Before(t) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	for _, key := range keys {
		select {
		case <-s.ctx.Done():
			return
		case s.jobs <- hlsDownloadJob{generation: s.generation, ref: s.known[key]}:
			s.inflight[key] = true
			s.attempts[key]++
		default:
			return
		}
	}
}

func (s *hlsSegmentScheduler) firstReadyLocked() (hlsWritableSegment, bool) {
	keys := make([]hlsSegmentKey, 0, len(s.ready))
	for key := range s.ready {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return hlsWritableSegment{}, false
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	return s.ready[keys[0]], true
}

func (s *hlsSegmentScheduler) canAdvanceLocked(candidate hlsSegmentKey, now time.Time, force bool) bool {
	if s.hasLast && candidate.msn > s.lastWritten.msn+1 {
		s.markMissingRangeLocked(s.lastWritten.msn+1, candidate.msn, now)
	}
	hasPendingBefore, hasObservedPendingBefore := s.pendingBeforeLocked(candidate)
	if force {
		if hasPendingBefore {
			s.stats.gaps += s.skipBeforeLocked(candidate)
		}
		s.edgeSince = time.Time{}
		return true
	}
	if !hasPendingBefore {
		return s.canPassLiveEdgeLocked(candidate, now)
	}

	gapWait := s.pendingGapWaitLocked(candidate, hasObservedPendingBefore)
	if !s.gapSince.IsZero() && now.Sub(s.gapSince) >= gapWait {
		s.stats.gaps += s.skipBeforeLocked(candidate)
		s.gapSince = time.Time{}
		return s.canPassLiveEdgeLocked(candidate, now)
	}
	if s.gapSince.IsZero() {
		s.gapSince = now
		s.stats.writeWaits++
	}
	return false
}

func (s *hlsSegmentScheduler) pendingGapWaitLocked(candidate hlsSegmentKey, observedPending bool) time.Duration {
	if observedPending {
		return hlsObservedGapWait
	}
	wait := hlsPendingGapWait
	if s.stats.lastSeenMSN <= candidate.msn {
		return wait
	}
	lag := s.stats.lastSeenMSN - candidate.msn
	if lag >= 8 {
		return 50 * time.Millisecond
	}
	if lag >= 5 {
		return 100 * time.Millisecond
	}
	if lag >= 3 {
		return 150 * time.Millisecond
	}
	return wait
}

func (s *hlsSegmentScheduler) markMissingRangeLocked(startMSN, endMSN int, now time.Time) {
	for msn := startMSN; msn < endMSN; msn++ {
		key := hlsSegmentKey{msn: msn}
		if s.finished[key] || s.hasRealMSNLocked(msn) {
			continue
		}
		if _, ok := s.missing[key]; !ok {
			s.missing[key] = now
		}
	}
}

func (s *hlsSegmentScheduler) canPassLiveEdgeLocked(candidate hlsSegmentKey, now time.Time) bool {
	if !s.hasLast || hlsLiveEdgeHold <= 0 || s.stats.lastSeenMSN <= 0 {
		s.edgeSince = time.Time{}
		return true
	}
	if s.stats.lastSeenMSN-candidate.msn >= hlsLiveEdgeHold {
		s.edgeSince = time.Time{}
		return true
	}
	if s.edgeSince.IsZero() || s.edgeHoldKey != candidate {
		s.edgeSince = now
		s.edgeHoldKey = candidate
		s.stats.writeWaits++
		return false
	}
	if now.Sub(s.edgeSince) >= hlsLiveEdgeMaxWait {
		s.edgeSince = time.Time{}
		return true
	}
	return false
}

func (s *hlsSegmentScheduler) pendingBeforeLocked(candidate hlsSegmentKey) (bool, bool) {
	hasMissing := false
	for key := range s.missing {
		if s.finished[key] {
			continue
		}
		if s.hasLast && !s.lastWritten.less(key) {
			continue
		}
		if key.less(candidate) {
			hasMissing = true
			break
		}
	}
	for key := range s.known {
		if s.finished[key] {
			continue
		}
		if s.hasLast && !s.lastWritten.less(key) {
			continue
		}
		if key.less(candidate) {
			return true, true
		}
	}
	return hasMissing, false
}

func (s *hlsSegmentScheduler) hasRealMSNLocked(msn int) bool {
	for key := range s.known {
		if key.msn == msn && !s.finished[key] {
			return true
		}
	}
	for key := range s.ready {
		if key.msn == msn && !s.finished[key] {
			return true
		}
	}
	for key := range s.inflight {
		if key.msn == msn && !s.finished[key] {
			return true
		}
	}
	return false
}

func (s *hlsSegmentScheduler) skipBeforeLocked(candidate hlsSegmentKey) int {
	skipped := 0
	for key := range s.missing {
		if s.hasLast && !s.lastWritten.less(key) {
			s.finishLocked(key)
			continue
		}
		if key.less(candidate) {
			s.finishLocked(key)
			skipped++
		}
	}
	for key := range s.known {
		if s.hasLast && !s.lastWritten.less(key) {
			s.finishLocked(key)
			continue
		}
		if key.less(candidate) {
			s.finishLocked(key)
			skipped++
		}
	}
	return skipped
}

func (s *hlsSegmentScheduler) finishLocked(key hlsSegmentKey) {
	s.finished[key] = true
	delete(s.known, key)
	delete(s.ready, key)
	delete(s.inflight, key)
	delete(s.missing, key)
	delete(s.retryAt, key)
}

func (s *hlsSegmentScheduler) queueLenLocked() int {
	n := len(s.jobs) + len(s.inflight) + len(s.ready)
	for key := range s.known {
		if !s.finished[key] && !s.inflight[key] {
			if _, ok := s.ready[key]; !ok {
				n++
			}
		}
	}
	n += len(s.missing)
	return n
}

func (s *hlsSegmentScheduler) pruneFinishedLocked() {
	if !s.hasLast {
		return
	}
	for key := range s.finished {
		if key.msn < s.lastWritten.msn-120 {
			delete(s.finished, key)
			delete(s.attempts, key)
			delete(s.missing, key)
		}
	}
}
