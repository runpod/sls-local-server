package testbeds

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"sls-local-server/packages/common"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	testConfig        []common.Test
	currentTestPtr    int = -1
	results           []common.Result
	testNumberChannel = make(chan int)
)

func parseTestConfig(log *zap.Logger) {
	if os.Getenv("RUNPOD_TEST") == "true" {
		tests := os.Getenv("RUNPOD_TESTS")

		// Parse JSON into testConfig
		if err := json.Unmarshal([]byte(tests), &testConfig); err != nil {
			log.Fatal("Failed to parse runpod tests",
				zap.Error(err))
		}

		log.Info("Parsed test config", zap.Any("testConfig", testConfig))
		for i, test := range testConfig {
			testConfig[i] = test
			testConfig[i].ID = &i
			if test.Timeout == nil {
				threeHundred := 300 * 1000
				testConfig[i].Timeout = &threeHundred
			}
		}
	} else {
		// errorMsg := "No tests found."
		return
		// sendResultsToGraphQL("FAILED", &errorMsg)
	}
}

type Handler struct {
	log *zap.Logger
}

func NewHandler(log *zap.Logger) *Handler {
	return &Handler{
		log: log,
	}
}

// Health endpoint for checking server status
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// GetStatus returns the status of a job
func (h *Handler) JobTake(c *gin.Context) {
	h.log.Info("Job take", zap.Int("current_test", currentTestPtr))
	fmt.Println("Job take", currentTestPtr)

	currentTestPtr++
	if currentTestPtr >= len(testConfig) {
		common.SendResultsToGraphQL("FAILED", nil, h.log, results)
		h.log.Error("No more tests", zap.Int("current_test", currentTestPtr))
		return
	}

	nextTestPayload := testConfig[currentTestPtr]
	testConfig[currentTestPtr].StartedAt = time.Now().UTC()
	h.log.Info("Job take", zap.Any("next_test_payload", nextTestPayload), zap.Any("current_test_ptr", currentTestPtr))

	go cancelJob(*nextTestPayload.Timeout, currentTestPtr, h.log)

	testNumberChannel <- currentTestPtr
	fmt.Println("currentTestPtr added to channel", currentTestPtr)

	c.JSON(http.StatusOK, gin.H{
		"delayTime":     0,
		"error":         "",
		"executionTime": nextTestPayload.Timeout,
		"id":            fmt.Sprintf("%d", currentTestPtr),
		"input":         nextTestPayload.Input,
		"retries":       0,
		"status":        200,
	})
}

func cancelJob(timeout int, jobIndex int, log *zap.Logger) {
	time.Sleep(time.Duration(timeout) * time.Millisecond)

	common.Mutex.Lock()
	defer common.Mutex.Unlock()
	if testConfig[jobIndex].Completed {
		return
	}

	// send a request to graphql with the job index and execution timeout result
	results = append(results, common.Result{
		ID:            *testConfig[jobIndex].ID,
		Name:          testConfig[jobIndex].Name,
		Status:        "FAILED",
		Error:         "Execution timeout exceeded",
		ExecutionTime: time.Since(testConfig[jobIndex].StartedAt).Milliseconds(),
	})

	errorMsg := "Execution timeout exceeded"
	common.SendResultsToGraphQL("FAILED", &errorMsg, log, results)
}

func (h *Handler) JobDone(c *gin.Context) {
	lastTest := testConfig[currentTestPtr]
	endTime := time.Now().UTC()

	var payload map[string]interface{}
	if err := c.BindJSON(&payload); err != nil {
		h.log.Error("Failed to parse request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
		})
		return
	}
	h.log.Info("Job done payload", zap.Any("payload", payload))

	if payload["error"] != nil {
		results = append(results, common.Result{
			ID:            *lastTest.ID,
			Name:          lastTest.Name,
			Error:         payload["error"].(string),
			ExecutionTime: endTime.Sub(testConfig[currentTestPtr].StartedAt).Milliseconds(),
			Status:        "FAILED",
		})
		common.SendResultsToGraphQL("FAILED", nil, h.log, results)

		h.log.Error("Error found in payload", zap.Any("payload", payload))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Error found in payload",
		})
		return
	}

	results = append(results, common.Result{
		ID:            currentTestPtr,
		Name:          lastTest.Name,
		Status:        "SUCCESS",
		Error:         "",
		ExecutionTime: endTime.Sub(testConfig[currentTestPtr].StartedAt).Milliseconds(),
	})

	h.log.Info("Job done", zap.Any("results", results), zap.Any("current_test_ptr", currentTestPtr), zap.Any("end_time", endTime), zap.Any("start_time", testConfig[currentTestPtr].StartedAt))

	testConfig[currentTestPtr].Completed = true

	if currentTestPtr == len(testConfig)-1 {
		common.SendResultsToGraphQL("PASSED", nil, h.log, results)
		h.log.Error("No more tests", zap.Int("current_test", currentTestPtr))
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "cancelled",
		"message": "Job successfully cancelled",
	})
}

// LoggerMiddleware creates a middleware for logging requests
func LoggerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Log request
		logger.Info("Incoming request",
			zap.String("path", path),
			zap.String("query", query),
			zap.String("method", c.Request.Method),
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
		)

		// Process request
		c.Next()

		// Log response
		latency := time.Since(start)

		logger.Info("Request completed",
			zap.Duration("latency", latency),
		)
	}
}

func RunServer(log *zap.Logger) {
	log.Info("Starting server")
	parseTestConfig(log)
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	// Add recovery middleware
	r.Use(gin.Recovery())
	// Add logging middleware
	r.Use(LoggerMiddleware(log))

	h := NewHandler(log)

	r.GET("/health", h.Health)
	workerAuthorized := r.Group("/v2/:model")
	{
		workerAuthorized.GET("/job-take/:pod_id", h.JobTake)
		workerAuthorized.POST("/job-done/:pod_id/:id", h.JobDone)
	}

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "19981"
	}

	log.Info("Server starting", zap.String("port", port))

	// Start server
	if err := r.Run(":" + port); err != nil {
		errorMsg := "Failed to start tests. Please push your changes again!"
		common.SendResultsToGraphQL("FAILED", &errorMsg, log, results)
		log.Fatal("Failed to start server", zap.Error(err))
	}
}
