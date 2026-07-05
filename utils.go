package bdds

import (
	"encoding/json"
	"io"
)

// readJSON reads and unmarshals JSON from a reader
func readJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	progressFn func(bytesWritten, totalBytes int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)
	if pr.progressFn != nil {
		pr.progressFn(pr.current, pr.total)
	}
	return n, err
}

// countingWriter wraps an io.Writer and counts bytes written, so download
// retries can detect a partially written destination.
type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}
