package filter

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	defaultDedupCapacity = 10000
	defaultDedupTTL      = 24 * time.Hour
	dedupShingleSize     = 3
	dedupJaccardMin      = 0.80
	dedupMaxTokens       = 120
	authorRecentLimit    = 8
	globalRecentLimit    = 128
)

// Deduper keeps a bounded, TTL-limited cache of exact fingerprints and shingle
// sets. Exact matches and >=80% Jaccard similarity per author/global are
// treated as duplicates. All access is mutex-protected.
type Deduper struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time

	entries  map[string]*dedupEntry
	lru      *list.List
	byAuthor map[int64][]string
	global   []string
}

type dedupEntry struct {
	fingerprint string
	authorID    int64
	shingles    map[uint64]struct{}
	created     time.Time
}

func newDeduper(capacity int, ttl time.Duration, now func() time.Time) *Deduper {
	if capacity <= 0 {
		capacity = defaultDedupCapacity
	}
	if ttl <= 0 {
		ttl = defaultDedupTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Deduper{
		capacity: capacity,
		ttl:      ttl,
		now:      now,
		entries:  make(map[string]*dedupEntry),
		lru:      list.New(),
		byAuthor: make(map[int64][]string),
	}
}

// Fingerprint returns a stable 128-bit hex digest of normalized text.
func Fingerprint(norm string) string {
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:16])
}

// Fingerprint exposes the deduper's canonical fingerprint helper.
func (d *Deduper) Fingerprint(norm string) string {
	return Fingerprint(norm)
}

// Check reports whether the message duplicates a recent one. When dryRun is
// false and the message is new, it is recorded; dryRun never mutates state.
func (d *Deduper) Check(fingerprint string, tokens []string, authorID int64, dryRun bool) bool {
	if fingerprint == "" {
		return false
	}
	now := d.now()
	shingles := buildShingles(tokens)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.evictExpired(now)

	if entry, ok := d.entries[fingerprint]; ok && !d.expired(entry, now) {
		return true
	}
	for _, recent := range d.byAuthor[authorID] {
		entry, ok := d.entries[recent]
		if !ok || d.expired(entry, now) {
			continue
		}
		if jaccard(shingles, entry.shingles) >= dedupJaccardMin {
			return true
		}
	}
	for _, recent := range d.global {
		entry, ok := d.entries[recent]
		if !ok || entry.authorID == authorID || d.expired(entry, now) {
			continue
		}
		if jaccard(shingles, entry.shingles) >= dedupJaccardMin {
			return true
		}
	}
	if dryRun {
		return false
	}
	d.addLocked(fingerprint, authorID, shingles, now)
	return false
}

func (d *Deduper) addLocked(fingerprint string, authorID int64, shingles map[uint64]struct{}, now time.Time) {
	entry := &dedupEntry{
		fingerprint: fingerprint,
		authorID:    authorID,
		shingles:    shingles,
		created:     now,
	}
	d.lru.PushFront(entry)
	d.entries[fingerprint] = entry

	d.byAuthor[authorID] = append(d.byAuthor[authorID], fingerprint)
	if len(d.byAuthor[authorID]) > authorRecentLimit {
		d.byAuthor[authorID] = d.byAuthor[authorID][len(d.byAuthor[authorID])-authorRecentLimit:]
	}
	d.global = append(d.global, fingerprint)
	if len(d.global) > globalRecentLimit {
		d.global = d.global[len(d.global)-globalRecentLimit:]
	}

	for len(d.entries) > d.capacity {
		oldest := d.lru.Back()
		if oldest == nil {
			break
		}
		evicted := oldest.Value.(*dedupEntry)
		d.lru.Remove(oldest)
		d.removeEntryLocked(evicted)
	}
}

func (d *Deduper) removeEntryLocked(entry *dedupEntry) {
	delete(d.entries, entry.fingerprint)
	bucket := d.byAuthor[entry.authorID]
	for i, fingerprint := range bucket {
		if fingerprint != entry.fingerprint {
			continue
		}
		bucket = append(bucket[:i], bucket[i+1:]...)
		break
	}
	if len(bucket) == 0 {
		delete(d.byAuthor, entry.authorID)
		return
	}
	d.byAuthor[entry.authorID] = bucket
}

func (d *Deduper) evictExpired(now time.Time) {
	for {
		oldest := d.lru.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*dedupEntry)
		if !d.expired(entry, now) {
			break
		}
		d.lru.Remove(oldest)
		d.removeEntryLocked(entry)
	}
}

func (d *Deduper) expired(entry *dedupEntry, now time.Time) bool {
	return now.After(entry.created.Add(d.ttl))
}

// Len exposes the current cached entry count (bounded by capacity).
func (d *Deduper) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}

func buildShingles(tokens []string) map[uint64]struct{} {
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) > dedupMaxTokens {
		tokens = tokens[:dedupMaxTokens]
	}
	size := dedupShingleSize
	if len(tokens) < size {
		size = 1
	}
	out := make(map[uint64]struct{}, len(tokens)-size+1)
	for i := 0; i+size <= len(tokens); i++ {
		out[shingleHash(tokens[i:i+size])] = struct{}{}
	}
	return out
}

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

func shingleHash(tokens []string) uint64 {
	h := uint64(fnvOffset64)
	for _, token := range tokens {
		for i := 0; i < len(token); i++ {
			h ^= uint64(token[i])
			h *= fnvPrime64
		}
		h ^= 0
		h *= fnvPrime64
	}
	return h
}

func jaccard(a, b map[uint64]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	intersection := 0
	for hash := range a {
		if _, ok := b[hash]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
