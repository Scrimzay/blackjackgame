package auth

import (
	"fmt"
	"net/http"
	"os"
	"time"
	"encoding/hex"
	"crypto/sha256"

	"github.com/Scrimzay/blackjackgame/db"
	"github.com/Scrimzay/loglogger"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/spotify"
)

var (
	log *logger.Logger
	store *sessions.CookieStore
)

func init() {
	var err error
	log, err = logger.New("providerlog.txt")
	if err != nil {
		log.Fatalf("Error starting new log in auth: %v", err)
	}
}

func ConnectToProvider() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file in auth: %v", err)
	}
	fmt.Println("***CONNECT TO PROVIDER RUNNING***")
	sessionKey := os.Getenv("SESSION_SECRET")
	if sessionKey == "" {
		log.Fatal("Session secret not loaded.")
		return
	}

	maxAge := 86400 * 30
	isProd := false // Change to true for production environment

	store = sessions.NewCookieStore([]byte(sessionKey))
	store.MaxAge(maxAge)
	store.Options.Path = ("/")
	store.Options.HttpOnly = true
	store.Options.Secure = isProd
	store.Options.SameSite = http.SameSiteLaxMode

	gothic.Store = store

	goth.UseProviders(
		spotify.New(os.Getenv("SPOTIFY_ID"), os.Getenv("SPOTIFY_SECRET"), "http://localhost:3000/auth/spotify/callback"),
	)
}

func BeginAuthHandler(c *gin.Context) {
	fmt.Println("****BEGIN AUTH HANDLER RUNNING****")

	// Get the users session
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println("Error getting session, most likely not signed in")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	fmt.Println("Got the users session in beginauth")
	// Check if user is already authenticated
	if userID, ok := session.Values["user_id"]; ok && userID != "" {
		// If user is already logged in, return to home
		c.Redirect(http.StatusFound, "/")
		return
	}

	// Extract the provider
	provider := c.Param("provider")
	if provider == "" {
		c.HTML(http.StatusBadRequest, "login.html", gin.H{
			"Message": "Provider not specified",
		})
		return
	}

	q := c.Request.URL.Query()
	q.Add("provider", provider)
	c.Request.URL.RawQuery = q.Encode()

	fmt.Println("Request URL: ", c.Request.URL.String())

	gothic.BeginAuthHandler(c.Writer, c.Request)
}

func CompleteAuthHandler(c *gin.Context) {
	fmt.Println("****COMPLETE AUTH HANDLER RUNNING****")

	provider := c.Param("provider")
	fmt.Println("Provider: ", provider)

	user, err := gothic.CompleteUserAuth(c. Writer, c.Request)
	if err != nil {
		fmt.Println("Could not complete user auth")
		c.Redirect(http.StatusTemporaryRedirect, "/login")
		return
	}

	fmt.Println("Authenticated user: ", user)

	fmt.Println("Connecting to DB")
	db, err := db.ConnectToDatabase()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer db.Close()

	fmt.Println("Connected to DB")
	email := user.Email
	userid := user.UserID

	// Get or create session
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println(err)
		log.Fatal("Failed to create or retrieve session")
		return
	}

	fmt.Println("Created or recieved session")

	// Checks DB for multiple users
	var userCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = $1", email).Scan(&userCount)
	if err != nil {
		log.Print(err)
		return
	}

	var obfuscatedID string

	// If no duplicates, inputs the user in the database, if there is
	// it just logs theres already a similar user (dupe)

	fmt.Println("Parsing SQL Query")
	if userCount == 0 {
		obfuscatedID = generateObfuscatedID(userid)

		p, err := db.Prepare("INSERT INTO users(oauthid, email, provider, obfuscatedid) VALUES ($1, $2, $3, $4)")
		if err != nil {
			log.Print(err)
			return
		}
		defer p.Close()

		fmt.Println("Executing SQL Query")
		_, err = p.Exec(userid, email, provider, obfuscatedID)
		if err != nil {
			log.Printf("Error inserting new user: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		fmt.Println("New user added to DB")
	} else {
		// Optional: Handle the case where the user already exists (e.g., update or skip)
		// User already exists in the database, so we skip the insertion and return the ob'd ID
		err := db.QueryRow("SELECT obfuscatedid FROM users WHERE oauthid = $1", userid).Scan(&obfuscatedID)
		if err != nil {
			log.Printf("Error retrieving ob'd ID: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
			return
		}
		fmt.Println("User already in database")
	}

	fmt.Println("User added to DB")

	session.Values["obfuscated_id"] = obfuscatedID
	session.Values["user_id"] = userid

	// Save session
	if err = session.Save(c.Request, c.Writer); err != nil {
		fmt.Println("Error saving session:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Session error"})
		return
	}

	fmt.Println("User Info stored in session")
	fmt.Println("UserID: ", userid)
	fmt.Println("Obfuscated UserID: ", obfuscatedID)

	c.Redirect(http.StatusFound, "/")
}

func LogoutHandler(c *gin.Context) {
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println(err)
		return
	}

	delete(session.Values, "user_id")
	delete(session.Values, "obfuscated_id")
	// #nosec G104 -- Ignore specific gosec warning
	session.Save(c.Request, c.Writer)
	time.Sleep(3 * time.Second)
	c.HTML(200, "login.html", gin.H{"Message": "Logged out"})
}

func DeleteProfile(c *gin.Context) {
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println("Could not get user session")
		return
	}

	userID, ok := session.Values["user_id"].(string)
	if !ok {
		fmt.Println(err)
		return
	}

	db, err := db.ConnectToDatabase()
	if err != nil {
		log.Fatalf("Error connecting to DB in delete profile: %v", err)
		return
	}

	stmt, err := db.Prepare("DELETE FROM users WHERE oauthuserid = ?")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(userID)
	if err != nil {
		fmt.Println(err)
		return
	}

	delete(session.Values, "user_id")
	delete(session.Values, "obfuscated_id")
	// #nosec G104 -- Ignore specific gosec warning
	session.Save(c.Request, c.Writer)
	defer db.Close()

	time.Sleep(3 * time.Second)

	c.HTML(200, "index.html", gin.H{"Message": "User deleted"})
}

func generateObfuscatedID(userID string) string {
	// Create a sha256 hash of the userID
	hash := sha256.Sum256([]byte(userID))
	// Take the first 4 bytes (8 characters) of the hash
	return hex.EncodeToString(hash[:4])
}