package main

// This program connects to the given Pusher app, subscribes to the channel,
// then prints a message when the given event occurs. For presence channels, it
// also prints member join/leave events.
//
// For a local auth server, see
// https://github.com/pusher/pusher-channels-auth-example

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"

	pusher "github.com/goguardian/pusher-ws-go"
)

var (
	cluster = flag.String("cluster", "mt1", "set the pusher cluster like 'us3'")
	authURL = flag.String("auth-url", "", "set the pusher auth URL for authenticated channels")
	appKey  = flag.String("key", "", "app key to connect to pusher")
)

func main() {
	flag.Parse()

	// create error channel and start printing errors
	errChan := make(chan error)
	go func() {
		for {
			log.Println("Error: ", <-errChan)
		}
	}()

	pusherClient := &pusher.Client{
		Errors: errChan,
	}

	pusherClient.Cluster = *cluster
	pusherClient.AuthURL = *authURL

	if *appKey == "" {
		fmt.Print("Missing app key\n\n")
		printUsage()
		return
	}

	channelName := flag.Arg(0)
	if channelName == "" {
		fmt.Print("Missing channel\n\n")
		printUsage()
		return
	}

	event := flag.Arg(1)
	if event == "" {
		fmt.Print("Missing event\n\n")
		printUsage()
		return
	}

	err := pusherClient.Connect(*appKey)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	messages := make(chan string)

	wg.Add(1)
	go func() {
		for message := range messages {
			fmt.Println(message)
		}
		wg.Done()
	}()

	switch strings.Split(channelName, "-")[0] {
	case "presence":
		channel, err := pusherClient.SubscribePresence(channelName)
		if err != nil {
			panic(err)
		}

		wg.Add(1)
		go func() {
			for event := range channel.Bind(event) {
				messages <- fmt.Sprintf("[%s] Message: %s", channelName, string(event))
			}
			wg.Done()
		}()

		wg.Add(1)
		go func() {
			for member := range channel.BindMemberAdded() {
				messages <- fmt.Sprintf("[%s] Member %s joined, info: %s", channelName, member.ID, string(member.Info))
			}
			wg.Done()
		}()

		wg.Add(1)
		go func() {
			for event := range channel.BindMemberRemoved() {
				messages <- fmt.Sprintf("[%s] Member %s left", channelName, event)
			}
			wg.Done()
		}()

		me, err := channel.Me()
		if err != nil {
			panic(err)
		}

		messages <- fmt.Sprintf("[%s] Subscribed. My ID: %s, My Info: %s", channelName, me.ID, string(me.Info))

	default:
		channel, err := pusherClient.Subscribe(channelName)
		if err != nil {
			panic(err)
		}

		wg.Add(1)
		go func() {
			for event := range channel.Bind(event) {
				messages <- string(event)
			}
			wg.Done()
		}()

		messages <- fmt.Sprintf("[%s] Subscribed", channelName)
	}

	wg.Wait()
}

func printUsage() {
	fmt.Println("Usage: listen -key <app key> [options] <channel> <event>")
	flag.PrintDefaults()
}
