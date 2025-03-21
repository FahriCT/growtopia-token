package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/ysmood/leakless/pkg/utils"
)

type Config struct {
	AccessKey string `json:"accessKey"`
}

type TaskRequest struct {
	AccessKey  string   `json:"accessKey"`
	AppleData  string   `json:"appleData"`
	Cookies    []string `json:"cookies"`
	Mail       string   `json:"mail"`
	Mobile     bool     `json:"mobile"`
	Pass       string   `json:"pass"`
	Proxy      Proxy    `json:"proxy"`
	Recovery   string   `json:"recovery"`
	Secret     string   `json:"secret"`
	URL        string   `json:"url"`
}

type TaskResultRequest struct {
	AccessKey string `json:"accessKey"`
	ID        string `json:"id"`
}

type Proxy struct {
	Data     string `json:"data"`
	Protocol string `json:"protocol"`
}

type TaskResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

type TaskStatusResponse struct {
	StatusCode int    `json:"statusCode"`
	Status     string `json:"status"`
	ID         string `json:"id"`
	Token      string `json:"token,omitempty"`
	Error      string `json:"error,omitempty"`
}

const (
	StatusProcessing = 1
	StatusCompleted  = 2
	StatusFailed     = 3
)

type TaskStatus struct {
	StatusCode int
	Status     string
	Token      string
	Error      string
	StartTime  time.Time
}

var appConfig = Config{
	AccessKey: "senvas", 
}

var (
	taskStatusMap = make(map[string]TaskStatus)
	taskMutex     = &sync.RWMutex{}
)

func generateRandomName() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)[:7]
}

func handleGrowtopia(page *rod.Page) (string, error) {
	page.MustWaitRequestIdle()
	
	bodyContent := page.MustElement("body").MustHTML()
	if strings.Contains(bodyContent, `id="login-name"`) {
		page.WaitLoad()
		page.MustElementX(`//*[@id="login-name"]`).MustInput(generateRandomName())
		page.MustElementX(`//*[@id="modalShow"]/div/div/div/div/section/div/div[2]/div/form/div[2]/input`).MustClick()
		page.WaitLoad()
	}

	if strings.Contains(bodyContent, `id="profile-conflict"`) {
		fmt.Println("Profile conflict detected")
		page.WaitLoad()
		page.MustElementX(`//*[@id="profile-conflict"]/div[3]/a/div/div[2]/button`).MustClick()
		for {
			bodyContent = page.MustElement("body").MustHTML()
			if !strings.Contains(bodyContent, `id="profile-conflict"`) {
				break
			}
			utils.Sleep(1)
		}
	}

	if strings.Contains(bodyContent, `id="modalShow"`) {
		fmt.Println("Modal show detected")
		page.WaitLoad()
		page.MustElementX(`//*[@id="modalShow"]/div/div/div/div/section/div/div[2]/div/div[3]/a`).MustClick()
		page.WaitLoad()
	}

	bodyContent = page.MustElement("body").MustText()

	if strings.Contains(bodyContent, "too many people") {
		return "", fmt.Errorf("too many people trying to logon")
	}

	var jsonData map[string]interface{}
	err := json.Unmarshal([]byte(bodyContent), &jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %v", err)
	}

	token, ok := jsonData["token"].(string)
	if !ok {
		return "", fmt.Errorf("token not found in the response")
	}

	fmt.Println("Token: " + token)
	return token, nil
}

func handleGoogleLoginForm(email, password string, page *rod.Page) (string, error) {
	page.MustWaitRequestIdle()
	page.MustElementX(`//*[@id="identifierId"]`).MustInput(email)
	page.MustElementX(`//*[@id="identifierNext"]/div/button/span`).MustClick()
	page.MustElementX(`//*[@id="password"]/div[1]/div/div[1]/input`).MustVisible()
	page.MustElementX(`//*[@id="password"]/div[1]/div/div[1]/input`).MustInput(password)
	page.MustElementX(`//*[@id="passwordNext"]/div/button/span`).MustClick()
	for {
		bodyContent := page.MustElement("body").MustHTML()
		if !strings.Contains(bodyContent, `id="passwordNext"`) {
			break
		}
		utils.Sleep(1)
	}
	return handleGrowtopia(page)
}

func handleClickOnEmail(email string, page *rod.Page) (string, error) {
	page.MustElementX(`//li/div[@data-identifier='` + email + `']`).MustClick()
	page.MustWaitRequestIdle()
	return handleGrowtopia(page)
}

func handleInitial(email, password string, page *rod.Page) (string, error) {
	fmt.Println("Handling initial page")
	page.MustElement("body")
	bodyContent := page.MustElement("body").MustHTML()

	if strings.Contains(bodyContent, "too many people") {
		return "", fmt.Errorf("too many people trying to logon. please try again later")
	}

	if strings.Contains(bodyContent, fmt.Sprintf(`data-identifier='%s'`, email)) {
		return handleClickOnEmail(email, page)
	}

	if strings.Contains(bodyContent, `id="identifierId"`) {
		return handleGoogleLoginForm(email, password, page)
	}

	otherNodes := page.MustElementsX(`//li/div[not(@data-identifier)]`)
	if len(otherNodes) > 0 {
		otherNodes[0].MustClick()
		return handleGoogleLoginForm(email, password, page)
	}

	return "", fmt.Errorf("unable to handle the page content")
}

func processTaskAsync(task TaskRequest) {
	taskID := task.Mail
	
	taskMutex.Lock()
	taskStatusMap[taskID] = TaskStatus{
		StatusCode: StatusProcessing,
		Status:     "processing",
		StartTime:  time.Now(),
	}
	taskMutex.Unlock()
	
	go func() {
		token, err := setupBrowser(task.URL, task)
		
		taskMutex.Lock()
		defer taskMutex.Unlock()
		
		if err != nil {
			taskStatusMap[taskID] = TaskStatus{
				StatusCode: StatusFailed,
				Status:     "failed",
				Error:      err.Error(),
				StartTime:  taskStatusMap[taskID].StartTime,
			}
		} else {
			taskStatusMap[taskID] = TaskStatus{
				StatusCode: StatusCompleted,
				Status:     "completed",
				Token:      token,
				StartTime:  taskStatusMap[taskID].StartTime,
			}
		}
		
		go cleanupOldTasks()
	}()
}

func cleanupOldTasks() {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	
	cutoff := time.Now().Add(-30 * time.Minute)
	for id, status := range taskStatusMap {
		if status.StartTime.Before(cutoff) {
			delete(taskStatusMap, id)
		}
	}
}

func setupBrowser(url string, task TaskRequest) (string, error) {
	l := launcher.New().
		Headless(false).
		Set(flags.Flag("--disable-extension")).
		Set("--excludeSwitches", "enable-automation").
		Set("--disable-blink-features", "AutomationControlled").
		Set("--lang", "en-US").
		Set("--user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	
	// Set up proxy if provided
	if task.Proxy.Data != "" && task.Proxy.Protocol != "" {
		proxyParts := strings.Split(task.Proxy.Data, "@")
		if len(proxyParts) == 2 {
			proxyServer := proxyParts[1]
			proxyAuth := strings.Replace(proxyParts[0], ":", "//", 1)
			proxyURL := fmt.Sprintf("%s://%s@%s", task.Proxy.Protocol, proxyAuth, proxyServer)
			l.Set("--proxy-server", proxyURL)
		}
	}
	
	defer l.Cleanup()

	controlURL := l.MustLaunch()
	browser := rod.New().ControlURL(controlURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(url)
	
	if len(task.Cookies) > 0 {
		for _, cookieStr := range task.Cookies {
			parts := strings.Split(cookieStr, "\t")
			if len(parts) >= 7 {
				domain := parts[0]
				path := parts[2]
				secure := parts[3] == "TRUE"
				expires := parts[4]
				name := parts[5]
				value := parts[6]
				
				cookie := &rod.Cookie{
					Domain:   domain,
					Path:     path,
					Secure:   secure,
					Name:     name,
					Value:    value,
				}
				
				page.SetCookies([]*rod.Cookie{cookie})
			}
		}
	}
	
	page.MustWaitRequestIdle()
	return handleInitial(task.Mail, task.Pass, page)
}

func handleCreateTaskRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{
			Success: false,
			Error:   "Failed to read request body",
		})
		return
	}
	
	var task TaskRequest
	if err := json.Unmarshal(body, &task); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskResponse{
			Success: false,
			Error:   "Invalid JSON format",
		})
		return
	}
	
	if task.AccessKey != appConfig.AccessKey {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(TaskResponse{
			Success: false,
			Error:   "Invalid access key",
		})
		return
	}
	
	processTaskAsync(task)
	
	json.NewEncoder(w).Encode(TaskResponse{
		Success: true,
		ID:      task.Mail,
	})
}

func handleGetTaskResult(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskStatusResponse{
			StatusCode: StatusFailed,
			Status:     "failed",
			Error:      "Failed to read request body",
		})
		return
	}
	
	var taskResult TaskResultRequest
	if err := json.Unmarshal(body, &taskResult); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TaskStatusResponse{
			StatusCode: StatusFailed,
			Status:     "failed",
			Error:      "Invalid JSON format",
		})
		return
	}
	
	if taskResult.AccessKey != appConfig.AccessKey {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(TaskStatusResponse{
			StatusCode: StatusFailed,
			Status:     "failed",
			Error:      "Invalid access key",
		})
		return
	}
	
	taskMutex.RLock()
	status, exists := taskStatusMap[taskResult.ID]
	taskMutex.RUnlock()
	
	if !exists {
		json.NewEncoder(w).Encode(TaskStatusResponse{
			StatusCode: StatusProcessing,
			Status:     "processing",
			ID:         taskResult.ID,
		})
		return
	}
	
	response := TaskStatusResponse{
		StatusCode: status.StatusCode,
		Status:     status.Status,
		ID:         taskResult.ID,
	}
	
	if status.Token != "" {
		response.Token = status.Token
	}
	
	if status.Error != "" {
		response.Error = status.Error
	}
	
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Legacy endpoint for backward compatibility with mori
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			url := r.FormValue("url")
			email := r.FormValue("email")
			password := r.FormValue("password")
			
			task := TaskRequest{
				AccessKey: appConfig.AccessKey,
				Mail:      email,
				Pass:      password,
				URL:       url,
			}
			
			token, err := setupBrowser(url, task)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			w.Write([]byte(token))
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})
	
	// New endpoint for lucifer
	http.HandleFunc("/createTask", handleCreateTaskRequest)
	
	// Endpoint for getting task results
	http.HandleFunc("/getTaskResult", handleGetTaskResult)
	
	// Root endpoint
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Available endpoints: /token, /createTask, /getTaskResult"))
	})
	
	fmt.Println("Server is running on port 5000")
	log.Fatal(http.ListenAndServe(":5000", nil))
}
