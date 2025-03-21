package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

const accessKey = "admin"

type ProxyConfig struct {
	Data     string `json:"data"`
	Protocol string `json:"protocol"`
}

type RequestData struct {
	AccessKey string   `json:"accessKey"`
	AppleData string   `json:"appleData"`
	Cookies   []string `json:"cookies"`
	Mail      string   `json:"mail"`
	Mobile    bool     `json:"mobile"`
	Pass      string   `json:"pass"`
	Proxy     ProxyConfig `json:"proxy"`
	Recovery  string   `json:"recovery"`
	Secret    string   `json:"secret"`
	URL       string   `json:"url"`
}

func handler(reqData RequestData, w http.ResponseWriter, handlerCtx context.Context) {
	l := launcher.New().
		Headless(false).
		Set("--disable-extension").
		Set("--excludeSwitches", "enable-automation").
		Set("--disable-blink-features", "AutomationControlled").
		Set("--lang", "en-US").
		Set("--user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")

	
	if reqData.Proxy.Data != "" && reqData.Proxy.Protocol != "" {
		proxyURL := fmt.Sprintf("%s://%s", reqData.Proxy.Protocol, reqData.Proxy.Data)
		l.Set("--proxy-server=" + proxyURL)
		fmt.Println("Using proxy:", proxyURL)
	}

	controlURL := l.MustLaunch()
	browser := rod.New().ControlURL(controlURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(reqData.URL)
	page.MustWaitRequestIdle()

	
	if len(reqData.Cookies) > 0 {
		for _, cookie := range reqData.Cookies {
			parts := strings.Split(cookie, "\t")
			if len(parts) >= 7 {
				cookieName := parts[5]
				cookieValue := parts[6]
				page.MustSetCookie(&rod.Cookie{
					Name:  cookieName,
					Value: cookieValue,
				})
			}
		}
		fmt.Println("Cookies set successfully.")
	}


	page.MustWaitRequestIdle()
	bodyContent := page.MustElement("body").MustText()

	if strings.Contains(bodyContent, "too many people") {
		http.Error(w, "Too many people trying to logon", http.StatusTooManyRequests)
		return
	}

	var jsonData map[string]interface{}
	err := json.Unmarshal([]byte(bodyContent), &jsonData)
	if err != nil {
		http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	token, ok := jsonData["token"].(string)
	if !ok {
		http.Error(w, "Token not found in the response", http.StatusUnauthorized)
		return
	}

	fmt.Println("Token:", token)
	w.Write([]byte(token))
}

func createTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var requestData RequestData
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		http.Error(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}


	if requestData.AccessKey != accessKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	handlerCtx := r.Context()
	handler(requestData, w, handlerCtx)
}

func main() {
	http.HandleFunc("/createTask", createTaskHandler)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Use /createTask endpoint with a POST request and accessKey."))
	})

	fmt.Println("Server is running on port 5000")
	log.Fatal(http.ListenAndServe(":5000", nil))
}
