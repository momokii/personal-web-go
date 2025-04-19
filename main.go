package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
	"github.com/momokii/go-llmbridge/pkg/openai"
	"github.com/momokii/personal-web-go/internal/handlers"
	"github.com/momokii/personal-web-go/pkg/monitoring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	// openai client
	openaiClient, err := openai.New(
		os.Getenv("OPENAI_API_KEY"),
		"",
		"",
		openai.WithModel(os.Getenv("OPENAI_MODEL_NAME")),
	)
	if err != nil {
		log.Println("Error creating OpenAI client: ", err)
		return
	} else {
		log.Println("OpenAI client created")
	}

	// handler init
	dashboardHandler := handlers.NewDashboardHandler(openaiClient)

	// setup prometheus server
	promReg := prometheus.NewRegistry()
	promReg.MustRegister(collectors.NewGoCollector()) // golang monitoring metrics default using prometheus
	promMetrics := monitoring.PrometheusNewMetrics(promReg)

	// add golang version
	promMetrics.Info.WithLabelValues(runtime.Version()).Set(1)

	pMux := http.NewServeMux()
	promHandler := promhttp.HandlerFor(promReg, promhttp.HandlerOpts{})
	pMux.Handle("/metrics", promHandler)

	// setup fiber
	engine := html.New("./web/views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).Render("error", fiber.Map{
				"Message": err.Error(),
			})
		},
	})

	app.Use(cors.New())
	app.Use(logger.New())
	app.Use(PromMiddleware(*promMetrics))
	app.Static("/web", "./web")

	app.Get("/", dashboardHandler.DashboardView)
	app.Post("/api/bot", dashboardHandler.ChatBotMessages)

	// setup paralel port for app and prometheus exporter
	// gracefull shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	signal.Notify(sig, syscall.SIGTERM)

	go func() {
		<-sig
		log.Println("Shutting down app")
		log.Println("Waiting 5 second for all request to finish")
		time.Sleep(5 * time.Second)
		cancel()
	}()

	// main service personal web
	go func() {
		log.Println("Starting app on port 3002")
		if err := app.Listen(":3002"); err != nil {
			log.Fatal("Error when running app, error: " + err.Error())
		}
	}()

	// prometheus exporter
	go func() {
		log.Println("Starting prometheus exporter on port 3001")
		if err := http.ListenAndServe(":3001", pMux); err != nil {
			log.Fatal("Error when running prometheus exporter, error: " + err.Error())
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Fatal("Error when shutting down app, error: " + err.Error())
	}
	log.Println("App is down")
}

func PromMiddleware(reg monitoring.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		now := time.Now()

		err := c.Next()

		respStatus := c.Response().StatusCode()
		duration := time.Since(now).Seconds()

		if err != nil {
			respStatus = fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				respStatus = e.Code
			}
		}

		reg.Duration.WithLabelValues(strconv.Itoa(respStatus), c.Method(), c.Path()).Observe(duration)
		reg.RequestTotal.With(prometheus.Labels{"response_code": strconv.Itoa(respStatus), "method": c.Method()}).Inc()
		reg.DurationSummary.Observe(duration)

		return err
	}
}
