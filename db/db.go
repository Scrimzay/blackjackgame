package db

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/Scrimzay/loglogger"
)

var (
	db *sql.DB
	host     string
	port     int
	user     string
	password string
	dbname   string
	log *logger.Logger
)

func init() {
	var err error
	log, err = logger.New("databaselog.txt")
	if err != nil {
		log.Fatalf("Failed to start new logger in DB: %v", err)
	}
}

func ConnectToDatabase() (*sql.DB, error) {
	err := godotenv.Load(".env")
	if err != nil {
		return nil, fmt.Errorf("Could not load .env file in db: %v", err)
	}

	// Fetch and parse environment variables
	host = os.Getenv("HOST")
	user = os.Getenv("USER")
	password = os.Getenv("PASSWORD")
	dbname = os.Getenv("DBNAME")
	portStr := os.Getenv("PORT")
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port value: %w", err)
	}

	// Build the connection string
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	// Open the database connection
	db, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// Verify the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("error connecting to the database: %w", err)
	}

	return db, nil
}