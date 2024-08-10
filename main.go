package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/ysmood/leakless/pkg/utils"
)

func generateRandomName() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)[:7]
}

func handleGrowtopia(page *rod.Page, w *http.ResponseWriter) {
	page.MustWaitRequestIdle()
	// elem := page.MustElementX(`//*[@id="yDmH0d"]/c-wiz/div/div[3]/div/div/div[2]/div/div/button`)
	// res, _ := elem.Visible()
	// if res {
	// 	elem.MustClick()
	// }
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
		//*[@id="modalShow"]/div/div/div/div/section/div/div[2]/div/div[3]/a
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
		http.Error(*w, "Too many people trying to logon", http.StatusTooManyRequests)
		return
	}

	var jsonData map[string]interface{}
	err := json.Unmarshal([]byte(bodyContent), &jsonData)
	if err != nil {
		http.Error(*w, "Failed to parse JSON", http.StatusInternalServerError)
		return
	}

	token, ok := jsonData["token"].(string)
	if !ok {
		http.Error(*w, "Token not found in the response", http.StatusUnauthorized)
		return
	}

	fmt.Println("Token: " + token)

	(*w).Write([]byte(token))
}

func handleGoogleLoginForm(email, password string, page *rod.Page, w *http.ResponseWriter) {
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
	handleGrowtopia(page, w)
}

func handleClickOnEmail(email string, page *rod.Page, w *http.ResponseWriter) {
	page.MustElementX(`//li/div[@data-identifier='` + email + `']`).MustClick()
	page.MustWaitRequestIdle()
	handleGrowtopia(page, w)
}

func handleInitial(email, password string, page *rod.Page, w *http.ResponseWriter) {
	fmt.Println("Handling initial page")
	page.MustElement("body")
	bodyContent := page.MustElement("body").MustHTML()

	if strings.Contains(bodyContent, "too many people") {
		http.Error(*w, "Too many people trying to logon. Please try again later.", http.StatusTooManyRequests)
		return
	}

	if strings.Contains(bodyContent, fmt.Sprintf(`data-identifier='%s'`, email)) {
		handleClickOnEmail(email, page, w)
		return
	}

	if strings.Contains(bodyContent, `id="identifierId"`) {
		handleGoogleLoginForm(email, password, page, w)
		return
	}

	otherNodes := page.MustElementsX(`//li/div[not(@data-identifier)]`)
	if len(otherNodes) > 0 {
		otherNodes[0].MustClick()
		handleGoogleLoginForm(email, password, page, w)
		return
	}
}

func handler(url, email, password string, w *http.ResponseWriter, handlerCtx context.Context) {
	l := launcher.New().Headless(false).Set(flags.Flag("--disable-extension")).Set("--excludeSwitches", "enable-automation").Set("--disable-blink-features", "AutomationControlled").Set("--lang", "en-US").Set("--user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	defer l.Cleanup()

	controlURL := l.MustLaunch()

	browser := rod.New().ControlURL(controlURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(url)
	page.MustWaitRequestIdle()
	handleInitial(email, password, page, w)
}

func main() {
	http.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			r.ParseForm()
			url := r.FormValue("url")
			email := r.FormValue("email")
			password := r.FormValue("password")
			handlerCtx := r.Context()
			handler(url, email, password, &w, handlerCtx)
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ResponseWriter.Write(w, []byte("You can use /token endpoint to send a POST request with url, email and password parameters"))
	})

	fmt.Println("Server is running on port 5000")
	log.Fatal(http.ListenAndServe(":5000", nil))
}
