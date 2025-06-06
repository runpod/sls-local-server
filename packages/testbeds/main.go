package testbeds

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"sls-local-server/packages/common"
	"sls-local-server/packages/vars"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	testConfig        []common.Test
	currentTestPtr    int = 0
	results           []common.Result
	testNumberChannel = make(chan int)
)

type Result struct {
	DelayTime     int         `json:"delayTime"`
	ExecutionTime int         `json:"executionTime"`
	ID            string      `json:"id"`
	Output        *OutputData `json:"output,omitempty"`
	Status        string      `json:"status"`
	WorkerID      string      `json:"workerId"`
}

// OutputData represents the optional output payload in the result
type OutputData struct {
	Payload string `json:"payload"`
}

func parseTestConfig(log *zap.Logger) {
	if os.Getenv("RUNPOD_TEST") == "true" {
		tests := os.Getenv("RUNPOD_TESTS")
		isURL := strings.HasPrefix(tests, "URL:")
		if isURL {
			testURL := strings.TrimPrefix(tests, "URL:")
			resp, err := http.Get(testURL)
			if err != nil {
				log.Error("Failed to get test URL", zap.Error(err))
				results = append(results, common.Result{
					ID:     0,
					Status: "FAILED",
					Error:  fmt.Sprintf("Could not parse the tests properly. %s", err.Error()),
				})
				common.SendResultsToGraphQL("FAILED", nil, log, results)
				os.Exit(1)
			}
			defer resp.Body.Close()

			decodedTestFromURL, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Error("Failed to read test from URL", zap.Error(err))
				results = append(results, common.Result{
					ID:     0,
					Status: "FAILED",
					Error:  fmt.Sprintf("Could not download the tests from the URL. %s", err.Error()),
				})
				common.SendResultsToGraphQL("FAILED", nil, log, results)
				os.Exit(1)
			}

			tests = string(decodedTestFromURL)
		}

		decoded, err := base64.StdEncoding.DecodeString(tests)
		if err != nil {
			results = append(results, common.Result{
				ID:     0,
				Status: "FAILED",
				Error:  fmt.Sprintf("Could not parse the tests properly. %s", err.Error()),
			})
			common.SendResultsToGraphQL("SUCCESS", nil, log, results)
			log.Fatal("Failed to decode base64 string",
				zap.Error(err))
		}

		// overwrite the same variable with the decoded string
		tests = string(decoded)

		// Parse JSON into testConfig
		if err := json.Unmarshal([]byte(tests), &testConfig); err != nil {
			results = append(results, common.Result{
				ID:     0,
				Status: "FAILED",
				Error:  fmt.Sprintf("Could not parse the tests properly. %s", err.Error()),
			})
			common.SendResultsToGraphQL("FAILED", nil, log, results)
			log.Fatal("Failed to parse runpod tests",
				zap.Error(err))
		}

		log.Info("Parsed test config", zap.Any("testConfig", testConfig))
		for i, test := range testConfig {
			testConfig[i].ID = &i

			if test.Timeout == nil {
				threeHundred := 30 * 1000
				testConfig[i].Timeout = &threeHundred
			}

			if test.Name == "" {
				testConfig[i].Name = fmt.Sprintf("Test %d", i+1)
			}

			if test.Input == nil {
				results = append(results, common.Result{
					ID:     i,
					Status: "FAILED",
					Error:  fmt.Sprintf("You did not send the tests in a proper format. %s", err.Error()),
				})
				log.Error("Failed to parse test input",
					zap.String("test_name", test.Name),
					zap.Error(err))
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

func startTests(log *zap.Logger) {
	for j, test := range testConfig {
		i := j + 1
		vars.CURRENT_TEST_ID = i
		log.Info("Sending request to IDE runsync endpoint", zap.String("test_name", test.Name))
		// Create HTTP client
		client := &http.Client{
			Timeout: time.Second * time.Duration(*test.Timeout),
		}

		// Marshal back to JSON to ensure proper formatting
		formattedInput, err := json.Marshal(map[string]any{
			"input": test.Input,
		})
		if err != nil {
			results = append(results, common.Result{
				ID:     i,
				Status: "FAILED",
				Error:  fmt.Sprintf("You did not send the tests in a proper format. %s", err.Error()),
			})
			log.Error("Failed to marshal test input",
				zap.String("test_name", test.Name),
				zap.Error(err))
			continue
		}

		fmt.Println("sending request to IDE runsync endpoint", formattedInput)

		// Send request to IDE runsync endpoint
		resp, err := client.Post("http://localhost:80/v2/IDE/runsync", "application/json", bytes.NewBuffer(formattedInput))
		if err != nil {
			log.Error("Failed to send request to IDE runsync endpoint",
				zap.String("test_name", test.Name),
				zap.Error(err))
			results = append(results, common.Result{
				ID:     i,
				Status: "FAILED",
				Error:  fmt.Sprintf("Something went wrong when sending the request to AIAPI. %s", err.Error()),
			})
			continue
		}
		defer resp.Body.Close()

		// Read and log response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Error("Failed to read response body",
				zap.String("test_name", test.Name),
				zap.Error(err))
			results = append(results, common.Result{
				ID:     i,
				Status: "FAILED",
				Error:  fmt.Sprintf("Could not read response body once test had already been completed. %s", err.Error()),
			})
		} else {
			log.Info("Received response from IDE runsync endpoint",
				zap.String("test_name", test.Name),
				zap.Int("status_code", resp.StatusCode),
				zap.String("response_body", string(body)))
			// Parse response body into a map
			var responseData map[string]interface{}
			if err := json.Unmarshal(body, &responseData); err != nil {
				log.Error("Failed to unmarshal response body",
					zap.String("test_name", test.Name),
					zap.Error(err))
				results = append(results, common.Result{
					Status: "FAILED",
					Error:  fmt.Sprintf("Failed to parse response from IDE. %s", err.Error()),
				})
			} else {
				result := common.Result{
					Name:   test.Name,
					Status: "COMPLETED",
					ID:     i,
				}

				if status, ok := responseData["status"].(string); ok && status == "FAILED" {
					result.Status = "FAILED"
					if errorPayload, exists := responseData["error"]; exists {
						result.Error = errorPayload
					}
				}

				if executionTime, executionTimeExists := responseData["executionTime"].(int64); executionTimeExists {
					result.ExecutionTime = executionTime
				}

				if outputPayload, exists := responseData["output"]; exists {
					// Marshal output to determine its size independently of its concrete type
					if marshaled, err := json.Marshal(outputPayload); err == nil && len(marshaled) > 10_000 {
						log.Warn("Output payload exceeded size limit; redacted",
							zap.String("test_name", test.Name),
							zap.Int("bytes", len(marshaled)))
						result.Output = "REDACTED (payload exceeded size limit)"
					} else {
						result.Output = outputPayload
					}
				}

				results = append(results, result)
			}
		}
	}

	common.SendResultsToGraphQL("SUCCESS", nil, log, results)
}

func RunTests(log *zap.Logger) {
	log.Info("Starting server")
	parseTestConfig(log)
	log.Info("Parsed test config")
	gin.SetMode(gin.ReleaseMode)
	common.InstallAndRunAiApi(log)

	startedAt := time.Now()
	// kind of mandatory to wait for the aiapi to start
	for {
		time.Sleep(time.Duration(1) * time.Second)
		aiApiStatus, err := http.Get("http://localhost:80/ping")
		if err != nil {
			continue
		}
		if time.Since(startedAt) > 8*time.Minute {
			log.Error("Failed to start AI API after 8 minutes")
			results = append(results, common.Result{
				ID:     0,
				Status: "FAILED",
				Error:  "Failed to start AI API after 8 minutes. This could be a network issue. Please restart the build and tests.",
			})
			common.SendResultsToGraphQL("FAILED", nil, log, results)
			time.Sleep(time.Duration(10) * time.Second)
			os.Exit(1)
		}
		if aiApiStatus.StatusCode == 200 {
			break
		}
	}

	time.Sleep(time.Duration(1) * time.Second)
	log.Info("Installed and ran AI API")
	startTests(log)
}
