package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
)

const numeroSensori = 3

func getOrGenerateKeys(sensorName string) (string, string) {
	_ = godotenv.Load(".env")

	privKeyVar := sensorName + "_PRIV_KEY"
	pubKeyVar := sensorName + "_PUB_KEY"

	privKey := os.Getenv(privKeyVar)
	pubKey := os.Getenv(pubKeyVar)

	if privKey == "" {
		privKey = nostr.GeneratePrivateKey()
		pubKey, _ = nostr.GetPublicKey(privKey)

		f, err := os.OpenFile(".env", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("Errore nell'apertura del file .env: %v", err)
		}
		defer f.Close()

		envContent := fmt.Sprintf("\n%s=%s\n%s=%s\n", privKeyVar, privKey, pubKeyVar, pubKey)
		if _, err := f.WriteString(envContent); err != nil {
			log.Fatalf("Errore nel salvataggio: %v", err)
		}
		fmt.Printf("✅ Nuove chiavi generate per %s e salvate in .env\n", sensorName)
	} else {
		if pubKey == "" {
			pubKey, _ = nostr.GetPublicKey(privKey)
		}
	}
	return privKey, pubKey
}

func simulaSensore(idSensore int, privKey string, pubKey string, dashboardPubKey string, wg *sync.WaitGroup) {
	defer wg.Done()

	sensorTagId := fmt.Sprintf("sim-go-%02d", idSensore)
	durataValiditaDati := 24 * time.Hour

	sharedSecret, err := nip04.ComputeSharedSecret(dashboardPubKey, privKey)
	if err != nil {
		log.Printf("❌ [%s] Errore calcolo segreto condiviso ECDH: %v", sensorTagId, err)
		return
	}

	ctx := context.Background()

	relayURLs := []string{"wss://relay.damus.io", "ws://localhost:3334"}
	var activeRelays []*nostr.Relay

	for _, url := range relayURLs {
		relay, err := nostr.RelayConnect(ctx, url)
		if err != nil {
			log.Printf("⚠️ [%s] Impossibile connettersi a %s: %v", sensorTagId, url, err)
			continue
		}

		fmt.Printf("📡 [%s] Connesso a %s!\n", sensorTagId, url)
		activeRelays = append(activeRelays, relay)
	}

	defer func() {
		for _, r := range activeRelays {
			r.Close()
		}
	}()

	time.Sleep(time.Duration(rand.IntN(3)) * time.Second)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		temperatura := 18.0 + rand.Float64()*7.0
		messaggioInChiaro := fmt.Sprintf("%.2f", temperatura)

		messaggioCriptato, err := nip04.Encrypt(messaggioInChiaro, sharedSecret)
		if err != nil {
			log.Printf("❌ [%s] Errore di crittografia: %v", sensorTagId, err)
			continue
		}

		expirationTime := time.Now().Add(durataValiditaDati).Unix()
		expirationString := strconv.FormatInt(expirationTime, 10)

		ev := nostr.Event{
			PubKey:    pubKey,
			CreatedAt: nostr.Now(),
			Kind:      4,
			Tags: nostr.Tags{
				nostr.Tag{"p", dashboardPubKey},
				nostr.Tag{"t", "temperatura"},
				nostr.Tag{"sensor_id", sensorTagId},
				nostr.Tag{"expiration", expirationString},
			},
			Content: messaggioCriptato,
		}

		if err := ev.Sign(privKey); err != nil {
			continue
		}

		pubblicazioniRiuscite := 0

		for _, r := range activeRelays {
			if err := r.Publish(ctx, ev); err != nil {
				log.Printf("⚠️ [%s] Errore invio su %s: %v", sensorTagId, r.URL, err)
				continue
			}
			pubblicazioniRiuscite++
		}

		if pubblicazioniRiuscite > 0 {
			fmt.Printf("🔒 [%s] Criptato e Pubblicato su %d relay (Scade il: %s) --> %.2f °C\n",
				sensorTagId,
				pubblicazioniRiuscite,
				time.Unix(expirationTime, 0).Format("02/01 15:04"),
				temperatura)
		}
	}
}

func main() {
	_ = godotenv.Load(".env")

	dashboardPubKey := os.Getenv("DASHBOARD_PUB_KEY")

	if dashboardPubKey == "" || len(dashboardPubKey) != 64 {
		log.Fatal("❌ ERRORE CRITICO: Variabile DASHBOARD_PUB_KEY non trovata o non valida nel file .env!\nInseriscila nel file .env prima di avviare i sensori.")
	}

	fmt.Println("=====================================================")
	fmt.Printf("🚀 AVVIO SIMULATORE FLOTTA IOT CRIPTATA E RIDONDANTE (%d Sensori)\n", numeroSensori)
	fmt.Println("=====================================================")

	var wg sync.WaitGroup

	type SensorIdentity struct {
		ID      int
		PrivKey string
		PubKey  string
	}

	identitaSensori := make([]SensorIdentity, numeroSensori)

	for i := 1; i <= numeroSensori; i++ {
		nomeVar := fmt.Sprintf("SENSOR_%d", i)
		priv, pub := getOrGenerateKeys(nomeVar)

		identitaSensori[i-1] = SensorIdentity{ID: i, PrivKey: priv, PubKey: pub}
	}

	fmt.Println("\n--- Avvio trasmissione parallela blindata ---")

	for _, sensore := range identitaSensori {
		wg.Add(1)
		go simulaSensore(sensore.ID, sensore.PrivKey, sensore.PubKey, dashboardPubKey, &wg)
	}

	wg.Wait()
}
