package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
)

var store = sessions.NewCookieStore([]byte(os.Getenv("KEY")))

var conf = &oauth2.Config{
	ClientID:     os.Getenv("OAUTH_CLIENT_ID"),
	ClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
	Scopes:       []string{"full", "refresh_token"},
	Endpoint: oauth2.Endpoint{
		AuthURL:  "https://login.salesforce.com/services/oauth2/authorize",
		TokenURL: "https://login.salesforce.com/services/oauth2/token",
	},
	RedirectURL: os.Getenv("REDIRECT_URL"),
}

func main() {
	http.HandleFunc("/config", config)
	http.HandleFunc("/", handleAuth)
	http.HandleFunc("/auth/heroku/callback", handleCallback)

	err := http.ListenAndServe(":"+os.Getenv("PORT"), nil)
	if err != nil {
		panic(err)
	}
}

func config(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Current Configuration:")
	fmt.Fprintln(w, "Authentication URL: "+conf.Endpoint.AuthURL)
	fmt.Fprintln(w, "Token URL: "+conf.Endpoint.TokenURL)
	fmt.Fprintln(w, "Redirect URL: "+conf.RedirectURL)
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	//Redirect user to consent page to ask for permission for the scopes specified above.
	url := conf.AuthCodeURL("state", oauth2.AccessTypeOffline)
	log.Println("****Auth URL: " + url)

	body := `<a href="` + url + `">Sign in with Salesforce</a>`
	w.Write([]byte(body))
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	//get the authorization code
	code := r.FormValue("code")
	log.Println("****Returned code: " + code)

	//set up parameters to do the refresh token request - the default oauth2 exchange method does not work because of
	//of the additional grant_type parameter required by Salesforce
	data := url.Values{}
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", conf.ClientID)
	data.Set("client_secret", conf.ClientSecret)
	data.Set("redirect_uri", conf.RedirectURL)

	//request the refresh token
	client := &http.Client{}
	tokenResp, err := client.Post("https://login.salesforce.com/services/oauth2/token", "application/x-www-form-urlencoded",
		bytes.NewBufferString(data.Encode())) // <-- URL-encoded payload
	showError(err)
	//displayOnPage(w, tokenResp)

	type RefreshTokenData struct {
		Id            string `json:"id"`
		Issued_at     string `json:"issued_at"`
		Scope         string `json:"scope"`
		Instance_url  string `json:"instance_url"`
		Token_type    string `json:"token_type"`
		Refresh_token string `json:"refresh_token"`
		Id_token      string `json:"id_token"`
		Signature     string `json:"signature"`
		Access_token  string `json:"access_token"`
	}

	var tokenData RefreshTokenData
	//the response comes back as JSON. We need to decode it
	decoder := json.NewDecoder(tokenResp.Body)
	if jsonerr := decoder.Decode(&tokenData); jsonerr != nil {
		log.Println("****Failed to decode json")
		panic(jsonerr)
	}

	log.Println("****Refresh token: " + tokenData.Refresh_token)
	log.Println("****Instance url: " + tokenData.Instance_url)

	//store access token in session
	session, err := store.Get(r, "go-cumulusci")
	showError(err)
	session.Values["ACCESS_TOKEN"] = tokenData.Access_token
	session.Save(r, w)

	//send test query
	url := fmt.Sprintf(tokenData.Instance_url+"/services/data/v32.0/query?q=%s", url.QueryEscape("select Id, name from account"))
	log.Println("****Query url: " + url)
	req, err := http.NewRequest("GET", url, nil)
	showError(err)
	req.Header.Add("Authorization", "Bearer "+tokenData.Access_token)
	queryResp, err := client.Do(req)

	displayOnPage(w, queryResp)
}

func displayOnPage(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	showError(err)
	w.Write([]byte(respBody))
}

func showError(err error) {
	if err != nil {
		panic(err)
	}
}
