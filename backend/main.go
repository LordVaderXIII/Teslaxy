package main

import (
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"teslaxy/api"
	"teslaxy/database"
	"teslaxy/services"

	"github.com/gin-gonic/gin"
)

//go:embed public/*
var frontendFS embed.FS

func main() {
	// Configuration
	footagePath := os.Getenv("FOOTAGE_PATH")
	if footagePath == "" {
		footagePath = "/footage"
	}
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config"
	}

	// Ensure config dir exists
	os.MkdirAll(configPath, 0755)

	// Init DB
	database.InitDB()
	defer database.CloseDB()

	// Init Scanner
	scanner := services.NewScannerService(footagePath, database.DB)
	scanner.Start()

	// Setup Server
	r := gin.Default()

	// CORS (Simple for now)
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	api.SetupRoutes(r)

	// Health check
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Serve Frontend
	distFS, _ := fs.Sub(frontendFS, "public")
	httpFS := http.FS(distFS)

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			c.JSON(404, gin.H{"error": "Not Found"})
			return
		}

		// Try to serve static file
		// Trim leading slash
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		// Open checks if file exists
		f, err := distFS.Open(cleanPath)
		if err == nil {
			defer f.Close()
			// Manually serve index.html to avoid 301 redirect loop
			if cleanPath == "index.html" {
				log.Println("Serving index.html manually from root")
				if rs, ok := f.(io.ReadSeeker); ok {
					stat, _ := f.Stat()
					http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), rs)
					return
				}
			}
			c.FileFromFS(cleanPath, httpFS)
			return
		}

		// Fallback to index.html for SPA
		log.Printf("Route %s not found, falling back to index.html", path)
		f, err = distFS.Open("index.html")
		if err == nil {
			defer f.Close()
			if rs, ok := f.(io.ReadSeeker); ok {
				stat, _ := f.Stat()
				http.ServeContent(c.Writer, c.Request, "index.html", stat.ModTime(), rs)
				return
			}
		}
		log.Println("Failed to serve index.html fallback")

		c.FileFromFS("index.html", httpFS)
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r.Run(":" + port)
}
