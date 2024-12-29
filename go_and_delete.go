package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-github/v51/github"
	"golang.org/x/oauth2"
)

var (
	githubClient *github.Client
	clientID     = os.Getenv("OAUTH_CLIENT_ID")     // Set your client ID here
	clientSecret = os.Getenv("OAUTH_CLIENT_SECRET") // Set your client secret here
	redirectURI  = "http://localhost:8989/callback"
	tpl          = `
<!DOCTYPE html>
<html>
<head>
	<title>Bulk GitHub Repo Deleter</title>
	<style>
		body { font-family: Arial, sans-serif; margin: 20px; background-color: #f4f4f9; }
		h1 { color: #333; text-align: center; }
		form { margin: 20px auto; width: 50%; padding: 20px; background: #fff; border-radius: 5px; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
		input[type='checkbox'] { margin-right: 10px; }
		input[type='submit'] { background-color: #28a745; color: white; padding: 10px 20px; border: none; border-radius: 5px; cursor: pointer; }
		input[type='submit']:hover { background-color: #218838; }
		.repo { transition: all 0.3s ease; }
		.repo:hover { background-color: #e8f0fe; cursor: pointer; }
		.pagination { text-align: center; margin: 20px 0; }
		.pagination a { margin: 0 5px; text-decoration: none; padding: 5px 10px; background: #007bff; color: white; border-radius: 3px; }
		.pagination a:hover { background: #0056b3; }
	</style>
</head>
<body>
	<h1>Bulk GitHub Repo Deleter</h1>
	{{if .IsLoggedIn}}
		<form method="POST" action="/delete">
			{{range .Repos}}
			<div class="repo">
				<input type="checkbox" name="repos" value="{{.}}"> {{.}}<br>
			</div>
			{{end}}
			<br>
			<input type="submit" value="Delete Selected Repos">
		</form>
		<form action="/logout" method="POST">
			<input type="submit" value="Logout">
		</form>
	{{else}}
		<a href="/login">Login with GitHub</a>
	{{end}}
	<div class="pagination">
		{{if .HasPrev}}
		<a href="/?page={{.PrevPage}}">Previous</a>
		{{end}}
		{{if .HasNext}}
		<a href="/?page={{.NextPage}}">Next</a>
		{{end}}
	</div>
</body>
</html>
`
)

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/callback", callbackHandler)
	http.HandleFunc("/delete", deleteHandler)
	http.HandleFunc("/logout", logoutHandler)

	fmt.Println("Starting server on :8989")
	log.Fatal(http.ListenAndServe(":8989", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if githubClient == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}

	options := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 10, Page: page},
	}

	repos, resp, err := githubClient.Repositories.List(context.Background(), "", options)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repoNames := []string{}
	for _, repo := range repos {
		repoNames = append(repoNames, *repo.FullName)
	}

	data := struct {
		Repos      []string
		HasPrev    bool
		HasNext    bool
		PrevPage   int
		NextPage   int
		IsLoggedIn bool
	}{
		Repos:      repoNames,
		HasPrev:    resp.PrevPage > 0,
		HasNext:    resp.NextPage > 0,
		PrevPage:   resp.PrevPage,
		NextPage:   resp.NextPage,
		IsLoggedIn: githubClient != nil,
	}

	t := template.Must(template.New("index").Parse(tpl))
	t.Execute(w, data)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&scope=repo,delete_repo&redirect_uri=%s", clientID, redirectURI)
	http.Redirect(w, r, url, http.StatusFound)
}

func callbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}

	token := exchangeCodeForToken(code)
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	client := oauth2.NewClient(context.Background(), ts)
	githubClient = github.NewClient(client)

	http.Redirect(w, r, "/", http.StatusFound)
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	repos := r.Form["repos"]
	for _, repo := range repos {
		parts := strings.Split(repo, "/")
		owner, repoName := parts[0], parts[1]

		// Attempt to delete the repository
		_, err := githubClient.Repositories.Delete(context.Background(), owner, repoName)
		if err != nil {
			if ghErr, ok := err.(*github.ErrorResponse); ok && ghErr.Response.StatusCode == 403 {
				// If it's a 403 error, provide a more specific message
				fmt.Fprintf(w, "Failed to delete %s: You must have admin rights to delete this repository.<br>", repo)
			} else {
				// For other errors, just display the generic error message
				fmt.Fprintf(w, "Failed to delete %s: %s<br>", repo, err)
			}
		} else {
			fmt.Fprintf(w, "Deleted: %s<br>", repo)
		}
	}
	fmt.Fprintf(w, "<a href='/'>Go Back</a>")
}

func exchangeCodeForToken(code string) string {
	url := "https://github.com/login/oauth/access_token"
	body := fmt.Sprintf("client_id=%s&client_secret=%s&code=%s", clientID, clientSecret, code)

	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to get token: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	token, ok := result["access_token"].(string)
	if !ok {
		log.Fatalf("Failed to extract access token: %v", result)
	}

	return token
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// To logout, just clear the githubClient
	githubClient = nil
	http.Redirect(w, r, "/", http.StatusFound)
}
