package main

import (
	"context"
	"encoding/json"
	"flavioamaral-dev/go-experts-lab-open-telemetry/service-b/config"
	"flavioamaral-dev/go-experts-lab-open-telemetry/service-b/entity"
	errors "flavioamaral-dev/go-experts-lab-open-telemetry/service-b/errors"
	"flavioamaral-dev/go-experts-lab-open-telemetry/service-b/infra"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel/trace"
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
	ot.ServiceName = "Service B"
	ot.ServiceVersion = "1"
	ot.ExporterEndpoint = fmt.Sprintf("%s/api/v2/spans", cfg.UrlZipKin)
	tracer = ot.GetTracer()
}

func startServer() {
	r := mux.NewRouter()
	r.Use(otelmux.Middleware("Service B"))
	r.HandleFunc("/weather", getWeather).Methods("POST")

	log.Println("Listening on port :8081")

	if err := http.ListenAndServe(":8081", r); err != nil {
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
		httpError(w, http.StatusBadRequest, "Failed to unmarshal request body", err)
		return
	}

	if err = validateCEP(cep); err != nil {
		httpError(w, http.StatusUnprocessableEntity, "Invalid ZIP code", err)
		return
	}

	responseViaCEP, err := fetchViaCEP(ctx, cep)
	if responseViaCEP == nil {
		httpError(w, http.StatusNotFound, "Can not find ZIP code", err)
		return
	}

	if err != nil {
		httpError(w, http.StatusInternalServerError, "Failed to fetch ViaCEP", err)
		return
	}

	responseWeather, err := fetchWeather(ctx, responseViaCEP.Localidade)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "Failed to fetch weather", err)
		return
	}

	tempF := responseWeather.Current.TempC*1.8 + 32
	tempK := responseWeather.Current.TempC + 273.15
	response := entity.ResponseHTTP{
		City:  responseWeather.Location.Name,
		TempC: responseWeather.Current.TempC,
		TempF: tempF,
		TempK: tempK,
	}
	handleResponse(w, http.StatusOK, &response)
}

func validateCEP(cep entity.CEP) error {
	var validCEP = regexp.MustCompile(`^\d{5}-?\d{3}$`)
	if !validCEP.MatchString(cep.Cep) {
		return errors.ErrInvalidZipCode
	}
	return nil
}

func fetchViaCEP(ctx context.Context, cep entity.CEP) (*entity.ResponseViaCEP, error) {
	ctx, span := tracer.Start(ctx, "request-via-cep")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://viacep.com.br/ws/%s/json/", cep.Cep), nil)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var response entity.ResponseViaCEP
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, err
	}

	if response.Cep == "" {
		return nil, errors.ErrInvalidZipCode
	}

	return &response, nil
}

func fetchWeather(ctx context.Context, city string) (*entity.ResponseWeather, error) {
	ctx, span := tracer.Start(ctx, "request-weather-api")
	defer span.End()

	weatherUrl := fmt.Sprintf("http://api.weatherapi.com/v1/current.json?key=%s&q=%s", "3841b81037a5427eb51191826241702", url.QueryEscape(city))
	req, err := http.NewRequestWithContext(ctx, "GET", weatherUrl, nil)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var response entity.ResponseWeather
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
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
		w.Write([]byte("Cannot find ZIP code"))
	default:
		w.Write([]byte("Internal Server Error"))
	}
}

func httpError(w http.ResponseWriter, statusCode int, message string, err error) {
	log.Printf("%s: %v", message, err)
	w.WriteHeader(statusCode)
	w.Write([]byte(message))
}
