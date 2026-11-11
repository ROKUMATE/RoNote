package importer

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/canon"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/store"
)

// Errors for URLs that are refused before anything is queued.
var (
	ErrInvalidURL        = errors.New("not an absolute URL with a host")
	ErrUnsupportedScheme = errors.New("only http and https URLs can be imported")
	ErrURLTooLong        = errors.New("URL is longer than 2048 bytes")
	ErrIPLiteralHost     = errors.New("the host must be a name, not an IP address")
)

// Quota bounds what one user may import (docs/adr/0010-sign-in-and-sessions.md).
type Quota struct {
	// ImportsPerDay counts imports started in any 24 hours.
	ImportsPerDay int
	// Documents counts documents, and imports that may still make one.
	Documents int
}

// quotaWindow is the span ImportsPerDay counts over.
const quotaWindow = 24 * time.Hour

// QuotaError is an import refused for being over a quota. RetryAfter is
// how long until the next import fits, or zero when waiting won't help.
type QuotaError struct {
	Quota      string
	RetryAfter time.Duration
}

func (e *QuotaError) Error() string {
	return "over the " + e.Quota + " quota"
}

// Service starts imports for the API.
type Service struct {
	store *store.Store
	queue jobs.Enqueuer
	quota Quota
}

// NewService returns a Service that records imports in s, within quota,
// and enqueues their jobs on queue.
func NewService(s *store.Store, queue jobs.Enqueuer, quota Quota) *Service {
	return &Service{store: s, queue: queue, quota: quota}
}

// Requested is what Request did.
type Requested struct {
	Import store.Import
	// Existing is true when the owner already had a document for the URL:
	// nothing was queued, and Import is the import that made the document.
	Existing bool
}

// Request starts an import of rawURL for owner, or returns the owner's
// existing document for it. A new import and its job are created in one
// transaction, so a job never runs for an import that was not saved. A new
// import over the owner's quota is a *QuotaError.
func (s *Service) Request(ctx context.Context, owner uuid.UUID, rawURL string) (Requested, error) {
	canonical, err := Validate(rawURL)
	if err != nil {
		return Requested{}, err
	}
	doc, err := s.store.FindOwnerDocumentForURL(ctx, owner, canonical)
	switch {
	case err == nil:
		imp, err := s.store.LatestImportForDocument(ctx, owner, doc)
		if err == nil {
			return Requested{Import: imp, Existing: true}, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return Requested{}, err
		}
	case !errors.Is(err, store.ErrNotFound):
		return Requested{}, err
	}

	var imp store.Import
	err = s.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
		if err := s.checkQuota(ctx, tx, owner); err != nil {
			return err
		}
		var err error
		if imp, err = tx.CreateImport(ctx, owner, strings.TrimSpace(rawURL), canonical); err != nil {
			return err
		}
		j, err := job(Args{ImportID: imp.ID, OwnerID: owner})
		if err != nil {
			return err
		}
		return s.queue.EnqueueTx(ctx, raw, j)
	})
	if err != nil {
		return Requested{}, err
	}
	return Requested{Import: imp}, nil
}

// checkQuota refuses an import over owner's quotas. It holds owner's quota
// lock for the rest of tx, so concurrent requests count one at a time.
func (s *Service) checkQuota(ctx context.Context, tx *store.Store, owner uuid.UUID) error {
	if err := tx.LockOwner(ctx, owner); err != nil {
		return err
	}
	usage, err := tx.QuotaUsage(ctx, owner, quotaWindow)
	switch {
	case err != nil:
		return err
	case usage.Documents >= s.quota.Documents:
		return &QuotaError{Quota: "documents"}
	case usage.RecentImports >= s.quota.ImportsPerDay:
		return &QuotaError{Quota: "imports per day", RetryAfter: max(usage.UntilOldestLeaves, time.Second)}
	}
	return nil
}

// Validate checks a URL a user asked to import and returns it canonicalized.
// Hosts must be names: an IP literal is refused here, before the fetcher's
// address guard ever sees it.
func Validate(rawURL string) (string, error) {
	canonical, err := canon.Normalize(rawURL)
	switch {
	case errors.Is(err, canon.ErrTooLong):
		return "", ErrURLTooLong
	case errors.Is(err, canon.ErrScheme):
		return "", ErrUnsupportedScheme
	case err != nil:
		return "", ErrInvalidURL
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return "", ErrInvalidURL
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return "", ErrIPLiteralHost
	}
	return canonical, nil
}
