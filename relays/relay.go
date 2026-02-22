package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// =================================================================
// ⚙️ CONFIGURAZIONE GARBAGE COLLECTOR
// Il tempo di scadenza effettivo è deciso dal sensore (tag expiration).
// Qui puoi decidere OGNI QUANTO TEMPO il relay si sveglia per
// scansionare il database e cancellare fisicamente i file scaduti.
// =================================================================
const IntervalloDiPulizia = 1 * time.Hour // Esegue il controllo ogni ora

func main() {
	db := badger.BadgerBackend{Path: "./relay_db"}
	if err := db.Init(); err != nil {
		log.Fatalf("Impossibile inizializzare il database: %v", err)
	}
	fmt.Println("[SISTEMA] Database avviato con successo.")

	relay := khatru.NewRelay()
	relay.Info.Name = "Il Mio Hub IoT Privato (NIP-40 Ready)"
	relay.Info.Description = "Relay che supporta la scadenza fisica degli eventi."
	relay.Info.Software = "khatru"

	// =================================================================
	// 🛑 NIP-40: 1. Rifiuto eventi già scaduti in fase di pubblicazione
	// =================================================================
	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
		if isEventExpired(event) {
			return true, "reject: event is already expired (NIP-40)"
		}
		return false, ""
	})

	// Salvataggio standard e Cancellazione standard
	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)

	// =================================================================
	// 🕵️‍♂️ NIP-40: 2. Filtro in uscita (Nascondiamo i vecchi dati ai client)
	// =================================================================
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

	// =================================================================
	// 🚀 AVVIO DEL GARBAGE COLLECTOR IN BACKGROUND
	// =================================================================
	go startExpirationGC(&db)

	port := ":3334"
	fmt.Printf("[SISTEMA] Relay Nostr in esecuzione. In ascolto su ws://localhost%s\n", port)

	err := http.ListenAndServe(port, relay)
	if err != nil {
		log.Fatalf("Errore del server: %v", err)
	}
}

// =================================================================
// 🧹 GARBAGE COLLECTOR: Elimina fisicamente i messaggi scaduti
// =================================================================
func startExpirationGC(db *badger.BadgerBackend) {
	ticker := time.NewTicker(IntervalloDiPulizia)
	defer ticker.Stop()

	for {
		<-ticker.C // Attende il prossimo intervallo (es. 1 ora)
		
		fmt.Println("[GC] Avvio scansione per eliminazione fisica messaggi scaduti (NIP-40)...")
		ctx := context.Background()
		
		// Passando un filtro vuoto, chiediamo a BadgerDB di scorrere gli eventi.
		// (Essendo un database key-value, l'iterazione è molto veloce).
		ch, err := db.QueryEvents(ctx, nostr.Filter{})
		if err != nil {
			log.Printf("[ERRORE GC] Impossibile interrogare il db: %v\n", err)
			continue
		}

		eliminati := 0
		// Analizziamo ogni evento presente nel database
		for event := range ch {
			if isEventExpired(event) {
				// Se è scaduto, lo eliminiamo FISICAMENTE dal disco
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

// =================================================================
// 🛠 HELPER NIP-40: Funzione che controlla se un evento è scaduto
// =================================================================
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