package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var mutex = &sync.Mutex{}
var Version = "dev"

type Test struct {
	ID    *int        `json:"id,omitempty"`
	Name  string      `json:"name"`
	Input interface{} `json:"input"`

	Timeout *int `json:"timeout"`

	StartedAt time.Time `json:"startedAt,omitempty"`
	Completed bool      `json:"completed,omitempty"`
}

type ExpectedOutput struct {
	Payload interface{} `json:"payload"`
	Error   string      `json:"error"`
}

type Result struct {
	ID            int    `json:"id"`
	Name          string `json:"name,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error"`
	ExecutionTime int64  `json:"executionTime"`
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
	testConfig     []Test
	log            *zap.Logger
	currentTestPtr int = -1
	results        []Result
	validTestModes = map[string]bool{
		"COMPARE_OUTPUTS_EQUAL":               true,
		"COMPARE_OUTPUTS_SIMILARITY_WITH_LLM": true,
		"COMPARE_OUTPUTS_NOT_NULL":            true,
	}
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
				threeHundred := 300
				testConfig[i].Timeout = &threeHundred
			}
		}
	} else {
		errorMsg := "No tests found."
		sendResultsToGraphQL("FAILED", &errorMsg)
	}
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

func (h *Handler) CompareOutputsWithLLM(expectedOutput interface{}, actualOutput interface{}, llmSimilarityThreshold float64) (bool, error) {
	similarity := 0.0

	if llmSimilarityThreshold > similarity {
		return true, nil
	}

	return false, nil
}

func sendResultsToGraphQL(status string, errorReason *string) {
	runpodPodId := os.Getenv("RUNPOD_POD_ID")
	jwtToken := os.Getenv("RUNPOD_JWT_TOKEN")
	runpodTestId := os.Getenv("RUNPOD_TEST_ID")

	webhookUrl := os.Getenv("RUNPOD_TEST_WEBHOOK_URL")
	if webhookUrl == "" {
		log.Error("RUNPOD_TEST_WEBHOOK_URL not set")
		return
	}

	// Convert results to JSON
	jsonData, err := json.Marshal(map[string]interface{}{
		"podId":   runpodPodId,
		"testId":  runpodTestId,
		"results": results,
		"status":  status,
		"error":   errorReason,
	})
	if err != nil {
		log.Error("Failed to marshal results", zap.Error(err))
		return
	}

	// Create request
	req, err := http.NewRequest("POST", webhookUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error("Failed to create request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("Failed to send request", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error("Request failed", zap.Int("status", resp.StatusCode))
		return
	}

	time.Sleep(time.Duration(20) * time.Second)

	log.Info("Results sent to GraphQL", zap.Any("results", results))
}

func cancelJob(timeout int, jobIndex int) {
	time.Sleep(time.Duration(timeout) * time.Second)

	mutex.Lock()
	defer mutex.Unlock()
	if testConfig[jobIndex].Completed {
		return
	}

	// send a request to graphql with the job index and execution timeout result
	results = append(results, Result{
		ID:            *testConfig[jobIndex].ID,
		Name:          testConfig[jobIndex].Name,
		Status:        "FAILED",
		Error:         "Execution timeout exceeded",
		ExecutionTime: time.Since(testConfig[jobIndex].StartedAt).Milliseconds(),
	})

	errorMsg := "Execution timeout exceeded"
	sendResultsToGraphQL("FAILED", &errorMsg)
}

// GetStatus returns the status of a job
func (h *Handler) JobTake(c *gin.Context) {
	h.log.Info("Job take", zap.Int("current_test", currentTestPtr))

	currentTestPtr++

	nextTestPayload := testConfig[currentTestPtr]
	testConfig[currentTestPtr].StartedAt = time.Now().UTC()
	h.log.Info("Job take", zap.Any("next_test_payload", nextTestPayload))

	go cancelJob(*nextTestPayload.Timeout, currentTestPtr)

	JSON(c, 200, gin.H{
		"delayTime":     0,
		"error":         "",
		"executionTime": nextTestPayload.Timeout,
		"id":            fmt.Sprintf("%d", currentTestPtr),
		"input":         nextTestPayload.Input,
		"retries":       0,
		"status":        200,
	})
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
		results = append(results, Result{
			ID:            *lastTest.ID,
			Name:          lastTest.Name,
			Error:         payload["error"].(string),
			ExecutionTime: endTime.Sub(testConfig[currentTestPtr].StartedAt).Milliseconds(),
			Status:        "FAILED",
		})
		sendResultsToGraphQL("FAILED", nil)

		h.log.Error("Error found in payload", zap.Any("payload", payload))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Error found in payload",
		})
		return
	}

	results = append(results, Result{
		ID:            currentTestPtr,
		Name:          lastTest.Name,
		Status:        "SUCCESS",
		Error:         "",
		ExecutionTime: endTime.Sub(testConfig[currentTestPtr].StartedAt).Milliseconds(),
	})
	testConfig[currentTestPtr].Completed = true

	if currentTestPtr == len(testConfig)-1 {
		sendResultsToGraphQL("PASSED", nil)
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

func runCommand(command string) {
	log.Info("Running command", zap.String("command", command))
	cmd := exec.Command("sh", "-c", command)
	err := cmd.Start()
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to start command: %s", err.Error())
		sendResultsToGraphQL("FAILED", &errorMsg)
		log.Fatal("Failed to start command", zap.Error(err))
	}
}

func RunServer() {
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
		errorMsg := "Failed to start tests. Please push your changes again!"
		sendResultsToGraphQL("FAILED", &errorMsg)
		log.Fatal("Failed to start server", zap.Error(err))
	}
}

func main() {
	command := flag.String("command", "python3 handler.py", "the user command to run")
	check := flag.String("check", "null", "the version of the server to run")

	flag.Parse()

	if check != nil && *check == "version" {
		fmt.Println(Version)
		return
	}
	defer log.Sync()

	go func() {
		runCommand(*command)
	}()
	RunServer()
}
