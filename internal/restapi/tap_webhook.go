package restapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const maxTapWebhookBody = 1 << 20

func newTapWebhookHandler(processor FederationProcessor, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		username, suppliedPassword, ok := r.BasicAuth()
		if password == "" || !ok || !constantTimeEqual(username, "admin") || !constantTimeEqual(suppliedPassword, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tap"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxTapWebhookBody)
		body, err := io.ReadAll(r.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil || !json.Valid(body) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := processor.Process(r.Context(), body); err != nil {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func constantTimeEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
