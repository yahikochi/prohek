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
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func generateUserID() string {
	b := make([]byte, userIDLength)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func rateLimitedRequest() {
	requestSemaphore <- struct{}{}
	defer func() { <-requestSemaphore }()

	if elapsed := time.Since(lastRequestTime); elapsed < rateLimitInterval {
		time.Sleep(rateLimitInterval - elapsed)
	}
	lastRequestTime = time.Now()
}

func reverseGeocode(lat, lon float64) (Address, error) {
	rateLimitedRequest()
	url := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=json&lat=%f&lon=%f", lat, lon)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Address{}, err
	}
	req.Header.Set("User-Agent", "LocationTracker/1.0 (contact@example.com)")

	resp, err := client.Do(req)
	if err != nil {
		return Address{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Address{}, fmt.Errorf("API error: %s", resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Address{}, err
	}

	var result NominatimResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return Address{}, err
	}

	return result.Address, nil
}

func cleanupOldData() {
	mutex.Lock()
	defer mutex.Unlock()

	now := time.Now()
	for userID, data := range locationStore {
		t, err := time.Parse(time.RFC3339, data.Timestamp)
		if err == nil && now.Sub(t) > dataTTL {
			delete(locationStore, userID)
		}
	}
}

func main() {
	r := gin.Default()
	r.LoadHTMLGlob("templates/*")

	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Printf("%s %s %d %s", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(start))
	})

	r.GET("/generate-link", handleGenerateLink)
	r.GET("/share", handleSharePage)
	r.POST("/location/:user_id", handleLocationPost)
	r.GET("/result/:user_id", handleResultGet)

	// Tambahan endpoint debug (opsional)
	r.GET("/debug-list", func(c *gin.Context) {
		mutex.Lock()
		defer mutex.Unlock()
		c.JSON(http.StatusOK, locationStore)
	})

	go func() {
		for {
			time.Sleep(dataCleanupInterval)
			cleanupOldData()
		}
	}()

	logger.Println("Starting server on :8080")
	if err := r.Run(":8080"); err != nil {
		logger.Fatalf("Server failed: %v", err)
	}
}

func handleGenerateLink(c *gin.Context) {
	userID := generateUserID()
	shareLink := fmt.Sprintf("http://%s/share?id=%s", c.Request.Host, userID)
	resultLink := fmt.Sprintf("http://%s/result/%s", c.Request.Host, userID)

	c.JSON(http.StatusOK, gin.H{
		"share_link":  shareLink,
		"result_link": resultLink,
		"user_id":     userID,
	})
}

func handleSharePage(c *gin.Context) {
	userID := c.Query("id")
	if userID == "" {
		c.String(http.StatusBadRequest, "Invalid sharing link")
		return
	}
	c.HTML(http.StatusOK, "index.html", gin.H{"UserID": userID})
}

func handleLocationPost(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var requestData struct {
		Latitude   float64    `json:"latitude" binding:"required"`
		Longitude  float64    `json:"longitude" binding:"required"`
		Accuracy   float64    `json:"accuracy"`
		RawAddress string     `json:"raw_address"`
		DeviceInfo DeviceInfo `json:"device_info"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		logger.Printf("Invalid request data: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	address, err := reverseGeocode(requestData.Latitude, requestData.Longitude)
	if err != nil {
		logger.Printf("Reverse geocoding failed, using raw address: %v", err)
		address = Address{Road: "", City: "", Country: "", Postcode: ""}
	}

	location := LocationData{
		UserID:     userID,
		Latitude:   requestData.Latitude,
		Longitude:  requestData.Longitude,
		Accuracy:   requestData.Accuracy,
		Address:    fmt.Sprintf("%s, %s", address.Road, address.City),
		City:       address.City,
		Country:    address.Country,
		Road:       address.Road,
		Postcode:   address.Postcode,
		Device:     requestData.DeviceInfo.Device,
		OS:         requestData.DeviceInfo.OS,
		Browser:    requestData.DeviceInfo.Browser,
		Language:   requestData.DeviceInfo.Language,
		ScreenSize: requestData.DeviceInfo.ScreenSize,
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	mutex.Lock()
	locationStore[userID] = location
	mutex.Unlock()

	logger.Printf("Stored location data for user %s", userID)
}

func handleResultGet(c *gin.Context) {
	userID := c.Param("user_id")

	mutex.Lock()
	data, exists := locationStore[userID]
	mutex.Unlock()

	if !exists {
		logger.Printf("Location data not found for user %s", userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Location data not found"})
		return
	}

	logger.Printf("Retrieved location data for user %s", userID)
	c.JSON(http.StatusOK, data)
}