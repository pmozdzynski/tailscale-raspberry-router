package handlers

import (
	"crypto/rand"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/sessions"
)

var (
	// Session store
	store *sessions.CookieStore

	// Default credentials (can be overridden by environment variables)
	defaultUsername = "admin"
	defaultPassword = "admin"
)

func init() {
	// Generate a random secret key for session encryption
	// In production, you should use a fixed secret key stored securely
	secretKey := make([]byte, 32)
	if _, err := rand.Read(secretKey); err != nil {
		log.Fatal("Failed to generate session secret:", err)
	}

	// Use environment variable for secret if available, otherwise use generated one
	envSecret := os.Getenv("SESSION_SECRET")
	if envSecret != "" {
		secretKey = []byte(envSecret)
	} else {
		// For production, you should set SESSION_SECRET environment variable
		log.Println("Warning: Using randomly generated session secret. Set SESSION_SECRET for production.")
	}

	store = sessions.NewCookieStore(secretKey)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteLaxMode,
	}

	// Get credentials from environment variables if set
	if username := os.Getenv("AUTH_USERNAME"); username != "" {
		defaultUsername = username
	}
	if password := os.Getenv("AUTH_PASSWORD"); password != "" {
		defaultPassword = password
	}
}

// LoginHandler handles the login POST request
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Serve login page
		http.ServeFile(w, r, "./templates/login.html")
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	// Validate credentials
	if username != defaultUsername || password != defaultPassword {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Create session
	session, err := store.Get(r, "auth-session")
	if err != nil {
		http.Error(w, "Error creating session", http.StatusInternalServerError)
		return
	}

	session.Values["authenticated"] = true
	session.Values["username"] = username

	if err := session.Save(r, w); err != nil {
		http.Error(w, "Error saving session", http.StatusInternalServerError)
		return
	}

	// Redirect to main page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LogoutHandler handles logout
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "auth-session")
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	session.Values["authenticated"] = false
	session.Options.MaxAge = -1
	session.Save(r, w)

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// RequireAuth is a middleware that checks if the user is authenticated
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow access to login page and static assets
		if r.URL.Path == "/login" || r.URL.Path == "/logout" {
			next(w, r)
			return
		}

		// Check for static assets (CSS, JS, etc.)
		if r.URL.Path == "/styles.css" || r.URL.Path == "/script.js" || r.URL.Path == "/friendly-names.json" {
			next(w, r)
			return
		}

		session, err := store.Get(r, "auth-session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Check if user is authenticated
		auth, ok := session.Values["authenticated"].(bool)
		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// User is authenticated, proceed
		next(w, r)
	}
}
