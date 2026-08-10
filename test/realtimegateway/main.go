package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	server := &http.Server{Addr: ":8080", Handler: http.HandlerFunc(proxy), ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func proxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sync/stars" {
		http.NotFound(w, r)
		return
	}
	upstream, err := url.Parse(strings.TrimRight(requiredEnv("ADENOSINE_B_URL"), "/") + r.URL.RequestURI())
	if err != nil {
		http.Error(w, "invalid upstream", http.StatusBadGateway)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), nil)
	if err != nil {
		http.Error(w, "construct upstream request", http.StatusBadGateway)
		return
	}
	phase := r.URL.Query().Get("cache-buster")
	if phase == "create-live" || phase == "delete-live" {
		request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{
			WroteRequest: func(info httptrace.WroteRequestInfo) {
				if info.Err == nil {
					_ = os.WriteFile("/tmp/"+phase+"-ready", []byte("request written to B\n"), 0o600)
				}
			},
		}))
	}
	response, err := (&http.Client{Timeout: 55 * time.Second}).Do(request)
	if err != nil {
		http.Error(w, "B sync request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for _, name := range []string{"Content-Type", "Electric-Handle", "Electric-Offset", "Electric-Up-To-Date"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintln(os.Stderr, name+" is required")
		os.Exit(2)
	}
	return value
}
