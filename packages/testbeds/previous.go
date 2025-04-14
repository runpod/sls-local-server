package testbeds

import (
	"fmt"
	"net/http"
	"sls-local-server/packages/common"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

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
