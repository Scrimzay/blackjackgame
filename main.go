package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/Scrimzay/blackjackgame/auth"
	"github.com/Scrimzay/blackjackgame/deposit"
	"github.com/Scrimzay/blackjackgame/db"
	"github.com/Scrimzay/blackjackgame/deck"
	"github.com/Scrimzay/blackjackgame/hand"

	"github.com/Scrimzay/loglogger"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
)

var (
	log *logger.Logger
	games = make(map[string]*hand.GameState) // store game states
	gamesMu sync.Mutex // mutex to protect concurrect access to game
	store *sessions.CookieStore
)

func init() {
	var err error
	log, err = logger.New("log.txt")
	if err !=nil {
		log.Fatalf("Failed to start new logger: %v", err)
	}
}

func authRequired(store *sessions.CookieStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := godotenv.Load(".env")
		if err != nil {
			log.Fatalf("Error loading .env file in auth: %v", err)
		}
		sessionKey := os.Getenv("SESSION_SECRET")
		if sessionKey == "" {
			log.Fatal("Session secret not loaded.")
			return
		}
		store = sessions.NewCookieStore([]byte(sessionKey))
		session, err := store.Get(c.Request, "session-name")
		if err != nil {
			fmt.Println("Error getting session, most likely not signed in")
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		// Check if user is already authenticated
		if userID, ok := session.Values["user_id"]; ok && userID != "" {
			// User is authenticated, proceed with request
			c.Next()
		} else {
			// User is not authenticated, redirect to login
			c.Redirect(http.StatusFound ,"/login")
			c.Abort()
		}
	}
}

func main() {
	r := gin.Default()
	db.ConnectToDatabase()
	auth.ConnectToProvider()

	// Register custom template function
	r.SetFuncMap(template.FuncMap{
		"cardImagePath": func(c deck.Card) string {
			return c.CardImagePath()
		},
	})
	r.LoadHTMLGlob("templates/*.html")
	r.Static("/static", "./static")

	r.GET("/", indexHandler)
	r.GET("/login", loginGETHandler)
	r.GET("/auth/:provider", auth.BeginAuthHandler)
	r.GET("/auth/:provider/callback", auth.CompleteAuthHandler)
	r.GET("/logout", authRequired(store), auth.LogoutHandler)
	r.GET("/delete", authRequired(store), auth.DeleteProfile)
	r.DELETE("/deleteaccount")
	r.GET("/deposit", authRequired(store), depositGETHandler)
	r.POST("/deposit", authRequired(store), deposit.DepositPOSTHandler)
	r.GET("/blackjack", authRequired(store), blackjackHandler)
	r.GET("/blackjack/game/:id", authRequired(store), blackjackGameIDHandler)
	r.POST("/blackjack/game/:id/deal", authRequired(store), blackjackDealHandler)
	r.POST("/blackjack/game/:id/hit", authRequired(store), blackjackHitHandler)
	r.POST("/blackjack/game/:id/stand", authRequired(store), blackjackStandHandler)
	r.POST("/blackjack/game/:id/bet", authRequired(store), betHandler)

	err := r.Run(":3000")
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

func indexHandler(c *gin.Context) {
	c.HTML(200, "index.html", nil)
}

func loginGETHandler(c *gin.Context) {
	c.HTML(200, "login.html", nil)
}

func blackjackHandler(c *gin.Context) {
	c.HTML(200, "blackjackIndex.html", nil)
}

func depositGETHandler(c *gin.Context) {
	c.HTML(200, "deposit.html", nil)
}

func betHandler(c *gin.Context) {
	gamesMu.Lock()
	defer gamesMu.Unlock()
	// Get the user's session
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println("Error getting session:", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Get the user's obfuscated ID from the session
	obfID, ok := session.Values["obfuscated_id"].(string)
	if !ok {
		fmt.Println("Error: obfuscated_id not found in session")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	betCurrency := c.PostForm("betCurrency")
	betAmountStr := c.PostForm("betAmount")

	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		log.Print("Invalid bet amount:", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	db, err := db.ConnectToDatabase()
	if err != nil {
		log.Fatalf("Error connecting to DB in betHandler: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var cashBalance float64
	var solanaBalance float64
	query := `
		select cash_balance, solana_balance
		from balance
		where obfuscatedid = $1
	`
	err = db.QueryRow(query, obfID).Scan(&cashBalance, &solanaBalance)
	if err != nil {
		fmt.Println("Error fetching balance:", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	switch betCurrency {
	case "cash":
		if betAmount > cashBalance {
			fmt.Println("Insufficient cash balance")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		cashBalance -= betAmount

	case "solana":
		if betAmount > solanaBalance {
			fmt.Println("Insufficient Solana balance")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		solanaBalance -= betAmount

	default:
		fmt.Println("Invalid bet currency:", betCurrency)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	updateQuery := `
		update balance
		set cash_balance = $1, solana_balance = $2
		where obfuscatedid = $3
	`
	_, err = db.Exec(updateQuery, cashBalance, solanaBalance, obfID)
	if err != nil {
		log.Print("Error updating balance:", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	gameID := c.Param("id")

	gs, exists := games[gameID]
	if !exists {
		gs = &hand.GameState{
			Deck: hand.Shuffle(hand.GameState{}).Deck,
			State: hand.StatePlayerTurn,
			BetAmount: betAmount,
			BetCurrency: betCurrency,
		}
	} else {
		gs.BetAmount = betAmount
		gs.BetCurrency = betCurrency
	}

	renderGame(c, gs)
}

func blackjackGameIDHandler(c *gin.Context) {
	gameID := c.Param("id")
	gamesMu.Lock()
	defer gamesMu.Unlock()

	// init a new game if it doesnt exist
	if _, exists := games[gameID]; !exists {
		games[gameID] = &hand.GameState{
			Deck: hand.Shuffle(hand.GameState{}).Deck, // shuffle a new deck
			State: hand.StatePlayerTurn,
		}
	}

	renderGame(c, games[gameID])
}

func blackjackDealHandler(c *gin.Context) {
	gameID := c.Param("id")
	gamesMu.Lock()
	defer gamesMu.Unlock()

	gs, exists := games[gameID]
	if !exists || gs == nil {
		// Initialize a new game if it doesn't exist
		gs = &hand.GameState{
			Deck:  hand.Shuffle(hand.GameState{}).Deck, // Shuffle a new deck
			State: hand.StatePlayerTurn,
		}
		games[gameID] = gs
	}
	*gs = hand.Deal(*gs) // deal cards

	renderGame(c, gs)
}

func blackjackHitHandler(c *gin.Context) {
	gameID := c.Param("id")
	gamesMu.Lock()
	defer gamesMu.Unlock()

	gs := games[gameID]
	*gs = hand.Hit(*gs)

	renderGame(c, gs)
}

func blackjackStandHandler(c *gin.Context) {
	gameID := c.Param("id")
	gamesMu.Lock()
	defer gamesMu.Unlock()

	gs := games[gameID]
	*gs = hand.Stand(*gs)

	// dealers turn
	for gs.State == hand.StateDealerTurn {
		*gs = hand.Hit(*gs)
	}

	renderGame(c, gs)
}

func renderGame(c *gin.Context, gs *hand.GameState) {
	// Ensure the dealer has at least one card
	if len(gs.Dealer) == 0 {
		gs.Dealer = make(hand.Hand, 0)
	}

	// Get the user's session
	session, err := store.Get(c.Request, "session-name")
	if err != nil {
		fmt.Println("Error getting session:", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Get the user's obfuscated ID from the session
	obfID, ok := session.Values["obfuscated_id"].(string)
	if !ok {
		fmt.Println("Error: obfuscated_id not found in session")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Connect to the database
	db, err := db.ConnectToDatabase()
	if err != nil {
		log.Fatalf("Error connecting to DB in renderGame: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// get the users balance from the DB
	var cashBalance float64
	var solanaBalance float64
	query := `
		select cash_balance, solana_balance
		from balance
		where obfuscatedid = $1
	`
	err = db.QueryRow(query, obfID).Scan(&cashBalance, &solanaBalance)
	if err != nil {
		log.Print("Error fetching balance:", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.HTML(200, "blackjackgame.html", gin.H{
		"Player": gs.Player,
		"Dealer": gs.Dealer,
		"PlayerScore": gs.Player.Score(),
		"DealerScore": gs.Dealer.Score(),
		"GameOver": gs.State == hand.StateHandOver,
		"DealerHidden": gs.State == hand.StatePlayerTurn, // hide dealers 2nd card on player turn
		"CashBalance": cashBalance,
		"SolanaBalance": solanaBalance,
		"BetAmount": gs.BetAmount,
		"BetCurrency": gs.BetCurrency,
	})
}