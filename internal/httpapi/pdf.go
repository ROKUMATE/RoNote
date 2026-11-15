package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/store"
)

// maxPDFBody is how large an upload's body may be: the PDF and the
// multipart framing around it.
const maxPDFBody = importer.MaxPDFBytes + 64<<10

// CreatePdfImport takes one uploaded PDF, spooled to a temporary file, and
// starts its import (docs/adr/0013-pdf-import.md).
func (s apiServer) CreatePdfImport(ctx context.Context, req gen.CreatePdfImportRequestObject) (gen.CreatePdfImportResponseObject, error) {
	tooLarge := gen.CreatePdfImport413JSONResponse(apiError(ctx, gen.ErrorCodePdfTooLarge, "the PDF is larger than 25 MB"))
	invalid := func(message string) gen.CreatePdfImportResponseObject {
		return gen.CreatePdfImport400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, message))
	}
	if req.Body == nil {
		return invalid("a multipart body with one file is required"), nil
	}
	spool, err := os.CreateTemp("", "ronote-upload-*.pdf")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()
	var filename string
	var size int64
	files := 0
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if tooBig := new(http.MaxBytesError); errors.As(err, &tooBig) {
			return tooLarge, nil
		}
		if err != nil {
			return invalid("the body is not multipart form data"), nil
		}
		if part.FormName() != "file" {
			continue
		}
		if files++; files > 1 {
			return invalid("send one file"), nil
		}
		filename = part.FileName()
		size, err = io.Copy(spool, io.LimitReader(part, importer.MaxPDFBytes+1))
		if tooBig := new(http.MaxBytesError); errors.As(err, &tooBig) {
			return tooLarge, nil
		}
		if err != nil {
			return invalid("the file could not be read"), nil
		}
	}
	if files == 0 {
		return invalid("a file part named file is required"), nil
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	owner := userFrom(ctx)
	res, err := s.backend.RequestPDF(ctx, owner, filename, spool, size)
	if over := new(importer.QuotaError); errors.As(err, &over) {
		r := gen.CreatePdfImport429JSONResponse{TooManyImportsJSONResponse: gen.TooManyImportsJSONResponse{
			Body: apiError(ctx, gen.ErrorCodeQuotaExceeded, err.Error()),
		}}
		if over.RetryAfter > 0 {
			seconds := retrySeconds(over.RetryAfter)
			r.Headers.RetryAfter = &seconds
		}
		return r, nil
	}
	switch {
	case errors.Is(err, importer.ErrPDFTooLarge):
		return tooLarge, nil
	case errors.Is(err, importer.ErrNotPDF):
		return gen.CreatePdfImport400JSONResponse(apiError(ctx, gen.ErrorCodeNotAPdf, err.Error())), nil
	case err != nil:
		return nil, err
	}
	if !res.Existing {
		return gen.CreatePdfImport202JSONResponse{Import: importJSON(res.Import)}, nil
	}
	doc, err := s.backend.GetDocument(ctx, owner, res.Import.DocumentID)
	if err != nil {
		return nil, err
	}
	return gen.CreatePdfImport200JSONResponse{Import: importJSON(res.Import), Document: summaryJSON(doc.DocumentSummary)}, nil
}

// GetDocumentOriginal streams the PDF a document was made from, to its
// owner only. A browser shows it in a sandbox with nothing allowed to load
// or run, and never guesses another type for it.
func (s apiServer) GetDocumentOriginal(ctx context.Context, req gen.GetDocumentOriginalRequestObject) (gen.GetDocumentOriginalResponseObject, error) {
	filename, obj, err := s.backend.Original(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.GetDocumentOriginal404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document"))}, nil
	}
	if err != nil {
		return nil, err
	}
	disposition := contentDisposition(filename)
	csp := "sandbox; default-src 'none'"
	nosniff := "nosniff"
	return gen.GetDocumentOriginal200ApplicationPdfResponse{
		Body:          closeAfter{obj.Body},
		ContentLength: obj.Size,
		Headers: gen.GetDocumentOriginal200ResponseHeaders{
			ContentDisposition: &disposition, ContentSecurityPolicy: &csp, XContentTypeOptions: &nosniff,
		},
	}, nil
}

// closeAfter closes its reader once it has been read to the end.
type closeAfter struct{ io.ReadCloser }

func (c closeAfter) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if err != nil {
		_ = c.Close()
	}
	return n, err
}

// contentDisposition is "inline" with filename made safe: no control
// characters, quotes, backslashes, separators or semicolons; ending in
// .pdf; ASCII in filename, with non-ASCII letters as "_", and the whole
// name in filename*, percent-encoded UTF-8 (RFC 6266).
func contentDisposition(filename string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`"\/;`, r) {
			return -1
		}
		return r
	}, filename)
	name = strings.TrimSpace(name)
	if name == "" || strings.Trim(name, ".") == "" {
		name = "document"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	ascii := strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || r == '%' {
			return '_'
		}
		return r
	}, name)
	return `inline; filename="` + ascii + `"; filename*=UTF-8''` + attrEncode(name)
}

// attrEncode percent-encodes s but for RFC 8187's attr-chars.
func attrEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
