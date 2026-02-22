package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

const numeroSensori = 2 // Facciamo finta di avere 2 sensori PM10 esterni

// Struttura JSON per il dato in chiaro
type DatiAria struct {
	PM10 float64 `json:"pm10"`
	Unit string  `json:"unit"`
}

// Generazione chiavi specifiche per i sensori PM10 (per non sovrascrivere quelle della temperatura)
func getOrGenerateKeys(sensorName string) (string, string) {
	_ = godotenv.Load(".env")

	privKeyVar := "PM10_" + sensorName + "_PRIV_KEY"
	pubKeyVar := "PM10_" + sensorName + "_PUB_KEY"

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

		envContent := fmt.Sprintf("%s=%s\n%s=%s\n\n", privKeyVar, privKey, pubKeyVar, pubKey)
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

func simulaSensorePM10(idSensore int, privKey string, pubKey string, wg *sync.WaitGroup) {
	defer wg.Done()

	sensorTagId := fmt.Sprintf("outdoor-pm10-%02d", idSensore)
	durataValiditaDati := 24 * time.Hour // I dati scadono dopo 24 ore

	ctx := context.Background()

	// Scriviamo sia su quello pubblico (Damus) che sul nostro (Raspberry)
	relayURLs := []string{"wss://relay.damus.io", "ws://localhost:3334"}
	var activeRelays []*nostr.Relay

	for _, url := range relayURLs {
		relay, err := nostr.RelayConnect(ctx, url)
		if err != nil {
			log.Printf("⚠️ [%s] Impossibile connettersi a %s: %v", sensorTagId, url, err)
			continue
		}
		activeRelays = append(activeRelays, relay)
		fmt.Printf("📡 [%s] Connesso a %s!\n", sensorTagId, url)
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
		// Simuliamo valori di PM10 (tra 10 e 60 µg/m³)
		valorePM10 := 10.0 + rand.Float64()*50.0

		// Creiamo il JSON in chiaro
		payload := DatiAria{
			PM10: valorePM10,
			Unit: "µg/m³",
		}
		messaggioInChiaro, _ := json.Marshal(payload)

		// Calcolo scadenza (NIP-40)
		expirationTime := time.Now().Add(durataValiditaDati).Unix()
		expirationString := strconv.FormatInt(expirationTime, 10)

		ev := nostr.Event{
			PubKey:    pubKey,
			CreatedAt: nostr.Now(),
			// KIND 10000: Indica che è un dato generico, pubblico e in chiaro
			Kind: 10000,
			Tags: nostr.Tags{
				nostr.Tag{"t", "air_quality"}, // Tag tematico
				nostr.Tag{"t", "pm10"},        // Tag specifico
				nostr.Tag{"sensor_id", sensorTagId},
				nostr.Tag{"expiration", expirationString},
			},
			Content: string(messaggioInChiaro),
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
			fmt.Printf("🌍 [%s] PUBBLICO su %d relay (Scade il: %s) --> %s\n",
				sensorTagId,
				pubblicazioniRiuscite,
				time.Unix(expirationTime, 0).Format("02/01 15:04"),
				string(messaggioInChiaro)) // Stampiamo il JSON in chiaro per conferma
		}
	}
}

func main() {
	// A differenza del sensore temperatura, NON ci serve la Dashboard PubKey
	// perché stiamo parlando a tutta la rete Nostr, non a una dashboard specifica!

	fmt.Println("=====================================================")
	fmt.Printf("🚀 AVVIO FLOTTA SENSORI PM10 (DATI PUBBLICI IN CHIARO - %d Sensori)\n", numeroSensori)
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

	fmt.Println("\n--- Avvio trasmissione dati aria aperti ---")

	for _, sensore := range identitaSensori {
		wg.Add(1)
		// Non passiamo più la dashboardPubKey
		go simulaSensorePM10(sensore.ID, sensore.PrivKey, sensore.PubKey, &wg)
	}

	wg.Wait()
}
