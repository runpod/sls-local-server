package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
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

// RunInference handles the main inference request
func (h *Handler) RunInference(c *gin.Context) {
	var request struct {
		Input map[string]interface{} `json:"input"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		h.log.Error("Failed to parse request", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
		})
		return
	}

	// TODO: Add your inference logic here
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"output": request.Input,
	})
}

// GetStatus returns the status of a job
func (h *Handler) JobTake(c *gin.Context) {
	h.log.Info("Job take", zap.Int("current_test", currentTest))

	currentTest++
	nextTestPayload := testConfig[currentTest]
	h.log.Info("Job take", zap.Any("next_test_payload", nextTestPayload))

	c.JSON(http.StatusOK, gin.H{
		"delayTime":     0,
		"error":         "",
		"executionTime": nextTestPayload.Timeout,
		"id":            currentTest,
		"input":         nextTestPayload.Input,
		"retries":       0,
		"status":        200,
	})
}

// CancelJob cancels a running job
func (h *Handler) JobDone(c *gin.Context) {
	jobID := c.Param("id")

	h.log.Info("Job done", zap.String("job_id", jobID))
	id, err := strconv.Atoi(jobID)
	if err != nil {
		h.log.Error("Failed to parse job ID", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid job ID",
		})
		return
	}

	// Get test case for this job ID
	if id >= len(testConfig) {
		h.log.Error("Job ID out of range", zap.Int("id", id))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid job ID",
		})
		return
	}

	test := testConfig[id]
	var result interface{}
	if err := c.BindJSON(&result); err != nil {
		h.log.Error("Failed to parse request body", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
		})
		return
	}

	h.log.Info("Job done", zap.Any("actual result", result), zap.Any("expected result", test))

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
		"id":      jobID,
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
		status := c.Writer.Status()
		
		logger.Info("Request completed",
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.Int("body_size", c.Writer.Size()),
			zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()),
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
