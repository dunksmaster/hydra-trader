package telegram

import (
	"encoding/json"
	"nofx/logger"
	"os"
	"time"
)

const debugLogPath = "/app/data/debug-e70047.log"

// #region agent log
func dbgLog(hypothesisID, location, message string, data map[string]any) {
	payload := map[string]any{
		"sessionId":    "e70047",
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	logger.Infof("[DBG-e70047] %s", string(b))
	if f, err := os.OpenFile(debugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
}

// #endregion
