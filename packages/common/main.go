package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var Mutex = &sync.Mutex{}
var GqlMutex = &sync.Mutex{}
var TestNumberChannel = make(chan int)

var (
	log        *zap.Logger
	testConfig []Test
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
}

func SendResultsToGraphQL(status string, errorReason *string, log *zap.Logger, results []Result) {
	GqlMutex.Lock()
	defer GqlMutex.Unlock()

	time.Sleep(time.Duration(100) * time.Second)

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

func SendLogsToTinyBird(logBuffer chan string, testNumChan chan int, log *zap.Logger) {
	// Start goroutine to collect and send logs
	buffer := make([]map[string]interface{}, 0)
	tinybirdToken := os.Getenv("TINYBIRD_TOKEN")
	runpodPodId := os.Getenv("RUNPOD_POD_ID")

	testNumber := 7081

	go func() {
		for num := range testNumChan {
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
				// Defer recovery from any panics that might occur during the HTTP request
				defer func() {
					if r := recover(); r != nil {
						log.Error("Recovered from panic in log sending goroutine",
							zap.Any("panic", r),
							zap.String("stack", string(debug.Stack())))
					}
				}()
				// Create and send request
				req, err := http.NewRequest("POST", url, strings.NewReader(payload))
				if err == nil {
					req.Header.Set("Authorization", "Bearer "+tinybirdToken)
					req.Header.Set("Content-Type", "text/plain")

					client := &http.Client{Timeout: 2 * time.Second}
					_, err := client.Do(req)
					if err != nil {
						log.Error("Failed to send logs to tinybird", zap.Error(err))
					}
					// } else if resp.StatusCode > 200 {
					// 	// body, err := io.ReadAll(resp.Body)
					// 	// if err != nil {
					// 	// 	log.Error("Failed to read response body", zap.Error(err))
					// 	// 	return
					// 	// }
					// 	// log.Error("Tinybird request failed",
					// 	// 	zap.Int("status", resp.StatusCode),
					// 	// 	zap.String("response", string(body)))
					// 	// resp.Body.Close()
					// }
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
			_, err := client.Do(req)
			if err != nil {
				log.Error("Failed to send final logs to tinybird", zap.Error(err))
			}
		}
	}
}
