package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

type Test struct {
	ID             int               `json:"id,omitempty"`
	Name           string            `json:"name"`
	Input          map[string]string `json:"input"`
	ExpectedOutput interface{}       `json:"expected_output"`
	ExpectedStatus int               `json:"expected_status"`
	Timeout        int               `json:"timeout"`
}

type Results struct {
	ID     int `json:"id"`
	Status int `json:"status"`

	ExpectedStatus int `json:"expected_status"`
	ActualStatus   int `json:"actual_status"`

	ExpectedOutput interface{} `json:"expected_output"`
	ActualOutput   interface{} `json:"actual_output"`

	Timeout        int `json:"timeout"`
	ActualDuration int `json:"actual_duration"`

	Error string `json:"error"`
}

type Handler struct {
	log *zap.Logger
}

func NewHandler(log *zap.Logger) *Handler {
	return &Handler{
		log: log,
	}
}

var (
	testConfig  []Test
	log         *zap.Logger
	currentTest int = -1
)

func Marshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(t)
	return buffer.Bytes(), err
}

func JSON(c *gin.Context, code int, obj interface{}) {
	c.Header("Content-Type", "application/json")
	jsonStr, _ := Marshal(obj)
	c.String(code, string(jsonStr))
}

func init() {
	// Initialize logger
	var err error
	if os.Getenv("ENV") == "local" {
		log = prettyconsole.NewLogger(zap.DebugLevel)
	} else {
		log, err = zap.NewProduction()
		if err != nil {
			panic("Failed to initialize logger: " + err.Error())
		}
	}

	log.Info(os.Getenv("RUNPOD_TEST"))
	if os.Getenv("RUNPOD_TEST") == "true" {
		testFilePath := os.Getenv("RUNPOD_TEST_FILE")
		data, err := os.ReadFile(testFilePath)
		if err != nil {
			log.Fatal("Failed to read runpod.tests.json",
				zap.Error(err))
		}

		// Parse JSON into testConfig
		if err := json.Unmarshal(data, &testConfig); err != nil {
			log.Fatal("Failed to parse runpod.tests.json",
				zap.Error(err))
		}

		log.Info("Parsed test config", zap.Any("testConfig", testConfig))
		for i, test := range testConfig {
			test.ID = i
		}
	}
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// GetStatus returns the status of a job
func (h *Handler) JobTake(c *gin.Context) {
	h.log.Info("Job take", zap.Int("current_test", currentTest))

	currentTest++

	if currentTest >= len(testConfig) {
		time.Sleep(time.Duration(10) * time.Second)
		h.log.Error("No more tests", zap.Int("current_test", currentTest))
		c.JSON(500, gin.H{
			"error": "No more tests",
		})
		return
	}

	nextTestPayload := testConfig[currentTest]
	h.log.Info("Job take", zap.Any("next_test_payload", nextTestPayload))

	JSON(c, 200, gin.H{
		"delayTime":     0,
		"error":         "",
		"executionTime": nextTestPayload.Timeout,
		"id":            fmt.Sprintf("%d", currentTest),
		"input":         nextTestPayload.Input,
		"retries":       0,
		"status":        200,
	})
}

// CancelJob cancels a running job
func (h *Handler) JobDone(c *gin.Context) {
	// jobID := c.Param("id")

	// h.log.Info("Job done", zap.String("job_id", jobID))
	// id, err := strconv.Atoi(jobID)
	// if err != nil {
	// 	h.log.Error("Failed to parse job ID", zap.Error(err))
	// 	c.JSON(http.StatusBadRequest, gin.H{
	// 		"error": "Invalid job ID",
	// 	})
	// 	return
	// }

	// // Get test case for this job ID
	// if id >= len(testConfig) {
	// 	h.log.Error("Job ID out of range. Sleeping for 30 seconds.", zap.Int("id", id))
	// 	time.Sleep(time.Duration(30) * time.Second)
	// 	c.JSON(http.StatusBadRequest, gin.H{
	// 		"error": "Invalid job ID",
	// 	})
	// 	return
	// }

	// test := testConfig[id]
	// var result interface{}
	// if err := c.BindJSON(&result); err != nil {
	// 	h.log.Error("Failed to parse request body", zap.Error(err))
	// 	c.JSON(http.StatusBadRequest, gin.H{
	// 		"error": "Invalid request body",
	// 	})
	// 	return
	// }

	var payload map[string]interface{}
	if err := c.BindJSON(&payload); err != nil {
		h.log.Error("Failed to parse request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
		})
		return
	}
	h.log.Info("Job done payload", zap.Any("payload", payload))

	// h.log.Info("Job done", zap.Any("actual result", result), zap.Any("expected result", test))

	// time.Sleep(time.Duration(30) * time.Second)

	// Compare results
	// testResult := Results{
	// 	ID:             id,
	// 	ExpectedStatus: test.ExpectedStatus,
	// 	ActualStatus:   int(result["status"].(float64)),
	// 	ExpectedOutput: test.ExpectedOutput,
	// 	ActualOutput:   result["output"],
	// 	Timeout:        test.Timeout,
	// 	ActualDuration: int(result["executionTime"].(float64)),
	// }

	// if err, ok := result["error"]; ok {
	// 	testResult.Error = err.(string)
	// }

	// TODO: Implement job cancellation logic
	c.JSON(http.StatusOK, gin.H{
		// "id":      jobID,
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

func main() {
	defer log.Sync()

	log.Info("Starting server")

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
		log.Fatal("Failed to start server", zap.Error(err))
	}
}
