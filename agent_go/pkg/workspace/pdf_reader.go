package workspace

import (
	"io"

	pdf "github.com/ledongthuc/pdf"
)

// pdfReaderWrapper wraps the ledongthuc/pdf library
type pdfReaderWrapper struct {
	reader *pdf.Reader
}

// newPDFReader creates a new PDF reader wrapper
func newPDFReader(r io.ReaderAt, size int64) (*pdfReaderWrapper, error) {
	reader, err := pdf.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	return &pdfReaderWrapper{reader: reader}, nil
}

// NumPage returns the number of pages in the PDF
func (p *pdfReaderWrapper) NumPage() int {
	return p.reader.NumPage()
}

// newPDFReaderWithPassword creates a new PDF reader wrapper for password-protected PDFs
func newPDFReaderWithPassword(r io.ReaderAt, size int64, password string) (*pdfReaderWrapper, error) {
	called := false
	reader, err := pdf.NewReaderEncrypted(r, size, func() string {
		if !called {
			called = true
			return password
		}
		return "" // stop retrying
	})
	if err != nil {
		return nil, err
	}
	return &pdfReaderWrapper{reader: reader}, nil
}

// GetPageText extracts text from a specific page
func (p *pdfReaderWrapper) GetPageText(pageNum int) string {
	page := p.reader.Page(pageNum)
	if page.V.IsNull() {
		return ""
	}

	text, err := page.GetPlainText(nil)
	if err != nil {
		return ""
	}
	return text
}
