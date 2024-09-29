package webserver

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Webserver struct {
	OTELTracer trace.Tracer
	CallURL    string
}

// NewServer creates a new server instance
func NewServer(tracer trace.Tracer, CallURL string) *Webserver {
	return &Webserver{
		OTELTracer: tracer,
		CallURL:    CallURL,
	}
}

// createServer creates a new server instance with go chi router
func (we *Webserver) CreateServer() *chi.Mux {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Logger)
	router.Use(middleware.Timeout(60 * time.Second))
	// promhttp
	router.Handle("/metrics", promhttp.Handler())
	router.Get("/{cep}", we.HandleRequest)
	return router
}

func (serv *Webserver) HandleRequest(w http.ResponseWriter, r *http.Request) {
	cep := chi.URLParam(r, "cep")
	// Validate the 'cep'
	if err := validateCEP(cep); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity) // 422 Unprocessable Entity
		return
	}
	callURL := fmt.Sprintf("%s/%s", serv.CallURL, cep)

	carrier := propagation.HeaderCarrier(r.Header)
	ctx := r.Context()
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := serv.OTELTracer.Start(ctx, "Get temperature")
	defer span.End()

	var req *http.Request
	var err error
	req, err = http.NewRequestWithContext(ctx, "GET", callURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		w.WriteHeader(http.StatusOK)
		_, err := io.Copy(w, resp.Body)
		if err != nil {
			http.Error(w, "Error copying response body", http.StatusInternalServerError)
		}
	case http.StatusNotFound:
		// Status 404, forward it
		http.Error(w, "Not Found", http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		// Status 422, forward it
		http.Error(w, "Unprocessable Entity", http.StatusUnprocessableEntity)
	default:
		// Handle any other status code
		http.Error(w, "Unexpected error", http.StatusInternalServerError)
	}

}

func validateCEP(cep string) error {
	if len(cep) != 8 {
		return errors.New("invalid zipcode")
	}
	for _, char := range cep {
		if !unicode.IsDigit(char) {
			return errors.New("invalid zipcode")
		}
	}
	return nil
}
