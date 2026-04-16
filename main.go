package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func initTracer(endpoint string) (*sdktrace.TracerProvider, error) {
	ctx := context.Background()

	moesifOrgId := "298:17"
	if id, ok := os.LookupEnv("MOESIF_ORG_ID"); ok {
		moesifOrgId = id
	}
	moesifAppId := "563:95"
	if id, ok := os.LookupEnv("MOESIF_APP_ID"); ok {
		moesifAppId = id
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithHeaders(map[string]string{
			"X-Moesif-Org-Id": moesifOrgId,
			"X-Moesif-App-Id": moesifAppId,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("my-trace-service"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("openchoreo.dev/environment", "development"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tracer = tp.Tracer("my-trace-service")
	return tp, nil
}

// traceMiddleware wraps handlers to automatically create a span per request
func traceMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		ctx, span := tracer.Start(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path))
		defer span.End()

		span.SetAttributes(
			semconv.HTTPMethod(r.Method),
			semconv.HTTPTarget(r.URL.Path),
			attribute.String("http.host", r.Host),
			attribute.String("http.user_agent", r.UserAgent()),
		)

		next(w, r.WithContext(ctx))
	}
}

// --- Resource Handlers ---

func handleUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, dbSpan := tracer.Start(ctx, "db.query.users")
	dbSpan.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM users LIMIT 100"),
	)
	time.Sleep(time.Duration(30+rand.Intn(70)) * time.Millisecond)
	dbSpan.End()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"users": [{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]}`)
}

func handleOrders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, authSpan := tracer.Start(ctx, "auth.verify")
	authSpan.SetAttributes(attribute.String("auth.method", "jwt"))
	time.Sleep(10 * time.Millisecond)
	authSpan.End()

	_, dbSpan := tracer.Start(ctx, "db.query.orders")
	dbSpan.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM orders WHERE status='active'"),
	)
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	dbSpan.End()

	_, cacheSpan := tracer.Start(ctx, "cache.set")
	cacheSpan.SetAttributes(
		attribute.String("cache.backend", "redis"),
		attribute.String("cache.key", "orders:active"),
	)
	time.Sleep(5 * time.Millisecond)
	cacheSpan.End()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"orders": [{"id":101,"total":59.99}]}`)
}

func handleProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, cacheSpan := tracer.Start(ctx, "cache.get")
	cacheSpan.SetAttributes(
		attribute.String("cache.backend", "redis"),
		attribute.String("cache.key", "products:all"),
		attribute.Bool("cache.hit", false),
	)
	time.Sleep(5 * time.Millisecond)
	cacheSpan.End()

	_, dbSpan := tracer.Start(ctx, "db.query.products")
	dbSpan.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM products"),
	)
	time.Sleep(time.Duration(40+rand.Intn(60)) * time.Millisecond)
	dbSpan.End()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"products": [{"id":1,"name":"Widget","price":9.99}]}`)
}

func handlePayments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, extSpan := tracer.Start(ctx, "http.client.payment-gateway")
	extSpan.SetAttributes(
		attribute.String("http.method", "POST"),
		attribute.String("http.url", "https://payments.example.com/charge"),
		attribute.Int("http.status_code", 200),
	)
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
	extSpan.End()

	_, dbSpan := tracer.Start(ctx, "db.insert.payment")
	dbSpan.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "INSERT INTO payments (order_id, amount) VALUES ($1, $2)"),
	)
	time.Sleep(30 * time.Millisecond)
	dbSpan.End()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"payment": {"id":"pay_123","status":"success"}}`)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := tracer.Start(ctx, "health.check")
	defer span.End()

	span.SetAttributes(attribute.String("health.status", "ok"))
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

func handleError(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)

	span.SetStatus(codes.Error, "simulated failure")
	span.RecordError(fmt.Errorf("something went wrong"))
	span.SetAttributes(attribute.Int("http.status_code", 500))

	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `{"error":"internal server error"}`)
}

func main() {
	endpoint := "localhost:4318"
	if e, ok := os.LookupEnv("OTEL_ENDPOINT"); ok {
		endpoint = e
	}
	port := "8080"
	if p, ok := os.LookupEnv("PORT"); ok {
		port = p
	}

	tp, err := initTracer(endpoint)
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer: %v", err)
		}
	}()

	http.HandleFunc("/api/users", traceMiddleware(handleUsers))
	http.HandleFunc("/api/orders", traceMiddleware(handleOrders))
	http.HandleFunc("/api/products", traceMiddleware(handleProducts))
	http.HandleFunc("/api/payments", traceMiddleware(handlePayments))
	http.HandleFunc("/api/error", traceMiddleware(handleError))
	http.HandleFunc("/health", traceMiddleware(handleHealth))

	log.Printf("Server starting on :%s (traces -> %s)", port, endpoint)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
