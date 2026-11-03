package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/importer"
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
	ListDocuments(ctx context.Context, owner uuid.UUID, after *store.DocumentCursor, limit int) ([]store.DocumentSummary, error)
}

// apiRoutes returns a mux with the routes of api/openapi.yaml: the
// generated ones, and the hand-written event stream. Errors from parsing a
// request become the error envelope.
func apiRoutes(o Options) *http.ServeMux {
	logger := o.Logger
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/imports/{id}/events", importEvents(o.Backend, o.Events, o.Done, heartbeat))
	strict := gen.NewStrictHandlerWithOptions(apiServer{backend: o.Backend}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "the request body is not valid JSON for this operation")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.LogAttrs(r.Context(), slog.LevelError, "api error",
				slog.String("error", err.Error()), slog.String("request_id", requestID(r.Context())))
			writeError(w, r, http.StatusInternalServerError, gen.ErrorCodeInternalError, "internal server error")
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
	backend Backend
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
	sum := summaryJSON(doc.DocumentSummary)
	return gen.GetDocument200JSONResponse{
		ID: sum.ID, Title: sum.Title, SourceURL: sum.SourceURL, Version: sum.Version,
		CreatedAt: sum.CreatedAt, UpdatedAt: sum.UpdatedAt, Content: *doc.Content,
	}, nil
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
