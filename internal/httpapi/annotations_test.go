package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/annotation"
	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// The fake's annotations follow the store's rules through the same checks.

func (f *fakeBackend) content(document uuid.UUID) (*doccontent.Doc, error) {
	for _, d := range f.docs {
		if d.ID == document {
			return d.Content, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeBackend) annotation(id uuid.UUID) (int, error) {
	for i, a := range f.annotations {
		if a.ID == id {
			return i, nil
		}
	}
	return 0, store.ErrNotFound
}

func (f *fakeBackend) ListAnnotations(_ context.Context, owner, document uuid.UUID) ([]store.Annotation, error) {
	f.owners = append(f.owners, owner)
	if _, err := f.content(document); err != nil {
		return nil, err
	}
	var out []store.Annotation
	for _, a := range f.annotations {
		if a.DocumentID == document {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeBackend) CreateAnnotation(_ context.Context, owner, document uuid.UUID, a store.NewAnnotation) (store.Annotation, error) {
	f.owners = append(f.owners, owner)
	content, err := f.content(document)
	if err != nil {
		return store.Annotation{}, err
	}
	if err := annotation.Check(content, a.BlockID, a.Selector); err != nil {
		return store.Annotation{}, err
	}
	if len(f.annotations) >= annotation.MaxPerDocument {
		return store.Annotation{}, annotation.LimitReached()
	}
	created := store.Annotation{
		ID: uuid.New(), DocumentID: document, BlockID: a.BlockID, Kind: a.Kind, Selector: a.Selector,
		Note: a.Note, Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	f.annotations = append(f.annotations, created)
	return created, nil
}

func (f *fakeBackend) UpdateAnnotation(_ context.Context, owner, id uuid.UUID, change store.AnnotationChange) (store.Annotation, error) {
	f.owners = append(f.owners, owner)
	i, err := f.annotation(id)
	if err != nil {
		return store.Annotation{}, err
	}
	a := f.annotations[i]
	if a.Version != change.Version {
		return store.Annotation{}, &store.VersionConflictError{Current: a.Version}
	}
	if change.Note != nil {
		a.Note = *change.Note
	}
	if change.BlockID != nil {
		content, _ := f.content(a.DocumentID)
		if err := annotation.Check(content, *change.BlockID, *change.Selector); err != nil {
			return store.Annotation{}, err
		}
		a.BlockID, a.Selector = *change.BlockID, *change.Selector
	}
	a.Version++
	f.annotations[i] = a
	return a, nil
}

func (f *fakeBackend) ReanchorAnnotations(_ context.Context, owner, document uuid.UUID, moves []store.Reanchor) ([]store.Annotation, error) {
	f.owners = append(f.owners, owner)
	content, err := f.content(document)
	if err != nil {
		return nil, err
	}
	next := append([]store.Annotation(nil), f.annotations...)
	var saved []store.Annotation
	for _, m := range moves {
		i, err := f.annotation(m.ID)
		if err != nil || next[i].DocumentID != document {
			return nil, store.ErrNotFound
		}
		if next[i].Version != m.Version {
			return nil, &store.VersionConflictError{Current: next[i].Version}
		}
		if err := annotation.Check(content, next[i].BlockID, m.Selector); err != nil {
			return nil, err
		}
		next[i].Selector, next[i].Version = m.Selector, next[i].Version+1
		saved = append(saved, next[i])
	}
	f.annotations = next
	return saved, nil
}

func (f *fakeBackend) DeleteAnnotation(_ context.Context, owner, id uuid.UUID) error {
	f.owners = append(f.owners, owner)
	i, err := f.annotation(id)
	if err != nil {
		return err
	}
	f.annotations = append(f.annotations[:i], f.annotations[i+1:]...)
	return nil
}

// annotated is a document of a paragraph and a code block, with one
// highlight of the first "the tool" in the paragraph, at version 2.
func annotated() (*fakeBackend, store.Document, store.Annotation) {
	doc := testDoc(1)
	doc.Content = doccontent.FromIR(&ir.Doc{Blocks: []ir.Block{
		{ID: "p1", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Install the tool, then the tool runs."}}},
		{ID: "c1", Kind: ir.KindCode, Text: "go install .\n"},
	}})
	a := store.Annotation{
		ID: uuid.New(), DocumentID: doc.ID, BlockID: "p1", Kind: "highlight",
		Selector: selector(8, 16, "the tool"), Version: 2, CreatedAt: t0, UpdatedAt: t0,
	}
	return &fakeBackend{docs: []store.Document{doc}, annotations: []store.Annotation{a}}, doc, a
}

func selector(start, end int, exact string) annotation.Selector {
	return annotation.Selector{Position: annotation.Position{Start: start, End: end}, Quote: annotation.Quote{Exact: exact}}
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCreateAnnotation(t *testing.T) {
	t.Parallel()
	highlight := func(block string, sel annotation.Selector) map[string]any {
		return map[string]any{"block_id": block, "kind": "highlight", "selector": sel}
	}
	tests := []struct {
		name       string
		body       map[string]any
		target     string
		full       bool
		wantStatus int
		wantCode   gen.ErrorCode
	}{
		{name: "words of a paragraph", body: highlight("p1", selector(0, 7, "Install")), wantStatus: http.StatusCreated},
		{name: "code, with a note", body: map[string]any{"block_id": "c1", "kind": "highlight", "selector": selector(3, 10, "install"), "note": "the build step"}, wantStatus: http.StatusCreated},
		{name: "a quote that is not the text", body: highlight("p1", selector(0, 7, "Instal1")), wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeQuoteMismatch},
		{name: "past the end", body: highlight("c1", selector(10, 40, "x")), wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeQuoteMismatch},
		{name: "no such block", body: highlight("p9", selector(0, 7, "Install")), wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeUnknownBlock},
		{name: "a full document", full: true, body: highlight("p1", selector(0, 7, "Install")), wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeAnnotationLimit},
		{name: "another kind", body: map[string]any{"block_id": "p1", "kind": "comment", "selector": selector(0, 7, "Install")}, wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest},
		{name: "a note too long", body: map[string]any{"block_id": "p1", "kind": "highlight", "selector": selector(0, 7, "Install"), "note": strings.Repeat("n", 5001)}, wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest},
		{name: "an empty range", body: highlight("p1", selector(3, 3, "")), wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest},
		{name: "another user's document", target: "/api/v1/documents/" + uuid.NewString() + "/annotations", body: highlight("p1", selector(0, 7, "Install")), wantStatus: http.StatusNotFound, wantCode: gen.ErrorCodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, doc, _ := annotated()
			if tt.full {
				for range annotation.MaxPerDocument {
					b.annotations = append(b.annotations, store.Annotation{ID: uuid.New(), DocumentID: doc.ID})
				}
			}
			target := tt.target
			if target == "" {
				target = "/api/v1/documents/" + doc.ID.String() + "/annotations"
			}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

			rec := apiRequest(t, h, http.MethodPost, target, jsonBody(t, tt.body))

			if rec.Code != tt.wantStatus {
				t.Fatalf("POST = %d %s, want %d", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantCode != "" {
				if got := decodeError(t, rec); got.Code != tt.wantCode {
					t.Errorf("error = %+v, want %s", got, tt.wantCode)
				}
				return
			}
			got := decodeJSON[gen.Annotation](t, rec)
			if got.BlockID != tt.body["block_id"] || got.Selector != tt.body["selector"] || got.Version != 1 {
				t.Errorf("created %+v, want the request's block and selector at version 1", got)
			}
		})
	}
}

func TestUpdateAnnotation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       map[string]any
		target     string
		wantStatus int
		wantCode   gen.ErrorCode
		wantNote   any
		wantBlock  string
	}{
		{name: "a note", body: map[string]any{"version": 2, "note": "read twice"}, wantStatus: http.StatusOK, wantNote: "read twice", wantBlock: "p1"},
		{name: "no note", body: map[string]any{"version": 2, "note": ""}, wantStatus: http.StatusOK, wantBlock: "p1"},
		{name: "re-applied to code", body: map[string]any{"version": 2, "block_id": "c1", "selector": selector(0, 2, "go")}, wantStatus: http.StatusOK, wantBlock: "c1"},
		{name: "a block without a selector", body: map[string]any{"version": 2, "block_id": "c1"}, wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest},
		{name: "a stale version", body: map[string]any{"version": 1, "note": "x"}, wantStatus: http.StatusConflict, wantCode: gen.ErrorCodeVersionConflict},
		{name: "re-applied to text that is not the quote", body: map[string]any{"version": 2, "block_id": "c1", "selector": selector(0, 2, "og")}, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeQuoteMismatch},
		{name: "another user's annotation", target: "/api/v1/annotations/" + uuid.NewString(), body: map[string]any{"version": 2, "note": "x"}, wantStatus: http.StatusNotFound, wantCode: gen.ErrorCodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, _, a := annotated()
			target := tt.target
			if target == "" {
				target = "/api/v1/annotations/" + a.ID.String()
			}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

			rec := apiRequest(t, h, http.MethodPatch, target, jsonBody(t, tt.body))

			if rec.Code != tt.wantStatus {
				t.Fatalf("PATCH = %d %s, want %d", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantCode == gen.ErrorCodeVersionConflict {
				if got := decodeJSON[gen.VersionConflict](t, rec); got.Error.Code != tt.wantCode || got.CurrentVersion != 2 {
					t.Errorf("conflict = %+v, want version_conflict at current version 2", got)
				}
				return
			}
			if tt.wantCode != "" {
				if got := decodeError(t, rec); got.Code != tt.wantCode {
					t.Errorf("error = %+v, want %s", got, tt.wantCode)
				}
				return
			}
			got := decodeJSON[gen.Annotation](t, rec)
			if got.Version != 3 || deref(got.Note) != tt.wantNote || got.BlockID != tt.wantBlock {
				t.Errorf("updated %+v, want version 3, note %v, block %s", got, tt.wantNote, tt.wantBlock)
			}
		})
	}
}

func TestReanchorAnnotations(t *testing.T) {
	t.Parallel()
	move := func(id uuid.UUID, sel annotation.Selector, version int) map[string]any {
		return map[string]any{"id": id, "selector": sel, "version": version}
	}
	tests := []struct {
		name       string
		moves      func(a store.Annotation) []map[string]any
		wantStatus int
		wantCode   gen.ErrorCode
	}{
		{name: "to the other occurrence", wantStatus: http.StatusOK, moves: func(a store.Annotation) []map[string]any {
			return []map[string]any{move(a.ID, selector(23, 31, "the tool"), 2)}
		}},
		{name: "none", wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest, moves: func(store.Annotation) []map[string]any {
			return []map[string]any{}
		}},
		{name: "a stale version", wantStatus: http.StatusConflict, wantCode: gen.ErrorCodeVersionConflict, moves: func(a store.Annotation) []map[string]any {
			return []map[string]any{move(a.ID, selector(23, 31, "the tool"), 1)}
		}},
		{name: "text that is not the quote", wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeQuoteMismatch, moves: func(a store.Annotation) []map[string]any {
			return []map[string]any{move(a.ID, selector(22, 30, "the tool"), 2)}
		}},
		{name: "an annotation of no document of the user's", wantStatus: http.StatusNotFound, wantCode: gen.ErrorCodeNotFound, moves: func(store.Annotation) []map[string]any {
			return []map[string]any{move(uuid.New(), selector(23, 31, "the tool"), 1)}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, doc, a := annotated()
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

			rec := apiRequest(t, h, http.MethodPost, "/api/v1/documents/"+doc.ID.String()+"/annotations/reanchor",
				jsonBody(t, map[string]any{"annotations": tt.moves(a)}))

			if rec.Code != tt.wantStatus {
				t.Fatalf("POST = %d %s, want %d", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantCode != "" {
				if got := decodeError(t, rec); got.Code != tt.wantCode {
					t.Errorf("error = %+v, want %s", got, tt.wantCode)
				}
				if b.annotations[0].Version != 2 {
					t.Error("a refused batch changed an annotation")
				}
				return
			}
			got := decodeJSON[gen.AnnotationList](t, rec)
			if len(got.Annotations) != 1 || got.Annotations[0].Selector.Position.Start != 23 || got.Annotations[0].Version != 3 {
				t.Errorf("re-anchored %+v, want the one annotation at 23, version 3", got.Annotations)
			}
		})
	}
}

func TestListAndDeleteAnnotations(t *testing.T) {
	t.Parallel()
	b, doc, a := annotated()
	h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})
	list := "/api/v1/documents/" + doc.ID.String() + "/annotations"

	rec := apiRequest(t, h, http.MethodGet, list, "")
	if got := decodeJSON[gen.AnnotationList](t, rec); rec.Code != http.StatusOK || len(got.Annotations) != 1 || got.Annotations[0].ID != a.ID {
		t.Fatalf("GET = %d %s, want the one annotation", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, h, http.MethodGet, "/api/v1/documents/"+uuid.NewString()+"/annotations", ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET another user's document's annotations = %d, want 404", rec.Code)
	}
	if rec := apiRequest(t, h, http.MethodDelete, "/api/v1/annotations/"+a.ID.String(), ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, h, http.MethodDelete, "/api/v1/annotations/"+a.ID.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE again = %d, want 404", rec.Code)
	}
	for _, owner := range b.owners {
		if owner != testUser {
			t.Errorf("asked for owner %s, want the signed-in user", owner)
		}
	}
}
