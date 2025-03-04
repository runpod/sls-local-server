package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sls-local-server/packages/ide"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var mutex = &sync.Mutex{}
var gqlMutex = &sync.Mutex{}
var Version = "dev"
var testNumberChannel = make(chan int)
var SYSTEM_INITIALIZED = false
var slash = "/"
var folder = &slash

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

func (h *Handler) Health(c *gin.Context) {
	if !SYSTEM_INITIALIZED {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
		})
	}

	// Check the heartbeat file for code-server
	heartbeatFile := "/root/.local/share/code-server/heartbeat"
	fileInfo, err := os.Stat(heartbeatFile)

	var heartbeat time.Time
	if err == nil {
		// Store the last modified time if file exists
		heartbeat = fileInfo.ModTime()
		log.Info("Code-server heartbeat found", zap.Time("lastModified", heartbeat))
	} else {
		// If file doesn't exist or can't be accessed
		log.Warn("Could not access code-server heartbeat file", zap.Error(err))
		heartbeat = time.Time{} // Zero time
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"folder":    *folder,
		"heartbeat": heartbeat,
	})
}

func sendResultsToGraphQL(status string, errorReason *string) {
	gqlMutex.Lock()
	defer gqlMutex.Unlock()

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

	// send request
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

	time.Sleep(time.Duration(300) * time.Second)

	log.Info("Results sent to GraphQL", zap.Any("results", results))
}

func cancelJob(timeout int, jobIndex int) {
	time.Sleep(time.Duration(timeout) * time.Millisecond)

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
	fmt.Println("Job take", currentTestPtr)

	currentTestPtr++
	if currentTestPtr >= len(testConfig) {
		sendResultsToGraphQL("FAILED", nil)
		h.log.Error("No more tests", zap.Int("current_test", currentTestPtr))
		return
	}

	nextTestPayload := testConfig[currentTestPtr]
	testConfig[currentTestPtr].StartedAt = time.Now().UTC()
	h.log.Info("Job take", zap.Any("next_test_payload", nextTestPayload), zap.Any("current_test_ptr", currentTestPtr))

	go cancelJob(*nextTestPayload.Timeout, currentTestPtr)

	testNumberChannel <- currentTestPtr
	fmt.Println("currentTestPtr added to channel", currentTestPtr)

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

	h.log.Info("Job done", zap.Any("results", results), zap.Any("current_test_ptr", currentTestPtr), zap.Any("end_time", endTime), zap.Any("start_time", testConfig[currentTestPtr].StartedAt))

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

func sendLogsToTinyBird(logBuffer chan string) {
	// Start goroutine to collect and send logs
	buffer := make([]map[string]interface{}, 0)
	tinybirdToken := os.Getenv("TINYBIRD_TOKEN")
	runpodPodId := os.Getenv("RUNPOD_POD_ID")

	testNumber := 7081

	go func() {
		for num := range testNumberChannel {
			testNumber = num
		}
	}()

	for logMsg := range logBuffer {
		level := "info"
		logMessageList := strings.Split(logMsg, "\n")

		for _, logMessage := range logMessageList {
			fmt.Println("logMsg: ### ", logMessage)
			// Create log entry
			if strings.HasPrefix(logMessage, "#ERROR:") {
				level = "error"
				logMessage = strings.TrimPrefix(logMessage, "#ERROR:")
			}
			logEntry := map[string]interface{}{
				"testId":     os.Getenv("RUNPOD_TEST_ID"),
				"level":      level,
				"podId":      runpodPodId,
				"testNumber": testNumber,
				"message":    logMessage,
				"timestamp":  time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			}

			buffer = append(buffer, logEntry)
		}

		// Send logs when buffer is full or channel is closed
		if len(buffer) >= 16 {
			url := "https://api.us-east.tinybird.co/v0/events?wait=true&name=sls_test_logs_v1"

			var records []string
			for _, entry := range buffer {
				jsonBytes, err := json.Marshal(entry)
				if err == nil {
					records = append(records, string(jsonBytes))
				}
			}
			payload := strings.Join(records, "\n")

			go func(payload string) {
				// Create and send request
				req, err := http.NewRequest("POST", url, strings.NewReader(payload))
				if err == nil {
					req.Header.Set("Authorization", "Bearer "+tinybirdToken)
					req.Header.Set("Content-Type", "text/plain")

					client := &http.Client{Timeout: 2 * time.Second}
					resp, err := client.Do(req)
					if err != nil {
						log.Error("Failed to send logs to tinybird", zap.Error(err))
					} else if resp.StatusCode > 200 {
						body, _ := io.ReadAll(resp.Body)
						log.Error("Tinybird request failed",
							zap.Int("status", resp.StatusCode),
							zap.String("response", string(body)))
						resp.Body.Close()
					}
				}

				buffer = make([]map[string]interface{}, 0)
			}(payload)
		}
	}

	// Send any remaining logs in buffer
	if len(buffer) > 0 {
		url := "https://api.us-east.tinybird.co/v0/events?wait=true&name=sls_test_logs_v1"

		var records []string
		for _, entry := range buffer {
			jsonBytes, err := json.Marshal(entry)
			if err == nil {
				records = append(records, string(jsonBytes))
			}
		}
		payload := strings.Join(records, "\n")

		req, err := http.NewRequest("POST", url, strings.NewReader(payload))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+tinybirdToken)
			req.Header.Set("Content-Type", "text/plain")

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				log.Error("Failed to send final logs to tinybird", zap.Error(err))
			} else if resp.StatusCode > 200 {
				body, _ := io.ReadAll(resp.Body)
				log.Error("Final tinybird request failed",
					zap.Int("status", resp.StatusCode),
					zap.String("response", string(body)))
				resp.Body.Close()
			}
		}
	}
}

func RunCommand(command string) error {
	// Create a buffered channel for logs
	logBuffer := make(chan string, 16)
	logBuffer <- fmt.Sprintf("Running command: %s", command)

	log.Info("Running command", zap.String("command", command))
	// Split the command string into command and arguments
	parts := strings.Fields(command)
	var cmd *exec.Cmd
	if len(parts) > 0 {
		fmt.Println("split into strings", parts)
		cmd = exec.Command(parts[0], parts[1:]...)
	} else {
		log.Error("Empty command provided")
		return fmt.Errorf("empty command provided")
	}
	cmd.Env = append(os.Environ(), "RUNPOD_LOG_LEVEL=INFO")

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to create stdout pipe: %s", err.Error())
		log.Error("Failed to create stdout pipe", zap.Error(err))
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to create stderr pipe: %s", err.Error())
		log.Error("Failed to create stderr pipe", zap.Error(err))
		return err
	}

	err = cmd.Start()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to start command: %s", err.Error())
		errorMsg := fmt.Sprintf("Failed to start command: %s", err.Error())
		sendResultsToGraphQL("FAILED", &errorMsg)
		fmt.Println("Failed to start command: ", err.Error())
		log.Error("Failed to start command", zap.Error(err))
		return err
	}

	go sendLogsToTinyBird(logBuffer)

	// Start goroutines to continuously read from pipes
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				log.Info("Command stdout", zap.ByteString("output", buf[:n]))

				// Add log to buffer channel
				select {
				case logBuffer <- string(buf[:n]):
					// Log added to buffer
				default:
					// Channel full, log discarded
					log.Warn("Log buffer full, discarding log")
				}

			}
			if err != nil {
				logBuffer <- fmt.Sprintf("Failed to read stdout: %s", err.Error())
				break
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Info("Command stderr", zap.String("output", string(buf[:n])))

				if strings.Contains(string(buf[:n]), "Failed to read stdout: EOF") {
					errorMsg := "Command closed"
					sendResultsToGraphQL("FAILED", &errorMsg)
					return
				}
				// Add log to buffer channel
				select {
				case logBuffer <- fmt.Sprintf("#ERROR: %s", string(buf[:n])):
					// Log added to buffer
				default:
					// Channel full, log discarded
					log.Warn("Log buffer full, discarding log")
				}

			}
			if err != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		errorMsg := fmt.Sprintf("Command closed: %s", err.Error())
		fmt.Println("Command closed: ", errorMsg)
		sendResultsToGraphQL("FAILED", &errorMsg)
		return nil
	}

	time.Sleep(time.Duration(5) * time.Second)
	close(logBuffer)
	errorMsg := "Command closed. Please view the logs for more information."
	sendResultsToGraphQL("FAILED", &errorMsg)
	return nil
}

func RunHealthServer() {
	gin.SetMode(gin.ReleaseMode)
	h := NewHandler(log)

	r := gin.New()
	// Add recovery middleware
	r.Use(gin.Recovery())
	// Add logging middleware
	r.Use(LoggerMiddleware(log))

	r.GET("/health", h.Health)

	if err := r.Run(":" + "8079"); err != nil {
		errorMsg := "Failed to start tests. Please push your changes again!"
		sendResultsToGraphQL("FAILED", &errorMsg)
		log.Fatal("Failed to start server", zap.Error(err))
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

func terminateIdePod() {
	runpodPodIDEJwt := os.Getenv("RUNPOD_IDE_POD_JWT")
	webhookUrl := os.Getenv("RUNPOD_IDE_POD_WEBHOOK_URL")

	if runpodPodIDEJwt == "" || webhookUrl == "" {
		log.Error("RUNPOD_IDE_POD_JWT or RUNPOD_IDE_POD_WEBHOOK_URL not set")
		return
	}

	req, err := http.NewRequest("POST", webhookUrl, nil)
	if err != nil {
		log.Error("Failed to create request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runpodPodIDEJwt)

	// send request
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

	time.Sleep(time.Duration(300) * time.Second)
}

func main() {
	command := flag.String("command", "python3 handler.py", "the user command to run")
	check := flag.String("check", "null", "the version of the server to run")
	aiApiIde := flag.String("ai-api-ide", "null", "should the binary server an ide")
	folder = flag.String("folder", "/", "the folder to run the command in")

	flag.Parse()

	if check != nil && *check == "version" {
		fmt.Println(Version)
		return
	}

	log, err := zap.NewProduction()
	if err != nil {
		log.Error("Failed to initialize logger", zap.Error(err))
		return
	}
	defer log.Sync()

	if aiApiIde != nil && *aiApiIde == "true" {
		go func() {
			RunHealthServer()
		}()

		err := ide.DownloadIde(log)
		if err != nil {
			log.Error("Failed to download ide", zap.Error(err))
			terminateIdePod()
			return
		}

		SYSTEM_INITIALIZED = true
		cmd := fmt.Sprintf("PASSWORD=runpod code-server --bind-addr 0.0.0.0:8080 --auth password --welcome-text \"Welcome to the Runpod IDE\" --app-name \"Runpod IDE\" %s", *folder)
		err = RunCommand(cmd)
		if err != nil {
			log.Error("Failed to run command", zap.Error(err))
			terminateIdePod()
			return
		}
	} else {
		go func() {
			var modifiedCommand string
			if command != nil {
				modifiedCommand = *command
				modifiedCommand = strings.Replace(modifiedCommand, "/bin/sh -c ", "", 1)
				modifiedCommand = strings.Replace(modifiedCommand, "/bin/bash -o pipefail -c ", "", 1)
			}
			fmt.Println("Running command", modifiedCommand)
			RunCommand(modifiedCommand)
		}()
		RunServer()
	}
}
