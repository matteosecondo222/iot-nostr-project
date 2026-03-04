# Istruzioni per l'esecuzione del codice

## Configurazione iniziale
È necessario inserire un .env nelle directory /temperature, /pm10, /relays e /NostrClient per settare le chiavi pubbliche e private.

In /temperature il .env deve contenere la variabile DASHBOARD_PUB_KEY, le chiavi dei sensori verranno inserite in automatico all'esecuzione dello script.

In /pm10 il .env può anche restare vuoto perchè le chiavi dei sensori verranno inserite in automatico all'esecuzione dello script.

In /relays il .env deve contenere le variabili contenenti le chiavi dei sensori nel formato TEMP_SENSOR_N_PUB_KEY.

In /NostrClient il .env deve contenere la variabile DASHBOARD_PRIV_KEY.

È possibile creare le chiavi eseguendo il seguente script:

```bash
cd ./keys_generator.go
go run relay.go
```

## Avvio
Aprire un terminale e digitare il seguente comando per avviare il relay: 

```bash
cd ./relays
go run relay.go
```

Aprire un nuovo terminale e digitare il seguente comando per avviare i sensori di temperatura: 

```bash
cd ./temperature
go run temperature.go
```

Aprire un nuovo terminale e digitare il seguente comando per avviare i sensori PM10: 

```bash
cd ./pm10
go run pm10.go
```

Aprire un nuovo terminale digitare il seguente comando per avviare il client: 

```bash
cd ./NostrClient
dotnet run
```