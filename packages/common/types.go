package common

import "time"

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

var results []Result
