package execution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxCallbackRequestBytes = 64 * 1024

type CallbackProcessor interface {
	HandleCallback(context.Context, Callback, string) (CallbackResult, error)
}

func NewCallbackHandler(processor CallbackProcessor) (http.Handler, error) {
	if processor == nil {
		return nil, ErrInvalidInput
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agent-callbacks/{logicalExecutionID}/{attemptID}", func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, maxCallbackRequestBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var callback Callback
		if err := decoder.Decode(&callback); err != nil {
			writeCallbackError(writer, http.StatusBadRequest, "invalid_callback_body")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeCallbackError(writer, http.StatusBadRequest, "invalid_callback_body")
			return
		}
		if callback.LogicalExecutionID != request.PathValue("logicalExecutionID") || callback.AttemptID != request.PathValue("attemptID") {
			writeCallbackError(writer, http.StatusBadRequest, "callback_identity_mismatch")
			return
		}
		result, err := processor.HandleCallback(request.Context(), callback, request.Header.Get("x-agent-signature"))
		if err != nil {
			switch {
			case errors.Is(err, ErrInvalidCallback), errors.Is(err, ErrCallbackReplay):
				writeCallbackError(writer, http.StatusUnauthorized, "invalid_callback_authentication")
			case errors.Is(err, ErrContentConflict), errors.Is(err, ErrStaleFence), errors.Is(err, ErrInvalidState):
				writeCallbackError(writer, http.StatusConflict, "callback_conflict")
			case errors.Is(err, ErrNotFound):
				writeCallbackError(writer, http.StatusNotFound, "execution_not_found")
			default:
				writeCallbackError(writer, http.StatusInternalServerError, "callback_processing_failed")
			}
			return
		}
		writer.Header().Set("content-type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(result)
	})
	return mux, nil
}

func writeCallbackError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"code": code})
}
