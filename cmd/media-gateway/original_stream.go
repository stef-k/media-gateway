package main

import (
	"io"
	"log/slog"
	"net/http"
	"time"
)

// streamOriginal refreshes a finite downstream deadline for each write and flush.
// Explicit writes avoid io.Copy fast paths bypassing the inactivity boundary.
func streamOriginal(w http.ResponseWriter, r *http.Request, body io.Reader, logger *slog.Logger, inactivity time.Duration) {
	controller := http.NewResponseController(w)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := body.Read(buffer)
		if n > 0 {
			if err := controller.SetWriteDeadline(time.Now().Add(inactivity)); err != nil {
				abortStream(r, logger)
			}
			written, err := w.Write(buffer[:n])
			if err != nil || written != n {
				abortStream(r, logger)
			}
			if err := controller.Flush(); err != nil {
				abortStream(r, logger)
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			abortStream(r, logger)
		}
	}
}

// abortStream never adds a plaintext suffix and keeps client cancellation quiet.
func abortStream(r *http.Request, logger *slog.Logger) {
	if r.Context().Err() == nil {
		logger.Warn("media stream failed")
	}
	panic(http.ErrAbortHandler)
}
