package ratelimiter

import (
    "bytes"
    "encoding/json"
    "io/ioutil"
    "net/http"
    "sync"
    "time"
)

type MethodRateLimiter struct {
    rate   float64
    burst  int
    tokens map[string]float64 // Key format: "nodeId-method"
    last   map[string]time.Time
    mutex  sync.Mutex
    methods map[string]bool
}

type requestBody struct {
    Method string `json:"method"`
}

func NewMethodRateLimiter(rate float64, burst int) *MethodRateLimiter {
    methods := map[string]bool{
        "eth_getChainId":           true,
        "eth_getBlockNumber":       true,
        "eth_getBlockByNumber":     true,
        "eth_getBlockReceipts":     true,
        "eth_getTransactionReceipt": true,
    }

    return &MethodRateLimiter{
        rate:    rate,
        burst:   burst,
        tokens:  make(map[string]float64),
        last:    make(map[string]time.Time),
        methods: methods,
    }
}

func (mrl *MethodRateLimiter) Allow(r *http.Request) (bool, error) {
    nodeId := r.URL.Query().Get("Id")
    if nodeId == "" {
        return true, nil // Allow if no nodeId is provided
    }

    body, err := ioutil.ReadAll(r.Body)
    if err != nil {
        return false, err
    }
    r.Body = ioutil.NopCloser(bytes.NewReader(body))

    var reqBody requestBody
    if err := json.Unmarshal(body, &reqBody); err != nil {
        return false, err
    }

    // Check if the method should be rate limited
    if !mrl.methods[reqBody.Method] {
        return true, nil // Allow if method is not in the rate-limited list
    }

    rateLimitKey := nodeId + "-" + reqBody.Method

    mrl.mutex.Lock()
    defer mrl.mutex.Unlock()

    now := time.Now()
    tokens, exists := mrl.tokens[rateLimitKey]
    last, timeExists := mrl.last[rateLimitKey]

    if !exists || !timeExists {
        mrl.tokens[rateLimitKey] = float64(mrl.burst) - 1
        mrl.last[rateLimitKey] = now
        return true, nil
    }

    elapsed := now.Sub(last).Seconds()
    tokens += elapsed * mrl.rate
    if tokens > float64(mrl.burst) {
        tokens = float64(mrl.burst)
    }

    if tokens < 1 {
        return false, nil // Rate limit exceeded
    }

    tokens-- // Consume one token for the current request
    mrl.tokens[rateLimitKey] = tokens
    mrl.last[rateLimitKey] = now

    return true, nil
}