package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func main() {
	fmt.Println("SynCent — Key Generator")
	fmt.Println()

	masterKey := make([]byte, 32)
	rand.Read(masterKey)
	fmt.Printf("MASTER_KEY=%s\n", base64.StdEncoding.EncodeToString(masterKey))

	jwtKey := make([]byte, 64)
	rand.Read(jwtKey)
	fmt.Printf("JWT_SECRET=%s\n", base64.StdEncoding.EncodeToString(jwtKey))

	fmt.Println()
	fmt.Println("Copy these values into your .env file.")
	fmt.Println("BACK UP your MASTER_KEY — if lost, all credentials are unrecoverable.")
}
