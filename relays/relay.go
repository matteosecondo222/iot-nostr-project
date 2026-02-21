package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

func main() {
	// 1. Inizializziamo il database locale (creerà una cartella "relay_db")
	db := badger.BadgerBackend{Path: "./relay_db"}
	if err := db.Init(); err != nil {
		log.Fatalf("Impossibile inizializzare il database: %v", err)
	}
	fmt.Println("[SISTEMA] Database avviato con successo.")

	// 2. Creiamo l'istanza del Relay Khatru
	relay := khatru.NewRelay()

	// Puoi personalizzare le informazioni pubbliche del tuo relay
	relay.Info.Name = "Il Mio Hub IoT Privato"
	relay.Info.Description = "Relay dedicato alla memorizzazione dei dati dei sensori"
	relay.Info.Software = "khatru"

	// 3. Colleghiamo il database al relay (Salvataggio, Lettura, Cancellazione)
	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)

	// (Opzionale) Aggiungiamo un piccolo log personalizzato ogni volta che arriva un evento
	relay.OnEventSaved = append(relay.OnEventSaved, func(ctx context.Context, event *nostr.Event) {
		fmt.Printf("-> Salvato evento %s dal sensore %s\n", event.ID[:8], event.PubKey[:8])
	})

	// 4. Avviamo il server sulla porta 3334
	port := ":3334"
	fmt.Printf("[SISTEMA] Relay Nostr in esecuzione. In ascolto su ws://localhost%s\n", port)

	// Il relay Khatru implementa l'interfaccia http.Handler nativamente!
	err := http.ListenAndServe(port, relay)
	if err != nil {
		log.Fatalf("Errore del server: %v", err)
	}
}
