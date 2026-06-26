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
	hlsMissingGapWait = 3 * time.Second
	hlsPendingGapWait = 12 * time.Second
	hlsRetryBase      = 300 * time.Millisecond
	hlsRetryMax       = 2 * time.Second
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
	key hlsSegmentKey
	url string
}

type hlsWritableSegment struct {
	key  hlsSegmentKey
	body []byte
}

type hlsSegmentStats struct {
	written          int
	gaps             int
	downloadFailures int
	retrySuccess     int
	queued           int
	writeWaits       int
	currentMSN       int
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
	attempts   map[hlsSegmentKey]int
	retryAt    map[hlsSegmentKey]time.Time

	hasLast     bool
	lastWritten hlsSegmentKey
	gapSince    time.Time
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
		if _, ok := byKey[key]; ok {
			continue
		}
		byKey[key] = hlsSegmentRef{
			key: key,
			url: strings.Replace(encURL, m[2], realHash, 1),
		}
	}
	segs := make([]hlsSegmentRef, 0, len(byKey))
	for _, seg := range byKey {
		segs = append(segs, seg)
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].key.less(segs[j].key) })
	return segs, failedDecode
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
	s.attempts = make(map[hlsSegmentKey]int)
	s.retryAt = make(map[hlsSegmentKey]time.Time)
	s.hasLast = false
	s.lastWritten = hlsSegmentKey{}
	s.gapSince = time.Time{}
	s.stats = hlsSegmentStats{}
}

func (s *hlsSegmentScheduler) add(segs []hlsSegmentRef) {
	s.collectResults()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range segs {
		if seg.key.msn <= 0 || s.finished[seg.key] {
			continue
		}
		if s.hasLast && !s.lastWritten.less(seg.key) {
			s.finished[seg.key] = true
			continue
		}
		s.known[seg.key] = seg
	}
	s.scheduleLocked(time.Now())
}

func (s *hlsSegmentScheduler) takeWritable(now time.Time) []hlsWritableSegment {
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
		if !s.canAdvanceLocked(candidate.key, now) {
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
	if resetPeriod {
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

func (s *hlsSegmentScheduler) canAdvanceLocked(candidate hlsSegmentKey, now time.Time) bool {
	hasPendingBefore := s.hasPendingBeforeLocked(candidate)
	hasMissingGap := s.hasLast && candidate.msn > s.lastWritten.msn+1
	if !hasPendingBefore && !hasMissingGap {
		return true
	}

	waitLimit := hlsMissingGapWait
	if hasPendingBefore {
		waitLimit = hlsPendingGapWait
	}
	if !s.gapSince.IsZero() && now.Sub(s.gapSince) >= waitLimit {
		if hasMissingGap {
			s.stats.gaps += candidate.msn - s.lastWritten.msn - 1
		}
		s.skipBeforeLocked(candidate)
		s.gapSince = time.Time{}
		return true
	}
	if s.gapSince.IsZero() {
		s.gapSince = now
		s.stats.writeWaits++
	}
	return false
}

func (s *hlsSegmentScheduler) hasPendingBeforeLocked(candidate hlsSegmentKey) bool {
	for key := range s.known {
		if s.finished[key] {
			continue
		}
		if s.hasLast && !s.lastWritten.less(key) {
			continue
		}
		if key.less(candidate) {
			return true
		}
	}
	return false
}

func (s *hlsSegmentScheduler) skipBeforeLocked(candidate hlsSegmentKey) {
	for key := range s.known {
		if s.hasLast && !s.lastWritten.less(key) {
			s.finishLocked(key)
			continue
		}
		if key.less(candidate) {
			s.finishLocked(key)
		}
	}
}

func (s *hlsSegmentScheduler) finishLocked(key hlsSegmentKey) {
	s.finished[key] = true
	delete(s.known, key)
	delete(s.ready, key)
	delete(s.inflight, key)
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
		}
	}
}
