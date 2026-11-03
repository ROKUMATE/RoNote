package importer

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/rokumate/ronote/internal/canon"
	"github.com/rokumate/ronote/internal/store"
)

// Errors for URLs that are refused before anything is queued.
var (
	ErrInvalidURL        = errors.New("not an absolute URL with a host")
	ErrUnsupportedScheme = errors.New("only http and https URLs can be imported")
	ErrURLTooLong        = errors.New("URL is longer than 2048 bytes")
	ErrIPLiteralHost     = errors.New("the host must be a name, not an IP address")
)

// Service starts imports for the API.
type Service struct {
	store *store.Store
	queue *river.Client[pgx.Tx]
}

// NewService returns a Service that records imports in s and queues their
// jobs on queue.
func NewService(s *store.Store, queue *river.Client[pgx.Tx]) *Service {
	return &Service{store: s, queue: queue}
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
// transaction, so a job never runs for an import that was not saved.
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
		var err error
		if imp, err = tx.CreateImport(ctx, owner, strings.TrimSpace(rawURL), canonical); err != nil {
			return err
		}
		_, err = s.queue.InsertTx(ctx, raw, Args{ImportID: imp.ID, OwnerID: owner}, nil)
		return err
	})
	if err != nil {
		return Requested{}, err
	}
	return Requested{Import: imp}, nil
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
