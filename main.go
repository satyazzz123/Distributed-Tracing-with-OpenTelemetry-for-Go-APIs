package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer       trace.Tracer
	otlpEndpoint string
	mongoClient  *mongo.Client
)

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func init() {
	otlpEndpoint = os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		log.Fatalln("You MUST set OTLP_ENDPOINT env variable!")
	}
}

func main() {
	ctx := context.Background()

	// Initialize MongoDB client

	// Set up OpenTelemetry
	exp, err := newOTLPExporter(ctx)
	if err != nil {
		log.Fatalf("failed to initialize exporter: %v", err)
	}

	tp := newTraceProvider(exp)
	defer func() { _ = tp.Shutdown(ctx) }()
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("userapp")
	initMongoDB(ctx)
	// Create HTTP server
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Post("/users", postUser)
	r.Get("/users", getUsers)

	http.ListenAndServe(":8082", r)
}

func initMongoDB(ctx context.Context) {
	_, span := tracer.Start(ctx, "connecing to the MongoDB")
	defer span.End()
	var err error
	// Replace with your MongoDB connection string
	mongoURI := "mongodb://localhost:27017"
	mongoClient, err = mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
}

// newOTLPExporter initializes an OTLP trace exporter.//reciever
func newOTLPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	return otlptracehttp.New(ctx, otlptracehttp.WithInsecure(), otlptracehttp.WithEndpoint(otlpEndpoint))
}

// newTraceProvider sets up the trace provider with the specified exporter. //processor
func newTraceProvider(exp sdktrace.SpanExporter) *sdktrace.TracerProvider {
	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("userapp"),
		),
	)
	if err != nil {
		panic(err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(r),
	)
}

func postUser(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "HTTP POST /users")
	defer span.End()

	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	err = insertUser(ctx, user)
	if err != nil {
		http.Error(w, "Failed to insert user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func insertUser(ctx context.Context, user User) error {
	ctx, span := tracer.Start(ctx, "MongoDB Insert User")
	defer span.End()

	collection := mongoClient.Database("testdb").Collection("users")
	_, err := collection.InsertOne(ctx, user)
	return err
}

func getUsers(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "HTTP GET /users")
	defer span.End()

	users, err := findUsers(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func findUsers(ctx context.Context) ([]User, error) {
	ctx, span := tracer.Start(ctx, "MongoDB Find Users")
	defer span.End()

	collection := mongoClient.Database("testdb").Collection("users")
	cursor, err := collection.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	var users []User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}
