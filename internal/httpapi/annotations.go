package httpapi

import (
	"context"
	"errors"

	"github.com/rokumate/ronote/internal/annotation"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// Annotations: highlights and notes in a user's document
// (docs/adr/0007-annotations-and-anchoring.md). The store checks every
// selector against the document's content under a lock; these handlers map
// what it refuses onto the API's codes.

func (s apiServer) ListAnnotations(ctx context.Context, req gen.ListAnnotationsRequestObject) (gen.ListAnnotationsResponseObject, error) {
	list, err := s.backend.ListAnnotations(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.ListAnnotations404JSONResponse{NotFoundJSONResponse: notFound(ctx, "no such document")}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ListAnnotations200JSONResponse(annotationList(list)), nil
}

func (s apiServer) CreateAnnotation(ctx context.Context, req gen.CreateAnnotationRequestObject) (gen.CreateAnnotationResponseObject, error) {
	if req.Body == nil || req.Body.Kind != gen.Highlight {
		return gen.CreateAnnotation400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body with block_id, kind highlight and selector is required")), nil
	}
	note := orEmpty(req.Body.Note)
	err := annotation.CheckNote(note)
	var a store.Annotation
	if err == nil {
		a, err = s.backend.CreateAnnotation(ctx, userFrom(ctx), req.ID, store.NewAnnotation{
			BlockID: req.Body.BlockID, Kind: string(req.Body.Kind), Selector: req.Body.Selector, Note: note,
		})
	}
	if errors.Is(err, store.ErrNotFound) {
		return gen.CreateAnnotation404JSONResponse{NotFoundJSONResponse: notFound(ctx, "no such document")}, nil
	}
	if body, invalid, ok := refusal(ctx, err); ok {
		if invalid {
			return gen.CreateAnnotation400JSONResponse(body), nil
		}
		return gen.CreateAnnotation422JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.CreateAnnotation201JSONResponse(annotationJSON(a)), nil
}

func (s apiServer) UpdateAnnotation(ctx context.Context, req gen.UpdateAnnotationRequestObject) (gen.UpdateAnnotationResponseObject, error) {
	if req.Body == nil || req.Body.Version < 1 || (req.Body.BlockID == nil) != (req.Body.Selector == nil) {
		return gen.UpdateAnnotation400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body with the version, and block_id and selector together, is required")), nil
	}
	err := annotation.CheckNote(orEmpty(req.Body.Note))
	var a store.Annotation
	if err == nil {
		a, err = s.backend.UpdateAnnotation(ctx, userFrom(ctx), req.ID, store.AnnotationChange{
			Version: req.Body.Version, Note: req.Body.Note, BlockID: req.Body.BlockID, Selector: req.Body.Selector,
		})
	}
	var conflict *store.VersionConflictError
	switch {
	case errors.Is(err, store.ErrNotFound):
		return gen.UpdateAnnotation404JSONResponse{NotFoundJSONResponse: notFound(ctx, "no such annotation")}, nil
	case errors.As(err, &conflict):
		return gen.UpdateAnnotation409JSONResponse{
			Error:          apiError(ctx, gen.ErrorCodeVersionConflict, "the annotation changed since this version").Error,
			CurrentVersion: conflict.Current,
		}, nil
	}
	if body, invalid, ok := refusal(ctx, err); ok {
		if invalid {
			return gen.UpdateAnnotation400JSONResponse(body), nil
		}
		return gen.UpdateAnnotation422JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.UpdateAnnotation200JSONResponse(annotationJSON(a)), nil
}

func (s apiServer) ReanchorAnnotations(ctx context.Context, req gen.ReanchorAnnotationsRequestObject) (gen.ReanchorAnnotationsResponseObject, error) {
	if req.Body == nil || len(req.Body.Annotations) == 0 || len(req.Body.Annotations) > annotation.MaxPerDocument {
		return gen.ReanchorAnnotations400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body with 1 to 2000 annotations is required")), nil
	}
	moves := make([]store.Reanchor, len(req.Body.Annotations))
	for i, m := range req.Body.Annotations {
		moves[i] = store.Reanchor{ID: m.ID, Selector: m.Selector, Version: m.Version}
	}
	list, err := s.backend.ReanchorAnnotations(ctx, userFrom(ctx), req.ID, moves)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return gen.ReanchorAnnotations404JSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document, or an annotation that is not one of it")), nil
	case errors.As(err, new(*store.VersionConflictError)):
		return gen.ReanchorAnnotations409JSONResponse(apiError(ctx, gen.ErrorCodeVersionConflict, "an annotation changed since its version")), nil
	}
	if body, invalid, ok := refusal(ctx, err); ok {
		if invalid {
			return gen.ReanchorAnnotations400JSONResponse(body), nil
		}
		return gen.ReanchorAnnotations422JSONResponse(body), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ReanchorAnnotations200JSONResponse(annotationList(list)), nil
}

func (s apiServer) DeleteAnnotation(ctx context.Context, req gen.DeleteAnnotationRequestObject) (gen.DeleteAnnotationResponseObject, error) {
	err := s.backend.DeleteAnnotation(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.DeleteAnnotation404JSONResponse{NotFoundJSONResponse: notFound(ctx, "no such annotation")}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.DeleteAnnotation204Response{}, nil
}

// refusal is the error body for an annotation rule err broke, and whether
// the request itself was malformed (400) rather than unfit for the document
// (422). ok is false when err is no rule.
func refusal(ctx context.Context, err error) (body gen.Error, invalid, ok bool) {
	var p *annotation.Problem
	if !errors.As(err, &p) {
		return gen.Error{}, false, false
	}
	return apiError(ctx, gen.ErrorCode(p.Code), p.Err.Error()), p.Code == annotation.CodeInvalidRequest, true
}

func notFound(ctx context.Context, message string) gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, message))
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func annotationJSON(a store.Annotation) gen.Annotation {
	var note *string
	if a.Note != "" {
		note = &a.Note
	}
	return gen.Annotation{
		ID: a.ID, DocumentID: a.DocumentID, BlockID: a.BlockID, Kind: gen.AnnotationKind(a.Kind),
		Selector: a.Selector, Note: note, Version: a.Version,
		CreatedAt: a.CreatedAt.UTC(), UpdatedAt: a.UpdatedAt.UTC(),
	}
}

func annotationList(list []store.Annotation) gen.AnnotationList {
	out := gen.AnnotationList{Annotations: make([]gen.Annotation, len(list))}
	for i, a := range list {
		out.Annotations[i] = annotationJSON(a)
	}
	return out
}
