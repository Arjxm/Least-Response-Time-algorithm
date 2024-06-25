package balancer

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"load-balancer/internal/backend"
)

type LoadBalancer struct {
	backends []*backend.Backend
	mutex    sync.RWMutex
	rdb      *redis.Client
	logger   *log.Logger
}

func NewLoadBalancer(redisAddr string, logger *log.Logger) (*LoadBalancer, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &LoadBalancer{
		rdb:    rdb,
		logger: logger,
	}, nil
}

func (lb *LoadBalancer) AddBackend(urlStr string) error {
	url, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	newBackend := backend.NewBackend(url)
	lb.backends = append(lb.backends, newBackend)

	ctx := context.Background()
	err = lb.rdb.HSet(ctx, "backends", urlStr, "0").Err() // Initialize response time to 0
	if err != nil {
		return err
	}

	return nil
}

func (lb *LoadBalancer) NextBackend() (*backend.Backend, error) {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()

	if len(lb.backends) == 0 {
		return nil, errors.New("no backends available")
	}

	ctx := context.Background()
	
	// Get all backend response times
	responseTimes, err := lb.rdb.HGetAll(ctx, "backends").Result()
	if err != nil {
		return nil, err
	}

	var minResponseTime float64 = float64(^uint64(0) >> 1) // Max float64 value
	var selectedBackendURL string

	for urlStr, responseTimeStr := range responseTimes {
		responseTime, _ := time.ParseDuration(responseTimeStr)
		if responseTime.Seconds() < minResponseTime {
			minResponseTime = responseTime.Seconds()
			selectedBackendURL = urlStr
		}
	}

	if selectedBackendURL == "" {
		return nil, errors.New("no backend selected")
	}

	for _, b := range lb.backends {
		if b.URL.String() == selectedBackendURL {
			return b, nil
		}
	}

	return nil, errors.New("backend not found")
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Log incoming request
	lb.logRequest(r, start)

	backend, err := lb.NextBackend()
	if err != nil {
		http.Error(w, "No backend available", http.StatusServiceUnavailable)
		lb.logError(r, err, start)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(backend.URL)
	
	// Wrap the ResponseWriter to capture the status code
	wrappedWriter := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}
	
	proxy.ServeHTTP(wrappedWriter, r)

	duration := time.Since(start)

	// Update response time in Redis
	ctx := context.Background()
	lb.rdb.HSet(ctx, "backends", backend.URL.String(), duration.String())

	// Log completed request
	lb.logCompletedRequest(r, wrappedWriter.statusCode, duration, backend.URL)
}

func (lb *LoadBalancer) Close() error {
	return lb.rdb.Close()
}

func (lb *LoadBalancer) logRequest(r *http.Request, start time.Time) {
	lb.logger.Printf(
		"[%s] Incoming request: %s %s %s from %s",
		start.Format(time.RFC3339),
		r.Method,
		r.URL.Path,
		r.Proto,
		r.RemoteAddr,
	)
}

func (lb *LoadBalancer) logCompletedRequest(r *http.Request, statusCode int, duration time.Duration, backendURL *url.URL) {
	lb.logger.Printf(
		"[%s] Completed request: %s %s %s from %s | Status: %d | Duration: %v | Backend: %s",
		time.Now().Format(time.RFC3339),
		r.Method,
		r.URL.Path,
		r.Proto,
		r.RemoteAddr,
		statusCode,
		duration,
		backendURL,
	)
}

func (lb *LoadBalancer) logError(r *http.Request, err error, start time.Time) {
	lb.logger.Printf(
		"[%s] Error processing request: %s %s %s from %s | Error: %v | Duration: %v",
		time.Now().Format(time.RFC3339),
		r.Method,
		r.URL.Path,
		r.Proto,
		r.RemoteAddr,
		err,
		time.Since(start),
	)
}

// responseWriterWrapper is a custom ResponseWriter that captures the status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rww *responseWriterWrapper) WriteHeader(statusCode int) {
	rww.statusCode = statusCode
	rww.ResponseWriter.WriteHeader(statusCode)
}