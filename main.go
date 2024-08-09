package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/go-rod/stealth"
)

func handleGoogleLoginForm(email, password string, w *http.ResponseWriter) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.SendKeys(`//*[@id="identifierId"]`, email, chromedp.BySearch),
		chromedp.Click(`//*[@id="identifierNext"]/div/button/span`, chromedp.BySearch),
		chromedp.WaitVisible(`//*[@id="password"]/div[1]/div/div[1]/input`, chromedp.BySearch),
		chromedp.Sleep(3 * time.Second),
		chromedp.SendKeys(`//*[@id="password"]/div[1]/div/div[1]/input`, password, chromedp.BySearch),
		chromedp.Click(`//*[@id="passwordNext"]/div/button/span`, chromedp.BySearch),
		chromedp.Sleep(3 * time.Second),
		chromedp.WaitReady(`body`),
		chromedp.Sleep(3 * time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var bodyContent string
			err := chromedp.InnerHTML("body", &bodyContent).Do(ctx)
			if err != nil {
				return err
			}

			if contains := strings.Contains(bodyContent, "too many people"); contains {
				http.Error(*w, "Too many people are using this account right now. Please try again later.", http.StatusTooManyRequests)
				return nil
			}

			var jsonData map[string]interface{}
			err = json.Unmarshal([]byte(bodyContent), &jsonData)
			if err != nil {
				return err
			}

			token, ok := jsonData["token"].(string)
			if !ok {
				http.Error(*w, "Token not found in the response", http.StatusUnauthorized)
				return nil
			}

			http.ResponseWriter.Write(*w, []byte(token))
			return nil
		}),
	}
}

func handleClickOnEmail(email string, w *http.ResponseWriter) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Click(`//li/div[@data-identifier='`+email+`']`, chromedp.BySearch),
		chromedp.WaitReady("body"),
		chromedp.Sleep(3 * time.Second),
		chromedp.WaitReady(`body`),
		chromedp.Sleep(3 * time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var bodyContent string
			err := chromedp.InnerHTML("body", &bodyContent).Do(ctx)
			if err != nil {
				return err
			}

			if contains := strings.Contains(bodyContent, "too many people"); contains {
				http.ResponseWriter.Write(*w, []byte("Too many people are using this account right now. Please try again later."))
				return nil
			}

			var jsonData map[string]interface{}
			err = json.Unmarshal([]byte(bodyContent), &jsonData)
			if err != nil {
				return err
			}

			token, ok := jsonData["token"].(string)
			if !ok {
				http.ResponseWriter.Write(*w, []byte("Token not found in the response"))
				return nil
			}

			http.ResponseWriter.Write(*w, []byte(token))
			return nil
		}),
	}
}

func handleGoogle(url, email, password string, w *http.ResponseWriter) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.EvaluateAsDevTools(stealth.JS, nil),
		chromedp.Navigate(url),
		chromedp.Sleep(3 * time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var bodyContent string
			err := chromedp.InnerHTML("body", &bodyContent).Do(ctx)
			if err != nil {
				return err
			}

			if contains := strings.Contains(bodyContent, "too many people"); contains {
				http.ResponseWriter.Write(*w, []byte("Too many people are using this account right now. Please try again later."))
				return nil
			}

			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var nodes []*cdp.Node
			err := chromedp.Nodes(`//li/div[@data-identifier='`+email+`']`, &nodes, chromedp.AtLeast(0)).Do(ctx)
			if err != nil {
				return err
			}

			if len(nodes) > 0 {
				fmt.Println("Clicking on the email")
				return handleClickOnEmail(email, w).Do(ctx)
			}

			err = chromedp.Nodes(`//*[@id="identifierId"]`, &nodes, chromedp.AtLeast(0)).Do(ctx)
			if err != nil {
				return err
			}
			if len(nodes) > 0 {
				return handleGoogleLoginForm(email, password, w).Do(ctx)
			}

			err = chromedp.Nodes(`//li/div[not(@data-identifier)]`, &nodes, chromedp.AtLeast(0)).Do(ctx)
			if err != nil {
				return err
			}

			if len(nodes) > 0 {
				err = chromedp.Click(`//li/div[not(@data-identifier)]`, chromedp.BySearch).Do(ctx)
				if err != nil {
					return err
				}
				return handleGoogleLoginForm(email, password, w).Do(ctx)
			}

			return nil
		}),
	}
}

func handler(url, email, password string, w *http.ResponseWriter, handlerCtx context.Context) {
	resultChan := make(chan error, 1)

	go func() {
		opts := []chromedp.ExecAllocatorOption{
			// chromedp.Headless,
			chromedp.UserDataDir("storage"),
			chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"),
			chromedp.NoDefaultBrowserCheck,
			chromedp.Flag("excludeSwitches", "enable-automation"),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
			chromedp.Flag("lang", "en-US"),
		}

		allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
		defer cancel()

		ctx, cancel := chromedp.NewContext(
			allocCtx,
		)
		defer cancel()

		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		err := chromedp.Run(ctx, handleGoogle(url, email, password, w))
		resultChan <- err
	}()

	select {
	case err := <-resultChan:
		if err != nil {
			http.Error(*w, err.Error(), http.StatusInternalServerError)
			log.Println("ChromeDP error:", err)
		} else {
			(*w).WriteHeader(http.StatusOK)
		}
	case <-handlerCtx.Done():
		http.Error(*w, "Request Timeout", http.StatusGatewayTimeout)
	}
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
