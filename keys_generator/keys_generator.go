package main

import (
	"fmt"

	"github.com/nbd-wtf/go-nostr"
)

func main() {
	// Genera una nuova chiave privata casuale
	privKey := nostr.GeneratePrivateKey()

	// Calcola la chiave pubblica associata nel formato corretto per Nostr
	pubKey, err := nostr.GetPublicKey(privKey)
	if err != nil {
		fmt.Println("Errore:", err)
		return
	}

	fmt.Println("==================================================")
	fmt.Println("🔑 CHIAVI")
	fmt.Println("==================================================")
	fmt.Println("Private Key:", privKey)
	fmt.Println("Public Key:", pubKey)
	fmt.Println("==================================================")
}
