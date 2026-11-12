package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Backend is what /api/v1 reads and writes through: the import service and
// the store, scoped to the signed-in user. It is an interface so handler
// tests can run without a database.
type Backend interface {
	Request(ctx context.Context, owner uuid.UUID, rawURL string) (importer.Requested, error)
	GetImport(ctx context.Context, owner, id uuid.UUID) (store.Import, error)
	GetDocument(ctx context.Context, owner, id uuid.UUID) (store.Document, error)
	DocumentExtraction(ctx context.Context, owner, id uuid.UUID) (*ir.Doc, error)
	SaveDocument(ctx context.Context, owner, id uuid.UUID, content *doccontent.Doc, version int) (int, error)
	ListDocuments(ctx context.Context, owner uuid.UUID, after *store.DocumentCursor, limit int) ([]store.DocumentSummary, error)
	ResolveDocuments(ctx context.Context, owner uuid.UUID, urls []string) (map[string]uuid.UUID, error)
	ListAnnotations(ctx context.Context, owner, document uuid.UUID) ([]store.Annotation, error)
	CreateAnnotation(ctx context.Context, owner, document uuid.UUID, a store.NewAnnotation) (store.Annotation, error)
	UpdateAnnotation(ctx context.Context, owner, id uuid.UUID, change store.AnnotationChange) (store.Annotation, error)
	ReanchorAnnotations(ctx context.Context, owner, document uuid.UUID, moves []store.Reanchor) ([]store.Annotation, error)
	DeleteAnnotation(ctx context.Context, owner, id uuid.UUID) error
}

// apiRoutes returns a mux with the routes of api/openapi.yaml: the
// generated ones, and the hand-written event stream. Errors from parsing a
// request become the error envelope.
func apiRoutes(o Options, lim limiters) *http.ServeMux {
	logger := o.Logger
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/imports/{id}/events", importEvents(o.Backend, o.Events, o.Done, heartbeat))
	mux.Handle("POST /api/v1/auth/logout", lim.byIP(logout(o)))
	mux.Handle("POST /api/v1/auth/logout-all", lim.byIP(logoutAll(o)))
	mux.Handle("DELETE /api/v1/me", deleteMe(o))
	strict := gen.NewStrictHandlerWithOptions(apiServer{backend: o.Backend, accounts: o.Accounts, searcher: o.Searcher, showScores: o.ShowScores}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			badBody(w, r, err, "the request body is not valid JSON for this operation")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			internalError(logger, w, r, err)
		},
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidParameter, err.Error())
		},
	})
	return mux
}

// apiServer implements the operations of api/openapi.yaml.
type apiServer struct {
	backend    Backend
	accounts   Accounts
	searcher   Searcher
	showScores bool
}

func apiError(ctx context.Context, code gen.ErrorCode, message string) gen.Error {
	return gen.Error{Error: gen.ErrorBody{Code: code, Message: message, RequestID: requestID(ctx)}}
}

// importErrorCodes maps the URLs Request refuses to their error codes.
var importErrorCodes = []struct {
	err  error
	code gen.ErrorCode
}{
	{importer.ErrInvalidURL, gen.ErrorCodeInvalidURL},
	{importer.ErrUnsupportedScheme, gen.ErrorCodeUnsupportedScheme},
	{importer.ErrURLTooLong, gen.ErrorCodeURLTooLong},
	{importer.ErrIPLiteralHost, gen.ErrorCodeIPLiteralHost},
}

func (s apiServer) CreateImport(ctx context.Context, req gen.CreateImportRequestObject) (gen.CreateImportResponseObject, error) {
	if req.Body == nil {
		return gen.CreateImport400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body with a url is required")), nil
	}
	owner := userFrom(ctx)
	res, err := s.backend.Request(ctx, owner, req.Body.URL)
	for _, e := range importErrorCodes {
		if errors.Is(err, e.err) {
			return gen.CreateImport400JSONResponse(apiError(ctx, e.code, err.Error())), nil
		}
	}
	if over := new(importer.QuotaError); errors.As(err, &over) {
		res := gen.CreateImport429JSONResponse{TooManyImportsJSONResponse: gen.TooManyImportsJSONResponse{
			Body: apiError(ctx, gen.ErrorCodeQuotaExceeded, err.Error()),
		}}
		if over.RetryAfter > 0 {
			seconds := retrySeconds(over.RetryAfter)
			res.Headers.RetryAfter = &seconds
		}
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	if !res.Existing {
		return gen.CreateImport202JSONResponse{Import: importJSON(res.Import)}, nil
	}
	doc, err := s.backend.GetDocument(ctx, owner, res.Import.DocumentID)
	if err != nil {
		return nil, err
	}
	return gen.CreateImport200JSONResponse{Import: importJSON(res.Import), Document: summaryJSON(doc.DocumentSummary)}, nil
}

func (s apiServer) GetImport(ctx context.Context, req gen.GetImportRequestObject) (gen.GetImportResponseObject, error) {
	imp, err := s.backend.GetImport(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.GetImport404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such import"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetImport200JSONResponse{Import: importJSON(imp)}, nil
}

func (s apiServer) GetDocument(ctx context.Context, req gen.GetDocumentRequestObject) (gen.GetDocumentResponseObject, error) {
	doc, err := s.backend.GetDocument(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.GetDocument404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetDocument200JSONResponse(documentJSON(doc)), nil
}

// revertAttempts bounds how often a revert retries when a save lands
// between its read and its write.
const revertAttempts = 3

func (s apiServer) SaveDocument(ctx context.Context, req gen.SaveDocumentRequestObject) (gen.SaveDocumentResponseObject, error) {
	if req.Body == nil || req.Body.Version < 1 {
		return gen.SaveDocument400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body with content and the version it was edited from is required")), nil
	}
	owner := userFrom(ctx)
	extraction, err := s.backend.DocumentExtraction(ctx, owner, req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.SaveDocument404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document"))}, nil
	}
	if err != nil {
		return nil, err
	}
	content := req.Body.Content
	if err := doccontent.Validate(&content); err != nil {
		return saveRefused(ctx, err)
	}
	if err := doccontent.CheckSource(&content, extraction); err != nil {
		return saveRefused(ctx, err)
	}
	version, err := s.backend.SaveDocument(ctx, owner, req.ID, &content, req.Body.Version)
	var conflict *store.VersionConflictError
	switch {
	case errors.As(err, &conflict):
		return gen.SaveDocument409JSONResponse{
			Error:          apiError(ctx, gen.ErrorCodeVersionConflict, "the document changed since this version").Error,
			CurrentVersion: conflict.Current,
		}, nil
	case errors.Is(err, store.ErrNotFound):
		return gen.SaveDocument404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document"))}, nil
	case err != nil:
		return nil, err
	}
	return gen.SaveDocument200JSONResponse{Version: version}, nil
}

// saveRefused answers 422 with the code of the rule err broke.
func saveRefused(ctx context.Context, err error) (gen.SaveDocumentResponseObject, error) {
	var p *doccontent.Problem
	if !errors.As(err, &p) {
		return nil, err
	}
	return gen.SaveDocument422JSONResponse(apiError(ctx, gen.ErrorCode(p.Code), p.Err.Error())), nil
}

func (s apiServer) RevertBlock(ctx context.Context, req gen.RevertBlockRequestObject) (gen.RevertBlockResponseObject, error) {
	owner := userFrom(ctx)
	notFound := gen.RevertBlock404JSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document or source block"))
	for range revertAttempts {
		doc, err := s.backend.GetDocument(ctx, owner, req.ID)
		if errors.Is(err, store.ErrNotFound) {
			return notFound, nil
		}
		if err != nil {
			return nil, err
		}
		extraction, err := s.backend.DocumentExtraction(ctx, owner, req.ID)
		if err != nil {
			return nil, err
		}
		reverted, err := doccontent.Revert(doc.Content, extraction, req.BlockID)
		if errors.Is(err, doccontent.ErrNoSourceBlock) {
			return notFound, nil
		}
		if err != nil {
			return nil, err
		}
		_, err = s.backend.SaveDocument(ctx, owner, req.ID, reverted, doc.Version)
		if errors.As(err, new(*store.VersionConflictError)) {
			continue
		}
		if err != nil {
			return nil, err
		}
		saved, err := s.backend.GetDocument(ctx, owner, req.ID)
		if err != nil {
			return nil, err
		}
		return gen.RevertBlock200JSONResponse{Document: documentJSON(saved)}, nil
	}
	return nil, fmt.Errorf("revert block %s: the document kept changing", req.BlockID)
}

// maxResolve is how many URLs one resolve request may ask about, as the
// spec's maxItems says.
const maxResolve = 200

func (s apiServer) ResolveDocuments(ctx context.Context, req gen.ResolveDocumentsRequestObject) (gen.ResolveDocumentsResponseObject, error) {
	if req.Body == nil || len(req.Body.Urls) == 0 || len(req.Body.Urls) > maxResolve {
		return gen.ResolveDocuments400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest,
			fmt.Sprintf("a JSON body with 1 to %d urls is required", maxResolve))), nil
	}
	canonical := make([]string, len(req.Body.Urls))
	var known []string
	for i, raw := range req.Body.Urls {
		// A URL an import would refuse has no canonical form and no document.
		if c, err := importer.Validate(raw); err == nil {
			canonical[i] = c
			known = append(known, c)
		}
	}
	found := map[string]uuid.UUID{}
	if len(known) > 0 {
		var err error
		if found, err = s.backend.ResolveDocuments(ctx, userFrom(ctx), known); err != nil {
			return nil, err
		}
	}
	links := make([]gen.ResolvedLink, len(req.Body.Urls))
	for i, raw := range req.Body.Urls {
		links[i] = gen.ResolvedLink{URL: raw}
		if canonical[i] == "" {
			continue
		}
		links[i].CanonicalURL = &canonical[i]
		if id, ok := found[canonical[i]]; ok {
			links[i].DocumentID = &id
		}
	}
	return gen.ResolveDocuments200JSONResponse{Links: links}, nil
}

func documentJSON(doc store.Document) gen.Document {
	sum := summaryJSON(doc.DocumentSummary)
	return gen.Document{
		ID: sum.ID, Title: sum.Title, SourceURL: sum.SourceURL, Version: sum.Version,
		CreatedAt: sum.CreatedAt, UpdatedAt: sum.UpdatedAt, Content: *doc.Content,
		Warnings: append([]ir.Warning{}, doc.Warnings...),
	}
}

func (s apiServer) ListDocuments(ctx context.Context, req gen.ListDocumentsRequestObject) (gen.ListDocumentsResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return gen.ListDocuments400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidParameter, "limit must be between 1 and 100")), nil
	}
	var after *store.DocumentCursor
	if req.Params.Cursor != nil {
		c, err := decodeCursor(*req.Params.Cursor)
		if err != nil {
			return gen.ListDocuments400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidCursor, "the cursor is not one this API made")), nil
		}
		after = &c
	}
	// One more than asked tells whether another page follows.
	docs, err := s.backend.ListDocuments(ctx, userFrom(ctx), after, limit+1)
	if err != nil {
		return nil, err
	}
	out := gen.ListDocuments200JSONResponse{Documents: []gen.DocumentSummary{}}
	if len(docs) > limit {
		docs = docs[:limit]
		last := docs[limit-1]
		next := encodeCursor(store.DocumentCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		out.NextCursor = &next
	}
	for _, d := range docs {
		out.Documents = append(out.Documents, summaryJSON(d))
	}
	return out, nil
}

// importJSON and summaryJSON give times in UTC: the driver returns them in the
// process's local zone, and the API always answers in UTC.
func importJSON(imp store.Import) gen.Import {
	out := gen.Import{
		ID: imp.ID, RequestedURL: imp.RequestedURL, CanonicalURL: imp.CanonicalURL,
		Status: gen.ImportStatus(imp.Status), CreatedAt: imp.CreatedAt.UTC(), UpdatedAt: imp.UpdatedAt.UTC(),
	}
	if imp.ErrorCode != "" {
		code := gen.ImportErrorCode(imp.ErrorCode)
		out.ErrorCode = &code
	}
	if imp.DocumentID != uuid.Nil {
		out.DocumentID = &imp.DocumentID
	}
	return out
}

func summaryJSON(d store.DocumentSummary) gen.DocumentSummary {
	return gen.DocumentSummary{
		ID: d.ID, Title: d.Title, SourceURL: d.SourceURL, Version: d.Version, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}
