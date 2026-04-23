package api

import (
	"io"
	"net/http"
)

// copyToResponse copies r into w, flushing if w supports it (gives the
// browser an early body for slow file streams). Wraps io.Copy so the
// API package owns the only http.Flusher import.
func copyToResponse(w http.ResponseWriter, r io.Reader) (int64, error) {
	if f, ok := w.(http.Flusher); ok {
		buf := make([]byte, 64*1024)
		var total int64
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return total, werr
				}
				total += int64(n)
				f.Flush()
			}
			if err == io.EOF {
				return total, nil
			}
			if err != nil {
				return total, err
			}
		}
	}
	return io.Copy(w, r)
}
