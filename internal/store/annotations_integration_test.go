//go:build integration

package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/annotation"
)

func at(start, end int, exact string) annotation.Selector {
	return annotation.Selector{Position: annotation.Position{Start: start, End: end}, Quote: annotation.Quote{Exact: exact}}
}

// paragraph is the id of the fixture document's one paragraph, "Guide".
func (f fixture) paragraph(t *testing.T, owner User, doc uuid.UUID) string {
	t.Helper()
	d, err := f.s.GetDocument(t.Context(), owner.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	return d.Content.Blocks[0].ID
}

func wantProblem(t *testing.T, err error, code annotation.Code) {
	t.Helper()
	var p *annotation.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Errorf("err = %v, want a %s problem", err, code)
	}
}

func TestAnnotations(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	p := f.paragraph(t, f.ada, doc)

	a, err := f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 2, "Gu")})
	if err != nil || a.Version != 1 || a.Note != "" {
		t.Fatalf("CreateAnnotation() = %+v, %v; want version 1 without a note", a, err)
	}
	_, err = f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 2, "Gx")})
	wantProblem(t, err, annotation.CodeQuoteMismatch)

	note := "read twice"
	a, err = f.s.UpdateAnnotation(ctx, f.ada.ID, a.ID, AnnotationChange{Version: 1, Note: &note})
	if err != nil || a.Note != note || a.Version != 2 {
		t.Fatalf("UpdateAnnotation(note) = %+v, %v; want the note at version 2", a, err)
	}
	_, err = f.s.UpdateAnnotation(ctx, f.ada.ID, a.ID, AnnotationChange{Version: 1, Note: &note})
	if conflict := (*VersionConflictError)(nil); !errors.As(err, &conflict) || conflict.Current != 2 {
		t.Errorf("UpdateAnnotation(stale) = %v, want a conflict at version 2", err)
	}
	sel := at(2, 5, "ide")
	a, err = f.s.UpdateAnnotation(ctx, f.ada.ID, a.ID, AnnotationChange{Version: 2, BlockID: &p, Selector: &sel})
	if err != nil || a.Selector != sel || a.Note != note {
		t.Fatalf("UpdateAnnotation(re-apply) = %+v, %v; want the new range and the note kept", a, err)
	}
	empty := ""
	if a, err = f.s.UpdateAnnotation(ctx, f.ada.ID, a.ID, AnnotationChange{Version: 3, Note: &empty}); err != nil || a.Note != "" {
		t.Errorf("UpdateAnnotation(no note) = %+v, %v; want the note gone", a, err)
	}

	moved, err := f.s.ReanchorAnnotations(ctx, f.ada.ID, doc, []Reanchor{{ID: a.ID, Selector: at(1, 3, "ui"), Version: 4}})
	if err != nil || len(moved) != 1 || moved[0].Selector.Position.Start != 1 || moved[0].Version != 5 {
		t.Fatalf("ReanchorAnnotations() = %+v, %v; want the annotation at 1, version 5", moved, err)
	}
	list, err := f.s.ListAnnotations(ctx, f.ada.ID, doc)
	if err != nil || len(list) != 1 || list[0] != moved[0] {
		t.Errorf("ListAnnotations() = %+v, %v; want the re-anchored annotation", list, err)
	}

	if err := f.s.DeleteAnnotation(ctx, f.ada.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAnnotation(ctx, f.ada.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAnnotation() again = %v, want ErrNotFound", err)
	}
}

// TestReanchorIsAllOrNothing: one stale version refuses the whole batch.
func TestReanchorIsAllOrNothing(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	p := f.paragraph(t, f.ada, doc)
	first, err := f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 1, "G")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(4, 5, "e")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.s.ReanchorAnnotations(ctx, f.ada.ID, doc, []Reanchor{
		{ID: first.ID, Selector: at(0, 2, "Gu"), Version: 1},
		{ID: second.ID, Selector: at(3, 5, "de"), Version: 7},
	})

	if !errors.As(err, new(*VersionConflictError)) {
		t.Fatalf("ReanchorAnnotations() = %v, want a version conflict", err)
	}
	list, err := f.s.ListAnnotations(ctx, f.ada.ID, doc)
	if err != nil || list[0] != first || list[1] != second {
		t.Errorf("after a refused batch: %+v, %v; want both unchanged", list, err)
	}
}

// TestAnnotationLimit fills a document to one short of the limit, then
// creates several at once: the document lock lets exactly one through.
func TestAnnotationLimit(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	p := f.paragraph(t, f.ada, doc)
	if _, err := testDB.Pool.Exec(ctx, `
		INSERT INTO annotations (owner_id, document_id, block_id, kind, selector)
		SELECT $1, $2, $3, 'highlight', '{"position":{"start":0,"end":1},"quote":{"exact":"G","prefix":"","suffix":""}}'
		FROM generate_series(1, $4::int)`, f.ada.ID, doc, p, annotation.MaxPerDocument-1); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = f.s.CreateAnnotation(context.WithoutCancel(ctx), f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 2, "Gu")})
		})
	}
	wg.Wait()

	created := 0
	for _, err := range errs {
		if err == nil {
			created++
			continue
		}
		wantProblem(t, err, annotation.CodeLimit)
	}
	list, err := f.s.ListAnnotations(ctx, f.ada.ID, doc)
	if created != 1 || err != nil || len(list) != annotation.MaxPerDocument {
		t.Errorf("created %d of 4 at once, and the document holds %d (%v); want 1 and %d", created, len(list), err, annotation.MaxPerDocument)
	}
}

// TestAnnotationOwnerIsolation: nothing of Ada's annotations reaches Grace,
// through any method.
func TestAnnotationOwnerIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	p := f.paragraph(t, f.ada, doc)
	a, err := f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 2, "Gu")})
	if err != nil {
		t.Fatal(err)
	}
	graces := f.document(t, f.grace)
	note := "mine now"
	sel := at(1, 3, "ui")

	if _, err := f.s.ListAnnotations(ctx, f.grace.ID, doc); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListAnnotations(Ada's document) = %v, want ErrNotFound", err)
	}
	if _, err := f.s.CreateAnnotation(ctx, f.grace.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: sel}); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateAnnotation(Ada's document) = %v, want ErrNotFound", err)
	}
	if _, err := f.s.UpdateAnnotation(ctx, f.grace.ID, a.ID, AnnotationChange{Version: 1, Note: &note}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateAnnotation(Ada's annotation) = %v, want ErrNotFound", err)
	}
	for _, target := range []uuid.UUID{doc, graces} {
		if _, err := f.s.ReanchorAnnotations(ctx, f.grace.ID, target, []Reanchor{{ID: a.ID, Selector: sel, Version: 1}}); !errors.Is(err, ErrNotFound) {
			t.Errorf("ReanchorAnnotations(Ada's annotation, through %v) = %v, want ErrNotFound", target, err)
		}
	}
	if err := f.s.DeleteAnnotation(ctx, f.grace.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAnnotation(Ada's annotation) = %v, want ErrNotFound", err)
	}
	if list, err := f.s.ListAnnotations(ctx, f.grace.ID, graces); err != nil || len(list) != 0 {
		t.Errorf("ListAnnotations(Grace's document) = %+v, %v; want none", list, err)
	}
	if list, err := f.s.ListAnnotations(ctx, f.ada.ID, doc); err != nil || len(list) != 1 || list[0] != a {
		t.Errorf("Ada's annotations after Grace's attempts = %+v, %v; want hers unchanged", list, err)
	}
}

func TestAnnotationsGoWithTheirDocument(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	p := f.paragraph(t, f.ada, doc)
	if _, err := f.s.CreateAnnotation(ctx, f.ada.ID, doc, NewAnnotation{BlockID: p, Kind: "highlight", Selector: at(0, 2, "Gu")}); err != nil {
		t.Fatal(err)
	}

	if _, err := testDB.Pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", doc); err != nil {
		t.Fatal(err)
	}

	var left int
	if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM annotations").Scan(&left); err != nil || left != 0 {
		t.Errorf("annotations left after their document went: %d (%v), want 0", left, err)
	}
}
