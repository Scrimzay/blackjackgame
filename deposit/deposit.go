package deposit

import (
	"os"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Scrimzay/blackjackgame/db"
	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/Scrimzay/loglogger"
	"github.com/gin-gonic/gin"
)

var (
	store *sessions.CookieStore
	log *logger.Logger
)

func init() {
	var err error
	log, err = logger.New("bettinglog.txt")
	if err != nil {
		log.Fatalf("Error starting new log in auth: %v", err)
	}
}

func DepositPOSTHandler(c *gin.Context) {
	fmt.Println("***DEPOSIT POST HANDLER RUNNING***")
	// Get the users session
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

	obfID, ok := session.Values["obfuscated_id"].(string)
	if !ok {
		log.Print(err)
		return
	}

	// parse the deposit type from the form
	depositType := c.PostForm("depositType")

	db, err := db.ConnectToDatabase()
	if err != nil {
		log.Fatalf("Error connecting to DB in depostPOSTHandler: %v", err)
		return
	}

	// process the deposit based on the deposit type
	switch depositType {
	case "solana":
		// parse solana deposit false
		walletAddress := c.PostForm("walletAddress")
		amountStr := c.PostForm("solanaAmount")
		if amountStr == "" {
			log.Print("Amount is missing in the request")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		// validate the amount
		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || amount <= 0 {
			log.Print("Invalid amount:", err)
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		// update solana balance
		query := `
			update balance
			set solana_balance = solana_balance + $1
			where obfuscatedid = $2
		`

		res, err := db.Exec(query, amount, obfID)
		if err != nil {
			log.Printf("Error updating balance: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			query := `
			insert into balance
			(obfuscatedid, solana_balance)
			values ($1, $2)
			`
			_, err := db.Exec(query, obfID, amount)
			if err != nil {
				log.Printf("Error inserting balance: %v", err)
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
		}

		// log deposit for debugging
		log.Printf("Solana deposit: obfID=%s, walletAddress=%s, amount=%f\n", obfID, walletAddress, amount)
	
	case "card":
		// parse card deposit fields
		name := c.PostForm("name")
		billingAddress := c.PostForm("billingAddress")
		cardNumber := c.PostForm("cardNumber")
		cvv := c.PostForm("cvv")
		expiry := c.PostForm("expiry")
		amountStr := c.PostForm("cardAmount")
		if amountStr == "" {
			log.Print("Amount is missing in the request")
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		// validate amount
		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || amount <= 0 {
			log.Print("Invalid amount:", err)
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		// update cash balance
		query := `
			update balance
			set cash_balance = cash_balance + $1
			where obfuscatedid = $2
		`

		res, err := db.Exec(query, amount, obfID)
		if err != nil {
			log.Printf("Error updating cash balance: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			query := `
			insert into balance
			(obfuscatedid, cash_balance) 
			values ($1, $2)
			`
			_, err := db.Exec(query, obfID, amount)
			if err != nil {
				log.Printf("Error inserting balance: %v", err)
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
		}

		// log deposit for debugging
		log.Printf("Card deposit: obfID=%s, name=%s, billingAddress=%s, cardNumber=%s, cvv=%s, expiry=%s, amount=%f\n",
			obfID, name, billingAddress, cardNumber, cvv, expiry, amount)

	default:
		log.Print("Invalid deposit type:", depositType)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	message := "Please do not close this tab."
	c.HTML(200, "success.html", gin.H{
		"Message": message,
	})
}