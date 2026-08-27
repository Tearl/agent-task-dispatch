package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	listenAddress = "127.0.0.1:8081"
	engineURL     = "http://127.0.0.1:8080"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	upstream, err := url.Parse(engineURL)
	if err != nil {
		logger.Error("invalid Engine upstream", "error", err)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
		logger.Warn("Engine public edge proxy failed", "error", proxyErr)
		http.Error(writer, "upstream unavailable", http.StatusBadGateway)
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" || !allowed(request.Method, request.URL.Path) {
			http.NotFound(writer, request)
			return
		}
		request.Header.Set("x-forwarded-proto", "https")
		request.Header.Set("x-forwarded-host", request.Host)
		proxy.ServeHTTP(writer, request)
	})
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("Engine public edge listening", "address", listenAddress)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Engine public edge stopped", "error", err)
		os.Exit(1)
	}
}

func allowed(method, path string) bool {
	return method == http.MethodPost && strings.HasPrefix(path, "/v1/agent-callbacks/")
}
