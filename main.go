// main.go
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	charset             = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	userIDLength        = 12
	rateLimitInterval   = time.Second
	dataCleanupInterval = time.Hour
	dataTTL             = 24 * time.Hour
)

type LocationData struct {
	UserID      string  `json:"user_id"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Accuracy    float64 `json:"accuracy"`
	Address     string  `json:"address"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Road        string  `json:"road"`
	Postcode    string  `json:"postcode"`
	Device      string  `json:"device"`
	OS          string  `json:"os"`
	Browser     string  `json:"browser"`
	Language    string  `json:"language"`
	ScreenSize  string  `json:"screen_size"`
	Timestamp   string  `json:"timestamp"`
}

type DeviceInfo struct {
	Device     string `json:"device"`
	OS         string `json:"os"`
	Browser    string `json:"browser"`
	Language   string `json:"language"`
	ScreenSize string `json:"screen_size"`
}

type Address struct {
	Road     string `json:"road"`
	City     string `json:"city"`
	Country  string `json:"country"`
	Postcode string `json:"postcode"`
}

type NominatimResponse struct {
	Address Address `json:"address"`
}

var (
	locationStore    = make(map[string]LocationData)
	mutex            = &sync.Mutex{}
	lastRequestTime  time.Time
	requestSemaphore = make(chan struct{}, 1)
	logger           = log.New(os.Stdout, "[LOCATION-API] ", log.LstdFlags|log.Lmsgprefix)
	router           *gin.Engine
)

func init() {
	rand.Seed(time.Now().UnixNano())
	setupRouter()
	go startCleanupJob()
}

func setupRouter() {
	router = gin.Default()
	
	// Setup middleware
	router.Use(loggingMiddleware())
	
	// Setup routes
	router.GET("/generate-link", handleGenerateLink)
	router.GET("/share", handleSharePage)
	router.POST("/location/:user_id", handleLocationPost)
	router.GET("/result/:user_id", handleResultGet)
	router.GET("/debug-list", handleDebugList)
}

func loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Printf("%s %s %d %s", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(start))
	}
}

// Handler utama untuk Vercel
func Handler(w http.ResponseWriter, r *http.Request) {
	router.ServeHTTP(w, r)
}

// Untuk development lokal
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	logger.Printf("Server running in LOCAL mode on :%s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		logger.Fatalf("Server failed: %v", err)
	}
}

// [Fungsi-fungsi helper yang sama: generateUserID, rateLimitedRequest, reverseGeocode]

func startCleanupJob() {
	ticker := time.NewTicker(dataCleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			cleanupOldData()
		}
	}
}

func cleanupOldData() {
	mutex.Lock()
	defer mutex.Unlock()

	now := time.Now()
	for userID, data := range locationStore {
		t, err := time.Parse(time.RFC3339, data.Timestamp)
		if err == nil && now.Sub(t) > dataTTL {
			delete(locationStore, userID)
			logger.Printf("Cleaned up old data for user %s", userID)
		}
	}
}

// [Fungsi-fungsi handler yang sama: handleGenerateLink, handleSharePage, handleLocationPost, handleResultGet]

func handleDebugList(c *gin.Context) {
	mutex.Lock()
	defer mutex.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"count": len(locationStore),
		"data":  locationStore,
	})
}
