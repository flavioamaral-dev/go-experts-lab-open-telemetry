package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"flavioamaral-dev/go-experts-lab-open-telemetry/service-a/config"
	"flavioamaral-dev/go-experts-lab-open-telemetry/service-a/entity"
	errors "flavioamaral-dev/go-experts-lab-open-telemetry/service-a/errors"
	"flavioamaral-dev/go-experts-lab-open-telemetry/service-a/infra"
)

var (
	cfg    *config.Config
	tracer trace.Tracer
)

func main() {
	var err error
	cfg = config.LoadConfig()

	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	setupOpenTelemetry()
	startServer()
}

func setupOpenTelemetry() {
	ot := infra.NewOpenTel()
	ot.ServiceName = "Service A"
	ot.ServiceVersion = "1"
	ot.ExporterEndpoint = fmt.Sprintf("%s/api/v2/spans", cfg.UrlZipKin)

	tracer = ot.GetTracer()
}

func startServer() {
	r := mux.NewRouter()
	r.Use(otelmux.Middleware("Service A"))
	r.HandleFunc("/weather", getWeather).Methods("POST")

	log.Printf("Service B URL: %s", cfg.UrlServiceB)
	log.Println("Listening on port :8080")

	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getWeather(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "validate-zipcode")
	defer span.End()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpError(w, http.StatusBadRequest, "Failed to read request body", err)
		return
	}

	var cep entity.CEP
	if err = json.Unmarshal(body, &cep); err != nil {
		httpError(w, http.StatusUnprocessableEntity, "Invalid ZIP code", err)
		return
	}

	if err = validateCEP(cep); err != nil {
		httpError(w, http.StatusUnprocessableEntity, "Invalid ZIP code", err)
		return
	}

	response, statusCode, err := requestServiceB(ctx, body)
	if err != nil {
		httpError(w, http.StatusUnprocessableEntity, errors.ErrInvalidZipCode.Error(), err)
		return
	}

	handleResponse(w, statusCode, response)
}

func validateCEP(cep entity.CEP) error {
	var validCEP = regexp.MustCompile(`^\d{5}-?\d{3}$`)
	if !validCEP.MatchString(cep.CEP) {
		return errors.ErrInvalidZipCode
	}

	return nil
}

func requestServiceB(ctx context.Context, body []byte) (*entity.ResponseHTTP, int, error) {
	ctx, span := tracer.Start(ctx, "request-service-b")
	defer span.End()

	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/weather", cfg.UrlServiceB), bytes.NewBuffer(body))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("Failed to close response body: %v", err)
		}
	}(res.Body)

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	var response entity.ResponseHTTP
	if err = json.Unmarshal(resBody, &response); err != nil {
		return nil, http.StatusInternalServerError, err
	}

	return &response, res.StatusCode, nil
}

func handleResponse(w http.ResponseWriter, statusCode int, response *entity.ResponseHTTP) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if statusCode == http.StatusOK {
		if err := json.NewEncoder(w).Encode(response); err != nil {
			httpError(w, http.StatusInternalServerError, "Failed to marshal response", err)
		}
		return
	}

	switch statusCode {
	case http.StatusUnprocessableEntity:
		w.Write([]byte("Invalid ZIP code"))
	case http.StatusNotFound:
		w.Write([]byte("ZIP code not found"))
	default:
		w.Write([]byte("Internal Server Error"))
	}
}

func httpError(w http.ResponseWriter, statusCode int, message string, err error) {
	log.Printf("%s: %v", message, err)
	w.WriteHeader(statusCode)
	w.Write([]byte(message))
}
