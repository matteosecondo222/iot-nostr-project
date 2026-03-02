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

const numeroSensori = 2

type DatiAria struct {
	PM10 float64 `json:"pm10"`
	Unit string  `json:"unit"`
}

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
	durataValiditaDati := 24 * time.Hour

	ctx := context.Background()

	relayURLs := []string{"wss://relay.damus.io", "wss://nos.lol"}
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		valorePM10 := 10.0 + rand.Float64()*50.0

		payload := DatiAria{
			PM10: valorePM10,
			Unit: "µg/m³",
		}
		messaggioInChiaro, _ := json.Marshal(payload)

		expirationTime := time.Now().Add(durataValiditaDati).Unix()
		expirationString := strconv.FormatInt(expirationTime, 10)

		ev := nostr.Event{
			PubKey:    pubKey,
			CreatedAt: nostr.Now(),
			Kind:      1000,
			Tags: nostr.Tags{
				nostr.Tag{"t", "air_quality"},
				nostr.Tag{"t", "pm10"},
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
				string(messaggioInChiaro))
		}
	}
}

func main() {
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
		go simulaSensorePM10(sensore.ID, sensore.PrivKey, sensore.PubKey, &wg)
	}

	wg.Wait()
}
