package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

// =================================================================
// ⚙️ CONFIGURAZIONE GARBAGE COLLECTOR
// =================================================================
const IntervalloDiPulizia = 1 * time.Hour

func main() {
	_ = godotenv.Load(".env")

	TEMP_SENSOR_1_PUB_KEY := os.Getenv("TEMP_SENSOR_1_PUB_KEY")
	TEMP_SENSOR_2_PUB_KEY := os.Getenv("TEMP_SENSOR_2_PUB_KEY")
	TEMP_SENSOR_3_PUB_KEY := os.Getenv("TEMP_SENSOR_3_PUB_KEY")
	PM10_SENSOR_1_PUB_KEY := os.Getenv("PM10_SENSOR_1_PUB_KEY")
	PM10_SENSOR_2_PUB_KEY := os.Getenv("PM10_SENSOR_2_PUB_KEY")

	// =================================================================
	// 📋 WHITELIST
	// =================================================================
	whitelist := map[string]bool{
		TEMP_SENSOR_1_PUB_KEY: true,
		TEMP_SENSOR_2_PUB_KEY: true,
		TEMP_SENSOR_3_PUB_KEY: true,
		PM10_SENSOR_1_PUB_KEY: true,
		PM10_SENSOR_2_PUB_KEY: true,
	}

	fmt.Println("Chiavi autorizzate in Whitelist:")
	for k := range whitelist {
		if k != "" {
			fmt.Printf("- %s\n", k[:8])
		}
	}

	db := badger.BadgerBackend{Path: "./relay_db"}
	if err := db.Init(); err != nil {
		log.Fatalf("Impossibile inizializzare il database: %v", err)
	}
	fmt.Println("[SISTEMA] Database avviato con successo.")

	relay := khatru.NewRelay()
	relay.Info.Name = "Il Mio Hub IoT Privato (Whitelist Ready)"
	relay.Info.Description = "Relay blindato basato sulla firma crittografica dell'evento."
	relay.Info.Software = "khatru"

	// =================================================================
	// 🛡️ FILTRO IN INGRESSO (Blocco scrittura per chi non è in Whitelist)
	// =================================================================
	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {

		// 1. Controllo Whitelist basato sulla PubKey dell'evento.
		// NOTA: khatru ha già verificato matematicamente la firma dell'evento in background.
		// Se siamo qui, significa che l'evento è stato realmente generato dal possessore di questa PubKey.
		if !whitelist[event.PubKey] {
			fmt.Printf("[SECURITY] Bloccato tentativo di scrittura da pubkey non autorizzata: %s\n", event.PubKey[:8])
			return true, "restricted: la tua pubkey non e' autorizzata a scrivere su questo hub"
		}

		// 2. Controllo eventi già scaduti in fase di pubblicazione (NIP-40)
		if isEventExpired(event) {
			return true, "reject: event is already expired (NIP-40)"
		}

		return false, ""
	})

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)

	relay.QueryEvents = append(relay.QueryEvents, func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		dbChannel, err := db.QueryEvents(ctx, filter)
		if err != nil {
			return nil, err
		}

		filteredChannel := make(chan *nostr.Event)

		go func() {
			defer close(filteredChannel)
			for event := range dbChannel {
				if !isEventExpired(event) {
					filteredChannel <- event
				}
			}
		}()

		return filteredChannel, nil
	})

	relay.OnEventSaved = append(relay.OnEventSaved, func(ctx context.Context, event *nostr.Event) {
		fmt.Printf("-> Salvato evento %s dal sensore %s\n", event.ID[:8], event.PubKey[:8])
	})

	go startExpirationGC(&db)

	port := ":3334"
	fmt.Printf("[SISTEMA] Relay Nostr in esecuzione. In ascolto su ws://localhost%s\n", port)

	err := http.ListenAndServe(port, relay)
	if err != nil {
		log.Fatalf("Errore del server: %v", err)
	}
}

func startExpirationGC(db *badger.BadgerBackend) {
	ticker := time.NewTicker(IntervalloDiPulizia)
	defer ticker.Stop()

	for {
		<-ticker.C

		fmt.Println("[GC] Avvio scansione per eliminazione fisica messaggi scaduti (NIP-40)...")
		ctx := context.Background()

		ch, err := db.QueryEvents(ctx, nostr.Filter{})
		if err != nil {
			log.Printf("[ERRORE GC] Impossibile interrogare il db: %v\n", err)
			continue
		}

		eliminati := 0
		for event := range ch {
			if isEventExpired(event) {
				err := db.DeleteEvent(ctx, event)
				if err == nil {
					eliminati++
				}
			}
		}

		if eliminati > 0 {
			fmt.Printf("[GC] Pulizia completata! Eliminati %d messaggi scaduti in modo permanente.\n", eliminati)
		} else {
			fmt.Println("[GC] Pulizia completata. Nessun nuovo messaggio scaduto da eliminare.")
		}
	}
}

func isEventExpired(event *nostr.Event) bool {
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "expiration" {
			expTime, err := strconv.ParseInt(tag[1], 10, 64)
			if err == nil {
				if time.Now().Unix() >= expTime {
					return true
				}
			}
		}
	}
	return false
}
