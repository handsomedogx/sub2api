package cyberevidence

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrUnavailable = errors.New("cyber evidence unavailable")
var ErrNotFound = errors.New("cyber evidence not found")

type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}
type Config struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
	ParseLimitMiB int  `json:"parse_limit_mib"`
	BodyLimitMiB  int  `json:"body_limit_mib"`
}

func DefaultConfig() Config {
	return Config{Enabled: true, RetentionDays: 7, ParseLimitMiB: 16, BodyLimitMiB: 32}
}
func (c Config) Valid() bool {
	return c.RetentionDays >= 1 && c.RetentionDays <= 30 && c.ParseLimitMiB >= 0 && c.ParseLimitMiB <= 64 && c.BodyLimitMiB >= 0 && c.BodyLimitMiB <= 64
}
func (c Config) bodyLimit() int {
	if c.BodyLimitMiB == 0 {
		return 32 << 20
	}
	return c.BodyLimitMiB << 20
}
func (c Config) parseLimit() int {
	if c.ParseLimitMiB <= 0 {
		return MaxParseBytes
	}
	return c.ParseLimitMiB * 1024 * 1024
}

type preparationLease struct {
	once    sync.Once
	release func()
	payload *payloadReservation
}

const retainedPayloadBudget = 128 << 20

type payloadReservation struct {
	refs    atomic.Int64
	bytes   int64
	service *Service
}

func (r *payloadReservation) retain() bool {
	for {
		n := r.refs.Load()
		if n <= 0 {
			return false
		}
		if r.refs.CompareAndSwap(n, n+1) {
			return true
		}
	}
}
func (r *payloadReservation) release() {
	if r.refs.Add(-1) == 0 {
		r.service.retainedBytes.Add(-r.bytes)
	}
}
func (s *Service) reservePayload(bytes int64) *payloadReservation {
	for {
		used := s.retainedBytes.Load()
		if bytes > retainedPayloadBudget-used {
			return nil
		}
		if s.retainedBytes.CompareAndSwap(used, used+bytes) {
			r := &payloadReservation{bytes: bytes, service: s}
			r.refs.Store(1)
			return r
		}
	}
}

// Release frees the bounded pre-queue reservation, including when the existing
// moderation scope declines the event or the parent audit insert fails.
func (p Prepared) Release() {
	if p.lease != nil {
		p.lease.once.Do(p.lease.release)
	}
}

type Prepared struct {
	lease            *preparationLease
	Ciphertext       string
	Status           string
	Truncated        bool
	RedactionVersion string
	OriginalBytes    int
	CapturedAt       time.Time
}
type Record struct {
	LogID      int64      `json:"log_id"`
	UserID     *int64     `json:"user_id,omitempty"`
	RequestID  string     `json:"request_id"`
	Endpoint   string     `json:"endpoint"`
	Model      string     `json:"model"`
	OccurredAt time.Time  `json:"occurred_at"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Ciphertext string     `json:"-"`
	Payload    *Payload   `json:"payload,omitempty"`
}

type Store interface {
	LoadConfig(context.Context) (Config, error)
	SaveConfig(context.Context, Config) error
	Save(context.Context, int64, Prepared, int) error
	Status(context.Context, int64, string) error
	Get(context.Context, int64) (Record, error)
	Delete(context.Context, int64) error
	Cleanup(context.Context, int) (int64, error)
}

type Stats struct {
	Queued            uint64 `json:"queued"`
	Saved             uint64 `json:"saved"`
	Failed            uint64 `json:"failed"`
	Dropped           uint64 `json:"dropped"`
	StatusWriteFailed uint64 `json:"status_write_failed"`
	CleanupFailed     uint64 `json:"cleanup_failed"`
	ConfigLoadFailed  uint64 `json:"config_load_failed"`
	EncryptionReady   bool   `json:"encryption_ready"`
	QueueDepth        int    `json:"queue_depth"`
	QueueCapacity     int    `json:"queue_capacity"`
}
type job struct {
	id int64
	p  Prepared
}

func (j job) release() {
	if j.p.lease != nil && j.p.lease.payload != nil {
		j.p.lease.payload.release()
	}
}

type Service struct {
	pendingPreparations                                                       chan struct{}
	store                                                                     Store
	cipher                                                                    Cipher
	ready                                                                     bool
	config                                                                    atomic.Pointer[Config]
	retainedBytes                                                             atomic.Int64
	reads                                                                     chan struct{}
	queue                                                                     chan job
	admission                                                                 chan struct{}
	stop                                                                      chan struct{}
	done                                                                      chan struct{}
	once                                                                      sync.Once
	mu                                                                        sync.RWMutex
	closed                                                                    bool
	queued, saved, failed, dropped, statusFailed, cleanupFailed, configFailed atomic.Uint64
}

func New(store Store, cipher Cipher, ready bool) *Service {
	s := &Service{reads: make(chan struct{}, 2), pendingPreparations: make(chan struct{}, 64), store: store, cipher: cipher, ready: ready && cipher != nil, queue: make(chan job, 64), admission: make(chan struct{}, 2), stop: make(chan struct{}), done: make(chan struct{})}
	c := DefaultConfig()
	s.config.Store(&c)
	// All configuration reads have deadlines; an unavailable store disables capture.
	s.refresh()
	go s.run()
	return s
}
func (s *Service) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := s.store.LoadConfig(ctx)
	if err != nil || !c.Valid() {
		s.configFailed.Add(1)
		c = DefaultConfig()
		c.Enabled = false
	}
	s.config.Store(&c)
}
func (s *Service) Config() Config { return *s.config.Load() }
func (s *Service) UpdateConfig(ctx context.Context, c Config) error {
	if !c.Valid() {
		return errors.New("retention_days must be 1..30; parse_limit_mib and body_limit_mib must be 1..64")
	}
	if err := s.store.SaveConfig(ctx, c); err != nil {
		return ErrUnavailable
	}
	s.config.Store(&c)
	return nil
}
func (s *Service) Prepare(body []byte, reason string, upstreamStatus ...int) Prepared {
	return s.prepare(body, reason, "", nil, upstreamStatus...)
}

// PrepareWithSession also records the explicit sanitized session reference ID.
func (s *Service) PrepareWithSession(body []byte, reason, sessionID string, upstreamStatus ...int) Prepared {
	return s.prepare(body, reason, sessionID, nil, upstreamStatus...)
}

// PrepareRejection retains only the input already held by the gateway and the
// upstream rejection body/event. It does not start general traffic capture.
type UpstreamRejection struct {
	Body          []byte
	OriginalBytes int
	Status        int
}

func (s *Service) PrepareRejection(body []byte, reason, sessionID string, upstream UpstreamRejection) Prepared {
	return s.prepare(body, reason, sessionID, &upstream, upstream.Status)
}

func (s *Service) prepare(body []byte, reason, sessionID string, upstream *UpstreamRejection, upstreamStatus ...int) Prepared {
	p := Prepared{Status: "disabled", CapturedAt: time.Now().UTC()}
	if !s.Config().Enabled {
		return p
	}
	if !s.ready {
		p.Status = "encryption_unavailable"
		return p
	}
	select {
	case s.pendingPreparations <- struct{}{}:
		lease := &preparationLease{}
		lease.release = func() {
			<-s.pendingPreparations
			if lease.payload != nil {
				lease.payload.release()
			}
		}
		p.lease = lease
	default:
		p.Status = "capture_busy"
		s.dropped.Add(1)
		return p
	}
	keepLease := false
	defer func() {
		if !keepLease {
			p.Release()
		}
	}()
	select {
	case s.admission <- struct{}{}:
		defer func() { <-s.admission }()
	default:
		p.Status = "capture_busy"
		s.dropped.Add(1)
		return p
	}
	payload := snapshotWithLimit(body, reason, sessionID, s.Config().parseLimit())
	payload.BodyRetentionVersion = 1
	payload.RequestBody, payload.RequestOmission = retainBody(body, s.Config().bodyLimit())
	payload.RequestBodyBytes = len(payload.RequestBody)
	payload.ResponseOmission = "body_unavailable"
	if upstream != nil {
		payload.ResponseBytes = max(len(upstream.Body), upstream.OriginalBytes)
		if payload.ResponseBytes > MaxRejectionBytes {
			payload.ResponseOmission = "body_storage_limit"
		} else {
			payload.UpstreamResponse, payload.ResponseOmission = retainBody(upstream.Body, MaxRejectionBytes)
		}
	}
	payload.ResponseScope = "upstream_rejection_body_or_event"
	if len(upstreamStatus) > 0 {
		payload.UpstreamHTTPStatus = upstreamStatus[0]
	}
	p.Truncated = payload.Truncated
	p.OriginalBytes = payload.OriginalBytes
	p.RedactionVersion = payload.RedactionVersion
	// Even without extractable input, retain the encrypted upstream reason,
	// original size and omission details so an audit never becomes a blank panel.
	raw, err := json.Marshal(payload)
	if err != nil {
		p.Status = "capture_failed"
		return p
	}
	p.lease.payload = s.reservePayload(int64(len(raw))*2 + 1024)
	if p.lease.payload == nil {
		p.Status = "capture_busy"
		s.dropped.Add(1)
		return p
	}
	ciphertext, err := s.cipher.Encrypt(string(raw))
	if err != nil || ciphertext == "" {
		p.Status = "encryption_failed"
		return p
	}
	p.Ciphertext = ciphertext
	p.Status = "pending"
	keepLease = true
	return p
}
func (s *Service) setStatus(id int64, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.store.Status(ctx, id, status) != nil {
		s.statusFailed.Add(1)
	}
}

// Enqueue receives only a redacted, encrypted payload; raw request bodies never
// enter the queue. The parent moderation row already exposes pending status.
func (s *Service) Enqueue(id int64, p Prepared) {
	defer p.Release()
	if id <= 0 {
		return
	}
	if p.Status != "pending" {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		s.dropped.Add(1)
		s.setStatus(id, "interrupted")
		return
	}
	if p.lease != nil && p.lease.payload != nil && !p.lease.payload.retain() {
		return
	}
	select {
	case s.queue <- job{id, p}:
		s.queued.Add(1)
	default:
		(job{p: p}).release()
		s.dropped.Add(1)
		s.setStatus(id, "queue_full")
	}
}
func (s *Service) write(j job) {
	defer j.release()
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.store.Save(ctx, j.id, j.p, s.Config().RetentionDays)
		cancel()
		if err == nil {
			s.saved.Add(1)
			return
		}
		if errors.Is(err, ErrNotFound) {
			s.failed.Add(1)
			return
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-s.stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				s.failed.Add(1)
				s.setStatus(j.id, "interrupted")
				return
			}
		}
	}
	s.failed.Add(1)
	s.setStatus(j.id, "storage_failed")
}

// markInterrupted records that a queued payload was not persisted. All status
// writes during one shutdown share a deadline so an unavailable database cannot
// stall process exit for the size of the queue.
func (s *Service) markInterrupted(id int64, deadline time.Time) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if s.store.Status(ctx, id, "interrupted") != nil {
		s.statusFailed.Add(1)
	}
}
func (s *Service) run() {
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var interruptDeadline time.Time
			for j := range s.queue {
				select {
				case <-s.stop:
					if interruptDeadline.IsZero() {
						interruptDeadline = time.Now().Add(time.Second)
					}
					s.dropped.Add(1)
					s.markInterrupted(j.id, interruptDeadline)
					j.release()
					continue
				default:
				}
				s.write(j)
			}
		}()
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			wg.Wait()
			return
		case <-ticker.C:
			s.refresh()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := s.store.Cleanup(ctx, s.Config().RetentionDays)
			cancel()
			if err != nil {
				s.cleanupFailed.Add(1)
			}
		}
	}
}
func (s *Service) Close() {
	s.once.Do(func() { s.mu.Lock(); s.closed = true; close(s.queue); close(s.stop); s.mu.Unlock() })
	<-s.done
}
func (s *Service) Stats() Stats {
	return Stats{Queued: s.queued.Load(), Saved: s.saved.Load(), Failed: s.failed.Load(), Dropped: s.dropped.Load(), StatusWriteFailed: s.statusFailed.Load(), CleanupFailed: s.cleanupFailed.Load(), ConfigLoadFailed: s.configFailed.Load(), EncryptionReady: s.ready, QueueDepth: len(s.queue), QueueCapacity: cap(s.queue)}
}
func (s *Service) Get(ctx context.Context, id int64) (Record, error) {
	select {
	case s.reads <- struct{}{}:
		defer func() { <-s.reads }()
	default:
		return Record{}, ErrUnavailable
	}
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "saved" {
		return r, nil
	}
	if !s.ready {
		return Record{}, ErrUnavailable
	}
	raw, err := s.cipher.Decrypt(r.Ciphertext)
	if err != nil {
		return Record{}, ErrUnavailable
	}
	var p Payload
	if json.Unmarshal([]byte(raw), &p) != nil {
		return Record{}, ErrUnavailable
	}
	r.Ciphertext = ""
	r.Payload = &p
	return r, nil
}
func (s *Service) Delete(ctx context.Context, id int64) error { return s.store.Delete(ctx, id) }
