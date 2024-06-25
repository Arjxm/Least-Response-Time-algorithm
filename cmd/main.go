package main

import (
	"fmt"
	"load-balancer/internal/balancer"
	"load-balancer/internal/ratelimiter"
	"log"
	"net/http"
	"os"
)

func main() {
	fmt.Println("Starting load balancer")

	// Create a logger
	logger := log.New(os.Stdout, "LoadBalancer: ", log.Ldate|log.Ltime|log.Lshortfile)

	// Create a new load balancer with Redis and logger
	lb, err := balancer.NewLoadBalancer("localhost:6379", logger)
	if err != nil {
		log.Fatalf("Failed to create load balancer: %v", err)
	}
	defer lb.Close()

	// Add backends
	backends := []string{
		"http://ms-backend:5001",
		"http://ms-backend1:5000",
		"http://ms-backend2:5002",
		"http://ms-backend3:5003",
	}

	for _, backend := range backends {
		if err := lb.AddBackend(backend); err != nil {
			logger.Printf("Failed to add backend %s: %v", backend, err)
		} else {
			fmt.Printf("Added backend: %s\n", backend)
		}
	}

	methodRateLimiter := ratelimiter.NewMethodRateLimiter(10, 60)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		allowed, err := methodRateLimiter.Allow(r)
		if err != nil {
			logger.Printf("Error checking rate limit: %v", err)
			http.Error(w, "Error checking rate limit", http.StatusInternalServerError)
			return
		}
		if !allowed {
			logger.Printf("Rate limit exceeded for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, "Rate limit exceeded or request body too large", http.StatusTooManyRequests)
			return
		}

		lb.ServeHTTP(w, r)
	})

	fmt.Println("Load balancer is running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
