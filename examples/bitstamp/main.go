package main

// Display live Trades and Orders from Bitstamp. https://www.bitstamp.net/websocket/

import (
	"log"

	pusher "github.com/goguardian/pusher-ws-go"
)

const (
	bitstampAppKey = "de504dc5763aeef9ff52"
)

func main() {

	// create error channel and start printing errors
	errChan := make(chan error)
	go func() {
		for {
			log.Println("Error: ", <-errChan)
		}
	}()

	// instantiate Pusher client with the error channel;
	// commented options are the defaults
	pusherClient := &pusher.Client{
		// Insecure: false,
		// Cluster: "mt1",
		Errors: errChan,
	}

	// connect to the Bitstamp Pusher app
	err := pusherClient.Connect(bitstampAppKey)

	if err != nil {
		panic(err)
	}

	// subscribe to the BTC/USD live ticker channel
	_, err = pusherClient.Subscribe("live_trades")
	if err != nil {
		panic(err)
	}

	// subscribe to the BTC/EUR live ticker channel
	_, err = pusherClient.Subscribe("live_trades_btceur")
	if err != nil {
		panic(err)
	}

	// subscribe to the BTC/USD order book
	usdOrderBook, err := pusherClient.Subscribe("order_book")
	if err != nil {
		panic(err)
	}

	// subscribe to the BTC/EUR order book
	eurOrderBook, err := pusherClient.Subscribe("order_book_btceur")
	if err != nil {
		panic(err)
	}

	// bind to trade events on all channels
	allTrades := pusherClient.Bind("trade")

	// bind to data events on each order channel
	usdOrderData := usdOrderBook.Bind("data")
	eurOrderData := eurOrderBook.Bind("data")

	// unsubscribe from the EUR channels
	err = pusherClient.Unsubscribe("live_trades_btceur")
	if err != nil {
		panic(err)
	}
	err = pusherClient.Unsubscribe("order_book_btceur")
	if err != nil {
		panic(err)
	}

	type orderData struct {
		Bids [][]string
		Asks [][]string
	}

	type tradeData struct {
		ID          uint64
		Amount      float64
		Price       float64
		Type        uint8
		Timestamp   string
		BuyOrderID  uint64 `json:"buy_order_id"`
		SellOrderID uint64 `json:"sell_order_id"`
	}

	// start printing events
	for {
		select {
		case usdOrder := <-usdOrderData:
			order := orderData{}
			err = pusher.UnmarshalDataString(usdOrder, &order)
			if err != nil {
				panic(err)
			}
			log.Printf("USD order data: %+v", order)
		case eurOrder := <-eurOrderData:
			order := orderData{}
			err = pusher.UnmarshalDataString(eurOrder, &order)
			if err != nil {
				panic(err)
			}
			log.Printf("EUR order data: %+v", order)
		case tradeEvt := <-allTrades:
			trade := tradeData{}
			err = pusher.UnmarshalDataString(tradeEvt.Data, &trade)
			if err != nil {
				panic(err)
			}
			log.Printf("Trade from channel '%s': %+v", tradeEvt.Channel, trade)
		}
	}
}
