package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// NewWorkerID returns one stable, process-instance identity. It is deliberately
// operational metadata rather than an authentication credential.
func NewWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), NewID()[:12])
}
