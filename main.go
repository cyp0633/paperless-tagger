package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

//go:embed web-app/dist
var webFS embed.FS

func main() {
	// Load .env if present
	if err := godotenv.Load(); err != nil {
		log.Println("[Main] No .env file found, using environment variables")
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	// Resolve data directory.
	// In Docker, DATA_DIR=/data is set in the Dockerfile.
	// When running natively (dev), defaults to ./data relative to CWD.
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("[Main] Cannot create data directory %s: %v", dataDir, err)
	}

	configPath := filepath.Join(dataDir, "config.json")
	queuePath := filepath.Join(dataDir, "queue.json")

	// Init settings
	InitSettings(configPath)
	settings, err := GetSettings()
	if err != nil {
		log.Fatalf("[Main] Failed to load settings: %v", err)
	}

	// Init queue and scanner
	queue := NewQueue(queuePath, settings.QueueConcurrency)
	scanner := NewScanner(queue)
	scanner.Start()

	// Init Gin
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// CORS for local development
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:8080"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Register API routes
	h := NewHandlers(queue, scanner)
	api := r.Group("/api")
	{
		// Settings
		api.GET("/settings", h.GetSettings)
		api.PUT("/settings", h.SaveSettings)
		api.GET("/settings/default-prompt", h.GetDefaultPrompt)
		api.POST("/settings/check/paperless", h.CheckPaperless)
		api.POST("/settings/check/ocr-llm", h.CheckOCRLLM)
		api.POST("/settings/check/extr-llm", h.CheckExtrLLM)

		// Queue
		api.GET("/queue", h.GetJobs)
		api.POST("/queue", h.EnqueueDocument)
		api.DELETE("/queue/:id", h.CancelJob)
		api.POST("/queue/:id/retry", h.RetryJob)
		api.GET("/queue/events", h.QueueEvents)

		// Scanner
		api.POST("/scan/trigger", h.TriggerScan)

		// Paperless proxy
		api.GET("/documents/untagged", h.GetPaperlessDocuments)
	}

	// Serve frontend static files
	distFS, err := fs.Sub(webFS, "web-app/dist")
	if err != nil {
		log.Printf("[Main] Warning: could not embed frontend assets: %v", err)
	} else {
		indexHTML, readIndexErr := fs.ReadFile(distFS, "index.html")
		if readIndexErr != nil {
			log.Printf("[Main] Warning: could not read frontend index.html: %v", readIndexErr)
		}
		r.NoRoute(func(c *gin.Context) {
			if len(c.Request.URL.Path) > 4 && c.Request.URL.Path[:5] == "/api/" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			filePath := c.Request.URL.Path
			if filePath == "/" {
				filePath = "/index.html"
			}
			f, err := distFS.Open(filePath[1:])
			if err != nil {
				if readIndexErr != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
					return
				}
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
				return
			}
			f.Close()
			assetContent, readAssetErr := fs.ReadFile(distFS, filePath[1:])
			if readAssetErr != nil {
				if readIndexErr != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
					return
				}
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
				return
			}
			contentType := mime.TypeByExtension(filepath.Ext(filePath))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			c.Data(http.StatusOK, contentType, assetContent)
		})
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("[Main] paperless-tagger starting on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("[Main] Server error: %v", err)
	}
}
